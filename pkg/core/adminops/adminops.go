// Package adminops implements bucket administration sub-resources
// (M3): policy, ACL, CORS, lifecycle, encryption, public access
// block, website hosting and tagging. Every getter maps "not configured" and
// "provider does not implement this API" onto empty results or readable
// errors so the GUI can disable unsupported panels instead of failing.
package adminops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/policy"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	awssmithy "github.com/aws/smithy-go"
)

// ---------------- bucket policy ----------------

// PolicyInfo is a bucket policy: raw JSON plus the analyzed summary.
type PolicyInfo struct {
	Raw     string                `json:"raw,omitempty"` // empty = no policy set
	Summary *policy.PolicySummary `json:"summary,omitempty"`
}

// GetPolicy fetches the bucket policy; an unset policy is not an error.
func GetPolicy(ctx context.Context, client *s3.Client, bucket string) (PolicyInfo, error) {
	out, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		if isCode(err, "NoSuchBucketPolicy") {
			return PolicyInfo{}, nil
		}
		return PolicyInfo{}, mapUnsupported(err, "bucket policy")
	}
	raw := aws.ToString(out.Policy)
	info := PolicyInfo{Raw: raw}
	if doc, perr := policy.ParsePolicy([]byte(raw)); perr == nil {
		s := policy.Analyze(doc)
		info.Summary = &s
	}
	return info, nil
}

// PutPolicy validates and stores a bucket policy document.
func PutPolicy(ctx context.Context, client *s3.Client, bucket, raw string) error {
	if _, err := policy.ParsePolicy([]byte(raw)); err != nil {
		return err
	}
	_, err := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket), Policy: aws.String(raw),
	})
	return mapUnsupported(err, "bucket policy")
}

// DeletePolicy removes the bucket policy.
func DeletePolicy(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "bucket policy")
}

// ---------------- ACL ----------------

// ACLInfo is the read-only ACL view (MinIO serves synthetic ACLs; B2 has none).
type ACLInfo struct {
	Owner   string            `json:"owner,omitempty"`
	Summary policy.ACLSummary `json:"summary"`
}

// GetACL fetches the bucket ACL and its analysis.
func GetACL(ctx context.Context, client *s3.Client, bucket string) (ACLInfo, error) {
	out, err := client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(bucket)})
	if err != nil {
		return ACLInfo{}, mapUnsupported(err, "ACL")
	}
	var owner s3types.Owner
	if out.Owner != nil {
		owner = *out.Owner
	}
	return ACLInfo{
		Owner:   firstNonEmpty(aws.ToString(owner.DisplayName), aws.ToString(owner.ID)),
		Summary: policy.AnalyzeACL(owner, out.Grants),
	}, nil
}

// ---------------- CORS ----------------

// CORSRule is the editor-friendly CORS representation.
type CORSRule struct {
	Origins []string `json:"origins"`
	Methods []string `json:"methods"`
	Headers []string `json:"headers,omitempty"`
	Expose  []string `json:"expose,omitempty"`
	MaxAge  int32    `json:"maxAge,omitempty"` // seconds
}

// GetCORS fetches CORS rules; none configured is not an error.
func GetCORS(ctx context.Context, client *s3.Client, bucket string) ([]CORSRule, error) {
	out, err := client.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(bucket)})
	if err != nil {
		// NoSuchCORSConfiguration is the documented "nothing set" answer
		// (AWS and MinIO); a bucket without CORS rules is the normal state,
		// not an error the admin panel should banner about
		if isCode(err, "NoSuchCORSConfiguration", "NoSuchBucketConfiguration") {
			return nil, nil
		}
		return nil, mapUnsupported(err, "CORS")
	}
	rules := make([]CORSRule, 0, len(out.CORSRules))
	for _, r := range out.CORSRules {
		rules = append(rules, CORSRule{
			Origins: r.AllowedOrigins,
			Methods: r.AllowedMethods,
			Headers: r.AllowedHeaders,
			Expose:  r.ExposeHeaders,
			MaxAge:  aws.ToInt32(r.MaxAgeSeconds),
		})
	}
	return rules, nil
}

// PutCORS replaces the CORS configuration.
func PutCORS(ctx context.Context, client *s3.Client, bucket string, rules []CORSRule) error {
	if len(rules) == 0 {
		return DeleteCORS(ctx, client, bucket)
	}
	var out []s3types.CORSRule
	for _, r := range rules {
		if len(r.Origins) == 0 || len(r.Methods) == 0 {
			return fmt.Errorf("CORS rule needs at least one origin and one method")
		}
		out = append(out, s3types.CORSRule{
			AllowedOrigins: r.Origins,
			AllowedMethods: r.Methods,
			AllowedHeaders: r.Headers,
			ExposeHeaders:  r.Expose,
			MaxAgeSeconds:  aws.Int32(r.MaxAge),
		})
	}
	_, err := client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: &s3types.CORSConfiguration{CORSRules: out},
	})
	return mapUnsupported(err, "CORS")
}

// DeleteCORS removes the CORS configuration.
func DeleteCORS(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "CORS")
}

// ---------------- lifecycle ----------------

// LifecycleTransition is one transition action: matching objects move to
// StorageClass after Days, or on the calendar Date (YYYY-MM-DD) — exactly
// one of the two forms per S3's grammar.
type LifecycleTransition struct {
	Days         int32  `json:"days,omitempty"`
	Date         string `json:"date,omitempty"` // YYYY-MM-DD
	StorageClass string `json:"storageClass"`
}

// NoncurrentTransition moves NONCURRENT versions to StorageClass after
// NoncurrentDays (the noncurrent twin of LifecycleTransition, minus dates —
// S3 allows day offsets only on this side).
type NoncurrentTransition struct {
	NoncurrentDays int32  `json:"noncurrentDays"`
	StorageClass   string `json:"storageClass"`
}

// LifecycleRule is the supported subset of S3 lifecycle rules: the filter
// (prefix, one tag, an object-size range), any number of transitions, the
// expiration (days or a calendar date, delete markers), noncurrent-version
// expiration (with how many newer versions to keep) and noncurrent
// transitions, and aborted-multipart cleanup.
//
// The legacy transitionDays/transitionClass pair is kept for older JSON
// payloads (the CLI's documented example, saved files): on READ the first
// days-form transition also mirrors into the pair; on WRITE the pair is
// honored only when the Transitions list is empty — the list, when present,
// is the truth.
type LifecycleRule struct {
	ID      string `json:"id,omitempty"`
	Enabled bool   `json:"enabled"`

	Prefix   string `json:"prefix,omitempty"`
	TagKey   string `json:"tagKey,omitempty"`
	TagValue string `json:"tagValue,omitempty"`
	MinBytes int64  `json:"minBytes,omitempty"` // filter: object size >=
	MaxBytes int64  `json:"maxBytes,omitempty"` // filter: object size <

	Transitions     []LifecycleTransition `json:"transitions,omitempty"`
	TransitionDays  int32                 `json:"transitionDays,omitempty"`  // legacy: Transitions[0]
	TransitionClass string                `json:"transitionClass,omitempty"` // legacy pair

	ExpirationDays int32  `json:"expirationDays,omitempty"`
	ExpirationDate string `json:"expirationDate,omitempty"` // YYYY-MM-DD
	DeleteMarker   bool   `json:"deleteMarker,omitempty"`   // expire delete markers

	NoncurrentDays        int32                  `json:"noncurrentDays,omitempty"`          // expire noncurrent versions
	NewerNoncurrent       int32                  `json:"newerNoncurrentVersions,omitempty"` // keep N newer while expiring
	NoncurrentTransitions []NoncurrentTransition `json:"noncurrentTransitions,omitempty"`
	AbortMPUDays          int32                  `json:"abortMpuDays,omitempty"` // abort incomplete uploads
}

// parseLifecycleDate parses the YYYY-MM-DD form both wire dates speak.
func parseLifecycleDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// GetLifecycle fetches lifecycle rules; none configured is not an error.
func GetLifecycle(ctx context.Context, client *s3.Client, bucket string) ([]LifecycleRule, error) {
	out, err := client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if isCode(err, "NoSuchLifecycleConfiguration") {
			return nil, nil
		}
		return nil, mapUnsupported(err, "lifecycle")
	}
	var rules []LifecycleRule
	for _, r := range out.Rules {
		lr := LifecycleRule{
			ID:      aws.ToString(r.ID),
			Enabled: r.Status == s3types.ExpirationStatusEnabled,
		}
		if r.Filter != nil {
			lr.Prefix = aws.ToString(r.Filter.Prefix)
			if tag := r.Filter.Tag; tag != nil {
				lr.TagKey, lr.TagValue = aws.ToString(tag.Key), aws.ToString(tag.Value)
			}
			if and := r.Filter.And; and != nil {
				// And is a pointer on the wire — a plain prefix filter never
				// carries one, and the unguarded deref was a latent crash on
				// the most common rule shape (caught by the v2 rigs)
				lr.Prefix = firstNonEmpty(aws.ToString(and.Prefix), lr.Prefix)
				if len(and.Tags) > 0 {
					lr.TagKey, lr.TagValue = aws.ToString(and.Tags[0].Key), aws.ToString(and.Tags[0].Value)
				}
				lr.MinBytes = aws.ToInt64(and.ObjectSizeGreaterThan)
				lr.MaxBytes = aws.ToInt64(and.ObjectSizeLessThan)
			}
		}
		for _, t := range r.Transitions {
			tr := LifecycleTransition{StorageClass: string(t.StorageClass)}
			if t.Days != nil {
				tr.Days = aws.ToInt32(t.Days)
			} else if t.Date != nil {
				tr.Date = t.Date.Format("2006-01-02")
			}
			lr.Transitions = append(lr.Transitions, tr)
		}
		if len(lr.Transitions) > 0 && lr.Transitions[0].Days > 0 {
			lr.TransitionDays, lr.TransitionClass = lr.Transitions[0].Days, lr.Transitions[0].StorageClass
		}
		if r.Expiration != nil {
			lr.ExpirationDays = aws.ToInt32(r.Expiration.Days)
			lr.DeleteMarker = aws.ToBool(r.Expiration.ExpiredObjectDeleteMarker)
			if r.Expiration.Date != nil {
				lr.ExpirationDate = r.Expiration.Date.Format("2006-01-02")
			}
		}
		if r.NoncurrentVersionExpiration != nil {
			lr.NoncurrentDays = aws.ToInt32(r.NoncurrentVersionExpiration.NoncurrentDays)
			lr.NewerNoncurrent = aws.ToInt32(r.NoncurrentVersionExpiration.NewerNoncurrentVersions)
		}
		for _, t := range r.NoncurrentVersionTransitions {
			lr.NoncurrentTransitions = append(lr.NoncurrentTransitions, NoncurrentTransition{
				NoncurrentDays: aws.ToInt32(t.NoncurrentDays),
				StorageClass:   string(t.StorageClass),
			})
		}
		if r.AbortIncompleteMultipartUpload != nil {
			lr.AbortMPUDays = aws.ToInt32(r.AbortIncompleteMultipartUpload.DaysAfterInitiation)
		}
		rules = append(rules, lr)
	}
	return rules, nil
}

// lifecycleRuleHasAction reports whether the rule carries at least one
// action or a narrowing filter — S3 refuses rules that do nothing.
func lifecycleRuleHasAction(r LifecycleRule) bool {
	return r.Prefix != "" || r.TagKey != "" || r.MinBytes > 0 || r.MaxBytes > 0 ||
		len(r.Transitions) > 0 || (r.TransitionDays > 0 && r.TransitionClass != "") ||
		r.ExpirationDays > 0 || r.ExpirationDate != "" || r.DeleteMarker ||
		r.NoncurrentDays > 0 || r.NewerNoncurrent > 0 || len(r.NoncurrentTransitions) > 0 ||
		r.AbortMPUDays > 0
}

// PutLifecycle replaces the lifecycle configuration.
func PutLifecycle(ctx context.Context, client *s3.Client, bucket string, rules []LifecycleRule) error {
	if len(rules) == 0 {
		return DeleteLifecycle(ctx, client, bucket)
	}
	var out []s3types.LifecycleRule
	for _, r := range rules {
		if !lifecycleRuleHasAction(r) {
			return fmt.Errorf("lifecycle rule %q does nothing (no filter or action)", r.ID)
		}
		if r.ExpirationDays > 0 && r.ExpirationDate != "" {
			return fmt.Errorf("lifecycle rule %q: expiration days and date are mutually exclusive", r.ID)
		}
		if r.MinBytes > 0 && r.MaxBytes > 0 && r.MinBytes >= r.MaxBytes {
			return fmt.Errorf("lifecycle rule %q: min size %d is not below max %d", r.ID, r.MinBytes, r.MaxBytes)
		}
		// the transitions list is the truth; the legacy pair stands in only
		// when no list rode the payload
		transitions := r.Transitions
		if len(transitions) == 0 && r.TransitionDays > 0 && r.TransitionClass != "" {
			transitions = []LifecycleTransition{{Days: r.TransitionDays, StorageClass: r.TransitionClass}}
		}
		var wireTransitions []s3types.Transition
		for _, t := range transitions {
			wt := s3types.Transition{StorageClass: s3types.TransitionStorageClass(t.StorageClass)}
			switch {
			case t.Date != "":
				d, err := parseLifecycleDate(t.Date)
				if err != nil {
					return fmt.Errorf("lifecycle rule %q: transition date %q is not YYYY-MM-DD", r.ID, t.Date)
				}
				wt.Date = aws.Time(d)
			default:
				wt.Days = aws.Int32(t.Days)
			}
			wireTransitions = append(wireTransitions, wt)
		}
		tr := s3types.LifecycleRule{
			ID:          aws.String(r.ID),
			Status:      s3types.ExpirationStatusDisabled,
			Filter:      &s3types.LifecycleRuleFilter{Prefix: aws.String(r.Prefix)},
			Transitions: wireTransitions,
		}
		if r.Enabled {
			tr.Status = s3types.ExpirationStatusEnabled
		}
		if r.TagKey != "" || r.MinBytes > 0 || r.MaxBytes > 0 {
			and := s3types.LifecycleRuleAndOperator{}
			if r.Prefix != "" {
				and.Prefix = aws.String(r.Prefix)
			}
			if r.TagKey != "" {
				and.Tags = []s3types.Tag{{Key: aws.String(r.TagKey), Value: aws.String(r.TagValue)}}
			}
			if r.MinBytes > 0 {
				and.ObjectSizeGreaterThan = aws.Int64(r.MinBytes)
			}
			if r.MaxBytes > 0 {
				and.ObjectSizeLessThan = aws.Int64(r.MaxBytes)
			}
			tr.Filter = &s3types.LifecycleRuleFilter{And: &and}
		}
		if r.ExpirationDays > 0 || r.ExpirationDate != "" || r.DeleteMarker {
			ex := s3types.LifecycleExpiration{
				ExpiredObjectDeleteMarker: aws.Bool(r.DeleteMarker && r.ExpirationDays == 0 && r.ExpirationDate == ""),
			}
			if r.ExpirationDays > 0 {
				ex.Days = aws.Int32(r.ExpirationDays)
			}
			if r.ExpirationDate != "" {
				d, err := parseLifecycleDate(r.ExpirationDate)
				if err != nil {
					return fmt.Errorf("lifecycle rule %q: expiration date %q is not YYYY-MM-DD", r.ID, r.ExpirationDate)
				}
				ex.Date = aws.Time(d)
			}
			tr.Expiration = &ex
		}
		if r.NoncurrentDays > 0 || r.NewerNoncurrent > 0 {
			ne := s3types.NoncurrentVersionExpiration{}
			if r.NoncurrentDays > 0 {
				ne.NoncurrentDays = aws.Int32(r.NoncurrentDays)
			}
			if r.NewerNoncurrent > 0 {
				ne.NewerNoncurrentVersions = aws.Int32(r.NewerNoncurrent)
			}
			tr.NoncurrentVersionExpiration = &ne
		}
		for _, t := range r.NoncurrentTransitions {
			tr.NoncurrentVersionTransitions = append(tr.NoncurrentVersionTransitions, s3types.NoncurrentVersionTransition{
				NoncurrentDays: aws.Int32(t.NoncurrentDays),
				StorageClass:   s3types.TransitionStorageClass(t.StorageClass),
			})
		}
		if r.AbortMPUDays > 0 {
			tr.AbortIncompleteMultipartUpload = &s3types.AbortIncompleteMultipartUpload{
				DaysAfterInitiation: aws.Int32(r.AbortMPUDays),
			}
		}
		out = append(out, tr)
	}
	_, err := client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(bucket),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{Rules: out},
	})
	return mapUnsupported(err, "lifecycle")
}

// DeleteLifecycle removes the lifecycle configuration.
func DeleteLifecycle(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "lifecycle")
}

// ---------------- encryption ----------------

// EncryptionInfo describes default server-side encryption.
type EncryptionInfo struct {
	Algorithm string `json:"algorithm,omitempty"` // AES256 | aws:kms
	KMSKeyID  string `json:"kmsKeyId,omitempty"`
}

// GetEncryption fetches default encryption; none set is not an error.
func GetEncryption(ctx context.Context, client *s3.Client, bucket string) (EncryptionInfo, error) {
	out, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: aws.String(bucket)})
	if err != nil {
		if isCode(err, "ServerSideEncryptionConfigurationNotFoundError") {
			return EncryptionInfo{}, nil
		}
		return EncryptionInfo{}, mapUnsupported(err, "encryption")
	}
	info := EncryptionInfo{}
	if len(out.ServerSideEncryptionConfiguration.Rules) > 0 {
		def := out.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault
		if def != nil {
			info.Algorithm = string(def.SSEAlgorithm)
			info.KMSKeyID = aws.ToString(def.KMSMasterKeyID)
		}
	}
	return info, nil
}

// PutEncryption sets default encryption ("AES256" or "aws:kms" + key ID).
func PutEncryption(ctx context.Context, client *s3.Client, bucket, algorithm, kmsKeyID string) error {
	def := &s3types.ServerSideEncryptionByDefault{
		SSEAlgorithm: s3types.ServerSideEncryption(algorithm),
	}
	if algorithm == "aws:kms" && kmsKeyID != "" {
		def.KMSMasterKeyID = aws.String(kmsKeyID)
	}
	_, err := client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
			Rules: []s3types.ServerSideEncryptionRule{{ApplyServerSideEncryptionByDefault: def}},
		},
	})
	return mapUnsupported(err, "encryption")
}

// DeleteEncryption removes default encryption.
func DeleteEncryption(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "encryption")
}

// ---------------- public access block ----------------

// PABInfo mirrors the four public-access-block settings.
type PABInfo struct {
	BlockPublicACLs       bool `json:"blockPublicAcls"`
	IgnorePublicACLs      bool `json:"ignorePublicAcls"`
	BlockPublicPolicy     bool `json:"blockPublicPolicy"`
	RestrictPublicBuckets bool `json:"restrictPublicBuckets"`
}

// All returns the recommended all-blocked configuration.
func (p PABInfo) All() PABInfo { return PABInfo{true, true, true, true} }

// GetPAB fetches public access block settings; none set is not an error.
func GetPAB(ctx context.Context, client *s3.Client, bucket string) (PABInfo, error) {
	out, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(bucket)})
	if err != nil {
		if isCode(err, "NoSuchPublicAccessBlockConfiguration") {
			return PABInfo{}, nil
		}
		return PABInfo{}, mapUnsupported(err, "public access block")
	}
	if out.PublicAccessBlockConfiguration == nil {
		return PABInfo{}, nil
	}
	cfg := out.PublicAccessBlockConfiguration
	return PABInfo{
		BlockPublicACLs:       aws.ToBool(cfg.BlockPublicAcls),
		IgnorePublicACLs:      aws.ToBool(cfg.IgnorePublicAcls),
		BlockPublicPolicy:     aws.ToBool(cfg.BlockPublicPolicy),
		RestrictPublicBuckets: aws.ToBool(cfg.RestrictPublicBuckets),
	}, nil
}

// PutPAB stores public access block settings.
func PutPAB(ctx context.Context, client *s3.Client, bucket string, p PABInfo) error {
	_, err := client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(bucket),
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(p.BlockPublicACLs),
			IgnorePublicAcls:      aws.Bool(p.IgnorePublicACLs),
			BlockPublicPolicy:     aws.Bool(p.BlockPublicPolicy),
			RestrictPublicBuckets: aws.Bool(p.RestrictPublicBuckets),
		},
	})
	return mapUnsupported(err, "public access block")
}

// DeletePAB removes public access block settings.
//
// The capability pre-check is a safety net: some S3-compatible servers
// (observed on MinIO RELEASE.2025-09-07) mis-handle DeletePublicAccessBlock
// and destroy the whole bucket instead of its PAB settings. A server that
// cannot even serve GetPublicAccessBlock has no PAB configuration to
// remove, so the delete is refused with the same "not supported"
// classification instead of handing the bucket to a broken code path.
func DeletePAB(ctx context.Context, client *s3.Client, bucket string) error {
	if _, err := GetPAB(ctx, client, bucket); err != nil {
		return err
	}
	_, err := client.DeletePublicAccessBlock(ctx, &s3.DeletePublicAccessBlockInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "public access block")
}

// ---------------- website hosting ----------------

// WebsiteInfo describes static website hosting.
type WebsiteInfo struct {
	IndexSuffix      string `json:"indexSuffix,omitempty"`      // e.g. index.html
	ErrorKey         string `json:"errorKey,omitempty"`         // e.g. 404.html
	RedirectHost     string `json:"redirectHost,omitempty"`     // redirect all requests
	RedirectProtocol string `json:"redirectProtocol,omitempty"` // http | https
}

// GetWebsite fetches website configuration; none set is not an error.
func GetWebsite(ctx context.Context, client *s3.Client, bucket string) (WebsiteInfo, error) {
	out, err := client.GetBucketWebsite(ctx, &s3.GetBucketWebsiteInput{Bucket: aws.String(bucket)})
	if err != nil {
		if isCode(err, "NoSuchWebsiteConfiguration") {
			return WebsiteInfo{}, nil
		}
		return WebsiteInfo{}, mapUnsupported(err, "website hosting")
	}
	info := WebsiteInfo{}
	if out.IndexDocument != nil {
		info.IndexSuffix = aws.ToString(out.IndexDocument.Suffix)
	}
	if out.ErrorDocument != nil {
		info.ErrorKey = aws.ToString(out.ErrorDocument.Key)
	}
	if out.RedirectAllRequestsTo != nil {
		info.RedirectHost = aws.ToString(out.RedirectAllRequestsTo.HostName)
		info.RedirectProtocol = string(out.RedirectAllRequestsTo.Protocol)
	}
	return info, nil
}

// PutWebsite stores website configuration.
func PutWebsite(ctx context.Context, client *s3.Client, bucket string, w WebsiteInfo) error {
	cfg := &s3types.WebsiteConfiguration{}
	if w.RedirectHost != "" {
		cfg.RedirectAllRequestsTo = &s3types.RedirectAllRequestsTo{
			HostName: aws.String(w.RedirectHost),
			Protocol: s3types.Protocol(w.RedirectProtocol),
		}
	} else {
		if w.IndexSuffix == "" {
			return fmt.Errorf("website hosting needs an index document (e.g. index.html)")
		}
		cfg.IndexDocument = &s3types.IndexDocument{Suffix: aws.String(w.IndexSuffix)}
		if w.ErrorKey != "" {
			cfg.ErrorDocument = &s3types.ErrorDocument{Key: aws.String(w.ErrorKey)}
		}
	}
	_, err := client.PutBucketWebsite(ctx, &s3.PutBucketWebsiteInput{
		Bucket: aws.String(bucket), WebsiteConfiguration: cfg,
	})
	return mapUnsupported(err, "website hosting")
}

// DeleteWebsite removes website configuration.
func DeleteWebsite(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.DeleteBucketWebsite(ctx, &s3.DeleteBucketWebsiteInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "website hosting")
}

// ---------------- tagging ----------------

// Tag is one key/value pair.
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GetTags fetches bucket tags; none set is not an error.
func GetTags(ctx context.Context, client *s3.Client, bucket string) ([]Tag, error) {
	out, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(bucket)})
	if err != nil {
		if isCode(err, "NoSuchTagSet") {
			return nil, nil
		}
		return nil, mapUnsupported(err, "tagging")
	}
	tags := make([]Tag, 0, len(out.TagSet))
	for _, t := range out.TagSet {
		tags = append(tags, Tag{Key: aws.ToString(t.Key), Value: aws.ToString(t.Value)})
	}
	return tags, nil
}

// PutTags replaces the bucket tag set.
func PutTags(ctx context.Context, client *s3.Client, bucket string, tags []Tag) error {
	set := make([]s3types.Tag, 0, len(tags))
	for _, t := range tags {
		if t.Key == "" {
			return fmt.Errorf("tag keys cannot be empty")
		}
		set = append(set, s3types.Tag{Key: aws.String(t.Key), Value: aws.String(t.Value)})
	}
	_, err := client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(bucket), Tagging: &s3types.Tagging{TagSet: set},
	})
	return mapUnsupported(err, "tagging")
}

// DeleteTags removes all bucket tags.
func DeleteTags(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{Bucket: aws.String(bucket)})
	return mapUnsupported(err, "tagging")
}

// ---------------- helpers ----------------

// mapUnsupported rewrites provider "not implemented" errors into a plain
// sentence the admin panel can show on a disabled control.
func mapUnsupported(err error, what string) error {
	if err == nil {
		return nil
	}
	if isCode(err, "NotImplemented", "XNotImplemented", "MethodNotAllowed", "AccessControlListNotSupported") {
		return fmt.Errorf("%s is not supported by this provider/endpoint", what)
	}
	return err
}

// isCode reports whether err carries one of the given smithy error codes.
// (This SDK generation does not model every "not found" as a Go type, so
// code matching is the portable way to detect "not configured".)
func isCode(err error, codes ...string) bool {
	var apiErr awssmithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, c := range codes {
		if apiErr.ErrorCode() == c {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Package adminops implements bucket administration sub-resources
// (PLAN.md §8.5 M3): policy, ACL, CORS, lifecycle, encryption, public access
// block, website hosting and tagging. Every getter maps "not configured" and
// "provider does not implement this API" onto empty results or readable
// errors so the GUI can disable unsupported panels instead of failing.
package adminops

import (
	"context"
	"errors"
	"fmt"

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
		if isCode(err, "NoSuchBucketConfiguration") {
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

// LifecycleRule is the supported subset of S3 lifecycle rules: transition to
// a cheaper class, expiration, noncurrent-version cleanup and aborted
// multipart cleanup. One transition per rule.
type LifecycleRule struct {
	ID              string `json:"id,omitempty"`
	Enabled         bool   `json:"enabled"`
	Prefix          string `json:"prefix,omitempty"`
	TransitionDays  int32  `json:"transitionDays,omitempty"`
	TransitionClass string `json:"transitionClass,omitempty"` // GLACIER, DEEP_ARCHIVE, ...
	ExpirationDays  int32  `json:"expirationDays,omitempty"`
	DeleteMarker    bool   `json:"deleteMarker,omitempty"`   // expire delete markers
	NoncurrentDays  int32  `json:"noncurrentDays,omitempty"` // expire noncurrent versions
	AbortMPUDays    int32  `json:"abortMpuDays,omitempty"`   // abort incomplete uploads
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
			lr.Prefix = firstNonEmpty(aws.ToString(r.Filter.And.Prefix), aws.ToString(r.Filter.Prefix))
		}
		if len(r.Transitions) > 0 {
			lr.TransitionDays = aws.ToInt32(r.Transitions[0].Days)
			lr.TransitionClass = string(r.Transitions[0].StorageClass)
		}
		if r.Expiration != nil {
			lr.ExpirationDays = aws.ToInt32(r.Expiration.Days)
			lr.DeleteMarker = aws.ToBool(r.Expiration.ExpiredObjectDeleteMarker)
		}
		if r.NoncurrentVersionExpiration != nil {
			lr.NoncurrentDays = aws.ToInt32(r.NoncurrentVersionExpiration.NoncurrentDays)
		}
		if r.AbortIncompleteMultipartUpload != nil {
			lr.AbortMPUDays = aws.ToInt32(r.AbortIncompleteMultipartUpload.DaysAfterInitiation)
		}
		rules = append(rules, lr)
	}
	return rules, nil
}

// PutLifecycle replaces the lifecycle configuration.
func PutLifecycle(ctx context.Context, client *s3.Client, bucket string, rules []LifecycleRule) error {
	if len(rules) == 0 {
		return DeleteLifecycle(ctx, client, bucket)
	}
	var out []s3types.LifecycleRule
	for _, r := range rules {
		if r.Prefix == "" && r.ExpirationDays == 0 && r.TransitionDays == 0 &&
			r.NoncurrentDays == 0 && r.AbortMPUDays == 0 {
			return fmt.Errorf("lifecycle rule %q does nothing (no prefix filter or action)", r.ID)
		}
		tr := s3types.LifecycleRule{
			ID:     aws.String(r.ID),
			Status: s3types.ExpirationStatusDisabled,
			Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String(r.Prefix)},
		}
		if r.Enabled {
			tr.Status = s3types.ExpirationStatusEnabled
		}
		if r.TransitionDays > 0 && r.TransitionClass != "" {
			tr.Transitions = []s3types.Transition{{
				Days:         aws.Int32(r.TransitionDays),
				StorageClass: s3types.TransitionStorageClass(r.TransitionClass),
			}}
		}
		if r.ExpirationDays > 0 || r.DeleteMarker {
			tr.Expiration = &s3types.LifecycleExpiration{
				Days:                      aws.Int32(r.ExpirationDays),
				ExpiredObjectDeleteMarker: aws.Bool(r.DeleteMarker && r.ExpirationDays == 0),
			}
		}
		if r.NoncurrentDays > 0 {
			tr.NoncurrentVersionExpiration = &s3types.NoncurrentVersionExpiration{
				NoncurrentDays: aws.Int32(r.NoncurrentDays),
			}
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
func DeletePAB(ctx context.Context, client *s3.Client, bucket string) error {
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

package api

import (
	"fmt"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
)

// AdminPanel is the whole bucket-administration view (M3).
// Every section carries its own error string: providers that do not
// implement an API disable that tab instead of failing the panel.
type AdminPanel struct {
	Region   string `json:"region"`
	Versions string `json:"versions"` // versioning status: Enabled/Suspended/""

	Policy        *adminops.PolicyInfo     `json:"policy,omitempty"`
	PolicyErr     string                   `json:"policyErr,omitempty"`
	ACL           *adminops.ACLInfo        `json:"acl,omitempty"`
	ACLErr        string                   `json:"aclErr,omitempty"`
	CORS          []adminops.CORSRule      `json:"cors,omitempty"`
	CORSErr       string                   `json:"corsErr,omitempty"`
	Lifecycle     []adminops.LifecycleRule `json:"lifecycle,omitempty"`
	LifecycleErr  string                   `json:"lifecycleErr,omitempty"`
	Encryption    adminops.EncryptionInfo  `json:"encryption"`
	EncryptionErr string                   `json:"encryptionErr,omitempty"`
	PAB           *adminops.PABInfo        `json:"pab,omitempty"`
	PABErr        string                   `json:"pabErr,omitempty"`
	Website       adminops.WebsiteInfo     `json:"website"`
	WebsiteErr    string                   `json:"websiteErr,omitempty"`
	Tags          []adminops.Tag           `json:"tags,omitempty"`
	TagsErr       string                   `json:"tagsErr,omitempty"`
	Lock          *adminops.LockConfig     `json:"lock,omitempty"`
	LockErr       string                   `json:"lockErr,omitempty"`

	// PublicWarning is non-empty when the configuration allows public
	// access (policy analyzer + public-access-block cross-check).
	PublicWarning string `json:"publicWarning,omitempty"`
}

// GetBucketAdmin fetches every admin section in one call.
func (a *App) GetBucketAdmin(bucket string) (AdminPanel, error) {
	c, err := a.client("")
	if err != nil {
		return AdminPanel{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()

	var p AdminPanel
	if loc, err := bucketops.Head(ctx, c.S3, bucket); err == nil {
		p.Region = string(loc.LocationConstraint)
		if p.Region == "" {
			p.Region = "us-east-1"
		}
	}
	p.Versions, _ = versioning.Status(ctx, c.S3, bucket)

	if info, err := adminops.GetPolicy(ctx, c.S3, bucket); err == nil {
		p.Policy = &info
	} else {
		p.PolicyErr = err.Error()
	}
	if acl, err := adminops.GetACL(ctx, c.S3, bucket); err == nil {
		p.ACL = &acl
	} else {
		p.ACLErr = err.Error()
	}
	if rules, err := adminops.GetCORS(ctx, c.S3, bucket); err == nil {
		p.CORS = rules
	} else {
		p.CORSErr = err.Error()
	}
	if rules, err := adminops.GetLifecycle(ctx, c.S3, bucket); err == nil {
		p.Lifecycle = rules
	} else {
		p.LifecycleErr = err.Error()
	}
	if enc, err := adminops.GetEncryption(ctx, c.S3, bucket); err == nil {
		p.Encryption = enc
	} else {
		p.EncryptionErr = err.Error()
	}
	if b, err := adminops.GetPAB(ctx, c.S3, bucket); err == nil {
		p.PAB = &b
	} else {
		p.PABErr = err.Error()
	}
	if w, err := adminops.GetWebsite(ctx, c.S3, bucket); err == nil {
		p.Website = w
	} else {
		p.WebsiteErr = err.Error()
	}
	if tags, err := adminops.GetTags(ctx, c.S3, bucket); err == nil {
		p.Tags = tags
	} else {
		p.TagsErr = err.Error()
	}
	if lock, err := adminops.GetLockConfig(ctx, c.S3, bucket); err == nil {
		p.Lock = &lock
	} else {
		p.LockErr = err.Error()
	}

	// Public-access banner: public policy/ACL without the block settings.
	public := (p.Policy != nil && p.Policy.Summary != nil &&
		(p.Policy.Summary.HasPublicRead || p.Policy.Summary.HasPublicWrite)) ||
		(p.ACL != nil && (p.ACL.Summary.PublicRead || p.ACL.Summary.AuthenticatedRead))
	blocked := p.PAB != nil && (p.PAB.BlockPublicACLs || p.PAB.BlockPublicPolicy)
	if public && !blocked {
		p.PublicWarning = "This bucket allows public access (see Security tab)."
	}
	return p, nil
}

// ---------------- setters (one per tab) ----------------

// SetBucketVersioning enables or suspends versioning.
func (a *App) SetBucketVersioning(bucket string, enable bool) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	status := "Suspended"
	note := "versioning suspended"
	if enable {
		status = "Enabled"
		note = "versioning enabled"
	}
	if err := versioning.SetStatus(ctx, c.S3, bucket, status); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("setting versioning failed: %v", err))
		return err
	}
	a.emitLogSrc(LogInfo, "admin", bucket, note)
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// PutBucketPolicy validates and stores a policy document (JSON).
func (a *App) PutBucketPolicy(bucket, raw string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutPolicy(ctx, c.S3, bucket, raw); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("updating bucket policy failed: %v", err))
		return err
	}
	a.emitLogSrc(LogInfo, "admin", bucket, "bucket policy updated")
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// DeleteBucketPolicy removes the policy.
func (a *App) DeleteBucketPolicy(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.DeletePolicy(ctx, c.S3, bucket); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("removing bucket policy failed: %v", err))
		return err
	}
	a.emitLogSrc(LogWarn, "admin", bucket, "bucket policy removed")
	return nil
}

// PutBucketCORS replaces the CORS rules.
func (a *App) PutBucketCORS(bucket string, rules []adminops.CORSRule) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutCORS(ctx, c.S3, bucket, rules); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("updating CORS rules failed: %v", err))
		return err
	}
	if len(rules) == 0 { // adminops routes an empty rule set to the delete wire-verb
		a.emitLogSrc(LogWarn, "admin", bucket, "CORS rules removed (empty set)")
		return nil
	}
	a.emitLogSrc(LogInfo, "admin", bucket, fmt.Sprintf("CORS rules updated (%d)", len(rules)))
	return nil
}

// DeleteBucketCORS removes all CORS rules.
func (a *App) DeleteBucketCORS(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.DeleteCORS(ctx, c.S3, bucket); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("removing CORS rules failed: %v", err))
		return err
	}
	a.emitLogSrc(LogWarn, "admin", bucket, "CORS rules removed")
	return nil
}

// PutBucketLifecycle replaces lifecycle rules.
func (a *App) PutBucketLifecycle(bucket string, rules []adminops.LifecycleRule) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutLifecycle(ctx, c.S3, bucket, rules); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("updating lifecycle rules failed: %v", err))
		return err
	}
	if len(rules) == 0 { // adminops routes an empty rule set to the delete wire-verb
		a.emitLogSrc(LogWarn, "admin", bucket, "lifecycle rules removed (empty set)")
		return nil
	}
	a.emitLogSrc(LogInfo, "admin", bucket, fmt.Sprintf("lifecycle rules updated (%d)", len(rules)))
	return nil
}

// DeleteBucketLifecycle removes lifecycle rules.
func (a *App) DeleteBucketLifecycle(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.DeleteLifecycle(ctx, c.S3, bucket); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("removing lifecycle rules failed: %v", err))
		return err
	}
	a.emitLogSrc(LogWarn, "admin", bucket, "lifecycle rules removed")
	return nil
}

// PutBucketEncryption sets default encryption ("AES256" | "aws:kms").
func (a *App) PutBucketEncryption(bucket, algorithm, kmsKeyID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutEncryption(ctx, c.S3, bucket, algorithm, kmsKeyID); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("setting default encryption failed: %v", err))
		return err
	}
	note := fmt.Sprintf("default encryption set to %s", algorithm)
	if algorithm == "aws:kms" && kmsKeyID != "" {
		note = fmt.Sprintf("default encryption set to %s (key %s)", algorithm, kmsKeyID)
	}
	a.emitLogSrc(LogInfo, "admin", bucket, note)
	return nil
}

// DeleteBucketEncryption removes default encryption.
func (a *App) DeleteBucketEncryption(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.DeleteEncryption(ctx, c.S3, bucket); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("removing default encryption failed: %v", err))
		return err
	}
	a.emitLogSrc(LogWarn, "admin", bucket, "default encryption removed")
	return nil
}

// PutBucketPAB stores public-access-block settings.
func (a *App) PutBucketPAB(bucket string, pab adminops.PABInfo) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutPAB(ctx, c.S3, bucket, pab); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("updating public-access-block settings failed: %v", err))
		return err
	}
	a.emitLogSrc(LogInfo, "admin", bucket, "public-access-block settings updated")
	return nil
}

// PutBucketWebsite stores website hosting configuration.
func (a *App) PutBucketWebsite(bucket string, w adminops.WebsiteInfo) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutWebsite(ctx, c.S3, bucket, w); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("configuring website hosting failed: %v", err))
		return err
	}
	a.emitLogSrc(LogInfo, "admin", bucket, "website hosting configured")
	return nil
}

// DeleteBucketWebsite removes website hosting.
func (a *App) DeleteBucketWebsite(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.DeleteWebsite(ctx, c.S3, bucket); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("removing website hosting failed: %v", err))
		return err
	}
	a.emitLogSrc(LogWarn, "admin", bucket, "website hosting removed")
	return nil
}

// PutBucketTags replaces the bucket tag set.
func (a *App) PutBucketTags(bucket string, tags []adminops.Tag) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.PutTags(ctx, c.S3, bucket, tags); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("updating tags failed: %v", err))
		return err
	}
	a.emitLogSrc(LogInfo, "admin", bucket, fmt.Sprintf("tags updated (%d)", len(tags)))
	return nil
}

// DeleteBucketTags removes all bucket tags.
func (a *App) DeleteBucketTags(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := adminops.DeleteTags(ctx, c.S3, bucket); err != nil {
		a.emitLogSrc(LogError, "admin", bucket, fmt.Sprintf("removing tags failed: %v", err))
		return err
	}
	a.emitLogSrc(LogWarn, "admin", bucket, "tags removed")
	return nil
}

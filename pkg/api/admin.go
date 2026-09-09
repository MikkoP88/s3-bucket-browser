package api

import (
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
)

// AdminPanel is the whole bucket-administration view (PLAN.md §8.5, M3).
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
	if enable {
		status = "Enabled"
	}
	if err := versioning.SetStatus(ctx, c.S3, bucket, status); err != nil {
		return err
	}
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
		return err
	}
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
	return adminops.DeletePolicy(ctx, c.S3, bucket)
}

// PutBucketCORS replaces the CORS rules.
func (a *App) PutBucketCORS(bucket string, rules []adminops.CORSRule) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutCORS(ctx, c.S3, bucket, rules)
}

// DeleteBucketCORS removes all CORS rules.
func (a *App) DeleteBucketCORS(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.DeleteCORS(ctx, c.S3, bucket)
}

// PutBucketLifecycle replaces lifecycle rules.
func (a *App) PutBucketLifecycle(bucket string, rules []adminops.LifecycleRule) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutLifecycle(ctx, c.S3, bucket, rules)
}

// DeleteBucketLifecycle removes lifecycle rules.
func (a *App) DeleteBucketLifecycle(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.DeleteLifecycle(ctx, c.S3, bucket)
}

// PutBucketEncryption sets default encryption ("AES256" | "aws:kms").
func (a *App) PutBucketEncryption(bucket, algorithm, kmsKeyID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutEncryption(ctx, c.S3, bucket, algorithm, kmsKeyID)
}

// DeleteBucketEncryption removes default encryption.
func (a *App) DeleteBucketEncryption(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.DeleteEncryption(ctx, c.S3, bucket)
}

// PutBucketPAB stores public-access-block settings.
func (a *App) PutBucketPAB(bucket string, pab adminops.PABInfo) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutPAB(ctx, c.S3, bucket, pab)
}

// PutBucketWebsite stores website hosting configuration.
func (a *App) PutBucketWebsite(bucket string, w adminops.WebsiteInfo) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutWebsite(ctx, c.S3, bucket, w)
}

// DeleteBucketWebsite removes website hosting.
func (a *App) DeleteBucketWebsite(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.DeleteWebsite(ctx, c.S3, bucket)
}

// PutBucketTags replaces the bucket tag set.
func (a *App) PutBucketTags(bucket string, tags []adminops.Tag) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutTags(ctx, c.S3, bucket, tags)
}

// DeleteBucketTags removes all bucket tags.
func (a *App) DeleteBucketTags(bucket string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.DeleteTags(ctx, c.S3, bucket)
}

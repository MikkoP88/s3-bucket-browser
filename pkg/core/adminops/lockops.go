// lockops.go implements object-lock administration (PLAN.md §8.7):
// bucket-level lock configuration and per-object retention / legal hold.
// Object lock requires versioning and is a one-way door on AWS: once
// enabled it cannot be disabled — only tightened.
package adminops

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// LockConfig is the bucket-level object-lock configuration.
type LockConfig struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode,omitempty"` // GOVERNANCE | COMPLIANCE
	Days    int32  `json:"days,omitempty"` // default retention period
}

// GetLockConfig reads the bucket object-lock configuration. A bucket that
// never configured object lock returns the zero config, no error.
func GetLockConfig(ctx context.Context, client *s3.Client, bucket string) (LockConfig, error) {
	out, err := client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return LockConfig{}, mapUnsupported(err, "object lock")
	}
	cfg := LockConfig{}
	if out.ObjectLockConfiguration == nil {
		return cfg, nil
	}
	if out.ObjectLockConfiguration.ObjectLockEnabled == s3types.ObjectLockEnabledEnabled {
		cfg.Enabled = true
	}
	if r := out.ObjectLockConfiguration.Rule; r != nil && r.DefaultRetention != nil {
		dr := r.DefaultRetention
		cfg.Mode = string(dr.Mode)
		cfg.Days = aws.ToInt32(dr.Days)
	}
	return cfg, nil
}

// PutLockConfig enables object lock (optionally with a default retention
// rule). mode must be GOVERNANCE or COMPLIANCE when days > 0. AWS rejects
// disabling once enabled; MinIO mirrors that.
func PutLockConfig(ctx context.Context, client *s3.Client, bucket string, cfg LockConfig) error {
	input := &s3.PutObjectLockConfigurationInput{Bucket: aws.String(bucket)}
	if cfg.Enabled {
		input.ObjectLockConfiguration = &s3types.ObjectLockConfiguration{
			ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
		}
		if cfg.Days > 0 || cfg.Mode != "" {
			input.ObjectLockConfiguration.Rule = &s3types.ObjectLockRule{
				DefaultRetention: &s3types.DefaultRetention{
					Mode: s3types.ObjectLockRetentionMode(cfg.Mode),
					Days: aws.Int32(cfg.Days),
				},
			}
		}
	} else {
		// Explicit "off": the SDK only defines the "Enabled" enum member, so
		// the disabled value goes over the wire as a raw string. Providers
		// reject it once lock is enabled; the error maps to the unsupported
		// message then.
		input.ObjectLockConfiguration = &s3types.ObjectLockConfiguration{
			ObjectLockEnabled: s3types.ObjectLockEnabled("Disabled"),
		}
	}
	_, err := client.PutObjectLockConfiguration(ctx, input)
	return mapUnsupported(err, "object lock")
}

// ObjectLock is the per-object lock state (retention + legal hold).
type ObjectLock struct {
	RetainUntil *time.Time `json:"retainUntil,omitempty"`
	Mode        string     `json:"mode,omitempty"`      // GOVERNANCE | COMPLIANCE
	LegalHold   string     `json:"legalHold,omitempty"` // ON | OFF | "" (unknown)
}

// GetObjectLock reads retention and legal hold for one object version.
func GetObjectLock(ctx context.Context, client *s3.Client, bucket, key, versionID string) (ObjectLock, error) {
	out := ObjectLock{}
	ret, err := client.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: versionIDOrNil(versionID),
	})
	if err == nil && ret.Retention != nil {
		out.Mode = string(ret.Retention.Mode)
		if ret.Retention.RetainUntilDate != nil {
			t := ret.Retention.RetainUntilDate.UTC()
			out.RetainUntil = &t
		}
	} else if err != nil && !isCode(err, "NoSuchObjectLockConfiguration") {
		return out, mapUnsupported(err, "object lock")
	}
	hold, err := client.GetObjectLegalHold(ctx, &s3.GetObjectLegalHoldInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: versionIDOrNil(versionID),
	})
	if err == nil && hold.LegalHold != nil {
		out.LegalHold = string(hold.LegalHold.Status)
	} else if err != nil && !isCode(err, "NoSuchObjectLockConfiguration") {
		return out, mapUnsupported(err, "object lock")
	}
	return out, nil
}

// PutObjectRetention sets retention on one object version. mode must be
// GOVERNANCE or COMPLIANCE; until is the retain-until instant. bypass sends
// x-amz-bypass-governance-retention: shortening (or later clearing) an
// existing GOVERNANCE retention is rejected without it, even for admins —
// the server still checks the s3:BypassGovernanceRetention permission.
func PutObjectRetention(ctx context.Context, client *s3.Client, bucket, key, versionID, mode string, until time.Time, bypass bool) error {
	_, err := client.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: versionIDOrNil(versionID),
		BypassGovernanceRetention: aws.Bool(bypass),
		Retention: &s3types.ObjectLockRetention{
			Mode:            s3types.ObjectLockRetentionMode(mode),
			RetainUntilDate: aws.Time(until),
		},
	})
	return mapUnsupported(err, "object lock")
}

// DeleteObjectRetention clears retention. S3 has no dedicated delete API:
// an empty retention body on PutObjectRetention is the documented way to
// remove it. GOVERNANCE cannot be cleared without bypass; COMPLIANCE cannot
// be cleared at all before expiry.
func DeleteObjectRetention(ctx context.Context, client *s3.Client, bucket, key, versionID string, bypass bool) error {
	_, err := client.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: versionIDOrNil(versionID),
		BypassGovernanceRetention: aws.Bool(bypass),
		Retention:                 &s3types.ObjectLockRetention{},
	})
	return mapUnsupported(err, "object lock")
}

// PutObjectLegalHold toggles the legal hold on one object version.
func PutObjectLegalHold(ctx context.Context, client *s3.Client, bucket, key, versionID string, on bool) error {
	status := s3types.ObjectLockLegalHoldStatusOff
	if on {
		status = s3types.ObjectLockLegalHoldStatusOn
	}
	_, err := client.PutObjectLegalHold(ctx, &s3.PutObjectLegalHoldInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: versionIDOrNil(versionID),
		LegalHold: &s3types.ObjectLockLegalHold{Status: status},
	})
	return mapUnsupported(err, "object lock")
}

func versionIDOrNil(id string) *string {
	if id == "" {
		return nil
	}
	return aws.String(id)
}

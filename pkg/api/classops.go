// classops.go: GUI-facing storage-class conversion + object-lock
// operations (M5, PLAN.md §8.6/8.7).
package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// guiConvertThreshold mirrors the CLI's rmForceThreshold: converting more
// than this many objects in one GUI action needs an explicit confirm.
const guiConvertThreshold = 50

// ConvertStorageClass converts one or more objects to the storage class
// via server-side self-copy. Keys ending in "/" expand to every object
// under that prefix. Without force the call refuses batches larger than
// guiConvertThreshold. Returns how many objects were converted.
func (a *App) ConvertStorageClass(bucket string, keys []string, class string, force bool) (int, error) {
	c, err := a.client("")
	if err != nil {
		return 0, err
	}
	if !transfer.ValidStorageClass(class) {
		return 0, fmt.Errorf("unknown storage class %q", class)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	// expand folder selections into their object keys
	var flat []string
	for _, k := range keys {
		if strings.HasSuffix(k, "/") {
			sub, err := transfer.CollectPrefixKeys(ctx, c.S3, bucket, k)
			if err != nil {
				return 0, err
			}
			flat = append(flat, sub...)
		} else {
			flat = append(flat, k)
		}
	}
	if !force && len(flat) > guiConvertThreshold {
		return 0, fmt.Errorf("would convert %d object(s) — confirm to proceed", len(flat))
	}
	done := 0
	a.emitLog(LogInfo, "admin", fmt.Sprintf("converting %d object(s) in %s to storage class %s", len(flat), bucket, class))
	for _, k := range flat {
		if err := transfer.ConvertStorageClass(ctx, c.S3, bucket, k, "", class); err != nil {
			a.emitLog(LogError, "admin", fmt.Sprintf("storage-class conversion in %s failed after %d object(s): %v", bucket, done, err))
			return done, err
		}
		done++
	}
	a.emitLog(LogInfo, "admin", fmt.Sprintf("converted %d object(s) in %s to %s", done, bucket, class))
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return done, nil
}

// PutBucketLockConfig enables object lock (optionally a default retention
// rule: mode GOVERNANCE|COMPLIANCE + days).
func (a *App) PutBucketLockConfig(bucket string, enabled bool, mode string, days int32) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutLockConfig(ctx, c.S3, bucket, adminops.LockConfig{
		Enabled: enabled, Mode: mode, Days: days,
	})
}

// GetObjectLock returns retention + legal hold for one object version.
func (a *App) GetObjectLock(bucket, key, versionID string) (adminops.ObjectLock, error) {
	c, err := a.client("")
	if err != nil {
		return adminops.ObjectLock{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.GetObjectLock(ctx, c.S3, bucket, key, versionID)
}

// PutObjectRetention sets retention until an RFC3339 instant
// ("+Nd"/"+Nh" relative forms are also accepted).
func (a *App) PutObjectRetention(bucket, key, versionID, mode, until string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	when, err := parseUntil(until, time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutObjectRetention(ctx, c.S3, bucket, key, versionID, mode, when, false)
}

// ClearObjectRetention removes GOVERNANCE retention.
func (a *App) ClearObjectRetention(bucket, key, versionID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.DeleteObjectRetention(ctx, c.S3, bucket, key, versionID, false)
}

// SetObjectLegalHold toggles the legal hold on one object version.
func (a *App) SetObjectLegalHold(bucket, key, versionID string, on bool) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return adminops.PutObjectLegalHold(ctx, c.S3, bucket, key, versionID, on)
}

// parseUntil accepts an RFC3339 timestamp or "+30d"/"+12h" relative
// durations measured from now.
func parseUntil(s string, now time.Time) (time.Time, error) {
	if len(s) > 1 && s[0] == '+' {
		d, err := parseRelDuration(s[1:])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid retention until %q (use RFC3339 or +Nd/+Nh)", s)
		}
		return now.Add(d), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid retention until %q (use RFC3339 or +Nd/+Nh)", s)
	}
	return t, nil
}

// parseRelDuration parses "30d", "12h", "90m" relative durations.
func parseRelDuration(s string) (time.Duration, error) {
	if n := len(s); n > 1 && s[n-1] == 'd' {
		days := 0
		if _, err := fmt.Sscanf(s[:n-1], "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

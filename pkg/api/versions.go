package api

import (
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
)

// ObjectVersions returns the version timeline of one key, newest first.
func (a *App) ObjectVersions(bucket, key string) ([]versioning.Version, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return versioning.ListForObject(ctx, c.S3, bucket, key)
}

// RestoreVersion makes an old version current again (server-side copy).
func (a *App) RestoreVersion(bucket, key, versionID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := versioning.RestoreVersion(ctx, c.S3, bucket, key, versionID); err != nil {
		return err
	}
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// UndoDelete removes a delete marker: the object reappears with its previous
// current version (PLAN.md §8.6 "delete marker UX").
func (a *App) UndoDelete(bucket, key, versionID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := versioning.RemoveDeleteMarker(ctx, c.S3, bucket, key, versionID); err != nil {
		return err
	}
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// DeleteVersionPermanent permanently deletes one version (L3: the frontend
// gates this behind a typed "permanent" confirmation, PLAN.md §9).
func (a *App) DeleteVersionPermanent(bucket, key, versionID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := versioning.DeleteVersion(ctx, c.S3, bucket, key, versionID); err != nil {
		return err
	}
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// DeleteObjectPermanently destroys every version of one key (L3: the GUI
// gates this behind a typed "permanent" confirmation, Shift+Del).
func (a *App) DeleteObjectPermanently(bucket, key string) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	res, err := versioning.DeleteAllVersions(ctx, c.S3, bucket, key)
	if res.Deleted > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return res, err
}

// PurgePreview counts what a purge would remove (count-then-act).
// mode: "noncurrent" | "markers" | "all".
func (a *App) PurgePreview(bucket, prefix, mode string) (int, error) {
	c, err := a.client("")
	if err != nil {
		return 0, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return versioning.CountPurge(ctx, c.S3, bucket, dirPrefix(prefix), versioning.PurgeMode(mode))
}

// PurgeVersions removes versions/markers under a prefix (L2/L3 in the GUI;
// force mirrors `--force` on the CLI).
func (a *App) PurgeVersions(bucket, prefix, mode string, force bool) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	n, err := versioning.CountPurge(ctx, c.S3, bucket, dirPrefix(prefix), versioning.PurgeMode(mode))
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	if n > deleteForceThreshold && !force {
		return transfer.DeleteResult{}, fmt.Errorf(
			"%d version(s) would be removed — typed confirmation (force) required", n)
	}
	res, err := versioning.Purge(ctx, c.S3, bucket, dirPrefix(prefix), versioning.PurgeMode(mode))
	if res.Deleted > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return res, err
}

// BucketVersionStats aggregates version counts for the dashboard.
func (a *App) BucketVersionStats(bucket string) (versioning.Stats, error) {
	c, err := a.client("")
	if err != nil {
		return versioning.Stats{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return versioning.CollectStats(ctx, c.S3, bucket, "")
}

// EmptyBucketAllVersions removes every version and delete marker (L2+L3 in
// the GUI: typed bucket name + permanent warning). Used before bucket
// removal on versioned buckets and by the "Empty bucket" tool.
func (a *App) EmptyBucketAllVersions(bucket string) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	res, err := versioning.EmptyBucketVersions(ctx, c.S3, bucket)
	if res.Deleted > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return res, err
}

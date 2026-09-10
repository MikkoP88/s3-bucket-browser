package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// ---------------- version compare (M10 panels v2) ----------------

// versionDiffCap bounds each side's fetched text; larger versions compare by
// metadata only.
const versionDiffCap = 512 * 1024

// VersionTexts carries the comparable content of two versions for the
// compare view. Metadata (size/ETag/time/class) comes from the version list
// the dialog already holds; this call is about content.
type VersionTexts struct {
	AText     string `json:"aText"`
	BText     string `json:"bText"`
	Truncated bool   `json:"truncated"` // a side hit the size cap
	Skipped   string `json:"skipped"`   // "" when text was fetched; otherwise why
}

// VersionDiffText fetches two versions' content for the compare view.
// Binary, oversized or delete-marker versions are skipped with a reason
// instead of streaming megabytes into the dialog.
func (a *App) VersionDiffText(bucket, key, versionA, versionB string) (VersionTexts, error) {
	c, err := a.client("")
	if err != nil {
		return VersionTexts{}, err
	}
	if versionA == "" || versionB == "" {
		return VersionTexts{}, errors.New("two version IDs are required")
	}
	ctx, cancel := a.quickCtx()
	defer cancel()

	var out VersionTexts
	for i, id := range [2]string{versionA, versionB} {
		txt, trunc, skip, err := fetchVersionText(ctx, c.S3, bucket, key, id)
		if err != nil {
			return out, err
		}
		if skip != "" {
			out.Skipped = fmt.Sprintf("version %s: %s", shortVersion(id), skip)
			return out, nil
		}
		if i == 0 {
			out.AText = txt
		} else {
			out.BText = txt
		}
		out.Truncated = out.Truncated || trunc
	}
	return out, nil
}

// shortVersion keeps skip messages readable.
func shortVersion(id string) string {
	if len(id) > 12 {
		return "…" + id[len(id)-8:]
	}
	return id
}

// fetchVersionText reads one version's body (capped) and reports whether it
// is comparable text.
func fetchVersionText(ctx context.Context, cl *s3.Client, bucket, key, versionID string) (string, bool, string, error) {
	out, err := cl.GetObject(ctx, &s3.GetObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		var rerr *awshttp.ResponseError
		if errors.As(err, &rerr) && rerr.HTTPStatusCode() == http.StatusMethodNotAllowed {
			return "", false, "delete marker — no content", nil
		}
		return "", false, "", err
	}
	defer out.Body.Close()
	if n := aws.ToInt64(out.ContentLength); n > versionDiffCap {
		return "", false, fmt.Sprintf("size %s exceeds the %d KB text cap", fmtSize(n), versionDiffCap/1024), nil
	}
	b, err := io.ReadAll(io.LimitReader(out.Body, versionDiffCap+1))
	if err != nil {
		return "", false, "", err
	}
	trunc := int64(len(b)) > versionDiffCap
	if trunc {
		b = b[:versionDiffCap]
	}
	if !looksTexty(b) {
		return "", false, "binary content", nil
	}
	return string(b), trunc, "", nil
}

// looksTexty reports whether b plausibly decodes as text: valid UTF-8 with
// no NUL bytes (unit-tested).
func looksTexty(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	return utf8.Valid(b)
}

// fmtSize is a tiny byte formatter for skip messages.
func fmtSize(n int64) string {
	const k = 1024
	switch {
	case n >= k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}

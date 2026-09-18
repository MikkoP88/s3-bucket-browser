package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
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
		a.emitLogSrc(LogError, "versions", bucket, fmt.Sprintf("restoring %s (%s) failed: %v", key, versionID, err))
		return err
	}
	a.emitLogSrc(LogInfo, "versions", bucket, fmt.Sprintf("restored %s (version %s)", key, versionID))
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// UndoDelete removes a delete marker: the object reappears with its previous
// current version ("delete marker UX").
func (a *App) UndoDelete(bucket, key, versionID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := versioning.RemoveDeleteMarker(ctx, c.S3, bucket, key, versionID); err != nil {
		a.emitLogSrc(LogError, "versions", bucket, fmt.Sprintf("undoing delete of %s failed: %v", key, err))
		return err
	}
	a.emitLogSrc(LogWarn, "versions", bucket, fmt.Sprintf("undid delete of %s (marker %s removed)", key, versionID))
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// DeleteVersionPermanent permanently deletes one version (L3: the frontend
// gates this behind a typed "permanent" confirmation).
func (a *App) DeleteVersionPermanent(bucket, key, versionID string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := versioning.DeleteVersion(ctx, c.S3, bucket, key, versionID); err != nil {
		a.emitLogSrc(LogError, "versions", bucket, fmt.Sprintf("permanently deleting version %s of %s failed: %v", versionID, key, err))
		return err
	}
	a.emitLogSrc(LogWarn, "versions", bucket, fmt.Sprintf("permanently deleted version %s of %s", versionID, key))
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
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
// force mirrors `--force` on the CLI). Runs as a tracked task — a purge
// too big for the quick-op bound stays visible and cancelable.
func (a *App) PurgeVersions(bucket, prefix, mode string, force bool) (res transfer.DeleteResult, err error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	task := a.tasks.add("purge", fmt.Sprintf("s3://%s/%s — purge %s versions", bucket, dirPrefix(prefix), mode))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()
	task.setPhase(TaskPhaseCount)
	n, err := versioning.CountPurge(ctx, c.S3, bucket, dirPrefix(prefix), versioning.PurgeMode(mode))
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	if n > deleteForceThreshold && !force {
		return transfer.DeleteResult{}, fmt.Errorf(
			"%d version(s) would be removed — typed confirmation (force) required", n)
	}
	task.setTotal(n, fmt.Sprintf("s3://%s/%s — purging %d %s version(s)", bucket, dirPrefix(prefix), n, mode))
	res, err = versioning.PurgeProg(ctx, c.S3, bucket, dirPrefix(prefix), versioning.PurgeMode(mode), func(deleted int) {
		task.progress(deleted)
	})
	task.progress(res.Deleted)
	if err != nil {
		a.emitLogSrc(LogError, "versions", bucket, fmt.Sprintf("purging %s versions under %s failed: %v", mode, dirPrefix(prefix), err))
	} else if res.Deleted > 0 {
		a.emitLogSrc(LogWarn, "versions", bucket, fmt.Sprintf("purged %d %s version(s) under %s", res.Deleted, mode, dirPrefix(prefix)))
	}
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

// PrefixVersionSummary reports per-immediate-child version aggregates for
// one folder view of a versioned bucket — the grid's delete-marker badges
// (files with markers in their history, directories whose every file is
// delete-marked). One ListObjectVersions pass under the prefix.
// Addresses the source the main view is browsing (SetViewSource).
func (a *App) PrefixVersionSummary(bucket, prefix string) ([]versioning.ChildSummary, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	return a.prefixVersionSummaryC(c, bucket, prefix)
}

// SourcePrefixVersionSummary is PrefixVersionSummary pinned to one named
// S3 source.
func (a *App) SourcePrefixVersionSummary(idOrName, bucket, prefix string) ([]versioning.ChildSummary, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return nil, err
	}
	return a.prefixVersionSummaryC(c, bucket, prefix)
}

func (a *App) prefixVersionSummaryC(c *s3client.Client, bucket, prefix string) ([]versioning.ChildSummary, error) {
	ctx, cancel := a.quickCtx()
	defer cancel()
	return versioning.ChildSummaries(ctx, c.S3, bucket, dirPrefix(prefix))
}

// PrefixVersionStats aggregates the version state under one prefix (the
// Content Versions window): totals for current objects, noncurrent
// versions, delete markers and noncurrent bytes. Addresses the source the
// main view is browsing.
func (a *App) PrefixVersionStats(bucket, prefix string) (versioning.Stats, error) {
	c, err := a.client("")
	if err != nil {
		return versioning.Stats{}, err
	}
	return a.prefixVersionStatsC(c, bucket, prefix)
}

func (a *App) prefixVersionStatsC(c *s3client.Client, bucket, prefix string) (versioning.Stats, error) {
	ctx, cancel := a.quickCtx()
	defer cancel()
	return versioning.CollectStats(ctx, c.S3, bucket, dirPrefix(prefix))
}

// markerListCap bounds the Delete Marker window's listing; larger sets are
// reported as truncated instead of streamed into the dialog.
const markerListCap = 500

// MarkerList is the Delete Marker window's payload: every delete marker
// under a prefix (or one key's markers), newest first.
type MarkerList struct {
	Markers   []versioning.Version `json:"markers"`
	Truncated bool                 `json:"truncated"`
}

// PrefixMarkers lists the delete markers under a prefix (the Delete Marker
// window). For a file key pass the exact key with exactKey=true; otherwise
// the prefix is treated as a folder. Addresses the source the main view is
// browsing.
func (a *App) PrefixMarkers(bucket, prefix string, exactKey bool) (MarkerList, error) {
	c, err := a.client("")
	if err != nil {
		return MarkerList{}, err
	}
	return a.prefixMarkersC(c, bucket, prefix, exactKey)
}

func (a *App) prefixMarkersC(c *s3client.Client, bucket, prefix string, exactKey bool) (MarkerList, error) {
	ctx, cancel := a.quickCtx()
	defer cancel()
	if exactKey {
		vers, err := versioning.ListForObject(ctx, c.S3, bucket, prefix)
		if err != nil {
			return MarkerList{}, err
		}
		out := MarkerList{}
		for _, v := range vers {
			if v.IsDeleteMarker {
				out.Markers = append(out.Markers, v)
			}
		}
		return out, nil
	}
	ms, err := versioning.ListMarkers(ctx, c.S3, bucket, dirPrefix(prefix), markerListCap)
	if err != nil {
		return MarkerList{}, err
	}
	return MarkerList{Markers: ms, Truncated: len(ms) >= markerListCap}, nil
}

// DeleteSelectionPermanent destroys every version AND delete marker of the
// selection (L3): file keys lose their whole timeline, folder keys purge
// every version beneath them (a plain DeleteObject on a folder key would
// only touch the folder marker). Count-then-act with the same force gate
// as DeleteSelection; the frontend gates it behind the typed "permanent"
// confirmation. Addresses the source the main view is browsing.
func (a *App) DeleteSelectionPermanent(bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return a.deleteSelectionPermanentC(c, bucket, keys, force)
}

// SourceDeleteSelectionPermanent is DeleteSelectionPermanent pinned to one
// named S3 source.
func (a *App) SourceDeleteSelectionPermanent(idOrName, bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return a.deleteSelectionPermanentC(c, bucket, keys, force)
}

func (a *App) deleteSelectionPermanentC(c *s3client.Client, bucket string, keys []string, force bool) (out transfer.DeleteResult, err error) {
	// Tracked task (Running tasks window): the count phase can walk a
	// whole subtree — canceling there destroys nothing.
	task := a.tasks.add("purge", fmt.Sprintf("s3://%s — destroy versions of %d item(s)", bucket, len(keys)))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()

	task.setPhase(TaskPhaseCount)
	total := 0
	for _, k := range keys {
		task.setCurrent(k)
		n, err := a.countPermanent(ctx, c, bucket, k)
		if err != nil {
			return out, err
		}
		total += n
	}
	if total > deleteForceThreshold && !force {
		return out, fmt.Errorf(
			"%d version(s) selected — typed confirmation (force) required to delete permanently", total)
	}
	task.setTotal(total, fmt.Sprintf("s3://%s — destroying %d version(s)/marker(s)", bucket, total))
	for _, k := range keys {
		task.setCurrent(k)
		res, err := a.purgePermanent(ctx, c, bucket, k)
		out.Deleted += res.Deleted
		out.Errors = append(out.Errors, res.Errors...)
		task.progress(out.Deleted)
		if err != nil {
			a.emitLogSrc(LogError, "versions", bucket, fmt.Sprintf("permanently deleting %s failed: %v", k, err))
			return out, err
		}
	}
	a.emitLogSrc(LogWarn, "versions", bucket, fmt.Sprintf(
		"permanently deleted %d version(s)/marker(s) across %d selection item(s)", out.Deleted, len(keys)))
	if out.Deleted > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return out, nil
}

// DeleteSelectionKeepCurrent destroys every version AND delete marker of
// the selection EXCEPT each key's current entry (L3): objects stay as they
// are right now, their history is erased. Folder keys keep only the current
// entry of every key beneath them. Same force gate as the permanent paths;
// the frontend's Delete Window gates it behind the typed "delete"
// confirmation. Addresses the source the main view is browsing.
func (a *App) DeleteSelectionKeepCurrent(bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return a.deleteSelectionKeepCurrentC(c, bucket, keys, force)
}

// SourceDeleteSelectionKeepCurrent is DeleteSelectionKeepCurrent pinned to
// one named S3 source.
func (a *App) SourceDeleteSelectionKeepCurrent(idOrName, bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return a.deleteSelectionKeepCurrentC(c, bucket, keys, force)
}

func (a *App) deleteSelectionKeepCurrentC(c *s3client.Client, bucket string, keys []string, force bool) (out transfer.DeleteResult, err error) {
	// Tracked task (Running tasks window) — same two-phase shape as the
	// permanent path: cancel during counting erases nothing.
	task := a.tasks.add("purge", fmt.Sprintf("s3://%s — clear history of %d item(s)", bucket, len(keys)))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()

	task.setPhase(TaskPhaseCount)
	total := 0
	for _, k := range keys {
		task.setCurrent(k)
		n, err := a.countKeepCurrent(ctx, c, bucket, k)
		if err != nil {
			return out, err
		}
		total += n
	}
	if total > deleteForceThreshold && !force {
		return out, fmt.Errorf(
			"%d version(s) would be removed — typed confirmation (force) required to delete all but the current version", total)
	}
	task.setTotal(total, fmt.Sprintf("s3://%s — removing %d noncurrent version(s)", bucket, total))
	for _, k := range keys {
		task.setCurrent(k)
		res, err := a.purgeKeepCurrent(ctx, c, bucket, k)
		out.Deleted += res.Deleted
		out.Errors = append(out.Errors, res.Errors...)
		task.progress(out.Deleted)
		if err != nil {
			a.emitLogSrc(LogError, "versions", bucket, fmt.Sprintf("deleting history of %s failed: %v", k, err))
			return out, err
		}
	}
	a.emitLogSrc(LogWarn, "versions", bucket, fmt.Sprintf(
		"deleted all but the current version of %d selection item(s) (%d version(s)/marker(s) removed)", len(keys), out.Deleted))
	if out.Deleted > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return out, nil
}

// countKeepCurrent counts the non-current versions+markers one selection
// item would destroy.
func (a *App) countKeepCurrent(ctx context.Context, c *s3client.Client, bucket, key string) (int, error) {
	if strings.HasSuffix(key, "/") {
		return versioning.CountPurge(ctx, c.S3, bucket, dirPrefix(key), versioning.PurgeKeepCurrent)
	}
	vers, err := versioning.ListForObject(ctx, c.S3, bucket, key)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, v := range vers {
		if !v.IsLatest {
			n++
		}
	}
	return n, nil
}

// purgeKeepCurrent destroys one selection item's non-current entries.
func (a *App) purgeKeepCurrent(ctx context.Context, c *s3client.Client, bucket, key string) (transfer.DeleteResult, error) {
	if strings.HasSuffix(key, "/") {
		return versioning.Purge(ctx, c.S3, bucket, dirPrefix(key), versioning.PurgeKeepCurrent)
	}
	return versioning.DeleteKeepCurrent(ctx, c.S3, bucket, key)
}

// countPermanent counts the versions+markers one selection item would
// destroy: the key's whole timeline, or every version under a folder.
func (a *App) countPermanent(ctx context.Context, c *s3client.Client, bucket, key string) (int, error) {
	if strings.HasSuffix(key, "/") {
		return versioning.CountPurge(ctx, c.S3, bucket, dirPrefix(key), versioning.PurgeAll)
	}
	vers, err := versioning.ListForObject(ctx, c.S3, bucket, key)
	if err != nil {
		return 0, err
	}
	return len(vers), nil
}

// purgePermanent destroys one selection item's versions+markers.
func (a *App) purgePermanent(ctx context.Context, c *s3client.Client, bucket, key string) (transfer.DeleteResult, error) {
	if strings.HasSuffix(key, "/") {
		return versioning.Purge(ctx, c.S3, bucket, dirPrefix(key), versioning.PurgeAll)
	}
	return versioning.DeleteAllVersions(ctx, c.S3, bucket, key)
}

// EmptyBucketAllVersions removes every version and delete marker (L2+L3 in
// the GUI: typed bucket name + permanent warning). Used before bucket
// removal on versioned buckets and by the "Empty bucket" tool. Runs as a
// tracked task — the one purge that can legitimately run for a long time.
func (a *App) EmptyBucketAllVersions(bucket string) (res transfer.DeleteResult, err error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	task := a.tasks.add("empty", fmt.Sprintf("s3://%s — empty bucket (all versions)", bucket))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()
	task.setPhase(TaskPhaseCount)
	res, err = versioning.EmptyBucketVersionsProg(ctx, c.S3, bucket,
		func(total int) {
			task.setTotal(total, fmt.Sprintf("s3://%s — emptying %d version(s)/marker(s)", bucket, total))
		},
		func(deleted int) { task.progress(deleted) })
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

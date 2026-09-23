// versions_copy.go: the GUI's version-preserving S3→S3 copy — a
// selection-driven wrapper over versioning.CopyVersionedTimelines (the
// engine `cp --versions` uses) running as a background job so the
// transfer manager shows live progress.
package api

import (
	"fmt"
	"path"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// vcopyItem is one selected entry's share of the plan: its timelines plus
// the mapping context (exact object vs "folder/" prefix).
type vcopyItem struct {
	timelines []versioning.KeyTimeline
	exact     bool
	prefix    string // folders: the selected prefix ("docs/"); objects: the key
}

// CopySelectionVersions recreates the full version timelines of a
// selection (object keys and "folder/" prefixes) in another bucket as a
// background job, returning the job ID. srcSource/dstSource name any S3
// source ("" = the view source); a same-client pair copies each version
// server-side, different sources stream through this machine. move
// permanently deletes every source version only after everything copied
// cleanly. Fails up front when the destination bucket is not versioning
// enabled — the copied timeline would collapse to the last write.
func (a *App) CopySelectionVersions(srcSource, srcBucket string, keys []string, dstSource, dstBucket, dstPrefix string, move bool) (string, error) {
	if len(keys) == 0 {
		return "", fmt.Errorf("nothing selected")
	}
	srcC, err := a.client(srcSource)
	if err != nil {
		return "", err
	}
	dstC, err := a.client(dstSource)
	if err != nil {
		return "", err
	}

	// Plan synchronously: the caller learns "cannot preserve versions
	// here" (destination not versioned, unreachable) before the dialog
	// even closes, and the job starts with real totals.
	pctx, pcancel := a.quickCtx()
	defer pcancel()
	status, err := versioning.Status(pctx, dstC.S3, dstBucket)
	if err != nil {
		return "", err
	}
	if status != "Enabled" {
		return "", fmt.Errorf("destination bucket %s must have versioning enabled (it is %q) — without it the copied timeline collapses to the last write",
			dstBucket, status)
	}
	dstPrefix = dirPrefix(dstPrefix)

	items := make([]vcopyItem, 0, len(keys))
	total, markers := 0, 0
	var totalBytes int64
	for _, k := range keys {
		exact := !strings.HasSuffix(k, "/")
		tp, err := versioning.PlanVersionedCopy(pctx, srcC.S3, srcBucket, k, exact)
		if err != nil {
			return "", err
		}
		items = append(items, vcopyItem{timelines: tp, exact: exact, prefix: k})
		for _, t := range tp {
			total += len(t.Versions)
			if t.IsDeleted {
				markers++
			}
			for _, v := range t.Versions {
				totalBytes += v.Size
			}
		}
	}
	if total+markers == 0 {
		return "", nil // nothing versioned under the selection — nothing to do
	}

	j := a.jobs.add("transfer", total+markers, totalBytes)
	j.setMeta(path.Base(strings.TrimSuffix(keys[0], "/")),
		s3Label(srcBucket, ""), s3Label(dstBucket, dstPrefix), len(keys), move)
	j.setNames(keys)
	id := j.info.ID
	verb := "copying"
	if move {
		verb = "moving"
	}
	a.emitLogSrc(LogInfo, "copy", dstBucket,
		fmt.Sprintf("job %s: %s %d version(s) and %d delete marker(s) from %s to %s", id, verb, total, markers, srcBucket, dstBucket))
	go a.runVersionsCopy(j, srcC, dstC, srcBucket, dstBucket, items, dstPrefix, move)
	return id, nil
}

// vcopyDstKey maps one source key to its destination key for a
// version-preserving copy: exact objects land under their base name,
// folder prefixes re-root their subtree under the folder's own name —
// mirroring the plain copy path (copyMove in ops.go), so pasting a
// folder creates <dstPrefix>/<folder>/... and never flattens the
// contents straight into the destination. The folder's own marker
// timeline keeps the trailing-slash marker form, like putMarker in the
// plain path — without it the recreated versions would show up as a
// plain file next to the folder they belong to.
func vcopyDstKey(dstPrefix, sel string, exact bool, srcKey string) string {
	base := path.Base(strings.TrimSuffix(sel, "/"))
	if exact {
		return joinKeyNoSlash(dstPrefix, path.Base(srcKey))
	}
	if rel := strings.TrimPrefix(srcKey, sel); rel != "" {
		return joinKeyNoSlash(dstPrefix, base, rel)
	}
	return transfer.JoinKey(dstPrefix, base) // the marker itself: folder form
}

// runVersionsCopy executes a planned versioned copy job: every source
// version re-written oldest→newest, delete markers recreated last, then —
// for a move with zero failures — the source timelines destroyed.
func (a *App) runVersionsCopy(j *jobHandle, srcC, dstC *s3client.Client, srcBucket, dstBucket string, items []vcopyItem, dstPrefix string, move bool) {
	ctx := j.ctx
	var lastErr string
	failed := 0
	n := 0 // 1-based ordinal across versions and markers

	for _, it := range items {
		for _, t := range it.timelines {
			dk := vcopyDstKey(dstPrefix, it.prefix, it.exact, t.Key)
			if dk == "" || (srcBucket == dstBucket && dk == t.Key) {
				n++
				j.startFile(n, t.Key, 0)
				failed++
				lastErr = fmt.Sprintf("%s: source and destination are the same", t.Key)
				j.fileDone(0, true)
				continue
			}
			for _, v := range t.Versions {
				n++
				j.startFile(n, fmt.Sprintf("%s@%s", t.Key, v.VersionID), v.Size)
				if err := versioning.CopyOneVersion(ctx, srcC.S3, srcBucket, t.Key, v.VersionID, dstC.S3, dstBucket, dk); err != nil {
					if ctx.Err() != nil {
						a.finishJob(j, JobCanceled, "canceled")
						return
					}
					failed++
					lastErr = fmt.Sprintf("%s@%s: %v", t.Key, v.VersionID, err)
					j.fileDone(v.Size, true)
					j.emit(true)
					continue
				}
				j.fileDone(v.Size, false)
				j.emit(false)
			}
			if t.IsDeleted {
				// Recreate the marker AFTER all versions of the key: the
				// delete becomes the destination's current state, exactly
				// as at the source.
				n++
				j.startFile(n, t.Key+" (delete marker)", 0)
				if _, err := dstC.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(dstBucket), Key: aws.String(dk),
				}); err != nil {
					failed++
					lastErr = fmt.Sprintf("%s: recreate delete marker: %v", t.Key, err)
					j.fileDone(0, true)
					continue
				}
				j.fileDone(0, false)
				j.emit(true)
			}
		}
	}

	if failed == 0 && move {
		// Everything copied — destroy the source timelines. A failure
		// here leaves the job in error but the destination intact.
		j.setPhase(PhaseCleanup)
		j.emit(true)
		for _, it := range items {
			for _, t := range it.timelines {
				if _, err := versioning.DeleteAllVersions(ctx, srcC.S3, srcBucket, t.Key); err != nil {
					if ctx.Err() != nil {
						a.finishJob(j, JobCanceled, "canceled")
						return
					}
					failed++
					lastErr = fmt.Sprintf("%s: deleting source versions: %v", t.Key, err)
				}
			}
		}
	}

	if failed > 0 {
		j.mu.Lock()
		total := j.info.TotalFiles
		j.mu.Unlock()
		a.finishJob(j, JobError, fmt.Sprintf("%d of %d item(s) failed — last error: %s", failed, total, lastErr))
	} else {
		a.finishJob(j, JobDone, "")
	}
	a.emit(EventS3Changed, map[string]string{"bucket": dstBucket, "prefix": dstPrefix})
	if move && failed == 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": srcBucket})
	}
}

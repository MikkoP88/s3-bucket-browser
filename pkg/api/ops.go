package api

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// deleteForceThreshold mirrors the CLI's L1 gate (internal/cli rmForceThreshold,
// safety ladder): above this count the GUI requires typed confirmation.
const deleteForceThreshold = 50

// DeletePreview feeds the confirmation dialog (count-then-act).
type DeletePreview struct {
	Count      int   `json:"count"`
	Bytes      int64 `json:"bytes"`
	Folders    int   `json:"folders"`
	Objects    int   `json:"objects"`
	RequiresL1 bool  `json:"requiresL1"` // simple confirm
	RequiresL2 bool  `json:"requiresL2"` // typed confirmation (>50 objects)
}

// PreviewDelete expands a selection (folder keys end with "/") and reports
// what a delete would remove — without deleting anything. Folder contents
// are walked; individually selected objects are stat'ed for their size.
// Addresses the source the main view is browsing (SetViewSource).
func (a *App) PreviewDelete(bucket string, keys []string) (DeletePreview, error) {
	c, err := a.client("")
	if err != nil {
		return DeletePreview{}, err
	}
	return a.previewDeleteC(c, bucket, keys)
}

// SourcePreviewDelete is PreviewDelete pinned to one named S3 source.
func (a *App) SourcePreviewDelete(idOrName, bucket string, keys []string) (DeletePreview, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return DeletePreview{}, err
	}
	return a.previewDeleteC(c, bucket, keys)
}

func (a *App) previewDeleteC(c *s3client.Client, bucket string, keys []string) (DeletePreview, error) {
	ctx, cancel := a.quickCtx()
	defer cancel()

	var p DeletePreview
	seen := map[string]bool{}
	for _, k := range keys {
		if !strings.HasSuffix(k, "/") {
			if seen[k] {
				continue
			}
			seen[k] = true
			p.Objects++
			p.Count++
			if head, err := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
				Bucket: aws.String(bucket), Key: aws.String(k),
			}); err == nil {
				p.Bytes += aws.ToInt64(head.ContentLength)
			}
			continue
		}
		p.Folders++
		err := listing.Walk(ctx, c.S3, bucket, k, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if !seen[key] {
				seen[key] = true
				p.Count++
				p.Bytes += aws.ToInt64(o.Size)
			}
			return nil
		})
		if err != nil {
			return DeletePreview{}, err
		}
	}
	p.RequiresL1 = p.Count > 0
	p.RequiresL2 = p.Count > deleteForceThreshold
	return p, nil
}

// DeleteSelection removes the selected objects/folders. The server re-counts
// and refuses large batches unless force=true, which the frontend only sets
// after the typed-confirmation dialog (L2) — same contract as `s3b rm --force`.
// Addresses the source the main view is browsing (SetViewSource).
func (a *App) DeleteSelection(bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return a.deleteSelectionC(c, bucket, keys, force)
}

// SourceDeleteSelection is DeleteSelection pinned to one named S3 source.
func (a *App) SourceDeleteSelection(idOrName, bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return a.deleteSelectionC(c, bucket, keys, force)
}

func (a *App) deleteSelectionC(c *s3client.Client, bucket string, keys []string, force bool) (res transfer.DeleteResult, err error) {
	// A task, not a quick op: the Running-tasks window shows it and can
	// kill it — canceling during the count/expand phase aborts before
	// anything is deleted.
	task := a.tasks.add("delete", fmt.Sprintf("s3://%s — %d selected item(s)", bucket, len(keys)))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()

	task.setPhase(TaskPhaseCount)
	all, err := expandSelection(ctx, c, bucket, keys)
	if err != nil {
		return res, err
	}
	if len(all) > deleteForceThreshold && !force {
		return res, fmt.Errorf(
			"%d object(s) selected — typed confirmation (force) required to delete", len(all))
	}
	task.setTotal(len(all), fmt.Sprintf("s3://%s — deleting %d object(s)", bucket, len(all)))
	a.emitLogSrc(LogInfo, "delete", bucket, fmt.Sprintf("deleting %d object(s)", len(all)))
	res, err = transfer.DeleteKeysProg(ctx, c.S3, bucket, all, func(deleted int) {
		task.progress(deleted)
	})
	task.progress(res.Deleted)
	switch {
	case err != nil:
		a.emitLogSrc(LogError, "delete", bucket, fmt.Sprintf("deleting %d object(s) failed: %v", len(all), err))
	case len(res.Errors) > 0:
		a.emitLogSrc(LogWarn, "delete", bucket, fmt.Sprintf("deleted %d of %d object(s) — %s",
			res.Deleted, len(all), strings.Join(res.Errors, "; ")))
	default:
		a.emitLogSrc(LogInfo, "delete", bucket, fmt.Sprintf("deleted %d object(s)", res.Deleted))
	}
	if res.Deleted > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return res, err
}

// expandSelection resolves folder selections into full object key lists.
func expandSelection(ctx context.Context, c *s3client.Client, bucket string, keys []string) ([]string, error) {
	var out []string
	for _, k := range keys {
		if !strings.HasSuffix(k, "/") {
			out = append(out, k)
			continue
		}
		sub, err := transfer.CollectPrefixKeys(ctx, c.S3, bucket, k)
		if err != nil {
			return nil, err
		}
		// Delete-only marker inclusion: some stores (MinIO) omit the
		// folder marker from a listing under its own prefix; missing it
		// would leave a ghost folder row behind after the delete.
		out = append(out, transfer.IncludeFolderMarker(sub, k)...)
	}
	return out, nil
}

// RenameObject renames in place: server-side copy to the new key, then delete
// of the original (S3 has no native rename). Works for folders too.
// Addresses the source the main view is browsing (SetViewSource).
func (a *App) RenameObject(bucket, key, newName string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	return a.renameObjectC(c, bucket, key, newName)
}

// SourceRenameObject is RenameObject pinned to one named S3 source.
func (a *App) SourceRenameObject(idOrName, bucket, key, newName string) error {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return err
	}
	return a.renameObjectC(c, bucket, key, newName)
}

func (a *App) renameObjectC(c *s3client.Client, bucket, key, newName string) error {
	newName = strings.Trim(newName, "/ ")
	if newName == "" || strings.Contains(newName, "/") {
		return fmt.Errorf("invalid name")
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	a.emitLogSrc(LogInfo, "rename", bucket, fmt.Sprintf("renaming %s to %q", key, newName))

	if strings.HasSuffix(key, "/") { // folder: move everything beneath it
		trimmed := strings.TrimSuffix(key, "/")
		parent := ""
		if i := strings.LastIndex(trimmed, "/"); i >= 0 {
			parent = trimmed[:i+1]
		}
		dstPrefix := joinKeyNoSlash(parent, newName)
		res, err := a.copyMove(ctx, c, bucket, []string{key}, bucket, dstPrefix, true, nil)
		if err != nil {
			a.emitLogSrc(LogError, "rename", bucket, fmt.Sprintf("renaming folder %s failed: %v", key, err))
			return err
		}
		if len(res.Errors) > 0 {
			err := fmt.Errorf("%s", strings.Join(res.Errors, "; "))
			a.emitLogSrc(LogError, "rename", bucket, fmt.Sprintf("renaming folder %s failed: %v", key, err))
			return err
		}
		a.emit(EventS3Changed, map[string]string{"bucket": bucket, "prefix": parent})
		return nil
	}

	dstKey := joinKeyNoSlash(path.Dir(key), newName)
	if dstKey == key {
		return nil
	}
	if err := transfer.Copy(ctx, c.S3, bucket, key, bucket, dstKey); err != nil {
		a.emitLogSrc(LogError, "rename", bucket, fmt.Sprintf("renaming %s to %q failed: %v", key, newName, err))
		return err
	}
	res, err := transfer.DeleteKeys(ctx, c.S3, bucket, []string{key})
	if err != nil {
		a.emitLogSrc(LogError, "rename", bucket, fmt.Sprintf("renaming %s: deleting the original failed: %v", key, err))
		return err
	}
	if len(res.Errors) > 0 {
		err := fmt.Errorf("%s", strings.Join(res.Errors, "; "))
		a.emitLogSrc(LogError, "rename", bucket, fmt.Sprintf("renaming %s: deleting the original failed: %v", key, err))
		return err
	}
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	return nil
}

// CopyResult reports a copy/move outcome.
type CopyResult struct {
	Copied int      `json:"copied"`
	Moved  int      `json:"moved"`
	Errors []string `json:"errors,omitempty"`
}

// CopySelection server-side copies (or moves) a selection into dstBucket/
// dstPrefix. Sources are copied first and only deleted afterwards when
// moving (count-then-act). Runs as a tracked task (Running tasks window).
func (a *App) CopySelection(bucket string, keys []string, dstBucket, dstPrefix string, move bool) (res CopyResult, err error) {
	c, err := a.client("")
	if err != nil {
		return CopyResult{}, err
	}
	kind := "copy"
	if move {
		kind = "move"
	}
	// A task, not a quick op: folder selections are walked into their full
	// object list first (count phase), then copied one by one — live
	// progress and current item all the way, cancellable from the
	// Running tasks window.
	task := a.tasks.add(kind, fmt.Sprintf("%d item(s): s3://%s → s3://%s/%s",
		len(keys), bucket, dstBucket, dirPrefix(dstPrefix)))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()
	res, err = a.copyMove(ctx, c, bucket, keys, dstBucket, dirPrefix(dstPrefix), move, task)
	verb := "copied"
	if move {
		verb = "moved"
	}
	switch {
	case err != nil:
		a.emitLogSrc(LogError, "copy", dstBucket, fmt.Sprintf("%s %d item(s) from %s to %s failed: %v",
			verb, len(keys), bucket, dirPrefix(dstPrefix), err))
	case len(res.Errors) > 0:
		a.emitLogSrc(LogWarn, "copy", dstBucket, fmt.Sprintf("%s %d item(s) with %d error(s) — %s",
			verb, res.Copied+res.Moved, len(res.Errors), strings.Join(res.Errors, "; ")))
	default:
		a.emitLogSrc(LogInfo, "copy", dstBucket, fmt.Sprintf("%s %d item(s) from %s to %s",
			verb, res.Copied+res.Moved, bucket, dirPrefix(dstPrefix)))
	}
	if res.Copied > 0 || res.Moved > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": dstBucket, "prefix": dirPrefix(dstPrefix)})
	}
	return res, err
}

// copyMove implements copy/move for a selection (see CopySelection). It
// plans first (count phase: folders are walked into per-object src→dst
// pairs), then copies pair by pair with live progress, and only deletes
// the sources when every copy succeeded (move semantics, cleanup phase).
// task may be nil (the single-key rename path runs without a task row).
func (a *App) copyMove(ctx context.Context, c *s3client.Client, bucket string, keys []string, dstBucket, dstPrefix string, move bool, task *taskHandle) (CopyResult, error) {
	res := CopyResult{}
	var toDelete []string

	type copyPair struct{ src, dst string }
	// One group per selected item: a single file or one folder (marker +
	// every object beneath it).
	type copyGroup struct {
		src    string // top-level selection item
		marker string // destination folder marker to create ("" = file)
		pairs  []copyPair
	}

	if task != nil {
		task.setPhase(TaskPhaseCount)
	}
	var groups []copyGroup
	for _, src := range keys {
		if !strings.HasSuffix(src, "/") {
			dstKey := joinKeyNoSlash(dstPrefix, path.Base(src))
			if bucket == dstBucket && src == dstKey {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: source and destination are the same", src))
				continue
			}
			groups = append(groups, copyGroup{src: src, pairs: []copyPair{{src, dstKey}}})
			continue
		}
		name := path.Base(strings.TrimSuffix(src, "/"))
		newPrefix := transfer.JoinKey(dstPrefix, name)
		g := copyGroup{src: src, marker: newPrefix}
		err := listing.Walk(ctx, c.S3, bucket, src, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil // markers: destination marker created below
			}
			g.pairs = append(g.pairs, copyPair{
				src: key,
				dst: joinKeyNoSlash(newPrefix, strings.TrimPrefix(key, src)),
			})
			return nil
		})
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		groups = append(groups, g)
	}
	total := 0
	for _, g := range groups {
		total += len(g.pairs)
	}
	if task != nil {
		task.setTotal(total, "")
	}

	done := 0
	for _, g := range groups {
		if g.marker != "" {
			if err := putMarker(ctx, c.S3, dstBucket, g.marker); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", g.src, err))
				continue
			}
		}
		for _, p := range g.pairs {
			if task != nil {
				task.setCurrent(p.src)
			}
			if err := transfer.Copy(ctx, c.S3, bucket, p.src, dstBucket, p.dst); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p.src, err))
				continue
			}
			res.Copied++
			done++
			if task != nil {
				task.progress(done)
			}
			if move {
				toDelete = append(toDelete, p.src)
			}
		}
		if move && g.marker != "" {
			toDelete = append(toDelete, g.src) // the old marker too
		}
	}

	// Delete sources only when every copy succeeded (move semantics).
	if move && len(toDelete) > 0 && len(res.Errors) == 0 {
		if task != nil {
			task.setPhase(TaskPhaseCleanup)
		}
		dr, err := transfer.DeleteKeysProg(ctx, c.S3, bucket, toDelete, func(deleted int) {
			if task != nil {
				task.progress(deleted)
			}
		})
		res.Moved = dr.Deleted
		if err != nil {
			return res, err
		}
		res.Errors = append(res.Errors, dr.Errors...)
	}
	return res, nil
}

// putMarker creates a zero-byte folder marker at prefix.
func putMarker(ctx context.Context, client *s3.Client, bucket, prefix string) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(transfer.JoinKey(prefix)),
		Body:   strings.NewReader(""),
	})
	return err
}

// PresignObject returns a pre-signed GET URL valid for ttlSeconds.
func (a *App) PresignObject(bucket, key string, ttlSeconds int) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	if ttlSeconds > 7*24*3600 {
		ttlSeconds = 7 * 24 * 3600 // S3 presign maximum
	}
	presigner := s3.NewPresignClient(c.S3)
	req, err := presigner.PresignGetObject(a.ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}, s3.WithPresignExpires(time.Duration(ttlSeconds)*time.Second))
	if err != nil {
		return "", err
	}
	a.emitLogSrc(LogInfo, "share", bucket, fmt.Sprintf("presigned GET %s (valid %s)", key, ttlLabel(ttlSeconds)))
	return req.URL, nil
}

// ttlLabel renders a presign TTL as a compact human string.
func ttlLabel(secs int) string {
	switch {
	case secs%86400 == 0:
		return fmt.Sprintf("%dd", secs/86400)
	case secs%3600 == 0:
		return fmt.Sprintf("%dh", secs/3600)
	default:
		return fmt.Sprintf("%dm", secs/60)
	}
}

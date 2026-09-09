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
// PLAN.md §9): above this count the GUI requires typed confirmation.
const deleteForceThreshold = 50

// DeletePreview feeds the confirmation dialog (count-then-act, PLAN.md §9).
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
func (a *App) PreviewDelete(bucket string, keys []string) (DeletePreview, error) {
	c, err := a.client("")
	if err != nil {
		return DeletePreview{}, err
	}
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
func (a *App) DeleteSelection(bucket string, keys []string, force bool) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()

	all, err := expandSelection(ctx, c, bucket, keys)
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	if len(all) > deleteForceThreshold && !force {
		return transfer.DeleteResult{}, fmt.Errorf(
			"%d object(s) selected — typed confirmation (force) required to delete", len(all))
	}
	res, err := transfer.DeleteKeys(ctx, c.S3, bucket, all)
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
		out = append(out, sub...)
	}
	return out, nil
}

// RenameObject renames in place: server-side copy to the new key, then delete
// of the original (S3 has no native rename). Works for folders too.
func (a *App) RenameObject(bucket, key, newName string) error {
	newName = strings.Trim(newName, "/ ")
	if newName == "" || strings.Contains(newName, "/") {
		return fmt.Errorf("invalid name")
	}
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()

	if strings.HasSuffix(key, "/") { // folder: move everything beneath it
		trimmed := strings.TrimSuffix(key, "/")
		parent := ""
		if i := strings.LastIndex(trimmed, "/"); i >= 0 {
			parent = trimmed[:i+1]
		}
		dstPrefix := joinKeyNoSlash(parent, newName)
		res, err := a.copyMove(ctx, c, bucket, []string{key}, bucket, dstPrefix, true)
		if err != nil {
			return err
		}
		if len(res.Errors) > 0 {
			return fmt.Errorf("%s", strings.Join(res.Errors, "; "))
		}
		a.emit(EventS3Changed, map[string]string{"bucket": bucket, "prefix": parent})
		return nil
	}

	dstKey := joinKeyNoSlash(path.Dir(key), newName)
	if dstKey == key {
		return nil
	}
	if err := transfer.Copy(ctx, c.S3, bucket, key, bucket, dstKey); err != nil {
		return err
	}
	res, err := transfer.DeleteKeys(ctx, c.S3, bucket, []string{key})
	if err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("%s", strings.Join(res.Errors, "; "))
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
// moving (count-then-act, PLAN.md §9).
func (a *App) CopySelection(bucket string, keys []string, dstBucket, dstPrefix string, move bool) (CopyResult, error) {
	c, err := a.client("")
	if err != nil {
		return CopyResult{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	res, err := a.copyMove(ctx, c, bucket, keys, dstBucket, dirPrefix(dstPrefix), move)
	if res.Copied > 0 || res.Moved > 0 {
		a.emit(EventS3Changed, map[string]string{"bucket": dstBucket, "prefix": dirPrefix(dstPrefix)})
	}
	return res, err
}

// copyMove implements copy/move for a selection (see CopySelection).
func (a *App) copyMove(ctx context.Context, c *s3client.Client, bucket string, keys []string, dstBucket, dstPrefix string, move bool) (CopyResult, error) {
	res := CopyResult{}
	var toDelete []string

	for _, src := range keys {
		if !strings.HasSuffix(src, "/") {
			dstKey := joinKeyNoSlash(dstPrefix, path.Base(src))
			if bucket == dstBucket && src == dstKey {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: source and destination are the same", src))
				continue
			}
			if err := transfer.Copy(ctx, c.S3, bucket, src, dstBucket, dstKey); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", src, err))
				continue
			}
			res.Copied++
			if move {
				toDelete = append(toDelete, src)
			}
			continue
		}

		// Folder: create the destination marker, copy everything beneath.
		name := path.Base(strings.TrimSuffix(src, "/"))
		newPrefix := transfer.JoinKey(dstPrefix, name)
		if err := putMarker(ctx, c.S3, dstBucket, newPrefix); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", src, err))
			continue
		}
		err := listing.Walk(ctx, c.S3, bucket, src, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil // markers: destination marker created above
			}
			dstKey := joinKeyNoSlash(newPrefix, strings.TrimPrefix(key, src))
			if err := transfer.Copy(ctx, c.S3, bucket, key, dstBucket, dstKey); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			res.Copied++
			if move {
				toDelete = append(toDelete, key)
			}
			return nil
		})
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if move {
			toDelete = append(toDelete, src) // the old marker too
		}
	}

	// Delete sources only when every copy succeeded (move semantics).
	if move && len(toDelete) > 0 && len(res.Errors) == 0 {
		dr, err := transfer.DeleteKeys(ctx, c.S3, bucket, toDelete)
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
	return req.URL, nil
}

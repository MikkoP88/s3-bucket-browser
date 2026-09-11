// Package versioning implements S3 version management (PLAN.md §8.6):
// version timelines, restore-as-latest, permanent deletes, bulk purges and
// force-emptying of versioned buckets. Deleting a specific version ID is
// permanent (safety ladder L3, §9) — callers must gate it behind typed
// confirmation; nothing here gates permanence, the count-then-act contract is
// the caller's responsibility (mirrored by the API layer). The one exception
// is RemoveDeleteMarker, which verifies its target is really a delete marker
// before deleting.
package versioning

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Version is one entry of an object's version timeline (a real object
// version or a delete marker).
type Version struct {
	Key            string     `json:"key"`
	VersionID      string     `json:"versionId"`
	IsLatest       bool       `json:"isLatest"`
	IsDeleteMarker bool       `json:"isDeleteMarker"`
	Size           int64      `json:"size"`
	LastModified   *time.Time `json:"lastModified,omitempty"`
	ETag           string     `json:"etag,omitempty"`
	StorageClass   string     `json:"storageClass,omitempty"`
}

// Stats summarizes versions under a bucket or prefix (Version Dashboard).
type Stats struct {
	Versions        int   `json:"versions"`
	DeleteMarkers   int   `json:"deleteMarkers"`
	CurrentObjects  int   `json:"currentObjects"`
	Noncurrent      int   `json:"noncurrent"`
	NoncurrentBytes int64 `json:"noncurrentBytes"`
}

// Status returns the bucket versioning status: "Enabled", "Suspended" or
// "" (never configured).
func Status(ctx context.Context, client *s3.Client, bucket string) (string, error) {
	out, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	return string(out.Status), nil
}

// SetStatus enables ("Enabled") or suspends ("Suspended") versioning.
func SetStatus(ctx context.Context, client *s3.Client, bucket, status string) error {
	s := s3types.BucketVersioningStatusEnabled
	if status == "Suspended" {
		s = s3types.BucketVersioningStatusSuspended
	}
	_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: s,
		},
	})
	return err
}

// ListForObject returns the full version timeline of one key, newest first.
func ListForObject(ctx context.Context, client *s3.Client, bucket, key string) ([]Version, error) {
	var out []Version
	var keyMarker, verMarker *string
	for {
		page, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          aws.String(bucket),
			Prefix:          aws.String(key),
			KeyMarker:       keyMarker,
			VersionIdMarker: verMarker,
		})
		if err != nil {
			return nil, err
		}
		for _, v := range page.Versions {
			if aws.ToString(v.Key) != key {
				continue // Prefix match, not exact
			}
			out = append(out, Version{
				Key:          key,
				VersionID:    aws.ToString(v.VersionId),
				IsLatest:     aws.ToBool(v.IsLatest),
				Size:         aws.ToInt64(v.Size),
				LastModified: v.LastModified,
				ETag:         aws.ToString(v.ETag),
				StorageClass: string(v.StorageClass),
			})
		}
		for _, m := range page.DeleteMarkers {
			if aws.ToString(m.Key) != key {
				continue
			}
			out = append(out, Version{
				Key:            key,
				VersionID:      aws.ToString(m.VersionId),
				IsLatest:       aws.ToBool(m.IsLatest),
				IsDeleteMarker: true,
				LastModified:   m.LastModified,
			})
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		keyMarker = page.NextKeyMarker
		verMarker = page.NextVersionIdMarker
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IsLatest != b.IsLatest {
			return a.IsLatest
		}
		at, bt := time.Time{}, time.Time{}
		if a.LastModified != nil {
			at = *a.LastModified
		}
		if b.LastModified != nil {
			bt = *b.LastModified
		}
		return at.After(bt)
	})
	return out, nil
}

// WalkVersions visits every version and delete marker under a prefix.
// Returning an error from fn stops the walk. Prefix "" walks the bucket.
func WalkVersions(ctx context.Context, client *s3.Client, bucket, prefix string, fn func(Version) error) error {
	var keyMarker, verMarker *string
	for {
		page, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          aws.String(bucket),
			Prefix:          aws.String(prefix),
			KeyMarker:       keyMarker,
			VersionIdMarker: verMarker,
		})
		if err != nil {
			return err
		}
		for _, v := range page.Versions {
			ver := Version{
				Key:          aws.ToString(v.Key),
				VersionID:    aws.ToString(v.VersionId),
				IsLatest:     aws.ToBool(v.IsLatest),
				Size:         aws.ToInt64(v.Size),
				LastModified: v.LastModified,
				ETag:         aws.ToString(v.ETag),
				StorageClass: string(v.StorageClass),
			}
			if err := fn(ver); err != nil {
				return err
			}
		}
		for _, m := range page.DeleteMarkers {
			ver := Version{
				Key:            aws.ToString(m.Key),
				VersionID:      aws.ToString(m.VersionId),
				IsLatest:       aws.ToBool(m.IsLatest),
				IsDeleteMarker: true,
				LastModified:   m.LastModified,
			}
			if err := fn(ver); err != nil {
				return err
			}
		}
		if !aws.ToBool(page.IsTruncated) {
			return nil
		}
		keyMarker = page.NextKeyMarker
		verMarker = page.NextVersionIdMarker
	}
}

// CollectStats aggregates version counts under a prefix (cancelable via ctx).
func CollectStats(ctx context.Context, client *s3.Client, bucket, prefix string) (Stats, error) {
	var st Stats
	latest := map[string]bool{}
	err := WalkVersions(ctx, client, bucket, prefix, func(v Version) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if v.IsDeleteMarker {
			st.DeleteMarkers++
		} else {
			st.Versions++
			if v.IsLatest {
				latest[v.Key] = true
			} else {
				st.Noncurrent++
				st.NoncurrentBytes += v.Size
			}
		}
		return nil
	})
	st.CurrentObjects = len(latest)
	return st, err
}

// RestoreVersion makes an old object version current again via a server-side
// copy pinned to the source version ID. The old current version (if any)
// stays in the timeline — nothing is lost.
func RestoreVersion(ctx context.Context, client *s3.Client, bucket, key, versionID string) error {
	u := url.URL{Path: bucket + "/" + key}
	_, err := client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		CopySource: aws.String(u.EscapedPath() + "?versionId=" + url.QueryEscape(versionID)),
	})
	return err
}

// RemoveDeleteMarker removes one delete marker ("undo delete": the object
// becomes visible again with its previous current version).
func RemoveDeleteMarker(ctx context.Context, client *s3.Client, bucket, key, versionID string) error {
	// S3 accepts DeleteObject(versionId=X) for ANY X: an unknown id is an
	// idempotent success, and the id of a REAL version destroys that version
	// — either way the caller would report "the object is back" while it
	// isn't. Verify the id is actually one of the key's delete markers.
	vers, err := ListForObject(ctx, client, bucket, key)
	if err != nil {
		return err
	}
	for _, v := range vers {
		if v.VersionID != versionID {
			continue
		}
		if !v.IsDeleteMarker {
			return fmt.Errorf("version %s of s3://%s/%s is a real version, not a delete marker — undo would destroy it; pass the marker's id from the version timeline", versionID, bucket, key)
		}
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:    aws.String(bucket),
			Key:       aws.String(key),
			VersionId: aws.String(versionID),
		})
		return err
	}
	return fmt.Errorf("version %s not found in the timeline of s3://%s/%s", versionID, bucket, key)
}

// DeleteVersion permanently deletes one specific version (L3).
func DeleteVersion(ctx context.Context, client *s3.Client, bucket, key, versionID string) error {
	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	return err
}

// DeleteAllVersions permanently removes every version and delete marker of
// exactly one key (L3): the object vanishes entirely, history included.
func DeleteAllVersions(ctx context.Context, client *s3.Client, bucket, key string) (transfer.DeleteResult, error) {
	vers, err := ListForObject(ctx, client, bucket, key)
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	ids := make([]s3types.ObjectIdentifier, 0, len(vers))
	for _, v := range vers {
		ids = append(ids, s3types.ObjectIdentifier{
			Key:       aws.String(key),
			VersionId: aws.String(v.VersionID),
		})
	}
	return deleteIdentifiers(ctx, client, bucket, ids)
}

// PurgeMode selects what Purge removes under a prefix.
type PurgeMode string

const (
	PurgeNoncurrent PurgeMode = "noncurrent" // all versions that are not IsLatest
	PurgeMarkers    PurgeMode = "markers"    // all delete markers
	PurgeAll        PurgeMode = "all"        // every version + marker (L3: objects vanish permanently)
)

// CountPurge reports how many versions/markers a purge would remove
// (count-then-act, PLAN.md §9).
func CountPurge(ctx context.Context, client *s3.Client, bucket, prefix string, mode PurgeMode) (int, error) {
	n := 0
	err := WalkVersions(ctx, client, bucket, prefix, func(v Version) error {
		switch mode {
		case PurgeAll:
			n++
		case PurgeNoncurrent:
			if !v.IsDeleteMarker && !v.IsLatest {
				n++
			}
		case PurgeMarkers:
			if v.IsDeleteMarker {
				n++
			}
		}
		return nil
	})
	return n, err
}

// Purge removes versions/markers under a prefix in batches of 1000.
func Purge(ctx context.Context, client *s3.Client, bucket, prefix string, mode PurgeMode) (transfer.DeleteResult, error) {
	var ids []s3types.ObjectIdentifier
	err := WalkVersions(ctx, client, bucket, prefix, func(v Version) error {
		switch mode {
		case PurgeAll:
		case PurgeNoncurrent:
			if v.IsDeleteMarker || v.IsLatest {
				return nil
			}
		case PurgeMarkers:
			if !v.IsDeleteMarker {
				return nil
			}
		}
		ids = append(ids, s3types.ObjectIdentifier{
			Key:       aws.String(v.Key),
			VersionId: aws.String(v.VersionID),
		})
		return nil
	})
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	return deleteIdentifiers(ctx, client, bucket, ids)
}

// EmptyBucketVersions force-empties a versioned bucket: every version and
// delete marker is permanently removed (L2: typed bucket name; L3: permanent).
// Used by `rb --force` and the GUI's empty-bucket when versioning is on.
func EmptyBucketVersions(ctx context.Context, client *s3.Client, bucket string) (transfer.DeleteResult, error) {
	var ids []s3types.ObjectIdentifier
	err := WalkVersions(ctx, client, bucket, "", func(v Version) error {
		ids = append(ids, s3types.ObjectIdentifier{
			Key:       aws.String(v.Key),
			VersionId: aws.String(v.VersionID),
		})
		return nil
	})
	if err != nil {
		return transfer.DeleteResult{}, fmt.Errorf("listing versions: %w", err)
	}
	return deleteIdentifiers(ctx, client, bucket, ids)
}

// deleteIdentifiers batch-deletes explicit (key, version) pairs.
func deleteIdentifiers(ctx context.Context, client *s3.Client, bucket string, ids []s3types.ObjectIdentifier) (transfer.DeleteResult, error) {
	out := transfer.DeleteResult{}
	for start := 0; start < len(ids); start += 1000 {
		end := start + 1000
		if end > len(ids) {
			end = len(ids)
		}
		resp, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: ids[start:end]},
		})
		if err != nil {
			return out, err
		}
		out.Deleted += len(resp.Deleted)
		for _, e := range resp.Errors {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %s", aws.ToString(e.Key), aws.ToString(e.Message)))
		}
	}
	return out, nil
}

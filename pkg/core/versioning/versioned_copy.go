// versioned_copy.go: recreate an S3 version timeline at another bucket —
// the engine behind `cp --versions` and the GUI's version-aware copy.
//
// S3 has no "copy all versions" API: CopyObject always writes a fresh
// destination version. What CAN be preserved is the shape of the history:
// every source version re-written oldest→newest (so the source's current
// version is written last and stays current), plus a delete marker for
// keys whose latest state is a marker. Destination version IDs and
// timestamps are provider-assigned — contents, ordering and marker state
// are identical, IDs are not (and cannot be).
package versioning

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// KeyTimeline is one key's recreated-history plan: its real versions
// ordered oldest→newest, and whether the key currently sits behind a
// delete marker.
type KeyTimeline struct {
	Key       string    `json:"key"`
	Versions  []Version `json:"versions"` // oldest → newest
	IsDeleted bool      `json:"isDeleted"`
}

// VersionsTotal is the number of object versions in the plan (markers
// excluded — they are recreated, not copied).
func VersionsTotal(plan []KeyTimeline) (n int) {
	for _, t := range plan {
		n += len(t.Versions)
	}
	return n
}

// PlanVersionedCopy groups the version timeline under a prefix into
// per-key write plans. exactKey restricts the plan to one object (an
// exact-object operand — ListObjectVersions prefix-matches neighbors).
// prefix "" plans the whole bucket.
func PlanVersionedCopy(ctx context.Context, client *s3.Client, bucket, prefix string, exactKey bool) ([]KeyTimeline, error) {
	groups := map[string]*KeyTimeline{}
	var order []string
	err := WalkVersions(ctx, client, bucket, prefix, func(v Version) error {
		if exactKey && v.Key != prefix {
			return nil // prefix neighbor, not the exact object
		}
		g, ok := groups[v.Key]
		if !ok {
			g = &KeyTimeline{Key: v.Key}
			groups[v.Key] = g
			order = append(order, v.Key)
		}
		// Exactly one entry per key carries IsLatest — the current state,
		// wherever it appears in the page order.
		if v.IsLatest {
			g.IsDeleted = v.IsDeleteMarker
		}
		if !v.IsDeleteMarker {
			g.Versions = append(g.Versions, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]KeyTimeline, 0, len(order))
	for _, k := range order {
		t := groups[k]
		// The walk yields newest→oldest; writing must go oldest→newest so
		// the current version is written last. Reversing first keeps
		// same-timestamp versions in their original order under the
		// stable sort.
		for i, j := 0, len(t.Versions)-1; i < j; i, j = i+1, j-1 {
			t.Versions[i], t.Versions[j] = t.Versions[j], t.Versions[i]
		}
		sort.SliceStable(t.Versions, func(i, j int) bool {
			a, b := t.Versions[i], t.Versions[j]
			if a.LastModified == nil || b.LastModified == nil {
				return false
			}
			return a.LastModified.Before(*b.LastModified)
		})
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// CopyVersionsOptions drives CopyVersionedTimelines.
type CopyVersionsOptions struct {
	DryRun bool
	// OnItem reports each written version (or recreated marker, with an
	// empty VersionID) and the running count over the planned total.
	// Nil keeps the copy quiet.
	OnItem func(key, versionID string, isMarker bool, n, total int)
}

// CopyStats reports what a versioned copy did.
type CopyStats struct {
	VersionsCopied int `json:"versionsCopied"`
	MarkersCreated int `json:"markersCreated"`
}

// CopyVersionedTimelines recreates planned timelines in dstBucket,
// mapping every source key through dstKey. The same client pointer copies
// server-side (CopyObject pinned to each source VersionId); different
// clients stream through this machine, carrying content type and user
// metadata along. Callers verify the destination bucket has versioning
// enabled first — otherwise the timeline silently collapses.
func CopyVersionedTimelines(ctx context.Context, src *s3.Client, srcBucket string, dst *s3.Client, dstBucket string, plan []KeyTimeline, dstKey func(srcKey string) string, opts CopyVersionsOptions) (CopyStats, error) {
	var stats CopyStats
	total := VersionsTotal(plan)
	for _, t := range plan {
		dk := dstKey(t.Key)
		if dk == "" {
			continue
		}
		for _, v := range t.Versions {
			if opts.DryRun {
				stats.VersionsCopied++
				if opts.OnItem != nil {
					opts.OnItem(t.Key, v.VersionID, false, stats.VersionsCopied, total)
				}
				continue
			}
			if err := CopyOneVersion(ctx, src, srcBucket, t.Key, v.VersionID, dst, dstBucket, dk); err != nil {
				return stats, fmt.Errorf("%s@%s: %w", t.Key, v.VersionID, err)
			}
			stats.VersionsCopied++
			if opts.OnItem != nil {
				opts.OnItem(t.Key, v.VersionID, false, stats.VersionsCopied, total)
			}
		}
		if t.IsDeleted && !opts.DryRun {
			// Recreate the marker AFTER all versions of the key: the
			// delete becomes the destination's current state, exactly as
			// at the source.
			if _, err := dst.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(dstBucket), Key: aws.String(dk),
			}); err != nil {
				return stats, fmt.Errorf("%s: recreate delete marker: %w", t.Key, err)
			}
			stats.MarkersCreated++
			if opts.OnItem != nil {
				opts.OnItem(t.Key, "", true, stats.VersionsCopied, total)
			}
		}
	}
	return stats, nil
}

// CopyOneVersion moves one pinned source version to dstKey: server-side
// within one client, streamed between clients. Exported for the GUI's
// job-driven versioned copy (per-version progress/error granularity).
func CopyOneVersion(ctx context.Context, src *s3.Client, srcBucket, srcKey, versionID string, dst *s3.Client, dstBucket, dstKey string) error {
	if src == dst {
		_, err := dst.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(dstBucket),
			Key:        aws.String(dstKey),
			CopySource: aws.String(fmt.Sprintf("%s/%s?versionId=%s", srcBucket, url.PathEscape(srcKey), versionID)),
		})
		return err
	}
	resp, err := src.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(srcBucket), Key: aws.String(srcKey), VersionId: aws.String(versionID),
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	opts := transfer.UploadOptions{
		ContentType: aws.ToString(resp.ContentType),
		Metadata:    resp.Metadata,
	}
	return transfer.UploadReader(ctx, dst, resp.Body, aws.ToInt64(resp.ContentLength), dstBucket, dstKey, opts)
}

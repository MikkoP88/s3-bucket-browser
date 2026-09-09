// Package listing implements streaming bucket and object listing.
package listing

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Entry is one row in a listing: a virtual folder or an object.
type Entry struct {
	Key          string     `json:"key"`
	Name         string     `json:"name"`
	IsDir        bool       `json:"isDir"`
	Size         int64      `json:"size"`
	LastModified *time.Time `json:"lastModified,omitempty"`
	StorageClass string     `json:"storageClass,omitempty"`
	ETag         string     `json:"etag,omitempty"`
}

// Options controls a List call.
type Options struct {
	Recursive bool // no delimiter: stream every object under prefix
	MaxKeys   int32
}

// ListBuckets returns all buckets visible to the client.
func ListBuckets(ctx context.Context, client s3.ListBucketsAPIClient) ([]s3types.Bucket, error) {
	var out []s3types.Bucket
	paginator := s3.NewListBucketsPaginator(client, &s3.ListBucketsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Buckets...)
	}
	return out, nil
}

// WalkDir streams one "directory view" of a prefix through fn: sub-folders
// (common prefixes, folder markers) and objects as the paginator yields
// them. Returning an error from fn stops the walk. Unlike List it never
// accumulates entries, so million-object folders cost O(page) memory on
// the Go side (PLAN.md §13).
func WalkDir(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string, opts Options, fn func(Entry) error) error {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix), Delimiter: aws.String("/"),
	}
	if opts.MaxKeys > 0 {
		input.MaxKeys = aws.Int32(opts.MaxKeys)
	}
	paginator := s3.NewListObjectsV2Paginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, cp := range page.CommonPrefixes {
			key := aws.ToString(cp.Prefix)
			if key == prefix {
				continue
			}
			if err := fn(DirEntry(key, prefix)); err != nil {
				return err
			}
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			// Zero-byte "dir/" markers appear as objects; show as folders.
			if strings.HasSuffix(key, "/") && key != prefix {
				if err := fn(DirEntry(key, prefix)); err != nil {
					return err
				}
				continue
			}
			if key == prefix {
				continue // the prefix marker itself
			}
			if err := fn(FromObject(obj, prefix)); err != nil {
				return err
			}
		}
	}
	return nil
}

// List returns one "directory view" of a prefix: sub-folders (common
// prefixes) plus the objects directly inside it. With Recursive set, it
// streams every object under the prefix instead (folders are synthesized
// only where the walk encounters them — callers that need a flat stream
// should use Walk).
func List(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string, opts Options) ([]Entry, error) {
	if opts.Recursive {
		var entries []Entry
		err := Walk(ctx, client, bucket, prefix, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if key == prefix || strings.HasSuffix(key, "/") {
				return nil
			}
			entries = append(entries, FromObject(o, prefix))
			return nil
		})
		return entries, err
	}
	var entries []Entry
	seenDirs := map[string]bool{}
	err := WalkDir(ctx, client, bucket, prefix, opts, func(e Entry) error {
		if e.IsDir {
			if seenDirs[e.Key] {
				return nil
			}
			seenDirs[e.Key] = true
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	SortEntries(entries)
	return entries, nil
}

// Walk streams every object under prefix via fn. Returning an error from fn
// stops the walk. The callback receives raw object keys (not names).
func Walk(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string, fn func(s3types.Object) error) error {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			if err := fn(obj); err != nil {
				return err
			}
		}
	}
	return nil
}

// Usage aggregates object counts and sizes under a prefix.
type Usage struct {
	Prefix      string `json:"prefix"`
	ObjectCount int64  `json:"objectCount"`
	TotalBytes  int64  `json:"totalBytes"`
}

// Du computes usage by walking the prefix.
func Du(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string) (Usage, error) {
	u := Usage{Prefix: prefix}
	err := Walk(ctx, client, bucket, prefix, func(o s3types.Object) error {
		u.ObjectCount++
		u.TotalBytes += aws.ToInt64(o.Size)
		return nil
	})
	return u, err
}

// DirEntry builds a folder Entry for a common prefix (or folder marker).
func DirEntry(key, parentPrefix string) Entry {
	name := strings.TrimPrefix(key, parentPrefix)
	name = strings.TrimSuffix(name, "/")
	return Entry{Key: key, Name: name, IsDir: true}
}

// FromObject builds an object Entry, trimming the parent prefix from Name.
func FromObject(o s3types.Object, parentPrefix string) Entry {
	key := aws.ToString(o.Key)
	name := strings.TrimPrefix(key, parentPrefix)
	e := Entry{
		Key:          key,
		Name:         name,
		Size:         aws.ToInt64(o.Size),
		StorageClass: string(o.StorageClass),
		ETag:         strings.Trim(aws.ToString(o.ETag), `"`),
	}
	if o.LastModified != nil {
		t := *o.LastModified
		e.LastModified = &t
	}
	return e
}

// SortEntries orders folders first, then by name (case-insensitive).
func SortEntries(entries []Entry) {
	less := func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a == b {
			return entries[i].Name < entries[j].Name
		}
		return a < b
	}
	// insertion sort keeps the sort stable without importing sort for
	// callers; volumes per page are small (<= a few thousand).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

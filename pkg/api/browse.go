package api

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/MikkoP88/s3-bucket-browser/pkg/doctor"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BucketView is one row of the bucket grid.
type BucketView struct {
	Name      string     `json:"name"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// ListBuckets returns every bucket visible to the active profile.
func (a *App) ListBuckets() ([]BucketView, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	buckets, err := listing.ListBuckets(ctx, c.S3)
	if err != nil {
		return nil, err
	}
	out := make([]BucketView, 0, len(buckets))
	for _, b := range buckets {
		v := BucketView{Name: aws.ToString(b.Name)}
		if b.CreationDate != nil {
			t := *b.CreationDate
			v.CreatedAt = &t
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// ListObjects returns one Explorer-style folder view of bucket/prefix:
// sub-folders first, then objects, sorted by name.
func (a *App) ListObjects(bucket, prefix string) ([]listing.Entry, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return listing.List(ctx, c.S3, bucket, dirPrefix(prefix), listing.Options{})
}

// dirPrefix normalizes a prefix into folder form ("photos" -> "photos/").
func dirPrefix(p string) string {
	p = strings.TrimPrefix(p, "/")
	if p == "" || strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// joinKeyNoSlash joins key parts without a trailing slash.
func joinKeyNoSlash(parts ...string) string {
	return strings.TrimSuffix(transfer.JoinKey(parts...), "/")
}

// FolderUsage aggregates count + size under a prefix (folder Properties).
func (a *App) FolderUsage(bucket, prefix string) (listing.Usage, error) {
	c, err := a.client("")
	if err != nil {
		return listing.Usage{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	return listing.Du(ctx, c.S3, bucket, dirPrefix(prefix))
}

// ObjectStat is the Properties dialog payload.
type ObjectStat struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	IsDir        bool              `json:"isDir"`
	Size         int64             `json:"size"`
	LastModified *time.Time        `json:"lastModified,omitempty"`
	ETag         string            `json:"etag,omitempty"`
	StorageClass string            `json:"storageClass,omitempty"`
	ContentType  string            `json:"contentType,omitempty"`
	SSE          string            `json:"sse,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Usage        *listing.Usage    `json:"usage,omitempty"`  // folders: aggregate
	Region       string            `json:"region,omitempty"` // bucket stat
}

// StatBucket returns location/metadata for a bucket.
func (a *App) StatBucket(bucket string) (ObjectStat, error) {
	c, err := a.client("")
	if err != nil {
		return ObjectStat{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	loc, err := bucketops.Head(ctx, c.S3, bucket)
	if err != nil {
		return ObjectStat{}, err
	}
	region := string(loc.LocationConstraint)
	if region == "" {
		region = "us-east-1"
	}
	return ObjectStat{Bucket: bucket, Region: region}, nil
}

// StatObject returns metadata for one object, or an aggregate for a folder.
func (a *App) StatObject(bucket, key string) (ObjectStat, error) {
	c, err := a.client("")
	if err != nil {
		return ObjectStat{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()

	if strings.HasSuffix(key, "/") {
		u, err := listing.Du(ctx, c.S3, bucket, key)
		if err != nil {
			return ObjectStat{}, err
		}
		return ObjectStat{
			Bucket: bucket, Key: key, Name: path.Base(strings.TrimSuffix(key, "/")),
			IsDir: true, Usage: &u,
		}, nil
	}

	head, err := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return ObjectStat{}, err
	}
	st := ObjectStat{
		Bucket:       bucket,
		Key:          key,
		Name:         path.Base(key),
		Size:         aws.ToInt64(head.ContentLength),
		StorageClass: string(head.StorageClass),
		ContentType:  aws.ToString(head.ContentType),
		Metadata:     map[string]string{},
	}
	if head.LastModified != nil {
		t := *head.LastModified
		st.LastModified = &t
	}
	if head.ETag != nil {
		st.ETag = strings.Trim(aws.ToString(head.ETag), `"`)
	}
	if head.ServerSideEncryption != "" {
		st.SSE = string(head.ServerSideEncryption)
	}
	for k, v := range head.Metadata {
		st.Metadata[k] = v
	}
	return st, nil
}

// CreateBucket makes a new bucket (region defaults to the profile region).
func (a *App) CreateBucket(name, region string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	if err := bucketops.Create(ctx, c.S3, name, firstNonEmpty(region, c.Region), false); err != nil {
		return err
	}
	a.emit(EventS3Changed, map[string]string{"bucket": name})
	return nil
}

// BucketDeletePreview feeds the L2 confirmation dialog: how many objects
// (and versions, when the bucket is versioned) would be removed.
type BucketDeletePreview struct {
	ObjectCount   int  `json:"objectCount"`
	VersionCount  int  `json:"versionCount"` // versions incl. delete markers
	DeleteMarkers int  `json:"deleteMarkers"`
	Versioned     bool `json:"versioned"`
	RequiresL2    bool `json:"requiresL2"` // non-empty: typed confirmation
}

// PreviewBucketDelete counts objects in a bucket before removal. On
// versioned buckets it also counts versions + delete markers, because
// removing the bucket requires deleting every version permanently.
func (a *App) PreviewBucketDelete(bucket string) (BucketDeletePreview, error) {
	c, err := a.client("")
	if err != nil {
		return BucketDeletePreview{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	n, err := bucketops.CountObjects(ctx, c.S3, bucket)
	if err != nil {
		return BucketDeletePreview{}, err
	}
	p := BucketDeletePreview{ObjectCount: n}
	if status, err := versioning.Status(ctx, c.S3, bucket); err == nil && status == "Enabled" {
		p.Versioned = true
		if st, err := versioning.CollectStats(ctx, c.S3, bucket, ""); err == nil {
			p.VersionCount = st.Versions
			p.DeleteMarkers = st.DeleteMarkers
		}
	}
	p.RequiresL2 = p.ObjectCount > 0 || p.VersionCount > 0 || p.DeleteMarkers > 0
	return p, nil
}

// DeleteBucket removes a bucket; non-empty buckets require force=true after
// the GUI showed the typed-confirmation dialog (L2, PLAN.md §9). Versioned
// buckets are emptied of ALL versions first (L3 — the preview dialog shows
// the version/delete-marker counts).
func (a *App) DeleteBucket(bucket string, force bool) (transfer.DeleteResult, error) {
	c, err := a.client("")
	if err != nil {
		return transfer.DeleteResult{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	status, _ := versioning.Status(ctx, c.S3, bucket)
	if status == "Enabled" {
		if !force {
			return transfer.DeleteResult{}, fmt.Errorf("refusing to empty versioned bucket without force confirmation")
		}
		res, err := versioning.EmptyBucketVersions(ctx, c.S3, bucket)
		if err != nil {
			return res, err
		}
		_, derr := c.S3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		if derr == nil {
			a.emit(EventS3Changed, map[string]string{"bucket": bucket})
		}
		return res, derr
	}
	res, err := bucketops.DeleteBucket(ctx, c.S3, bucket, force)
	if err == nil {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return res, err
}

// CreateFolder creates a zero-byte "name/" folder marker.
func (a *App) CreateFolder(bucket, prefix, name string) error {
	name = strings.Trim(name, "/ ")
	if name == "" || strings.Contains(name, "/") {
		return errors.New("invalid folder name")
	}
	c, err := a.client("")
	if err != nil {
		return err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	key := transfer.JoinKey(prefix, name) // trailing-slash marker form
	_, err = c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(nil),
	})
	if err == nil {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket, "prefix": dirPrefix(prefix)})
	}
	return err
}

// RunDoctor runs the deep diagnosis for a bucket and returns the report.
func (a *App) RunDoctor(bucket string) (*doctor.Report, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	return doctor.Run(a.ctx, c, bucket, c.Profile.Insecure), nil
}

// ListVersions reports whether a bucket has versioning enabled (used by the
// object grid's version badge; full version browsing is M4).
func (a *App) BucketVersioning(bucket string) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	out, err := c.S3.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	return string(out.Status), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

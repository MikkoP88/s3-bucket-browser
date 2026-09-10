package api

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
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
	return a.bucketsOf(c)
}

// ListSourceBuckets returns the buckets visible to one named S3 source —
// the per-source root view for non-default S3 sources (their object
// operations still route through the default profile until multi-source
// transfers land).
func (a *App) ListSourceBuckets(idOrName string) ([]BucketView, error) {
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return nil, err
	}
	if src.Type != profile.TypeS3 || src.S3 == nil {
		return nil, fmt.Errorf("source %q is not an S3 source", src.Name)
	}
	c, err := a.client(src.Name)
	if err != nil {
		return nil, err
	}
	return a.bucketsOf(c)
}

func (a *App) bucketsOf(c *s3client.Client) ([]BucketView, error) {
	ctx, cancel := a.quickCtx()
	defer cancel()
	buckets, err := listing.ListBuckets(ctx, c.S3)
	if err != nil {
		a.emitLog(LogError, "list", fmt.Sprintf("listing buckets failed: %v", err))
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
	entries, err := listing.List(ctx, c.S3, bucket, dirPrefix(prefix), listing.Options{})
	if err != nil {
		// Routine listings are user-visible in the UI itself (and would flood
		// the log drawer once auto-refresh ticks); log failures only.
		a.emitLog(LogError, "list", fmt.Sprintf("listing %s/%s failed: %v", bucket, dirPrefix(prefix), err))
	}
	return entries, err
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
		a.emitLog(LogError, "admin", fmt.Sprintf("creating bucket %s failed: %v", name, err))
		return err
	}
	a.emitLog(LogInfo, "admin", "bucket "+name+" created")
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
			a.emitLog(LogError, "delete", fmt.Sprintf("emptying versioned bucket %s failed: %v", bucket, err))
			return res, err
		}
		_, derr := c.S3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		if derr == nil {
			a.emitLog(LogInfo, "delete", "versioned bucket "+bucket+" deleted")
			a.emit(EventS3Changed, map[string]string{"bucket": bucket})
		} else {
			a.emitLog(LogError, "delete", fmt.Sprintf("deleting versioned bucket %s failed: %v", bucket, derr))
		}
		return res, derr
	}
	res, err := bucketops.DeleteBucket(ctx, c.S3, bucket, force)
	if err != nil {
		a.emitLog(LogError, "delete", fmt.Sprintf("deleting bucket %s failed: %v", bucket, err))
	} else {
		a.emitLog(LogInfo, "delete", "bucket "+bucket+" deleted")
	}
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
		a.emitLog(LogInfo, "mkdir", fmt.Sprintf("folder %s/%s created", bucket, key))
		a.emit(EventS3Changed, map[string]string{"bucket": bucket, "prefix": dirPrefix(prefix)})
	} else {
		a.emitLog(LogError, "mkdir", fmt.Sprintf("creating folder %s/%s failed: %v", bucket, key, err))
	}
	return err
}

// RunDoctor runs the deep diagnosis for a bucket and returns the report.
func (a *App) RunDoctor(bucket string) (*doctor.Report, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	a.emitLog(LogInfo, "doctor", "running all checks on "+bucket)
	report := doctor.Run(a.ctx, c, bucket, c.Profile.Insecure)
	failed := 0
	for _, r := range report.Checks {
		if r.Status == doctor.StatusFail {
			failed++
		}
	}
	if failed > 0 {
		a.emitLog(LogWarn, "doctor", fmt.Sprintf("doctor finished on %s: %d of %d check(s) failed", bucket, failed, len(report.Checks)))
	} else {
		a.emitLog(LogInfo, "doctor", fmt.Sprintf("doctor finished on %s: all %d check(s) passed", bucket, len(report.Checks)))
	}
	return report, nil
}

// DoctorChecks returns the available doctor check names in execution order,
// so the GUI can render the per-check list before running anything.
func (a *App) DoctorChecks() []string {
	return doctor.CheckNames()
}

// RunDoctorCheck runs a single doctor check by name for a bucket and returns
// the result (same shape as each entry of a full Report).
func (a *App) RunDoctorCheck(bucket, name string) (*doctor.CheckResult, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	res, err := doctor.RunCheck(a.ctx, c, bucket, name, c.Profile.Insecure)
	if err != nil {
		a.emitLog(LogError, "doctor", fmt.Sprintf("check %s on %s failed to run: %v", name, bucket, err))
		return nil, err
	}
	switch res.Status {
	case doctor.StatusPass:
		a.emitLog(LogInfo, "doctor", fmt.Sprintf("check %s on %s passed", name, bucket))
	case doctor.StatusWarn, doctor.StatusSkip:
		a.emitLog(LogWarn, "doctor", fmt.Sprintf("check %s on %s: %s", name, bucket, res.Detail))
	default:
		a.emitLog(LogError, "doctor", fmt.Sprintf("check %s on %s failed: %s", name, bucket, res.Detail))
	}
	return &res, nil
}

// BucketVersioning reports whether a bucket has versioning enabled (used by the
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

// BucketGuard is the cheap per-view guard state of a bucket: versioning
// status and whether object lock is configured (two quick calls — the full
// admin panel costs ten more).
type BucketGuard struct {
	Versioning  string `json:"versioning"` // "" | Suspended | Enabled
	LockEnabled bool   `json:"lockEnabled"`
	LockMode    string `json:"lockMode,omitempty"` // GOVERNANCE | COMPLIANCE
	LockDays    int32  `json:"lockDays,omitempty"` // default retention days
}

// GetBucketGuard reads the bucket's versioning + object-lock state for the
// navbar chips; a failing section degrades to its zero value (that chip
// just doesn't show).
func (a *App) GetBucketGuard(bucket string) (BucketGuard, error) {
	c, err := a.client("")
	if err != nil {
		return BucketGuard{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	var g BucketGuard
	g.Versioning, _ = versioning.Status(ctx, c.S3, bucket)
	if lock, err := adminops.GetLockConfig(ctx, c.S3, bucket); err == nil {
		g.LockEnabled, g.LockMode, g.LockDays = lock.Enabled, lock.Mode, lock.Days
	}
	return g, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

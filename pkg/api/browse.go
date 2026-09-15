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

// s3ClientFor resolves the S3 client of a named source ("" = the source
// the main view is browsing). Accepts the source name or ID, so frontend
// payloads can carry either. Non-S3 sources are rejected.
func (a *App) s3ClientFor(idOrName string) (*s3client.Client, error) {
	if idOrName == "" {
		return a.client("")
	}
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return nil, err
	}
	if src.Type != profile.TypeS3 || src.S3 == nil {
		return nil, fmt.Errorf("source %q is not an S3 source", src.Name)
	}
	return a.client(src.Name)
}

// ListSourceBuckets returns the buckets visible to one named S3 source —
// the per-source root view.
func (a *App) ListSourceBuckets(idOrName string) ([]BucketView, error) {
	c, err := a.s3ClientFor(idOrName)
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
// Addresses the source the main view is browsing (SetViewSource).
func (a *App) StatObject(bucket, key string) (ObjectStat, error) {
	c, err := a.client("")
	if err != nil {
		return ObjectStat{}, err
	}
	return a.statObjectC(c, bucket, key)
}

// SourceStatObject is StatObject pinned to one named S3 source (side pane,
// tree nodes of other sources) — the main view's context is not touched.
func (a *App) SourceStatObject(idOrName, bucket, key string) (ObjectStat, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return ObjectStat{}, err
	}
	return a.statObjectC(c, bucket, key)
}

func (a *App) statObjectC(c *s3client.Client, bucket, key string) (ObjectStat, error) {
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
		a.emitLogSrc(LogError, "admin", name, fmt.Sprintf("creating bucket failed: %v", err))
		return err
	}
	a.emitLogSrc(LogInfo, "admin", name, "bucket created")
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
// the GUI showed the typed-confirmation dialog (L2). Versioned
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
			a.emitLogSrc(LogError, "delete", bucket, fmt.Sprintf("emptying versioned bucket failed: %v", err))
			return res, err
		}
		_, derr := c.S3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		if derr == nil {
			a.emitLogSrc(LogInfo, "delete", bucket, "versioned bucket deleted")
			a.emit(EventS3Changed, map[string]string{"bucket": bucket})
		} else {
			a.emitLogSrc(LogError, "delete", bucket, fmt.Sprintf("deleting versioned bucket failed: %v", derr))
		}
		return res, derr
	}
	res, err := bucketops.DeleteBucket(ctx, c.S3, bucket, force)
	if err != nil {
		a.emitLogSrc(LogError, "delete", bucket, fmt.Sprintf("deleting bucket failed: %v", err))
	} else {
		a.emitLogSrc(LogInfo, "delete", bucket, "bucket deleted")
	}
	if err == nil {
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return res, err
}

// CreateFolder makes a folder marker object under prefix. Addresses the
// source the main view is browsing (SetViewSource).
func (a *App) CreateFolder(bucket, prefix, name string) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	return a.createFolderC(c, bucket, prefix, name)
}

// SourceCreateFolder is CreateFolder pinned to one named S3 source.
func (a *App) SourceCreateFolder(idOrName, bucket, prefix, name string) error {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return err
	}
	return a.createFolderC(c, bucket, prefix, name)
}

func (a *App) createFolderC(c *s3client.Client, bucket, prefix, name string) error {
	name = strings.Trim(name, "/ ")
	if name == "" || strings.Contains(name, "/") {
		return errors.New("invalid folder name")
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	key := transfer.JoinKey(prefix, name) // trailing-slash marker form
	_, err := c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(nil),
	})
	if err == nil {
		a.emitLogSrc(LogInfo, "mkdir", bucket, fmt.Sprintf("folder %s created", key))
		a.emit(EventS3Changed, map[string]string{"bucket": bucket, "prefix": dirPrefix(prefix)})
	} else {
		a.emitLogSrc(LogError, "mkdir", bucket, fmt.Sprintf("creating folder %s failed: %v", key, err))
	}
	return err
}

// RunDoctor runs the deep diagnosis for a bucket and returns the report.
func (a *App) RunDoctor(bucket string) (*doctor.Report, error) {
	c, err := a.client("")
	if err != nil {
		return nil, err
	}
	a.emitLogSrc(LogInfo, "doctor", bucket, "running all checks")
	report := doctor.Run(a.ctx, c, bucket, c.Profile.Insecure)
	failed := 0
	for _, r := range report.Checks {
		if r.Status == doctor.StatusFail {
			failed++
		}
	}
	if failed > 0 {
		a.emitLogSrc(LogWarn, "doctor", bucket, fmt.Sprintf("doctor finished: %d of %d check(s) failed", failed, len(report.Checks)))
	} else {
		a.emitLogSrc(LogInfo, "doctor", bucket, fmt.Sprintf("doctor finished: all %d check(s) passed", len(report.Checks)))
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
		a.emitLogSrc(LogError, "doctor", bucket, fmt.Sprintf("check %s failed to run: %v", name, err))
		return nil, err
	}
	switch res.Status {
	case doctor.StatusPass:
		a.emitLogSrc(LogInfo, "doctor", bucket, fmt.Sprintf("check %s passed", name))
	case doctor.StatusWarn, doctor.StatusSkip:
		a.emitLogSrc(LogWarn, "doctor", bucket, fmt.Sprintf("check %s: %s", name, res.Detail))
	default:
		a.emitLogSrc(LogError, "doctor", bucket, fmt.Sprintf("check %s failed: %s", name, res.Detail))
	}
	return &res, nil
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

// GetBucketGuard reports versioning/object-lock state for the tree icons.
// Addresses the source the main view is browsing (SetViewSource).
func (a *App) GetBucketGuard(bucket string) (BucketGuard, error) {
	c, err := a.client("")
	if err != nil {
		return BucketGuard{}, err
	}
	return a.bucketGuardC(c, bucket)
}

// SourceGetBucketGuard is GetBucketGuard pinned to one named S3 source —
// the tree shows buckets of every source, each with its own guard state.
func (a *App) SourceGetBucketGuard(idOrName, bucket string) (BucketGuard, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return BucketGuard{}, err
	}
	return a.bucketGuardC(c, bucket)
}

func (a *App) bucketGuardC(c *s3client.Client, bucket string) (BucketGuard, error) {
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

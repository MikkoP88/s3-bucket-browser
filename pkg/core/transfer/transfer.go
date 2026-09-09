// Package transfer implements uploads, downloads, server-side copies and
// batch deletes with progress reporting and retries.
package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ProgressFn receives transferred and total bytes (total may be 0/unknown).
type ProgressFn func(sent, total int64)

// progressReader wraps a reader, calling the callback after each Read.
type progressReader struct {
	r        io.Reader
	fn       ProgressFn
	sent     int64
	total    int64
	reportOn bool // report on every read
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	if p.fn != nil && (p.reportOn || err == io.EOF) {
		p.fn(p.sent, p.total)
	}
	return n, err
}

// UploadOptions controls an upload.
type UploadOptions struct {
	StorageClass string
	SSE          string // "AES256" or "KMS" (M1: SSE-S3 only)
	NoClobber    bool   // skip if the object already exists
	PartSize     int64
	Concurrency  int
	Progress     ProgressFn
}

// UploadFile uploads one local file to bucket/key.
func UploadFile(ctx context.Context, client *s3.Client, localPath, bucket, key string, opts UploadOptions) error {
	if opts.NoClobber {
		if _, err := client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
		}); err == nil {
			return nil // already exists, skip
		}
	}

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	var body io.Reader = f
	if opts.Progress != nil {
		body = &progressReader{r: f, fn: opts.Progress, total: st.Size(), reportOn: true}
	}

	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		if opts.PartSize > 0 {
			u.PartSize = opts.PartSize
		}
		if opts.Concurrency > 0 {
			u.Concurrency = opts.Concurrency
		}
	})
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if opts.StorageClass != "" {
		input.StorageClass = s3types.StorageClass(opts.StorageClass)
	}
	if opts.SSE == "AES256" {
		input.ServerSideEncryption = s3types.ServerSideEncryptionAes256
	}

	_, err = uploader.Upload(ctx, input)
	return err
}

// DownloadOptions controls a download.
type DownloadOptions struct {
	PartSize    int64
	Concurrency int
	Progress    ProgressFn
}

// DownloadFile downloads bucket/key to localPath (parent dirs created).
func DownloadFile(ctx context.Context, client *s3.Client, bucket, key, localPath string, opts DownloadOptions) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return err
	}
	total := aws.ToInt64(head.ContentLength)

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var w io.WriterAt = f
	downloader := manager.NewDownloader(client, func(d *manager.Downloader) {
		if opts.PartSize > 0 {
			d.PartSize = opts.PartSize
		}
		if opts.Concurrency > 0 {
			d.Concurrency = opts.Concurrency
		}
	})
	if opts.Progress != nil {
		w = &progressWriterAt{w: f, fn: opts.Progress, total: total, lastReport: 0}
	}

	_, err = downloader.Download(ctx, w, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	return err
}

// progressWriterAt reports byte progress for ranged downloads.
type progressWriterAt struct {
	w          io.WriterAt
	fn         ProgressFn
	total      int64
	sent       int64
	lastReport int64
}

func (p *progressWriterAt) WriteAt(b []byte, off int64) (int, error) {
	n, err := p.w.WriteAt(b, off)
	p.sent += int64(n)
	if p.sent-p.lastReport >= 1<<20 { // report at most every MiB
		p.lastReport = p.sent
		p.fn(p.sent, p.total)
	}
	return n, err
}

// Copy performs a server-side copy (single request; objects > 5 GB need
// multipart copy — added in M2 per PLAN.md §8.4).
func Copy(ctx context.Context, client *s3.Client, srcBucket, srcKey, dstBucket, dstKey string) error {
	_, err := client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(srcBucket + "/" + srcKey),
	})
	return err
}

// DeleteResult reports the outcome of a batch delete.
type DeleteResult struct {
	Deleted int      `json:"deleted"`
	Errors  []string `json:"errors,omitempty"`
}

// DeleteKeys deletes keys in batches of 1000 (the S3 maximum).
func DeleteKeys(ctx context.Context, client *s3.Client, bucket string, keys []string) (DeleteResult, error) {
	out := DeleteResult{}
	for start := 0; start < len(keys); start += 1000 {
		end := start + 1000
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		objects := make([]s3types.ObjectIdentifier, len(batch))
		for i, k := range batch {
			objects[i] = s3types.ObjectIdentifier{Key: aws.String(k)}
		}
		resp, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			// Quiet=false: the Deleted list is how we report accurate counts.
			Delete: &s3types.Delete{Objects: objects},
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

// CollectPrefixKeys gathers every object key under a prefix (used by rm -r,
// sync --delete and rb --force).
func CollectPrefixKeys(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string) ([]string, error) {
	var keys []string
	err := listing.Walk(ctx, client, bucket, prefix, func(o s3types.Object) error {
		keys = append(keys, aws.ToString(o.Key))
		return nil
	})
	return keys, err
}

// JoinKey joins prefix parts into a clean object key.
func JoinKey(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.ReplaceAll(p, "\\", "/")
		for _, seg := range strings.Split(p, "/") {
			if seg == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('/')
			}
			b.WriteString(seg)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteByte('/') // prefix form
	return b.String()
}

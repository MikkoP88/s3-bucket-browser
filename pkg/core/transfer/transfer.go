// Package transfer implements uploads, downloads, server-side copies and
// batch deletes with progress reporting and retries.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// rateLimiter throttles throughput to ~bps bytes/second shared across all
// concurrent parts of a transfer (multipart included). It grants a burst of
// up to one second worth of bytes, then makes readers sleep proportionally.
type rateLimiter struct {
	mu        sync.Mutex
	bps       int64
	allowance float64
	last      time.Time
}

func newRateLimiter(bps int64) *rateLimiter {
	if bps <= 0 {
		return nil
	}
	// Start with a full one-second burst budget so the first small write
	// flies immediately; steady state settles to bps.
	return &rateLimiter{bps: bps, allowance: float64(bps), last: time.Now()}
}

// wait accounts for n transferred bytes, sleeping if the budget is spent.
func (r *rateLimiter) wait(n int) {
	if r == nil || n <= 0 {
		return
	}
	r.mu.Lock()
	now := time.Now()
	if !r.last.IsZero() {
		r.allowance += now.Sub(r.last).Seconds() * float64(r.bps)
	}
	if max := float64(r.bps); r.allowance > max {
		r.allowance = max // burst cap: one second of budget
	}
	r.last = now
	var sleep time.Duration
	if need := float64(n); r.allowance < need {
		sleep = time.Duration((need - r.allowance) / float64(r.bps) * float64(time.Second))
		r.allowance = 0
	} else {
		r.allowance -= float64(n)
	}
	r.mu.Unlock()
	if sleep > 0 {
		time.Sleep(sleep)
	}
}

// ProgressFn receives transferred and total bytes (total may be 0/unknown).
type ProgressFn func(sent, total int64)

// progressReader wraps a reader, calling the callback after each Read.
type progressReader struct {
	r        io.Reader
	fn       ProgressFn
	limiter  *rateLimiter
	sent     int64
	total    int64
	reportOn bool // report on every read
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	p.limiter.wait(n)
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
	MaxBPS       int64 // 0 = unlimited (transfer throttle)
	Progress     ProgressFn
	// ContentType and Metadata are carried through by streamed versioned
	// copies (recreated versions must look like the source object). Empty
	// values keep the SDK defaults.
	ContentType string
	Metadata    map[string]string
}

// newUploader / newDownloader configure the SDK transfer manager from
// UploadOptions / DownloadOptions. The manager package is deprecated in
// favor of feature/s3/transfermanager, which is still pre-GA and may
// change — migrate once it ships stable.
//
//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader comment
func newUploader(client *s3.Client, opts UploadOptions) *manager.Uploader {
	//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader
	return manager.NewUploader(client, func(u *manager.Uploader) {
		if opts.PartSize > 0 {
			u.PartSize = opts.PartSize
		}
		if opts.Concurrency > 0 {
			u.Concurrency = opts.Concurrency
		}
	})
}

//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader comment
func newDownloader(client *s3.Client, opts DownloadOptions) *manager.Downloader {
	//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader
	return manager.NewDownloader(client, func(d *manager.Downloader) {
		if opts.PartSize > 0 {
			d.PartSize = opts.PartSize
		}
		if opts.Concurrency > 0 {
			d.Concurrency = opts.Concurrency
		}
	})
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

	// The integrity wrap rides under the progress reader: a file rewritten
	// shorter while the upload streams it ends in a clean EOF io.Copy
	// would accept, and the upload would commit a short object believing
	// it whole. VerifiedStream makes that clean short end fail the upload
	// instead; a file that grew past its open-time stat is capped at the
	// size the transfer planned.
	var body io.Reader = VerifiedStream(f, st.Size())
	if opts.Progress != nil || opts.MaxBPS > 0 {
		body = &progressReader{
			r: body, fn: opts.Progress, limiter: newRateLimiter(opts.MaxBPS),
			total: st.Size(), reportOn: true,
		}
	}

	uploader := newUploader(client, opts)
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

	//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader
	_, err = uploader.Upload(ctx, input)
	return err
}

// UploadFileIfMatch uploads one local file with a conditional guard: the
// PUT carries If-Match, so it lands only while the object still is the
// version the ETag named — a remote change in flight fails the push with
// the server's PreconditionFailed instead of silently overwriting it (the
// lost-update cure). The single direct PUT, not the multipart manager, is
// deliberate: the manager swaps PutObject for CreateMultipartUpload past
// its part size, and that wire does not carry the condition — riding it
// would let the guard silently vanish exactly on the biggest files. The
// body is the file pinned to its open-time stat as a seekable section:
// seekability is what lets the SDK compute its header checksum over plain
// HTTP, and the section's fixed length carries the whole-file contract —
// a staged file rewritten shorter fails the request at the transport's
// own length accounting (never lands short), one grown past the stat
// delivers exactly the planned prefix. An empty ifMatch writes
// unconditionally — today's shape. Returns the response ETag so the
// caller can rebase its guard without a second round-trip.
func UploadFileIfMatch(ctx context.Context, client *s3.Client, localPath, bucket, key, ifMatch string) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   io.NewSectionReader(f, 0, st.Size()),
	}
	if ifMatch != "" {
		input.IfMatch = aws.String(ifMatch)
	}
	out, err := client.PutObject(ctx, input)
	if err != nil {
		return "", err
	}
	return aws.ToString(out.ETag), nil
}

// UploadReader uploads size bytes from r to bucket/key — the streaming
// counterpart of UploadFile, used by cross-source transfers where the
// body is an open remote/S3 stream rather than a local file.
func UploadReader(ctx context.Context, client *s3.Client, r io.Reader, size int64, bucket, key string, opts UploadOptions) error {
	body := r
	if opts.Progress != nil || opts.MaxBPS > 0 {
		body = &progressReader{
			r: r, fn: opts.Progress, limiter: newRateLimiter(opts.MaxBPS),
			total: size, reportOn: true,
		}
	}
	uploader := newUploader(client, opts)
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
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}
	//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader
	_, err := uploader.Upload(ctx, input)
	return err
}

// NewProgressReader wraps r with progress reporting and optional rate
// limiting — the building block for streaming between engines that have
// no native progress hook (remote readers into remote/local writers).
func NewProgressReader(r io.Reader, fn ProgressFn, total, maxBPS int64) io.Reader {
	if fn == nil && maxBPS <= 0 {
		return r
	}
	return &progressReader{
		r: r, fn: fn, limiter: newRateLimiter(maxBPS),
		total: total, reportOn: true,
	}
}

// ErrShortStream is the integrity verdict: a source whose wire promised a
// size ended clean before delivering it. io.Copy cannot see this — a
// truncated stream terminates in a perfectly ordinary EOF, and a nil
// error would commit the partial as though it were the whole file: the
// silent-corruption class. Every streaming leg wraps its source in
// VerifiedStream so a clean short end becomes this error and the transfer
// aborts with nothing committed at the final name.
var ErrShortStream = errors.New("short stream")

// VerifiedStream wraps a body whose promised size is known and enforces
// the promise in both directions: a clean EOF while bytes remain turns
// into ErrShortStream, and reads cap at the promised size so a source
// that over-delivers cannot push past what its wire announced — the
// transfer lands exactly what was planned, or it fails loud. size < 0
// (unknown) returns the body unwrapped: an unknown size cannot be
// verified and streams exactly as before. Zero is a real size (an empty
// object or file) and verifies like any other: the first read must be
// the EOF.
func VerifiedStream(rc io.ReadCloser, size int64) io.ReadCloser {
	if rc == nil || size < 0 {
		return rc
	}
	return &verifiedStream{rc: rc, size: size, remaining: size}
}

// verifiedStream is VerifiedStream's reader: remaining counts down with
// every byte delivered.
type verifiedStream struct {
	rc        io.ReadCloser
	size      int64
	remaining int64
}

func (v *verifiedStream) Read(p []byte) (int, error) {
	if v.remaining <= 0 {
		return 0, io.EOF // the promise is met: nothing past it exists
	}
	if int64(len(p)) > v.remaining {
		p = p[:v.remaining]
	}
	n, err := v.rc.Read(p)
	v.remaining -= int64(n)
	if err == io.EOF && v.remaining > 0 {
		return n, fmt.Errorf("%w: got %d of %d bytes", ErrShortStream, v.size-v.remaining, v.size)
	}
	return n, err
}

func (v *verifiedStream) Close() error { return v.rc.Close() }

// DownloadOptions controls a download.
type DownloadOptions struct {
	PartSize    int64
	Concurrency int
	MaxBPS      int64 // 0 = unlimited (transfer throttle)
	Progress    ProgressFn
}

// DownloadFile downloads bucket/key to localPath (parent dirs created).
// The payload is staged through StageAndCommit: a canceled or failed
// download can never truncate a file already at localPath and never
// leaves a partial behind at the final name.
func DownloadFile(ctx context.Context, client *s3.Client, bucket, key, localPath string, opts DownloadOptions) error {
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return err
	}
	total := aws.ToInt64(head.ContentLength)

	return StageAndCommit(localPath, func(f *os.File) error {
		var w io.WriterAt = f
		downloader := newDownloader(client, opts)
		if opts.Progress != nil || opts.MaxBPS > 0 {
			w = &progressWriterAt{
				w: f, fn: opts.Progress, limiter: newRateLimiter(opts.MaxBPS),
				total: total, lastReport: 0,
			}
		}

		//lint:ignore SA1019 deprecated in favor of the pre-GA transfermanager; see newUploader
		n, err := downloader.Download(ctx, w, &s3.GetObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
			// Ask for response checksums: the SDK validates the payload
			// against them when the server sends any (single-part
			// downloads get the real check; ranged parts carry none and
			// validate nothing — the count below is the guarantee that
			// always runs).
			ChecksumMode: s3types.ChecksumModeEnabled,
		})
		if err != nil {
			return err
		}
		// The manager sizes the download from what the wire serves, not
		// from the HEAD that planned it: an object replaced between the
		// two (or a peer that under-delivers) downloads cleanly at the
		// wrong length while the job believed the HEAD. The count is the
		// integrity contract's last line — a download lands whole at the
		// size it was planned, or it fails loud.
		if n != total {
			return fmt.Errorf("%w: downloaded %d of %d bytes", ErrShortStream, n, total)
		}
		return nil
	})
}

// StageAndCommit is the local-file write half of the critical-data
// contract: bytes are written to a sibling temp file (same directory →
// same volume → the final rename is atomic) and renamed onto finalPath
// only after write reports success. Any error or cancellation removes the
// temp and leaves whatever was already at finalPath untouched — the
// download-side twin of "S3 materializes nothing until the upload
// completes". A hard process kill can at worst leave one .s3b-part-*
// staging file behind; it never leaves a partial at the final name.
func StageAndCommit(finalPath string, write func(*os.File) error) error {
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".s3b-part-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	discard := func() {
		tmp.Close()
		os.Remove(name)
	}

	if err := write(tmp); err != nil {
		discard()
		return err
	}
	if err := tmp.Sync(); err != nil {
		discard()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}

	// Keep the permissions a plain os.Create would have produced, or the
	// ones of the file being replaced (CreateTemp clamps to 0600).
	mode := os.FileMode(0o644)
	if st, err := os.Stat(finalPath); err == nil {
		mode = st.Mode().Perm()
	}
	_ = os.Chmod(name, mode) // best-effort: Windows ignores most of it

	if err := os.Rename(name, finalPath); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// progressWriterAt reports byte progress for ranged downloads.
type progressWriterAt struct {
	w          io.WriterAt
	fn         ProgressFn
	limiter    *rateLimiter
	total      int64
	sent       int64
	lastReport int64
}

func (p *progressWriterAt) WriteAt(b []byte, off int64) (int, error) {
	n, err := p.w.WriteAt(b, off)
	p.sent += int64(n)
	p.limiter.wait(n)
	if p.fn != nil && p.sent-p.lastReport >= 1<<20 { // report at most every MiB
		p.lastReport = p.sent
		p.fn(p.sent, p.total)
	}
	return n, err
}

// ObjectExists reports whether the object is already present — the
// --no-clobber check for paths that do not go through UploadFile
// (server-side copies, reader uploads from remote sources).
func ObjectExists(ctx context.Context, client *s3.Client, bucket, key string) bool {
	_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	return err == nil
}

// Copy performs a server-side copy (single request; objects > 5 GB need
// multipart copy — added in M2).
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
	return DeleteKeysProg(ctx, client, bucket, keys, nil)
}

// DeleteKeysProg is DeleteKeys with a per-batch progress callback: onProg
// fires with the running deleted count after every batch, so a long delete
// of many thousands shows real-time units instead of 0% → 100% at the end.
func DeleteKeysProg(ctx context.Context, client *s3.Client, bucket string, keys []string, onProg func(deleted int)) (DeleteResult, error) {
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
		if onProg != nil {
			onProg(out.Deleted)
		}
	}
	return out, nil
}

// CollectPrefixKeys gathers every object key under a prefix (used by rm -r,
// sync --delete and rb --force). Note: some stores (MinIO) omit "dir/" from
// a listing under "dir/" — recursive DELETES must additionally include the
// folder marker via IncludeFolderMarker or a ghost folder row survives.
func CollectPrefixKeys(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string) ([]string, error) {
	var keys []string
	err := listing.Walk(ctx, client, bucket, prefix, func(o s3types.Object) error {
		keys = append(keys, aws.ToString(o.Key))
		return nil
	})
	return keys, err
}

// IncludeFolderMarker appends the folder prefix to a recursive-delete key
// set when the listing did not already return it: stores disagree whether
// "dir/" appears in a listing under "dir/" (AWS lists it, MinIO omits it),
// and a delete that misses the marker leaves a ghost folder behind. Only
// deletes want this — converting or copying a marker is pointless and can
// fail outright on stores where the folder is implicit (no marker object).
func IncludeFolderMarker(keys []string, prefix string) []string {
	if strings.HasSuffix(prefix, "/") && !slices.Contains(keys, prefix) {
		return append(keys, prefix)
	}
	return keys
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

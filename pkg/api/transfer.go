package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Job statuses.
const (
	JobRunning  = "running"
	JobDone     = "done"
	JobError    = "error"
	JobCanceled = "canceled"
)

// Conflict policies shared by upload and download.
const (
	PolicyOverwrite = "overwrite"
	PolicySkip      = "skip"
	PolicyRename    = "rename"
)

// emitInterval throttles progress events to the frontend.
const emitInterval = 100 * time.Millisecond

// JobInfo is the transfer-manager view model (event payload).
type JobInfo struct {
	ID          string  `json:"id"`
	Op          string  `json:"op"` // "upload" | "download"
	Status      string  `json:"status"`
	TotalFiles  int     `json:"totalFiles"`
	DoneFiles   int     `json:"doneFiles"`
	FailedFiles int     `json:"failedFiles"`
	TotalBytes  int64   `json:"totalBytes"`
	SentBytes   int64   `json:"sentBytes"` // completed files + current file
	CurrentFile string  `json:"currentFile,omitempty"`
	SpeedBps    float64 `json:"speedBps"`
	StartedAt   int64   `json:"startedAt"` // unix millis
	Error       string  `json:"error,omitempty"`
}

// DownloadItem pairs an object key with its size (sizes come from the grid,
// avoiding one HeadObject per file).
type DownloadItem struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
}

// jobHandle is one running/finished transfer.
type jobHandle struct {
	mu       sync.Mutex
	info     JobInfo
	ctx      context.Context
	cancel   context.CancelFunc
	start    time.Time
	lastEm   time.Time
	doneBase int64 // bytes of fully transferred files
	fileSent int64 // bytes of the in-flight file
}

// jobManager owns all jobs in insertion order.
type jobManager struct {
	ctx context.Context
	mu  sync.Mutex
	all []*jobHandle
	seq int
}

func newJobManager() *jobManager { return &jobManager{} }

func (m *jobManager) setContext(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
}

func (m *jobManager) cancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.all {
		j.mu.Lock()
		running := j.info.Status == JobRunning
		j.mu.Unlock()
		if running {
			j.cancel()
		}
	}
}

// add registers a job and derives its cancellable context.
func (m *jobManager) add(op string, totalFiles int, totalBytes int64) *jobHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	j := &jobHandle{info: JobInfo{
		ID:         fmt.Sprintf("%s-%d", op, m.seq),
		Op:         op,
		Status:     JobRunning,
		TotalFiles: totalFiles,
		TotalBytes: totalBytes,
		StartedAt:  time.Now().UnixMilli(),
	}}
	base := m.ctx
	if base == nil {
		base = context.Background()
	}
	j.ctx, j.cancel = context.WithCancel(base)
	j.start = time.Now()
	m.all = append(m.all, j)
	return j
}

func (m *jobManager) snapshot() []JobInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]JobInfo, 0, len(m.all))
	for _, j := range m.all {
		j.mu.Lock()
		out = append(out, j.info)
		j.mu.Unlock()
	}
	return out
}

func (m *jobManager) clearFinished() {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.all[:0]
	for _, j := range m.all {
		j.mu.Lock()
		running := j.info.Status == JobRunning
		j.mu.Unlock()
		if running {
			kept = append(kept, j)
		}
	}
	m.all = kept
}

func (m *jobManager) cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.all {
		j.mu.Lock()
		match := j.info.ID == id && j.info.Status == JobRunning
		j.mu.Unlock()
		if match {
			j.cancel()
			return true
		}
	}
	return false
}

// emit pushes a JobInfo snapshot to the frontend (throttled unless force).
func (j *jobHandle) emit(m *jobManager, force bool) {
	j.mu.Lock()
	now := time.Now()
	if !force && now.Sub(j.lastEm) < emitInterval {
		j.mu.Unlock()
		return
	}
	j.lastEm = now
	info := j.info
	if elapsed := now.Sub(j.start).Seconds(); elapsed > 0 && info.SentBytes > 0 {
		info.SpeedBps = float64(info.SentBytes) / elapsed
	}
	ctx := m.ctx
	j.mu.Unlock()
	if ctx != nil {
		emitEvent(ctx, EventTransferUpdate, info)
	}
}

// progress is the per-byte callback wired into the engine's transfer options.
func (j *jobHandle) progress(sent, total int64) {
	j.mu.Lock()
	j.fileSent = sent
	j.info.SentBytes = j.doneBase + sent
	j.mu.Unlock()
}

// fileDone advances the completed-file counters.
func (j *jobHandle) fileDone(size int64, failed bool) {
	j.mu.Lock()
	j.doneBase += size
	j.fileSent = 0
	j.info.SentBytes = j.doneBase
	j.info.DoneFiles++
	if failed {
		j.info.FailedFiles++
	}
	j.mu.Unlock()
}

// ActiveTransfers lists all jobs (running and finished) for the manager view.
func (a *App) ActiveTransfers() []JobInfo { return a.jobs.snapshot() }

// ClearFinishedTransfers removes done/error/canceled jobs from the list.
func (a *App) ClearFinishedTransfers() { a.jobs.clearFinished() }

// CancelTransfer cancels a running job by ID.
func (a *App) CancelTransfer(id string) bool { return a.jobs.cancel(id) }

// uploadPair is one planned file upload.
type uploadPair struct {
	local string
	key   string
	size  int64
}

// Upload starts a background upload job and returns its ID. Paths may be
// files or directories (directories upload recursively). policy is one of
// overwrite | skip | rename; maxBPS 0 = unlimited.
func (a *App) Upload(paths []string, bucket, prefix, policy string, maxBPS int64) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	pairs, err := expandUploadPaths(paths, dirPrefix(prefix))
	if err != nil {
		return "", err
	}
	if len(pairs) == 0 {
		return "", fmt.Errorf("nothing to upload")
	}
	var total int64
	for _, p := range pairs {
		total += p.size
	}
	j := a.jobs.add("upload", len(pairs), total)
	id := j.info.ID
	a.emitLog(LogInfo, "upload", fmt.Sprintf("job %s: uploading %d file(s) (%d bytes) to %s/%s", id, len(pairs), total, bucket, dirPrefix(prefix)))
	go a.runUpload(j, c, bucket, pairs, policy, maxBPS)
	return id, nil
}

func (a *App) runUpload(j *jobHandle, c *s3client.Client, bucket string, pairs []uploadPair, policy string, maxBPS int64) {
	ctx := j.ctx
	for _, p := range pairs {
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.mu.Lock()
		j.info.CurrentFile = p.local
		j.mu.Unlock()
		j.emit(a.jobs, true)

		key := p.key
		opts := transfer.UploadOptions{Progress: j.progress, MaxBPS: maxBPS}
		switch policy {
		case PolicySkip:
			opts.NoClobber = true
		case PolicyRename:
			if alt, err := uniqueRemoteKey(ctx, c, bucket, key); err == nil {
				key = alt
			}
		}

		err := transfer.UploadFile(ctx, c.S3, p.local, bucket, key, opts)
		if err != nil {
			if ctx.Err() != nil {
				a.finishJob(j, JobCanceled, "canceled")
				return
			}
			j.mu.Lock()
			j.info.Error = fmt.Sprintf("%s: %v", filepath.Base(p.local), err)
			j.mu.Unlock()
		}
		j.fileDone(p.size, err != nil)
		j.emit(a.jobs, true)
	}

	j.mu.Lock()
	failed, total, lastErr := j.info.FailedFiles, j.info.TotalFiles, j.info.Error
	j.mu.Unlock()
	if failed > 0 {
		a.finishJob(j, JobError, fmt.Sprintf("%d of %d file(s) failed — last error: %s", failed, total, lastErr))
	} else {
		a.finishJob(j, JobDone, "")
	}
	a.emit(EventS3Changed, map[string]string{"bucket": bucket})
}

// finishJob stamps the final status, emits it and appends it to the local
// transfer history log (JSONL, PLAN.md §8.4 M3).
func (a *App) finishJob(j *jobHandle, status, errMsg string) {
	j.mu.Lock()
	id, done, totalFiles, sentBytes, failedFiles :=
		j.info.ID, j.info.DoneFiles, j.info.TotalFiles, j.info.SentBytes, j.info.FailedFiles
	j.info.Status = status
	j.info.Error = errMsg
	j.info.CurrentFile = ""
	if status == JobDone {
		j.info.SpeedBps = 0
	}
	logEntry := struct {
		At          string `json:"at"`
		Op          string `json:"op"`
		Status      string `json:"status"`
		TotalFiles  int    `json:"totalFiles"`
		FailedFiles int    `json:"failedFiles"`
		TotalBytes  int64  `json:"totalBytes"`
		SentBytes   int64  `json:"sentBytes"`
		Error       string `json:"error,omitempty"`
	}{
		At:          time.Now().Format(time.RFC3339),
		Op:          j.info.Op,
		Status:      status,
		TotalFiles:  j.info.TotalFiles,
		FailedFiles: j.info.FailedFiles,
		TotalBytes:  j.info.TotalBytes,
		SentBytes:   j.info.SentBytes,
		Error:       errMsg,
	}
	j.mu.Unlock()
	j.emit(a.jobs, true)
	a.logTransfer(logEntry)
	a.emitLog(jobStatusLevel(status), logEntry.Op,
		fmt.Sprintf("job %s finished: %s — %d/%d file(s), %d bytes sent, %d failed",
			id, status, done, totalFiles, sentBytes, failedFiles))
}

// logTransfer appends one JSONL line to <configdir>/transfers.log. Logging
// must never fail a transfer: errors are swallowed.
func (a *App) logTransfer(entry any) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "transfers.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
}

// expandUploadPaths resolves files and directories into (local, key, size).
func expandUploadPaths(paths []string, prefix string) ([]uploadPair, error) {
	var out []uploadPair
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, uploadPair{local: p, key: joinKeyNoSlash(prefix, filepath.Base(p)), size: st.Size()})
			continue
		}
		root := p
		base := filepath.Base(filepath.Clean(p)) // dropped folder keeps its name
		err = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, fp)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out = append(out, uploadPair{
				local: fp,
				key:   joinKeyNoSlash(prefix, base, filepath.ToSlash(rel)),
				size:  info.Size(),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// remoteExists reports whether an object key exists (HeadObject probe).
func remoteExists(ctx context.Context, c *s3client.Client, bucket, key string) bool {
	_, err := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	return err == nil
}

// uniqueRemoteKey returns key, or a "name (n).ext" variant, that is free.
func uniqueRemoteKey(ctx context.Context, c *s3client.Client, bucket, key string) (string, error) {
	if !remoteExists(ctx, c, bucket, key) {
		return key, nil
	}
	ext := filepath.Ext(key)
	stem := strings.TrimSuffix(key, ext)
	for n := 1; n < 1000; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if !remoteExists(ctx, c, bucket, cand) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no free name for %s", key)
}

// uniqueLocalPath returns p, or a "name (n).ext" variant, that is free.
func uniqueLocalPath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for n := 1; n < 1000; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
	return p + ".new"
}

// Download starts a background download job and returns its ID. policy is
// one of overwrite | skip | rename; maxBPS 0 = unlimited.
func (a *App) Download(bucket string, items []DownloadItem, destDir, policy string, maxBPS int64) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(destDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("destination folder not found: %s", destDir)
	}
	if len(items) == 0 {
		return "", fmt.Errorf("nothing to download")
	}
	var total int64
	for _, it := range items {
		total += it.Size
	}
	j := a.jobs.add("download", len(items), total)
	id := j.info.ID
	a.emitLog(LogInfo, "download", fmt.Sprintf("job %s: downloading %d object(s) (%d bytes) from %s to %s", id, len(items), total, bucket, destDir))
	go a.runDownload(j, c, bucket, items, destDir, policy, maxBPS)
	return id, nil
}

func (a *App) runDownload(j *jobHandle, c *s3client.Client, bucket string, items []DownloadItem, destDir, policy string, maxBPS int64) {
	ctx := j.ctx
	for _, it := range items {
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.mu.Lock()
		j.info.CurrentFile = it.Key
		j.mu.Unlock()
		j.emit(a.jobs, true)

		local := filepath.Join(destDir, filepath.FromSlash(strings.TrimPrefix(it.Key, "/")))
		switch policy {
		case PolicySkip:
			if _, err := os.Stat(local); err == nil {
				j.fileDone(0, false)
				continue
			}
		case PolicyRename:
			if _, err := os.Stat(local); err == nil {
				local = uniqueLocalPath(local)
			}
		}

		err := transfer.DownloadFile(ctx, c.S3, bucket, it.Key, local, transfer.DownloadOptions{
			Progress: j.progress,
			MaxBPS:   maxBPS,
		})
		if err != nil {
			if ctx.Err() != nil {
				a.finishJob(j, JobCanceled, "canceled")
				return
			}
			j.mu.Lock()
			j.info.Error = fmt.Sprintf("%s: %v", it.Key, err)
			j.mu.Unlock()
		}
		j.fileDone(it.Size, err != nil)
		j.emit(a.jobs, true)
	}

	j.mu.Lock()
	failed, total, lastErr := j.info.FailedFiles, j.info.TotalFiles, j.info.Error
	j.mu.Unlock()
	if failed > 0 {
		a.finishJob(j, JobError, fmt.Sprintf("%d of %d file(s) failed — last error: %s", failed, total, lastErr))
	} else {
		a.finishJob(j, JobDone, "")
	}
}

// DownloadRef is one dual-pane drop reference: a file key with its known
// size, or a folder key that gets expanded recursively.
type DownloadRef struct {
	Key   string `json:"key"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
}

// DownloadRefs starts a download job from mixed file/folder references:
// folders are walked recursively (sizes from listing), files use the size
// shipped by the grid.
func (a *App) DownloadRefs(bucket string, refs []DownloadRef, destDir, policy string, maxBPS int64) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	var items []DownloadItem
	for _, r := range refs {
		if !r.IsDir {
			items = append(items, DownloadItem{Key: r.Key, Size: r.Size})
			continue
		}
		err := listing.Walk(ctx, c.S3, bucket, dirPrefix(r.Key), func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil // folder markers
			}
			items = append(items, DownloadItem{Key: key, Size: aws.ToInt64(o.Size)})
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	if len(items) == 0 {
		return "", fmt.Errorf("nothing to download")
	}
	return a.Download(bucket, items, destDir, policy, maxBPS)
}

// PickUploadFiles opens the native multi-select file dialog.
func (a *App) PickUploadFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose files to upload",
	})
}

// PickFolder opens the native directory dialog (download target, uploads).
func (a *App) PickFolder(title string) (string, error) {
	if title == "" {
		title = "Choose a folder"
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
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
)

// Job statuses.
const (
	JobRunning  = "running"
	JobDone     = "done"
	JobError    = "error"
	JobCanceled = "canceled"
)

// Job phases (what the worker is doing right now — the status says
// running, the phase says why nothing moves yet).
const (
	PhaseTransfer = "transfer"
	PhaseCleanup  = "cleanup" // move: deleting fully-transferred sources
)

// stallAfter is how long a known-size in-flight file may sit without a
// byte moving before the job is flagged Stalled (the ticker keeps the
// flag honest in both directions).
const stallAfter = 10 * time.Second

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
	ID           string  `json:"id"`
	Op           string  `json:"op"` // "upload" | "download" | "transfer"
	Status       string  `json:"status"`
	TotalFiles   int     `json:"totalFiles"`
	DoneFiles    int     `json:"doneFiles"`
	FailedFiles  int     `json:"failedFiles"`
	SkippedFiles int     `json:"skippedFiles"`
	TotalBytes   int64   `json:"totalBytes"`
	SentBytes    int64   `json:"sentBytes"` // completed files + current file
	CurrentFile  string  `json:"currentFile,omitempty"`
	SpeedBps     float64 `json:"speedBps"`
	StartedAt    int64   `json:"startedAt"` // unix millis
	Error        string  `json:"error,omitempty"`

	// The full-picture fields: what the row is called, where data flows,
	// and where the bytes are right now. Name/Items feed the localized
	// "Copying photos +2" title; From/To the route line; the current-*
	// triple the "File 2/3 — pic.jpg · 18/41 MB" line.
	Move         bool   `json:"move"`                // transfer jobs: copy-then-delete
	Name         string `json:"name,omitempty"`      // primary source item
	Items        int    `json:"items"`               // top-level items dropped
	From         string `json:"from,omitempty"`      // human source label
	To           string `json:"to,omitempty"`        // human destination label
	Phase        string `json:"phase"`               // "transfer" | "cleanup"
	FileIndex    int    `json:"fileIndex"`           // 1-based in-flight file ordinal
	CurrentSent  int64  `json:"currentSent"`         // bytes of the in-flight file
	CurrentTotal int64  `json:"currentTotal"`        // its size (0 = unknown/server-side)
	EtaMs        int64  `json:"etaMs,omitempty"`     // computed on emit while running
	ElapsedMs    int64  `json:"elapsedMs,omitempty"` // stamped at finish
	Stalled      bool   `json:"stalled"`             // no byte movement in stallAfter
	ErrorKind    string `json:"errorKind,omitempty"` // "timeout" | ""
}

// DownloadItem pairs an object key with its size (sizes come from the grid,
// avoiding one HeadObject per file). Local optionally overrides the relative
// path under destDir (slash-separated; "" = derive from Key).
type DownloadItem struct {
	Key   string `json:"key"`
	Size  int64  `json:"size"`
	Local string `json:"local,omitempty"`
}

// jobHandle is one running/finished transfer.
type jobHandle struct {
	mu         sync.Mutex
	info       JobInfo
	src        string // source tag for log lines (bucket / source name)
	ctx        context.Context
	cancel     context.CancelFunc
	start      time.Time
	lastEm     time.Time
	doneBase   int64 // bytes of fully transferred files
	fileSent   int64 // bytes of the in-flight file
	mgr        *jobManager
	lastByteAt time.Time // last time a byte actually moved
	lastEmSent int64     // SentBytes at the previous emit (EMA speed input)
	lastEmAt   time.Time // when that was
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
	now := time.Now()
	j := &jobHandle{info: JobInfo{
		ID:         fmt.Sprintf("%s-%d", op, m.seq),
		Op:         op,
		Status:     JobRunning,
		TotalFiles: totalFiles,
		TotalBytes: totalBytes,
		StartedAt:  now.UnixMilli(),
		Phase:      PhaseTransfer,
	}}
	base := m.ctx
	if base == nil {
		base = context.Background()
	}
	j.ctx, j.cancel = context.WithCancel(base)
	j.start = now
	j.mgr = m
	j.lastByteAt = now
	j.lastEmAt = now
	m.all = append(m.all, j)
	// The heartbeat: progress callbacks only fire when the engine reads
	// bytes — a server-side copy, a slow HeadObject or a hung socket
	// would otherwise freeze the row at its last snapshot. The ticker
	// keeps emitting (speed decays, ETA moves, Stalled can appear) until
	// the job leaves "running".
	go j.heartbeat()
	return j
}

// heartbeat re-emits a running job ~4x/second and maintains the Stalled
// flag. It exits as soon as the job finishes (finishJob is the only
// status transition, and it happens-before this read under j.mu).
func (j *jobHandle) heartbeat() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		j.mu.Lock()
		if j.info.Status != JobRunning {
			j.mu.Unlock()
			return
		}
		j.info.Stalled = j.info.Phase == PhaseTransfer &&
			j.info.CurrentTotal > 0 && time.Since(j.lastByteAt) > stallAfter
		j.mu.Unlock()
		j.emit(false)
	}
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

// clearFinished retires finished jobs: every one when ids is nil (the
// classic "clear all finished"), otherwise only the named ones — and
// only if they really are finished. The transfer window passes the ids
// it can actually see, so rows hidden as pre-open history survive.
func (m *jobManager) clearFinished(ids []string) {
	var only map[string]bool
	if ids != nil {
		only = make(map[string]bool, len(ids))
		for _, id := range ids {
			only[id] = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.all[:0]
	for _, j := range m.all {
		j.mu.Lock()
		running := j.info.Status == JobRunning
		id := j.info.ID
		j.mu.Unlock()
		if running || (only != nil && !only[id]) {
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
// Besides the raw counters it keeps the derived numbers honest: an EMA
// speed over the bytes moved since the previous emit, and the ETA those
// two imply. This is the ONLY path that computes them, so the heartbeat
// ticker keeps a quiet job's row alive simply by calling emit.
func (j *jobHandle) emit(force bool) {
	j.mu.Lock()
	now := time.Now()
	if !force && now.Sub(j.lastEm) < emitInterval {
		j.mu.Unlock()
		return
	}
	j.lastEm = now
	info := j.info
	// EMA speed: weigh the instantaneous rate since the last emit with
	// the smoothed history, so one fast burst doesn't fling the number
	// around and a stall decays it visibly toward zero.
	if dt := now.Sub(j.lastEmAt).Seconds(); dt > 0.05 {
		inst := float64(info.SentBytes-j.lastEmSent) / dt
		if inst < 0 {
			inst = 0
		}
		const w = 0.35
		if info.SpeedBps <= 0 || j.lastEmSent == 0 {
			info.SpeedBps = inst
		} else {
			info.SpeedBps = (1-w)*info.SpeedBps + w*inst
		}
		j.info.SpeedBps = info.SpeedBps
		j.lastEmSent = info.SentBytes
		j.lastEmAt = now
	}
	if info.Status == JobRunning && info.TotalBytes > 0 && info.SentBytes > 0 &&
		info.SpeedBps > 1 && info.TotalBytes > info.SentBytes {
		info.EtaMs = int64(float64(info.TotalBytes-info.SentBytes) / info.SpeedBps * 1000)
		j.info.EtaMs = info.EtaMs
	} else if j.info.EtaMs != 0 {
		j.info.EtaMs = 0
		info.EtaMs = 0
	}
	m := j.mgr
	j.mu.Unlock()
	if m != nil && m.ctx != nil {
		emitEvent(EventTransferUpdate, info)
	}
}

// progress is the per-byte callback wired into the engine's transfer
// options. THE live path: it emits (throttled) on every read, so a
// 15-second single-file transfer paints its percentage as it climbs
// instead of jumping 0% → 100% at the end.
func (j *jobHandle) progress(sent, total int64) {
	j.mu.Lock()
	j.fileSent = sent
	j.info.SentBytes = j.doneBase + sent
	j.info.CurrentSent = sent
	if total > 0 {
		j.info.CurrentTotal = total
	}
	j.lastByteAt = time.Now()
	j.mu.Unlock()
	j.emit(false)
}

// startFile announces the next in-flight file: its 1-based ordinal, its
// display path and its expected size (0 = unknown / server-side copy).
func (j *jobHandle) startFile(idx int, name string, size int64) {
	j.mu.Lock()
	j.info.FileIndex = idx
	j.info.CurrentFile = name
	j.info.CurrentSent = 0
	j.info.CurrentTotal = size
	j.info.Stalled = false
	j.lastByteAt = time.Now()
	j.mu.Unlock()
}

// setMeta stamps the identity fields (title name, item count, route).
func (j *jobHandle) setMeta(name, from, to string, items int, move bool) {
	j.mu.Lock()
	j.info.Name, j.info.From, j.info.To = name, from, to
	j.info.Items = items
	j.info.Move = move
	j.mu.Unlock()
}

// setPhase labels what the worker is doing between "running" snapshots.
func (j *jobHandle) setPhase(p string) {
	j.mu.Lock()
	j.info.Phase = p
	j.mu.Unlock()
}

// fileDone advances the completed-file counters.
func (j *jobHandle) fileDone(size int64, failed bool) {
	j.mu.Lock()
	j.doneBase += size
	j.fileSent = 0
	j.info.SentBytes = j.doneBase
	j.info.CurrentSent = 0
	j.info.CurrentTotal = 0
	j.info.DoneFiles++
	j.lastByteAt = time.Now()
	if failed {
		j.info.FailedFiles++
	}
	j.mu.Unlock()
}

// ActiveTransfers lists all jobs (running and finished) for the manager view.
func (a *App) ActiveTransfers() []JobInfo { return a.jobs.snapshot() }

// ClearFinishedTransfers removes done/error/canceled jobs from the list:
// all of them when ids is null, otherwise only the named ones (running
// jobs are never touched).
func (a *App) ClearFinishedTransfers(ids []string) { a.jobs.clearFinished(ids) }

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
// overwrite | skip | rename; maxBPS 0 = unlimited. decisions optionally
// overrides the policy per destination object key (the conflict dialog's
// per-file choices — keys are the CheckConflicts decisionKeys).
func (a *App) Upload(paths []string, bucket, prefix, policy string, maxBPS int64, decisions map[string]string) (string, error) {
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
	j.src = bucket
	j.setMeta(uploadTitle(paths), filepath.Dir(paths[0]), s3Label(bucket, dirPrefix(prefix)), len(paths), false)
	id := j.info.ID
	a.emitLogSrc(LogInfo, "upload", bucket, fmt.Sprintf("job %s: uploading %d file(s) (%d bytes) to %s/%s", id, len(pairs), total, bucket, dirPrefix(prefix)))
	logDecisions(a, "upload", decisions)
	go a.runUpload(j, c, bucket, pairs, policy, decisions, maxBPS)
	return id, nil
}

// uploadTitle names an upload after its first dropped item (the row is
// "Uploading <name>", the item count rides beside it).
func uploadTitle(paths []string) string { return filepath.Base(paths[0]) }

// s3Label renders an S3 location as "s3://bucket/prefix".
func s3Label(bucket, prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return "s3://" + bucket
	}
	return "s3://" + bucket + "/" + prefix
}

// timeoutKind classifies an error string for the critical "Timed out"
// treatment in the transfer rows. Empty means an ordinary error.
func timeoutKind(errMsg string) string {
	if errMsg == "" {
		return ""
	}
	s := strings.ToLower(errMsg)
	for _, k := range []string{"timeout", "timed out", "deadline exceeded", "context deadline"} {
		if strings.Contains(s, k) {
			return "timeout"
		}
	}
	return ""
}

// logDecisions records the conflict dialog's per-file choices in the log.
func logDecisions(a *App, scope string, decisions map[string]string) {
	if len(decisions) == 0 {
		return
	}
	n := map[string]int{}
	for _, d := range decisions {
		n[d]++
	}
	a.emitLog(LogInfo, scope, fmt.Sprintf("conflict decisions: %d overwrite, %d skip, %d rename",
		n[PolicyOverwrite], n[PolicySkip], n[PolicyRename]))
}

func (a *App) runUpload(j *jobHandle, c *s3client.Client, bucket string, pairs []uploadPair, policy string, decisions map[string]string, maxBPS int64) {
	ctx := j.ctx
	for i, p := range pairs {
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.startFile(i+1, p.local, p.size)
		j.emit(true)

		pol := filePolicy(decisions, p.key, policy)
		if pol == PolicySkip { // user kept the destination file
			j.mu.Lock()
			j.info.SkippedFiles++
			j.mu.Unlock()
			j.fileDone(0, false)
			j.emit(true)
			continue
		}

		key := p.key
		opts := transfer.UploadOptions{Progress: j.progress, MaxBPS: maxBPS}
		switch pol {
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
			j.info.ErrorKind = timeoutKind(err.Error())
			j.mu.Unlock()
		}
		j.fileDone(p.size, err != nil)
		j.emit(true)
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
// transfer history log (JSONL M3).
func (a *App) finishJob(j *jobHandle, status, errMsg string) {
	j.mu.Lock()
	now := time.Now()
	id, done, totalFiles, sentBytes, failedFiles, skipped :=
		j.info.ID, j.info.DoneFiles, j.info.TotalFiles, j.info.SentBytes, j.info.FailedFiles, j.info.SkippedFiles
	j.info.Status = status
	j.info.Error = errMsg
	j.info.ErrorKind = timeoutKind(errMsg)
	j.info.CurrentFile = ""
	j.info.CurrentSent = 0
	j.info.CurrentTotal = 0
	j.info.Stalled = false
	j.info.EtaMs = 0
	j.info.ElapsedMs = now.Sub(j.start).Milliseconds()
	// The finished row keeps the lifetime average — "done in 12s at
	// 8 MB/s" — rather than the last live EMA sample.
	if elapsed := now.Sub(j.start).Seconds(); elapsed > 0 && sentBytes > 0 {
		j.info.SpeedBps = float64(sentBytes) / elapsed
	} else {
		j.info.SpeedBps = 0
	}
	logEntry := struct {
		At           string `json:"at"`
		Op           string `json:"op"`
		Status       string `json:"status"`
		TotalFiles   int    `json:"totalFiles"`
		FailedFiles  int    `json:"failedFiles"`
		SkippedFiles int    `json:"skippedFiles"`
		TotalBytes   int64  `json:"totalBytes"`
		SentBytes    int64  `json:"sentBytes"`
		Error        string `json:"error,omitempty"`
	}{
		At:           now.Format(time.RFC3339),
		Op:           j.info.Op,
		Status:       status,
		TotalFiles:   j.info.TotalFiles,
		FailedFiles:  j.info.FailedFiles,
		SkippedFiles: j.info.SkippedFiles,
		TotalBytes:   j.info.TotalBytes,
		SentBytes:    j.info.SentBytes,
		Error:        errMsg,
	}
	j.mu.Unlock()
	j.emit(true)
	a.logTransfer(logEntry)
	a.emitLogSrc(jobStatusLevel(status), logEntry.Op, j.src,
		fmt.Sprintf("job %s finished: %s — %d/%d file(s), %d bytes sent, %d failed, %d skipped",
			id, status, done, totalFiles, sentBytes, failedFiles, skipped))
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
// one of overwrite | skip | rename; maxBPS 0 = unlimited. decisions
// optionally overrides the policy per destination LOCAL path (the conflict
// dialog's decisionKeys).
func (a *App) Download(bucket string, items []DownloadItem, destDir, policy string, maxBPS int64, decisions map[string]string) (string, error) {
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
	j.src = bucket
	j.setMeta(downloadTitle(items), s3Label(bucket, ""), destDir, len(items), false)
	id := j.info.ID
	a.emitLogSrc(LogInfo, "download", bucket, fmt.Sprintf("job %s: downloading %d object(s) (%d bytes) from %s to %s", id, len(items), total, bucket, destDir))
	logDecisions(a, "download", decisions)
	go a.runDownload(j, c, bucket, items, destDir, policy, decisions, maxBPS)
	return id, nil
}

// downloadTitle names a download after its first item (honoring a local
// rename override when the grid shipped one).
func downloadTitle(items []DownloadItem) string {
	if it := items[0]; it.Local != "" {
		return path.Base(it.Local)
	}
	return path.Base(strings.TrimSuffix(items[0].Key, "/"))
}

func (a *App) runDownload(j *jobHandle, c *s3client.Client, bucket string, items []DownloadItem, destDir, policy string, decisions map[string]string, maxBPS int64) {
	ctx := j.ctx
	for i, it := range items {
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.startFile(i+1, it.Key, it.Size)
		j.emit(true)

		rel := it.Local
		if rel == "" {
			rel = strings.TrimPrefix(it.Key, "/")
		}
		local := filepath.Join(destDir, filepath.FromSlash(rel))
		pol := filePolicy(decisions, local, policy)
		switch pol {
		case PolicySkip:
			if _, err := os.Stat(local); err == nil {
				j.mu.Lock()
				j.info.SkippedFiles++
				j.mu.Unlock()
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
			j.info.ErrorKind = timeoutKind(err.Error())
			j.mu.Unlock()
		}
		j.fileDone(it.Size, err != nil)
		j.emit(true)
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
// shipped by the grid. Dragged files land flat in destDir (the basename —
// dragging zz-live/live-b.txt onto a pane yields live-b.txt, not a nested
// zz-live/), folders keep their structure. decisions optionally overrides
// the policy per destination LOCAL path.
func (a *App) DownloadRefs(bucket string, refs []DownloadRef, destDir, policy string, maxBPS int64, decisions map[string]string) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	var items []DownloadItem
	for _, r := range refs {
		if !r.IsDir {
			items = append(items, DownloadItem{Key: r.Key, Size: r.Size, Local: path.Base(r.Key)})
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
	return a.Download(bucket, items, destDir, policy, maxBPS, decisions)
}

// PickFolder opens the native directory dialog (download target, uploads).
func (a *App) PickFolder(title string) (string, error) {
	if pickerSeams.folder != nil {
		return pickerSeams.folder(title)
	}
	if title == "" {
		title = "Choose a folder"
	}
	if shell == nil {
		return "", errNoShell
	}
	return shell.OpenDir(title)
}

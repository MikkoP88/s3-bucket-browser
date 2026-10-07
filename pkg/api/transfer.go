package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
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

// Conflict policies shared by upload and download.
const (
	PolicyOverwrite = "overwrite"
	PolicySkip      = "skip"
	PolicyRename    = "rename"
)

// Item-row states (TransferItem.State): the per-item lifecycle inside one
// job — pending until its first file starts, active while any of its files
// moves, then the settled outcome once every planned file landed.
const (
	ItemPending = "pending"
	ItemActive  = "active"
	ItemDone    = "done"
	ItemFailed  = "failed"
	ItemSkipped = "skipped"
)

// itemRowCap bounds the per-item rows riding the progress events: a job
// of dozens shows live per-file states in its expanded panel, a huge one
// falls back to the aggregate counters and TransferItems' plain names
// instead of copying hundreds of rows into every snapshot.
const itemRowCap = 50

// TransferItem is one top-level item of a job with its own live outcome.
// A flat multi-file drop is one row per FILE; a folder item aggregates
// the files beneath it (Files/Total count the planner's expansion).
type TransferItem struct {
	Name    string `json:"name"`
	State   string `json:"state"` // ItemPending | ItemActive | ItemDone | ItemFailed | ItemSkipped
	Files   int    `json:"files"` // planned files under this item
	Done    int    `json:"done"`  // settled files (done + failed + skipped)
	Failed  int    `json:"failed,omitempty"`
	Skipped int    `json:"skipped,omitempty"`
	Sent    int64  `json:"sent"`  // bytes settled under this item
	Total   int64  `json:"total"` // planned bytes under this item
}

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
	Move         bool           `json:"move"`                // transfer jobs: copy-then-delete
	Name         string         `json:"name,omitempty"`      // primary source item
	Items        int            `json:"items"`               // top-level items dropped
	ItemRows     []TransferItem `json:"itemRows,omitempty"`  // live per-item states (≤ itemRowCap) — the expanded panel's rows
	From         string         `json:"from,omitempty"`      // human source label
	To           string         `json:"to,omitempty"`        // human destination label
	Phase        string         `json:"phase"`               // "transfer" | "cleanup"
	FileIndex    int            `json:"fileIndex"`           // 1-based in-flight file ordinal
	CurrentSent  int64          `json:"currentSent"`         // bytes of the in-flight file
	CurrentTotal int64          `json:"currentTotal"`        // its size (0 = unknown/server-side)
	EtaMs        int64          `json:"etaMs,omitempty"`     // computed on emit while running
	ElapsedMs    int64          `json:"elapsedMs,omitempty"` // stamped at finish
	EndedAt      int64          `json:"endedAt,omitempty"`   // unix millis, stamped at finish
	Stalled      bool           `json:"stalled"`             // no byte movement within the stall threshold
	ErrorKind    string         `json:"errorKind,omitempty"` // "timeout" | ""
	Hidden       bool           `json:"hidden,omitempty"`    // internal staging (drag-out scratch download) — never shown in any list or badge
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
	finishing  bool   // set by finishJob: the first settle stands
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
	stallAfter time.Duration
	itemNames  []string // item display names (TransferItems — the >itemRowCap fallback)
}

// jobManager owns all jobs in insertion order.
type jobManager struct {
	ctx        context.Context
	mu         sync.Mutex
	all        []*jobHandle
	seq        int
	stallAfter time.Duration // Stalled threshold for NEW jobs (Settings → Transfers)
	onPanic    func(string)  // heartbeat panic net (guard.go); App installs the hook
}

func newJobManager() *jobManager { return &jobManager{} }

// recoverHeartbeat is the job heartbeat's slice of the worker panic net
// (guard.go): the loop is a leaf (lock, stamp, emit), but a panic in it
// must not take the process — report and let the row settle through its
// own worker.
func (m *jobManager) recoverHeartbeat() {
	if e := recover(); e != nil {
		logRegistryPanic(m.onPanic, "transfer", e)
	}
}

func (m *jobManager) setContext(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
}

// setStallAfter updates the Stalled threshold for future jobs; running
// jobs keep the value they captured at start (a mid-job change must not
// flip a live row's flag on its own).
func (m *jobManager) setStallAfter(d time.Duration) {
	m.mu.Lock()
	m.stallAfter = d
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
	// Captured at start: later setting changes skip live jobs. Managers
	// that never ran Startup (unit tests) fall back to the stored/default
	// threshold so a zero value can never make every job "stalled".
	j.stallAfter = m.stallAfter
	if j.stallAfter <= 0 {
		j.stallAfter = appsettings.Load().StallAfter()
	}
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
	defer j.mgr.recoverHeartbeat() // the worker panic net (guard.go)
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		j.mu.Lock()
		if j.info.Status != JobRunning {
			j.mu.Unlock()
			return
		}
		j.info.Stalled = j.info.Phase == PhaseTransfer &&
			j.info.CurrentTotal > 0 && time.Since(j.lastByteAt) > j.stallAfter
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

// itemsOf returns the stored item names of a job (nil when unknown —
// the frontend shows no list).
func (m *jobManager) itemsOf(id string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.all {
		j.mu.Lock()
		match := j.info.ID == id
		names := j.itemNames
		j.mu.Unlock()
		if match {
			return names
		}
	}
	return nil
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

// startFile announces the next in-flight file: its owning item (index
// into the per-item rows), 1-based ordinal, display path and expected
// size (0 = unknown / server-side copy).
func (j *jobHandle) startFile(item, idx int, name string, size int64) {
	j.mu.Lock()
	j.info.FileIndex = idx
	j.info.CurrentFile = name
	j.info.CurrentSent = 0
	j.info.CurrentTotal = size
	j.info.Stalled = false
	j.lastByteAt = time.Now()
	j.touchItemLocked(item, func(r *TransferItem) {
		if r.State == ItemPending {
			r.State = ItemActive
		}
	})
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

// setItems records the top-level items: the names always feed
// TransferItems (the >itemRowCap fallback list), and jobs within the cap
// also carry live per-item state rows on every event so the expanded
// panel shows each file's outcome without extra bridge chatter.
func (j *jobHandle) setItems(items []TransferItem) {
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	j.mu.Lock()
	j.itemNames = names
	if len(items) <= itemRowCap {
		rows := make([]TransferItem, len(items))
		copy(rows, items)
		for i := range rows {
			switch {
			case rows[i].Files == 0:
				rows[i].State = ItemDone // nothing planned under it — never pending
			case rows[i].State == "":
				rows[i].State = ItemPending
			}
		}
		j.info.ItemRows = rows
	}
	j.mu.Unlock()
}

// touchItemLocked mutates one item row copy-on-write: emitted snapshots
// share the backing array with a marshal riding outside the lock, so the
// worker never edits a slice a consumer may still be reading — it swaps
// in a fresh copy instead. Caller holds j.mu.
func (j *jobHandle) touchItemLocked(item int, fn func(*TransferItem)) {
	if item < 0 || item >= len(j.info.ItemRows) {
		return
	}
	rows := make([]TransferItem, len(j.info.ItemRows))
	copy(rows, j.info.ItemRows)
	fn(&rows[item])
	j.info.ItemRows = rows
}

// setPhase labels what the worker is doing between "running" snapshots.
func (j *jobHandle) setPhase(p string) {
	j.mu.Lock()
	j.info.Phase = p
	j.mu.Unlock()
}

// fileDone settles the in-flight file: the job counters, the byte base
// and the owning item's row all advance together. outcome is ItemDone,
// ItemFailed or ItemSkipped — a skip settles nothing byte-wise (the
// destination kept its own file, the source keeps this one).
func (j *jobHandle) fileDone(item int, size int64, outcome string) {
	j.mu.Lock()
	switch outcome {
	case ItemSkipped:
		j.info.SkippedFiles++
	case ItemFailed:
		j.doneBase += size
		j.info.FailedFiles++
	default:
		j.doneBase += size
		j.info.DoneFiles++
	}
	j.fileSent = 0
	j.info.SentBytes = j.doneBase
	j.info.CurrentSent = 0
	j.info.CurrentTotal = 0
	j.lastByteAt = time.Now()
	j.touchItemLocked(item, func(r *TransferItem) {
		r.Done++
		switch outcome {
		case ItemFailed:
			r.Failed++
			r.Sent += size
		case ItemSkipped:
			r.Skipped++
		default:
			r.Sent += size
		}
		if r.Done >= r.Files { // every planned file settled — the outcome
			switch {
			case r.Failed > 0:
				r.State = ItemFailed
			case r.Skipped > 0:
				r.State = ItemSkipped
			default:
				r.State = ItemDone
			}
		}
	})
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

// TransferItems lists the top-level items of a job by ID — the expanded
// panel's contents list ("photos +2" finally says who the other two are).
func (a *App) TransferItems(id string) []string { return a.jobs.itemsOf(id) }

// uploadPair is one planned file upload.
type uploadPair struct {
	item  int // index of the originating dropped path (per-item rows)
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
	pairs, err := expandUploadPaths(a.ctx, paths, dirPrefix(prefix))
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
	its := make([]TransferItem, len(paths))
	for i, p := range paths {
		its[i] = TransferItem{Name: p}
	}
	for _, pr := range pairs {
		its[pr.item].Files++
		its[pr.item].Total += pr.size
	}
	j.setItems(its)
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
	defer a.guardJob("upload", j) // the worker panic net (guard.go)
	ctx := j.ctx
	for i, p := range pairs {
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.startFile(p.item, i+1, p.local, p.size)
		j.emit(true)

		pol := filePolicy(decisions, p.key, policy)
		if pol == PolicySkip && remoteExists(ctx, c, bucket, p.key) {
			// "skip" keeps an EXISTING destination file — with nothing at
			// the key there is no conflict to resolve, and silently
			// dropping a file the caller asked to transfer is data loss
			j.fileDone(p.item, 0, ItemSkipped)
			j.emit(true)
			continue
		}

		key := p.key
		partSize, conc := a.partTunables() // Settings → Transfers engine tuning
		opts := transfer.UploadOptions{Progress: j.progress, MaxBPS: maxBPS, PartSize: partSize, Concurrency: conc}
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
		outcome := ItemDone
		if err != nil {
			outcome = ItemFailed
		}
		j.fileDone(p.item, p.size, outcome)
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
// transfer history log (JSONL M3). Idempotent: the first call wins, so a
// worker that panics in a deferred cleanup AFTER settling (the guard's
// finish, guard.go) cannot rewrite the row's real outcome.
func (a *App) finishJob(j *jobHandle, status, errMsg string) {
	j.mu.Lock()
	if j.finishing {
		j.mu.Unlock()
		return
	}
	j.finishing = true
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
	j.info.EndedAt = now.UnixMilli()
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

// expandUploadPaths resolves files and directories into (item, local,
// key, size); item is the dropped path each file descends from. The
// per-path stat and every directory read are bounded steps, and the walk
// is the STRICT variant — an unreadable directory aborts the expansion
// (a silently skipped file here would be a silently missing upload), so
// a wedged drop root fails the upload promise in bounded time instead of
// parking the binding goroutine forever.
func expandUploadPaths(ctx context.Context, paths []string, prefix string) ([]uploadPair, error) {
	var out []uploadPair
	for item, p := range paths {
		st, err := remotefs.LocalStep(ctx, func() (os.FileInfo, error) { return localStat(p) })
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, uploadPair{item: item, local: p, key: joinKeyNoSlash(prefix, filepath.Base(p)), size: st.Size()})
			continue
		}
		root := p
		base := filepath.Base(filepath.Clean(p)) // dropped folder keeps its name
		err = walkLocalStrict(ctx, root, func(e localEntryInfo) {
			if e.isDir {
				return
			}
			out = append(out, uploadPair{
				item:  item,
				local: filepath.Join(root, filepath.FromSlash(e.rel)),
				key:   joinKeyNoSlash(prefix, base, e.rel),
				size:  e.size,
			})
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

// destTaken reports whether a destination path is taken (anything the OS
// can stat there). A wedged probe returns the deadline verdict — the
// caller fails the item rather than guessing skip-or-overwrite onto a
// volume it cannot see; any other probe miss is simply not-taken.
func destTaken(ctx context.Context, p string) (bool, error) {
	_, err := remotefs.LocalStep(ctx, func() (os.FileInfo, error) { return localStat(p) })
	if err != nil {
		if errors.Is(err, remotefs.ErrLocalDeadline) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// uniqueLocalPath returns p, or a "name (n).ext" variant, that is free.
// Each probe is a bounded step — a wedged destination volume fails the
// rename honestly instead of parking the download job mid-item.
func uniqueLocalPath(ctx context.Context, p string) (string, error) {
	taken, err := destTaken(ctx, p)
	if err != nil {
		return "", err
	}
	if !taken {
		return p, nil
	}
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for n := 1; n < 1000; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if taken, err := destTaken(ctx, cand); err != nil {
			return "", err
		} else if !taken {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no free name for %s", p)
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
	if st, err := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return localStat(destDir) }); err != nil || !st.IsDir() {
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
	its := make([]TransferItem, len(items))
	for i, it := range items {
		nm := it.Key
		if it.Local != "" {
			nm = it.Local
		}
		its[i] = TransferItem{Name: nm, Files: 1, Total: it.Size}
	}
	j.setItems(its)
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
	defer a.guardJob("download", j) // the worker panic net (guard.go)
	ctx := j.ctx
	// failItem records one item's verdict without stopping the job (the
	// DownloadFile error path below reports the same way).
	failItem := func(i int, key string, err error) {
		j.mu.Lock()
		j.info.Error = fmt.Sprintf("%s: %v", key, err)
		j.info.ErrorKind = timeoutKind(err.Error())
		j.mu.Unlock()
		j.fileDone(i, 0, ItemFailed)
		j.emit(true)
	}
	for i, it := range items {
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.startFile(i, i+1, it.Key, it.Size)
		j.emit(true)

		rel := it.Local
		if rel == "" {
			rel = strings.TrimPrefix(it.Key, "/")
		}
		local := transfer.SafeLocalJoin(destDir, rel)
		pol := filePolicy(decisions, local, policy)
		// The destination probes are bounded steps: a destination volume
		// that wedged mid-job fails the item honestly — a parked probe
		// would park the whole job in "running" forever, and a verdict of
		// unknown must never guess skip-or-overwrite onto an unseen volume.
		proceed := true
		switch pol {
		case PolicySkip:
			taken, perr := destTaken(ctx, local)
			switch {
			case perr != nil:
				failItem(i, it.Key, perr)
				proceed = false
			case taken:
				j.fileDone(i, 0, ItemSkipped)
				j.emit(true)
				proceed = false
			}
		case PolicyRename:
			taken, perr := destTaken(ctx, local)
			switch {
			case perr != nil:
				failItem(i, it.Key, perr)
				proceed = false
			case taken:
				if local, perr = uniqueLocalPath(ctx, local); perr != nil {
					failItem(i, it.Key, perr)
					proceed = false
				}
			}
		}
		if !proceed {
			continue
		}

		partSize, conc := a.partTunables() // Settings → Transfers engine tuning
		err := transfer.DownloadFile(ctx, c.S3, bucket, it.Key, local, transfer.DownloadOptions{
			Progress:    j.progress,
			MaxBPS:      maxBPS,
			PartSize:    partSize,
			Concurrency: conc,
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
		outcome := ItemDone
		if err != nil {
			outcome = ItemFailed
		}
		j.fileDone(i, it.Size, outcome)
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

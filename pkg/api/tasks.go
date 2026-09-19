// tasks.go: the unified task registry behind the Running tasks window.
// Transfer jobs have their own manager (transfer.go); everything else
// that can outlive a click — deep searches, listing streams, bulk
// deletes/purges/conversions — registers here with a cancellable
// context, so one view shows (and can kill) every running action.
package api

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Task statuses. "queued" is reserved for future queueing; every task
// today starts running immediately (contexts still honor cancel while
// queued, so nothing is uncancellable).
const (
	TaskQueued   = "queued"
	TaskRunning  = "running"
	TaskDone     = "done"
	TaskError    = "error"
	TaskCanceled = "canceled"
)

// Task phases. "" (the zero value) is plain working; producers that must
// walk/count before acting announce it (the row says "Counting"), and
// move-shaped tasks say "cleanup" while deleting their sources.
const (
	TaskPhaseCount   = "count"
	TaskPhaseAct     = "act"
	TaskPhaseCleanup = "cleanup"
)

// TaskInfo is the Running-tasks view model (event payload and list row).
type TaskInfo struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"` // transfer|move|copy|search|list|delete|purge|empty|convert
	Label      string  `json:"label"`
	Status     string  `json:"status"`
	DoneUnits  int     `json:"doneUnits"`
	TotalUnits int     `json:"totalUnits"`          // 0 = unknown
	Phase      string  `json:"phase"`               // "" (act) | count | cleanup
	Current    string  `json:"current,omitempty"`   // item in flight
	Speed      float64 `json:"speed"`               // units/sec, EMA over emits
	EtaMs      int64   `json:"etaMs,omitempty"`     // while running, from Speed
	ElapsedMs  int64   `json:"elapsedMs,omitempty"` // stamped at finish
	StartedAt  int64   `json:"startedAt"`           // unix millis
	EndedAt    int64   `json:"endedAt,omitempty"`
	Error      string  `json:"error,omitempty"`
	ErrorKind  string  `json:"errorKind,omitempty"` // "timeout" | ""
	Stalled    bool    `json:"stalled"`             // merged transfer rows only
	Move       bool    `json:"move,omitempty"`      // merged transfer rows: copy-then-delete
}

// taskHandle is one registered task.
type taskHandle struct {
	mu         sync.Mutex
	info       TaskInfo
	reg        *taskRegistry
	ctx        context.Context
	cancel     context.CancelFunc
	finishing  bool
	start      time.Time
	lastEm     time.Time // last emit (throttle window)
	lastEmAt   time.Time // last speed sample point
	lastEmDone int       // DoneUnits at the last speed sample
}

// taskRegistry owns all tasks in insertion order (jobManager's shape).
type taskRegistry struct {
	ctx    context.Context
	mu     sync.Mutex
	all    []*taskHandle
	seq    int
	notify func() // fires EventTasksUpdate (App installs the closure)
}

func newTaskRegistry() *taskRegistry { return &taskRegistry{} }

func (r *taskRegistry) setContext(ctx context.Context) {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
}

func (r *taskRegistry) changed() {
	if r.notify != nil {
		r.notify()
	}
}

// add registers a task under a generated ID ("task-N") and derives its
// cancellable context. Totals arrive later via setTotal (count-then-act).
func (r *taskRegistry) add(kind, label string) *taskHandle {
	r.mu.Lock()
	r.seq++
	h := r.newHandle(fmt.Sprintf("task-%d", r.seq), kind, label)
	r.mu.Unlock()
	r.changed()
	return h
}

// addWithID registers a task under a caller-chosen ID (deep searches and
// listing streams use their stream token, so their own cancel endpoints
// and CancelTask agree on one ID).
func (r *taskRegistry) addWithID(id, kind, label string) *taskHandle {
	r.mu.Lock()
	h := r.newHandle(id, kind, label)
	r.mu.Unlock()
	r.changed()
	return h
}

// newHandle builds and stores a running task. Callers hold r.mu.
func (r *taskRegistry) newHandle(id, kind, label string) *taskHandle {
	now := time.Now()
	h := &taskHandle{info: TaskInfo{
		ID: id, Kind: kind, Label: label,
		Status:    TaskRunning,
		StartedAt: now.UnixMilli(),
	}}
	base := r.ctx
	if base == nil {
		base = context.Background()
	}
	h.ctx, h.cancel = context.WithCancel(base)
	h.reg = r
	h.start = now
	h.lastEm = now
	h.lastEmAt = now
	r.all = append(r.all, h)
	// The heartbeat (jobHandle's): engine callbacks only fire when a unit
	// completes — one slow batch, a hung socket or a count-phase walk
	// would otherwise freeze the row at its last snapshot. The ticker
	// keeps emitting (speed decays, ETA moves, elapsed lives) until the
	// task leaves running.
	go h.heartbeat()
	return h
}

// heartbeat re-emits a running task ~4x/second. It exits as soon as the
// task finishes (finish is the only status transition, and it
// happens-before this read under h.mu).
func (h *taskHandle) heartbeat() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		h.mu.Lock()
		live := h.info.Status == TaskRunning || h.info.Status == TaskQueued
		h.mu.Unlock()
		if !live {
			return
		}
		h.emit(false)
	}
}

func (r *taskRegistry) snapshot() []TaskInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TaskInfo, 0, len(r.all))
	for _, h := range r.all {
		h.mu.Lock()
		out = append(out, h.info)
		h.mu.Unlock()
	}
	return out
}

// clearFinished retires finished tasks: every one when ids is nil (the
// classic "clear all finished"), otherwise only the named ones — and
// only if they really are finished. The Running tasks window passes the
// ids it can actually see, so rows hidden as pre-open history survive.
func (r *taskRegistry) clearFinished(ids []string) {
	var only map[string]bool
	if ids != nil {
		only = make(map[string]bool, len(ids))
		for _, id := range ids {
			only[id] = true
		}
	}
	r.mu.Lock()
	kept := r.all[:0]
	for _, h := range r.all {
		h.mu.Lock()
		fin := h.info.Status != TaskRunning && h.info.Status != TaskQueued
		id := h.info.ID
		h.mu.Unlock()
		if fin && (only == nil || only[id]) {
			continue
		}
		kept = append(kept, h)
	}
	r.all = kept
	r.mu.Unlock()
	r.changed()
}

func (r *taskRegistry) cancel(id string) bool {
	r.mu.Lock()
	var hit *taskHandle
	for _, h := range r.all {
		h.mu.Lock()
		match := h.info.ID == id && (h.info.Status == TaskRunning || h.info.Status == TaskQueued)
		h.mu.Unlock()
		if match {
			hit = h
			break
		}
	}
	r.mu.Unlock()
	if hit == nil {
		return false
	}
	hit.cancel()
	return true
}

func (r *taskRegistry) cancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.all {
		h.mu.Lock()
		running := h.info.Status == TaskRunning || h.info.Status == TaskQueued
		h.mu.Unlock()
		if running {
			h.cancel()
		}
	}
}

// drop removes a transient task outright (listings: navigation noise
// should not pile up as finished rows). Callers hold no locks.
func (r *taskRegistry) drop(h *taskHandle) {
	r.mu.Lock()
	kept := r.all[:0]
	for _, x := range r.all {
		if x != h {
			kept = append(kept, x)
		}
	}
	r.all = kept
	r.mu.Unlock()
	r.changed()
}

// setTotal refines the units after a count phase (count-then-act) —
// the count phase is over the moment a total exists.
func (h *taskHandle) setTotal(total int, label string) {
	h.mu.Lock()
	h.info.TotalUnits = total
	h.info.Phase = ""
	h.info.EtaMs = 0
	if label != "" {
		h.info.Label = label
	}
	h.mu.Unlock()
	h.reg.changed()
}

// setPhase announces a phase change ("count" while walking/counting,
// "cleanup" while a move deletes its sources); forced through the
// throttle — phase transitions are rare and must show immediately.
func (h *taskHandle) setPhase(phase string) {
	h.mu.Lock()
	h.info.Phase = phase
	h.mu.Unlock()
	h.emit(true)
}

// setCurrent names the item in flight (the row's "now working on" line).
func (h *taskHandle) setCurrent(cur string) {
	h.mu.Lock()
	h.info.Current = cur
	h.mu.Unlock()
	h.emit(false)
}

// progress advances the done counter and emits (throttled): a unit moved,
// so the row, its EMA speed and its ETA must all move with it.
func (h *taskHandle) progress(done int) {
	h.mu.Lock()
	h.info.DoneUnits = done
	h.mu.Unlock()
	h.emit(false)
}

// emit notifies the frontend (throttled unless forced) and keeps the
// derived numbers honest: an EMA speed over the units completed since the
// previous emit, and the ETA those two imply. This is the ONLY path that
// computes them, so the heartbeat keeps a quiet task's row alive simply
// by calling emit (the speed visibly decays toward zero on a stall).
func (h *taskHandle) emit(force bool) {
	h.mu.Lock()
	now := time.Now()
	if !force && now.Sub(h.lastEm) < emitInterval {
		h.mu.Unlock()
		return
	}
	h.lastEm = now
	info := h.info
	if dt := now.Sub(h.lastEmAt).Seconds(); dt > 0.05 {
		inst := float64(info.DoneUnits-h.lastEmDone) / dt
		if inst < 0 {
			inst = 0
		}
		const w = 0.35
		if info.Speed <= 0 || h.lastEmDone == 0 {
			info.Speed = inst
		} else {
			info.Speed = (1-w)*info.Speed + w*inst
		}
		h.info.Speed = info.Speed
		h.lastEmDone = info.DoneUnits
		h.lastEmAt = now
	}
	if info.Status == TaskRunning && info.TotalUnits > 0 &&
		info.TotalUnits > info.DoneUnits && info.Speed > 0.05 {
		h.info.EtaMs = int64(float64(info.TotalUnits-info.DoneUnits) / info.Speed * 1000)
	} else {
		h.info.EtaMs = 0
	}
	h.mu.Unlock()
	h.reg.changed()
}

// finish stamps the outcome: a context already canceled before
// completion records "canceled" whatever the engine error says (the
// cancel() here is our own cleanup and must not count). transient drops
// the row instead (listings). Elapsed and the lifetime-average speed are
// stamped here; timeouts are classified so the row can shout them.
func (h *taskHandle) finish(err error, transient bool) {
	h.mu.Lock()
	if h.finishing {
		h.mu.Unlock()
		return
	}
	h.finishing = true
	canceled := h.ctx.Err() != nil
	h.cancel()
	ended := time.Now()
	h.info.EndedAt = ended.UnixMilli()
	elapsed := ended.Sub(h.start)
	h.info.ElapsedMs = elapsed.Milliseconds()
	h.info.Phase = ""
	h.info.Current = ""
	h.info.EtaMs = 0
	h.info.ErrorKind = ""
	switch {
	case canceled:
		h.info.Status = TaskCanceled
		h.info.Error = "canceled"
	case err != nil:
		h.info.Status = TaskError
		h.info.Error = err.Error()
		h.info.ErrorKind = timeoutKind(err.Error())
	default:
		h.info.Status = TaskDone
		h.info.Error = ""
	}
	if secs := elapsed.Seconds(); secs > 0.05 {
		h.info.Speed = float64(h.info.DoneUnits) / secs
	}
	h.mu.Unlock()
	if transient {
		h.reg.drop(h)
	} else {
		h.reg.changed()
	}
}

// RunningTasks merges the transfer jobs with every other tracked task
// for the Running-tasks window (newest last; the view sorts).
func (a *App) RunningTasks() []TaskInfo {
	out := []TaskInfo{}
	for _, j := range a.jobs.snapshot() {
		if j.Hidden { // internal staging (drag-out scratch download) — invisible to every UI
			continue
		}
		label := j.Name
		if label == "" {
			label = j.CurrentFile
		}
		if label == "" {
			label = j.ID
		}
		if j.Items > 1 {
			label = fmt.Sprintf("%s +%d", label, j.Items-1)
		}
		out = append(out, TaskInfo{
			ID: j.ID, Kind: j.Op, Label: label, Status: j.Status,
			DoneUnits:  j.DoneFiles + j.FailedFiles + j.SkippedFiles,
			TotalUnits: j.TotalFiles, StartedAt: j.StartedAt, Error: j.Error,
			// the full-picture fields ride along so the merged rows show
			// the same chips, speed, ETA and current item as the
			// transfer window
			Phase: j.Phase, Current: j.CurrentFile, Speed: j.SpeedBps,
			EtaMs: j.EtaMs, ElapsedMs: j.ElapsedMs, ErrorKind: j.ErrorKind,
			Stalled: j.Stalled, Move: j.Move,
		})
	}
	out = append(out, a.tasks.snapshot()...)
	return out
}

// ClearFinishedTasks prunes done/error/canceled rows in both managers:
// all of them when ids is null, otherwise only the named ones (running
// work is never touched).
func (a *App) ClearFinishedTasks(ids []string) {
	a.jobs.clearFinished(ids)
	a.tasks.clearFinished(ids)
}

// CancelTask cancels any running task by ID: a transfer job, a tracked
// engine op, a deep search or a listing stream. Unknown IDs are a no-op.
func (a *App) CancelTask(id string) bool {
	if a.jobs.cancel(id) {
		return true
	}
	if a.tasks.cancel(id) {
		return true
	}
	a.searchMu.Lock()
	_, searching := a.searches[id]
	a.searchMu.Unlock()
	if searching {
		a.CancelSearch(id)
		return true
	}
	a.streamMu.Lock()
	_, listing := a.streams[id]
	a.streamMu.Unlock()
	if listing {
		a.CancelList(id)
		return true
	}
	return false
}

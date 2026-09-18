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

// TaskInfo is the Running-tasks view model (event payload and list row).
type TaskInfo struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // transfer|search|list|delete|purge|empty|convert|copy
	Label      string `json:"label"`
	Status     string `json:"status"`
	DoneUnits  int    `json:"doneUnits"`
	TotalUnits int    `json:"totalUnits"` // 0 = unknown
	StartedAt  int64  `json:"startedAt"`  // unix millis
	EndedAt    int64  `json:"endedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

// taskHandle is one registered task.
type taskHandle struct {
	mu        sync.Mutex
	info      TaskInfo
	reg       *taskRegistry
	ctx       context.Context
	cancel    context.CancelFunc
	finishing bool
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
	h := &taskHandle{info: TaskInfo{
		ID: id, Kind: kind, Label: label,
		Status:    TaskRunning,
		StartedAt: time.Now().UnixMilli(),
	}}
	base := r.ctx
	if base == nil {
		base = context.Background()
	}
	h.ctx, h.cancel = context.WithCancel(base)
	h.reg = r
	r.all = append(r.all, h)
	return h
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

// setTotal refines the units after a count phase (count-then-act).
func (h *taskHandle) setTotal(total int, label string) {
	h.mu.Lock()
	h.info.TotalUnits = total
	if label != "" {
		h.info.Label = label
	}
	h.mu.Unlock()
	h.reg.changed()
}

// progress advances the done counter without emitting (snapshots poll).
func (h *taskHandle) progress(done int) {
	h.mu.Lock()
	h.info.DoneUnits = done
	h.mu.Unlock()
}

// finish stamps the outcome: a context already canceled before
// completion records "canceled" whatever the engine error says (the
// cancel() here is our own cleanup and must not count). transient drops
// the row instead (listings).
func (h *taskHandle) finish(err error, transient bool) {
	h.mu.Lock()
	if h.finishing {
		h.mu.Unlock()
		return
	}
	h.finishing = true
	canceled := h.ctx.Err() != nil
	h.cancel()
	h.info.EndedAt = time.Now().UnixMilli()
	switch {
	case canceled:
		h.info.Status = TaskCanceled
		h.info.Error = "canceled"
	case err != nil:
		h.info.Status = TaskError
		h.info.Error = err.Error()
	default:
		h.info.Status = TaskDone
		h.info.Error = ""
	}
	h.mu.Unlock()
	if transient {
		h.reg.drop(h)
	} else {
		h.reg.changed()
	}
}

// beginTask wraps one long engine op as a tracked task: the context
// carries cancellation (no quick-op deadline — big batches would trip
// it; the Running-tasks window is the guard now), and done() must be
// called exactly once with the op's final error (defer).
func (a *App) beginTask(kind, label string) (context.Context, func(error)) {
	h := a.tasks.add(kind, label)
	return h.ctx, func(err error) { h.finish(err, false) }
}

// RunningTasks merges the transfer jobs with every other tracked task
// for the Running-tasks window (newest last; the view sorts).
func (a *App) RunningTasks() []TaskInfo {
	out := []TaskInfo{}
	for _, j := range a.jobs.snapshot() {
		label := j.CurrentFile
		if label == "" {
			label = j.ID
		}
		out = append(out, TaskInfo{
			ID: j.ID, Kind: j.Op, Label: label, Status: j.Status,
			DoneUnits:  j.DoneFiles + j.FailedFiles + j.SkippedFiles,
			TotalUnits: j.TotalFiles, StartedAt: j.StartedAt, Error: j.Error,
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

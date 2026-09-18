package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskRegistryLifecycle(t *testing.T) {
	a := newTestApp(t)
	h := a.tasks.add("delete", "s3://b — 3 item(s)")
	if got := h.info.ID; got != "task-1" {
		t.Fatalf("first task ID = %q, want task-1", got)
	}

	// count-then-act: the label and total refine mid-flight
	h.setTotal(7, "s3://b — deleting 7 object(s)")
	h.progress(7)
	snap := a.tasks.snapshot()
	if len(snap) != 1 || snap[0].Status != TaskRunning || snap[0].TotalUnits != 7 || snap[0].DoneUnits != 7 {
		t.Fatalf("running snapshot = %+v", snap)
	}

	// cancel hits a running task; the canceled context wins over the
	// engine error in finish()
	if !a.tasks.cancel("task-1") {
		t.Fatal("cancel missed a running task")
	}
	select {
	case <-h.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("task context never canceled")
	}
	h.finish(errors.New("boom"), false)
	snap = a.tasks.snapshot()
	if snap[0].Status != TaskCanceled || snap[0].Error != "canceled" {
		t.Fatalf("canceled snapshot = %+v", snap)
	}

	// clearFinished prunes it; a second finish is a no-op
	a.tasks.clearFinished(nil)
	if got := len(a.tasks.snapshot()); got != 0 {
		t.Fatalf("clearFinished left %d task(s)", got)
	}

	// id-scoped clear mirrors the job manager: only named finished
	// tasks retire; an empty list retires nothing; running work stays
	h1 := a.tasks.add("purge", "p1")
	h2 := a.tasks.add("purge", "p2")
	h3 := a.tasks.add("purge", "p3")
	h1.finish(nil, false)
	h2.finish(nil, false)
	a.tasks.clearFinished([]string{h1.info.ID})
	snap = a.tasks.snapshot()
	if len(snap) != 2 || snap[0].ID != h2.info.ID || snap[1].ID != h3.info.ID {
		t.Fatalf("scoped clear left %+v", snap)
	}
	a.tasks.clearFinished([]string{})
	if len(a.tasks.snapshot()) != 2 {
		t.Fatal("empty id list must clear nothing")
	}
}

func TestTaskFinishMapsErrorAndDone(t *testing.T) {
	a := newTestApp(t)

	hErr := a.tasks.add("convert", "c1")
	hErr.finish(errors.New("nope"), false)
	hOK := a.tasks.add("convert", "c2")
	hOK.finish(nil, false)

	snap := a.tasks.snapshot()
	if snap[0].Status != TaskError || snap[0].Error != "nope" {
		t.Fatalf("error task = %+v", snap[0])
	}
	if snap[1].Status != TaskDone || snap[1].Error != "" || snap[1].EndedAt < snap[1].StartedAt {
		t.Fatalf("done task = %+v", snap[1])
	}

	// canceling a finished task must miss
	if a.tasks.cancel(snap[0].ID) {
		t.Fatal("cancel hit a finished task")
	}
}

func TestTransientTaskDroppedOnFinish(t *testing.T) {
	a := newTestApp(t)
	h := a.tasks.addWithID("l123", "list", "s3://b/p/")
	if got := a.tasks.snapshot(); len(got) != 1 || got[0].Kind != "list" {
		t.Fatalf("transient task not listed while running: %+v", got)
	}
	h.finish(nil, true)
	if got := len(a.tasks.snapshot()); got != 0 {
		t.Fatalf("transient task survived finish: %d left", got)
	}
}

func TestRunningTasksMergesJobsAndTasks(t *testing.T) {
	a := newTestApp(t)
	a.jobs.setContext(context.Background())
	j := a.jobs.add("upload", 3, 100)
	j.fileDone(30, false)
	h := a.tasks.add("search", `"x" — s3://b/`)

	got := a.RunningTasks()
	if len(got) != 2 {
		t.Fatalf("RunningTasks = %+v, want one job + one task", got)
	}
	job, task := got[0], got[1]
	if job.ID != j.info.ID || job.Kind != "upload" || job.Status != JobRunning || job.DoneUnits != 1 || job.TotalUnits != 3 {
		t.Fatalf("merged job = %+v", job)
	}
	if task.ID != h.info.ID || task.Kind != "search" || task.Label != `"x" — s3://b/` {
		t.Fatalf("merged task = %+v", task)
	}

	a.ClearFinishedTasks(nil) // nothing finished yet: both stay
	if len(a.RunningTasks()) != 2 {
		t.Fatal("ClearFinishedTasks pruned running work")
	}
}

func TestCancelTaskDispatch(t *testing.T) {
	a := newTestApp(t)

	// registry task
	h := a.tasks.add("purge", "p")
	if !a.CancelTask(h.info.ID) {
		t.Fatal("CancelTask missed a registry task")
	}
	select {
	case <-h.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("registry task never canceled")
	}

	// deep-search token: CancelTask routes to the search canceler
	searchCanceled := make(chan struct{})
	a.searchMu.Lock()
	a.searches["s42"] = func() { close(searchCanceled) }
	a.searchMu.Unlock()
	if !a.CancelTask("s42") {
		t.Fatal("CancelTask missed a search token")
	}
	select {
	case <-searchCanceled:
	case <-time.After(time.Second):
		t.Fatal("search cancel never invoked")
	}

	// listing token: same dispatch
	listCanceled := make(chan struct{})
	a.streamMu.Lock()
	a.streams["l42"] = func() { close(listCanceled) }
	a.streamMu.Unlock()
	if !a.CancelTask("l42") {
		t.Fatal("CancelTask missed a list token")
	}
	select {
	case <-listCanceled:
	case <-time.After(time.Second):
		t.Fatal("list cancel never invoked")
	}

	if a.CancelTask("does-not-exist") {
		t.Fatal("CancelTask claimed an unknown ID")
	}
}

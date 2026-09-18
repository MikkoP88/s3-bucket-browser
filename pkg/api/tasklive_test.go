// tasklive_test.go pins the full-picture task-row contract: live unit
// state and events from progress(), the count-then-act phase transitions,
// the EMA speed and the ETA it implies, and the finish stamps (elapsed,
// lifetime speed, timeout classification) plus the Running-tasks
// full-field merge of transfer rows.
package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskProgressFeedsLiveStateAndEmits(t *testing.T) {
	a := newTestApp(t)
	var emits atomic.Int32
	a.tasks.notify = func() { emits.Add(1) }

	h := a.tasks.add("delete", "s3://b — 3 item(s)")
	// count-then-act: the count phase is announced and forced through
	// the throttle — the row says "Counting" immediately.
	h.setPhase(TaskPhaseCount)
	if got := a.tasks.snapshot()[0].Phase; got != TaskPhaseCount {
		t.Fatalf("phase after setPhase = %q, want %q", got, TaskPhaseCount)
	}
	if emits.Load() == 0 {
		t.Fatal("setPhase never emitted")
	}

	h.setTotal(120, "s3://b — deleting 120 object(s)")
	h.setCurrent("photos/2024/")
	h.progress(60)
	snap := a.tasks.snapshot()[0]
	if snap.TotalUnits != 120 || snap.DoneUnits != 60 || snap.Phase != "" {
		t.Fatalf("live state after progress = %+v", snap)
	}
	if snap.Current != "photos/2024/" {
		t.Fatalf("current item = %q", snap.Current)
	}

	// The 0%→100% bug: progress must EMIT, not just store. The first
	// emit inside the throttle window is coalesced (the heartbeat keeps
	// the row alive in production); one outside it fires.
	h.mu.Lock()
	h.lastEm = time.Now().Add(-emitInterval - time.Millisecond)
	h.mu.Unlock()
	before := emits.Load()
	h.progress(90)
	if emits.Load() <= before {
		t.Fatal("progress outside the throttle window did not emit")
	}
	if got := a.tasks.snapshot()[0].DoneUnits; got != 90 {
		t.Fatalf("DoneUnits = %d, want 90", got)
	}
}

func TestTaskSpeedAndEtaMath(t *testing.T) {
	a := newTestApp(t)
	h := a.tasks.add("convert", "s3://b — convert")
	h.setTotal(100, "")
	h.progress(10)

	// One sample: 10 units over ~1s → ~10 units/s, so 80 remaining
	// imply ~8s. The sample point and the previous baseline are rewound
	// by hand (the heartbeat does this by just ticking).
	h.mu.Lock()
	h.lastEm = time.Now().Add(-emitInterval - time.Millisecond)
	h.lastEmAt = time.Now().Add(-time.Second)
	h.lastEmDone = 10
	h.mu.Unlock()
	h.progress(20)

	snap := a.tasks.snapshot()[0]
	if snap.Speed < 9 || snap.Speed > 11 {
		t.Fatalf("speed = %.2f units/s, want ~10", snap.Speed)
	}
	if snap.EtaMs < 7000 || snap.EtaMs > 9000 {
		t.Fatalf("etaMs = %d, want ~8000", snap.EtaMs)
	}

	// finish swaps ETA for elapsed and the lifetime-average speed
	h.finish(nil, false)
	snap = a.tasks.snapshot()[0]
	if snap.EtaMs != 0 || snap.ElapsedMs < 0 || snap.Status != TaskDone {
		t.Fatalf("finished row = %+v", snap)
	}
	if snap.Speed < 0 {
		t.Fatalf("lifetime speed = %.2f", snap.Speed)
	}
}

func TestTaskFinishClassifiesTimeoutAndStamps(t *testing.T) {
	a := newTestApp(t)
	h := a.tasks.add("delete", "s3://b — delete")
	h.setTotal(9, "")
	h.setCurrent("k1")
	h.progress(3)
	h.finish(errors.New("delete tcp: i/o timeout"), false)

	snap := a.tasks.snapshot()[0]
	if snap.Status != TaskError || snap.ErrorKind != "timeout" {
		t.Fatalf("timeout classification = %+v", snap)
	}
	if snap.Phase != "" || snap.Current != "" || snap.EtaMs != 0 {
		t.Fatalf("in-flight fields survived finish: %+v", snap)
	}
	if snap.ElapsedMs < 0 || snap.EndedAt < snap.StartedAt {
		t.Fatalf("finish stamps missing: %+v", snap)
	}
}

func TestRunningTasksMergesJobFullPicture(t *testing.T) {
	a := newTestApp(t)
	a.jobs.setContext(context.Background())

	// finished job: the timeout flag and elapsed ride the merged row
	j1 := a.jobs.add("download", 1, 10)
	j1.startFile(1, "a.txt", 10)
	a.finishJob(j1, JobError, "a.txt: read tcp: i/o timeout")

	// running job: phase, current item, speed and ETA flow through
	j2 := a.jobs.add("download", 2, 100)
	j2.setMeta("docs", "s3://a", "s3://b", 2, true)
	j2.startFile(1, "docs/a.txt", 100)
	j2.progress(50, 100)
	j2.mu.Lock()
	j2.info.SpeedBps = 2048
	j2.info.EtaMs = 25000
	j2.mu.Unlock()

	got := a.RunningTasks()
	if len(got) != 2 {
		t.Fatalf("RunningTasks = %+v, want two merged rows", got)
	}
	// insertion order, not status order — the frontend sorts
	fin, run := got[0], got[1]
	if run.Status != JobRunning || run.Phase != PhaseTransfer || run.Current != "docs/a.txt" {
		t.Fatalf("running merged row = %+v", run)
	}
	if run.Speed != 2048 || run.EtaMs != 25000 {
		t.Fatalf("speed/ETA passthrough = %+v", run)
	}
	if fin.Status != JobError || fin.ErrorKind != "timeout" || fin.ElapsedMs < 0 {
		t.Fatalf("finished merged row = %+v", fin)
	}
}

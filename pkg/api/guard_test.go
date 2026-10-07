// guard_test.go: pins the worker panic net (guard.go) — a panicking
// worker settles its job/task and leaves one readable line behind
// instead of taking the process (and every in-flight transfer) down.
package api

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

func TestGuardJobSettlesPanickingWorker(t *testing.T) {
	a := newTestApp(t)
	j := a.jobs.add("upload", 2, 100)
	j.src = "bucket-a"

	// The worker's shape: guard registered first, panic mid-flight.
	func() {
		defer a.guardJob("upload", j)
		panic("mid-transfer boom")
	}()

	j.mu.Lock()
	status, errMsg := j.info.Status, j.info.Error
	j.mu.Unlock()
	if status != JobError {
		t.Fatalf("status = %q, want %q", status, JobError)
	}
	if !strings.HasPrefix(errMsg, "internal error: mid-transfer boom") {
		t.Fatalf("error = %q, want internal error prefix", errMsg)
	}
}

func TestGuardJobKeepsFirstSettle(t *testing.T) {
	// A worker that already finished (cancel path) and then panics in a
	// deferred cleanup must not resurrect the row: finish is idempotent,
	// so the guard's settle is a no-op and the first outcome stands.
	a := newTestApp(t)
	j := a.jobs.add("download", 1, 10)
	a.finishJob(j, JobDone, "")
	func() {
		defer a.guardJob("download", j)
		panic("late cleanup boom")
	}()
	j.mu.Lock()
	status, errMsg := j.info.Status, j.info.Error
	j.mu.Unlock()
	if status != JobDone || errMsg != "" {
		t.Fatalf("late panic rewrote the outcome: %q %q", status, errMsg)
	}
}

func TestGuardWorkerSettleReceivesError(t *testing.T) {
	a := newTestApp(t)

	var mu sync.Mutex
	var got []error
	settle := func(err error) {
		mu.Lock()
		got = append(got, err)
		mu.Unlock()
	}

	func() {
		defer a.guardWorker("search", settle)
		panic(errors.New("walk blew up"))
	}()
	func() {
		defer a.guardWorker("list", settle)
		panic("string panic") // non-error panics normalize
	}()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("settle called %d times, want 2", len(got))
	}
	if got[0] == nil || got[0].Error() != "walk blew up" {
		t.Errorf("error panic: settle got %v", got[0])
	}
	if got[1] == nil || got[1].Error() != "string panic" {
		t.Errorf("string panic: settle got %v", got[1])
	}
}

func TestGuardWorkerNoPanicIsNoop(t *testing.T) {
	a := newTestApp(t)
	called := false
	a.guardWorker("app", func(error) { called = true }) // returns without unwinding
	if called {
		t.Error("settle called without a panic")
	}
}

func TestPanicReportCarriesStack(t *testing.T) {
	rep := panicReport(errors.New("boom"))
	if !strings.HasPrefix(rep, "internal error (recovered): boom") {
		t.Fatalf("prefix missing: %q", rep)
	}
	if !strings.Contains(rep, "goroutine") {
		t.Error("stack trace missing from the report")
	}
}

func TestReportPanicLogsAndCapsTrace(t *testing.T) {
	a := newTestApp(t)
	if err := eventlog.SaveSettings(eventlog.Settings{Mode: "default"}); err != nil {
		t.Fatal(err)
	}

	// A nil error is silence — the framework can fire the handler for a
	// nil panic value and that must not log a phantom line.
	base, _ := eventlog.Tail(0, "error", "")
	a.ReportPanic(nil, "trace")
	if lines, _ := eventlog.Tail(0, "error", ""); len(lines) != len(base) {
		t.Fatalf("nil error appended %d line(s)", len(lines)-len(base))
	}

	// A normal short trace rides along verbatim.
	a.ReportPanic(errors.New("boom"), "goroutine 1 [running]:\nshort")
	last, _ := eventlog.Tail(1, "error", "")
	if len(last) != 1 {
		t.Fatal("no line appended for the short-trace panic")
	}
	if got := last[0]; got.Scope != "app" || !strings.HasPrefix(got.Message, "internal error (recovered): boom\ngoroutine") {
		t.Fatalf("mangled line: %+v", got)
	}

	// An oversized trace is capped at panicTraceMax with an ellipsis.
	a.ReportPanic(errors.New("boom"), strings.Repeat("a", panicTraceMax+500))
	last, _ = eventlog.Tail(1, "error", "")
	if len(last) != 1 {
		t.Fatal("no line appended for the capped panic")
	}
	want := "internal error (recovered): boom\n" + strings.Repeat("a", panicTraceMax) + "…"
	if last[0].Message != want {
		t.Fatalf("trace not capped: %d bytes, want %d", len(last[0].Message), len(want))
	}
}

func TestHeartbeatRecoverReportsThroughHook(t *testing.T) {
	// Both registries route the report through the App hook when one is
	// installed (the log drawer sees it)…
	jm := newJobManager()
	var jobLine string
	jm.onPanic = func(p string) { jobLine = p }
	func() {
		defer jm.recoverHeartbeat()
		panic("job heartbeat boom")
	}()
	if !strings.Contains(jobLine, "internal error (recovered): job heartbeat boom") {
		t.Fatalf("job hook line = %q", jobLine)
	}

	tr := newTaskRegistry()
	var taskLine string
	tr.onPanic = func(p string) { taskLine = p }
	func() {
		defer tr.recoverHeartbeat()
		panic("task heartbeat boom")
	}()
	if !strings.Contains(taskLine, "internal error (recovered): task heartbeat boom") {
		t.Fatalf("task hook line = %q", taskLine)
	}
}

func TestHeartbeatRecoverFallsBackToEventlog(t *testing.T) {
	// …and a bare registry (unit tests, pre-New shells) still lands the
	// line in the persisted event log instead of dying.
	newTestApp(t) // S3B_CONFIG → temp dir
	if err := eventlog.SaveSettings(eventlog.Settings{Mode: "default"}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer newJobManager().recoverHeartbeat()
		panic("orphan heartbeat boom")
	}()
	last, _ := eventlog.Tail(1, "error", "")
	if len(last) != 1 || !strings.Contains(last[0].Message, "orphan heartbeat boom") {
		t.Fatalf("fallback line missing: %+v", last)
	}
}

package api

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// The worker panic net. Wails recovers panics in bound methods (the
// frontend gets the call's error result and the process lives), but the
// goroutines the app itself spawns — listing streams, deep searches,
// transfer workers, the drag stage, the editor watcher — had no net:
// one panic anywhere in them took the whole desktop process and every
// in-flight transfer with it, and whatever the spawn was driving froze
// at "running" waiting for a final event that never came. guardWorker
// and guardJob convert a panic into the same shape every other failure
// already rides: one event-log line carrying the message and the trace
// (log drawer, `s3b log`, the opt-in file log), the task or job settled
// as failed, and the process alive to finish everything else.

// panicTraceMax bounds the stack trace that rides the event log — a
// deep call stack can run kilobytes; the app's own frames sit at the
// top, and the cap keeps one line from flooding the drawer and the file.
const panicTraceMax = 4096

// panicErr normalizes a recovered value into an error (the settle side
// hands its caller an error, whatever the panic carried).
func panicErr(e any) error {
	if err, ok := e.(error); ok {
		return err
	}
	return fmt.Errorf("%v", e)
}

// panicReport renders one recovered panic as the event-log payload:
// message plus trace. The word "recovered" is deliberate — the line
// tells the reader the app kept running, which is exactly what
// happened and what would not have happened before the net.
func panicReport(e any) string {
	err := panicErr(e)
	trace := strings.TrimSpace(string(debug.Stack()))
	if len(trace) > panicTraceMax {
		trace = trace[:panicTraceMax] + "…"
	}
	if trace != "" {
		return fmt.Sprintf("internal error (recovered): %v\n%s", err, trace)
	}
	return fmt.Sprintf("internal error (recovered): %v", err)
}

// guardWorker is the panic net for one app-spawned worker goroutine —
// install as a defer inside the goroutine, placed after any state the
// settle closure needs is in scope (defers run last-registered-first,
// so the guard settles before the goroutine's own cleanup unwinds).
// settle, when given, lands the failure where the worker's own error
// path would have: emit the final page, finish the task, unblock the
// waiter. A nil settle is right for fire-and-forget workers whose only
// obligation is the log line.
func (a *App) guardWorker(scope string, settle func(error)) {
	e := recover()
	if e == nil {
		return
	}
	a.emitLogSrc(LogError, scope, "", panicReport(e))
	if settle != nil {
		settle(panicErr(e))
	}
}

// guardJob is guardWorker for a transfer worker: the job fails visibly
// (never freezes at "running"), the log line carries the job id and
// source beside the trace, and every other in-flight job keeps moving.
// j.src and the id are write-once before the worker spawns, so reading
// them here needs no lock.
func (a *App) guardJob(scope string, j *jobHandle) {
	e := recover()
	if e == nil {
		return
	}
	a.emitLogSrc(LogError, scope, j.src,
		fmt.Sprintf("job %s: %s", j.info.ID, panicReport(e)))
	a.finishJob(j, JobError, "internal error: "+panicErr(e).Error())
}

// ReportPanic lands a framework-level panic in the event log. The
// desktop shell registers it as the Wails PanicHandler: bound-method
// panics would otherwise sink into the discarded default logger (the
// webview shell silences it), and framework-internal ones would
// otherwise take the fatal path. The frontend already sees bound-method
// panics as the call's error result; this is the durable record.
func (a *App) ReportPanic(err error, trace string) {
	if err == nil {
		return
	}
	trace = strings.TrimSpace(trace)
	if len(trace) > panicTraceMax {
		trace = trace[:panicTraceMax] + "…"
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "internal error (recovered): %v", err)
	if trace != "" {
		fmt.Fprintf(&msg, "\n%s", trace)
	}
	a.emitLogSrc(LogError, "app", "", msg.String())
}

// logRegistryPanic is the heartbeat registries' floor (guard.go): the
// App installs an onPanic hook so the line reaches the log drawer too;
// a registry built without one (unit tests) still never re-panics —
// the persisted event log carries it alone.
func logRegistryPanic(onPanic func(string), scope string, e any) {
	if onPanic != nil {
		onPanic(panicReport(e))
		return
	}
	eventlog.Append(LogError, scope, "", panicReport(e))
}

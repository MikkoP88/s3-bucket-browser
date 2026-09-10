package api

import "testing"

// Hermetic tests for the in-app log events (log.go). emitLog itself needs a
// wails runtime context to reach the event bus; with a nil ctx (pre-Startup)
// it must simply be a no-op — that contract is asserted here rather than
// faking a context.

func TestEmitLogNilContextIsNoop(t *testing.T) {
	a := newTestApp(t) // a.ctx == nil, exactly like pre-Startup
	a.emitLog(LogInfo, "upload", "must not panic")
	a.emitLog(LogError, "delete", "nor this")
}

func TestJobStatusLevel(t *testing.T) {
	cases := map[string]string{
		JobDone:     LogInfo,
		JobCanceled: LogWarn,
		JobError:    LogError,
		"weird":     LogError, // unknown statuses never log as success
	}
	for status, want := range cases {
		if got := jobStatusLevel(status); got != want {
			t.Errorf("jobStatusLevel(%q) = %q, want %q", status, got, want)
		}
	}
}

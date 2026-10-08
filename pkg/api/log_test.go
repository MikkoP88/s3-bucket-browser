package api

import (
	"slices"
	"testing"
)

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

// The file-log scope selector's vocabulary must cover every scope the app
// emits — cleanLogFilter silently drops unlisted values, so a scope missing
// here is a line the file log can never be scoped to (the four that once
// drifted: edit, license, mkfile, security).
func TestLogScopesVocabulary(t *testing.T) {
	for _, scope := range []string{"admin", "app", "copy", "delete", "doctor",
		"download", "drag", "edit", "import", "license", "list", "mkdir",
		"mkfile", "profile", "rename", "security", "settings", "share",
		"sources", "transfer", "upload", "versions"} {
		if !slices.Contains(LogScopes, scope) {
			t.Errorf("LogScopes is missing emitted scope %q", scope)
		}
	}
	if !slices.IsSorted(LogScopes) {
		t.Errorf("LogScopes is not sorted: %v", LogScopes)
	}
}

package api

import (
	"strings"
	"testing"
)

func TestExitBusyReasonCoversRunningTasks(t *testing.T) {
	a := newTestApp(t)
	if got := a.exitBusyReason(); got != "" {
		t.Fatalf("clean app reason = %q, want empty", got)
	}

	// a running registry task refuses the exit, naming kind and label
	h := a.tasks.add("purge", "purging s3://b (noncurrent versions)")
	got := a.exitBusyReason()
	if !strings.Contains(got, "1 task(s) still running") || !strings.Contains(got, "purge") {
		t.Fatalf("task reason = %q", got)
	}

	// finished work no longer blocks; a transient listing never did
	h.finish(nil, false)
	l := a.tasks.addWithID("l7", "list", "s3://b/p/")
	if got := a.exitBusyReason(); got != "" {
		t.Fatalf("reason after finish = %q, want empty", got)
	}
	l.finish(nil, true)
}

package api

import (
	"strings"
	"testing"
	"time"
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

// A dirty edit session refuses the exit like any unsaved work: the
// watcher uploads only after the save settles, and the launch wipe
// would discard whatever was never pushed.
func TestExitBusyReasonCoversDirtyEditors(t *testing.T) {
	a := newTestApp(t)
	a.editorsMu.Lock()
	a.editors["b\x00docs/notes.md"] = &editSession{Bucket: "b", Key: "docs/notes.md", dirty: true}
	clean := &editSession{Bucket: "b", Key: "logo.png"}
	a.editors["b\x00logo.png"] = clean
	a.editorsMu.Unlock()

	got := a.exitBusyReason()
	if !strings.Contains(got, "1 edited file(s) not yet uploaded") || !strings.Contains(got, "notes.md") {
		t.Fatalf("dirty editor reason = %q", got)
	}

	// the upload landed (watcher's dirty=false): no reason left
	clean.dirty = false
	clean.mu.Lock()
	a.editors["b\x00docs/notes.md"].dirty = false
	clean.mu.Unlock()
	if got := a.exitBusyReason(); got != "" {
		t.Fatalf("reason after uploads = %q, want empty", got)
	}
}

// An active pre-signed share refuses the exit like any outstanding item:
// the URL keeps serving whoever holds it until it expires, and closing
// the app closes the Shares window that tracks it. An expired share is
// dead and never counts; stopping the share clears the reason.
func TestExitBusyReasonCoversActiveShares(t *testing.T) {
	a := newTestApp(t)
	// seat the registry's own shape directly: one live, one expired
	a.shareMu.Lock()
	a.shares["sh-1"] = &shareEntry{info: ShareInfo{
		ID: "sh-1", Kind: "presign", Bucket: "team", Key: "docs/notes.md",
	}, expires: time.Now().Add(time.Hour)}
	expired := time.Now().Add(-time.Minute)
	a.shares["sh-old"] = &shareEntry{info: ShareInfo{
		ID: "sh-old", Kind: "presign", Bucket: "team", Key: "gone.txt",
	}, expires: expired}
	a.shareMu.Unlock()

	got := a.exitBusyReason()
	if !strings.Contains(got, "1 active share(s) still working") || !strings.Contains(got, "team/docs/notes.md") {
		t.Fatalf("share reason = %q", got)
	}
	if strings.Contains(got, "gone.txt") {
		t.Fatalf("expired share named in reason = %q", got)
	}

	// stopped: nothing outstanding anymore
	a.shareMu.Lock()
	delete(a.shares, "sh-1")
	delete(a.shares, "sh-old")
	a.shareMu.Unlock()
	if got := a.exitBusyReason(); got != "" {
		t.Fatalf("reason after stop = %q, want empty", got)
	}
}

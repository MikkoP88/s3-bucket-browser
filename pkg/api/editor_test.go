package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The editor pull rides the task registry (kind "edit"): while the
// download runs the row is visible in the Running tasks window with its
// Cancel, and killing it there aborts the pull — before this, EditObject's
// download was an invisible, uncancellable bound call.
func TestEditObjectPullIsCancellableTask(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "big.bin", "staged bytes")
	release := make(chan struct{})
	released := false
	letGo := func() {
		if !released {
			released = true
			close(release)
		}
	}
	defer letGo()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket, key := splitS3Path(r.URL.Path)
		if bucket == "docs" && key == "big.bin" && r.URL.Query().Get("list-type") == "" {
			<-release // hold the pull until the test has seen and canceled the row
		}
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.mu.Lock()
	f.url = srv.URL
	f.mu.Unlock()
	if err := a.SaveSource(fakeS3Source("edits", srv.URL)); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := a.EditObject("docs", "big.bin", false)
		done <- err
	}()

	// the row appears while the pull runs
	var id string
	deadline := time.Now().Add(5 * time.Second)
	for id == "" {
		for _, tk := range a.RunningTasks() {
			if tk.Kind == "edit" && tk.Status == TaskRunning {
				id = tk.ID
			}
		}
		if id == "" {
			if time.Now().After(deadline) {
				t.Fatal("edit task never appeared in RunningTasks")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !a.CancelTask(id) {
		t.Fatalf("CancelTask(%s) = false, want true", id)
	}
	if err := <-done; err == nil {
		t.Fatal("canceled editor pull must fail the call")
	}
	// the row settles and drops (transient: the session, not the row, is
	// the pull's lasting surface)
	for _, tk := range a.RunningTasks() {
		if tk.ID == id {
			t.Fatalf("edit row %s survived its cancel", id)
		}
	}
	// the download never landed, so no session was left behind
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d session(s), want 0", n)
	}
}

// A landed pull settles its transient row (gone from the Running tasks
// window — the status-bar editor indicator carries the session from there)
// and registers the session even when the OS refuses to open an editor.
func TestEditObjectPullSettlesAndRegistersSession(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "hello edit")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("edits2", url)); err != nil {
		t.Fatal(err)
	}
	// keep the test hermetic: no real editor launches on the test machine
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	defer func() { openInEditor = oldOpen }()

	info, err := a.EditObject("docs", "notes.md", false)
	if err == nil || !strings.Contains(err.Error(), "downloaded but could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	// the pull landed: staged content matches the object
	b, rerr := os.ReadFile(info.Local)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(b) != "hello edit" {
		t.Fatalf("staged content = %q, want %q", b, "hello edit")
	}
	sessions := a.EditingFiles()
	if len(sessions) != 1 || sessions[0].Bucket != "docs" ||
		sessions[0].Key != "notes.md" || sessions[0].Dirty {
		t.Fatalf("EditingFiles = %+v, want one clean docs/notes.md session", sessions)
	}
	for _, tk := range a.RunningTasks() {
		if tk.Kind == "edit" {
			t.Fatal("edit row survived a landed pull")
		}
	}
}

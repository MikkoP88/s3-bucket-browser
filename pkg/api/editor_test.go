package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// editPushBackoff doubles per consecutive failure and caps at sixteen
// polls (a dead endpoint is retried forever, but gently).
func TestEditPushBackoff(t *testing.T) {
	old := watcherPoll
	defer func() { watcherPoll = old }()
	watcherPoll = time.Second
	for _, tc := range []struct{ fails, polls int }{
		{0, 2}, {1, 2}, {2, 4}, {3, 8}, {4, 16}, {9, 16},
	} {
		if got := editPushBackoff(tc.fails); got != watcherPoll*time.Duration(tc.polls) {
			t.Errorf("editPushBackoff(%d) = %s, want %d poll(s)", tc.fails, got, tc.polls)
		}
	}
}

// A push that keeps failing gets a voice and a gentler cadence: the
// session turns dirty AND pushFailed in the status view (the indicator's
// failing state and the dialog's warning ride it), and once the endpoint
// recovers the retry lands the edit and clears both flags.
func TestWatchEditorVoicesAndRetriesFailingPush(t *testing.T) {
	oldPoll := watcherPoll
	watcherPoll = 10 * time.Millisecond
	defer func() { watcherPoll = oldPoll }()
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	defer func() { openInEditor = oldOpen }()

	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "v1")
	var fault atomic.Bool
	fault.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket, key := splitS3Path(r.URL.Path)
		if fault.Load() && r.Method == http.MethodPut && bucket == "docs" && key == "notes.md" {
			// 403, not 5xx: the SDK retries server errors, and the test
			// wants the attempt to fail fast, not ride the retry ladder
			http.Error(w, "injected fault", http.StatusForbidden)
			return
		}
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.mu.Lock()
	f.url = srv.URL
	f.mu.Unlock()
	if err := a.SaveSource(fakeS3Source("editf", srv.URL)); err != nil {
		t.Fatal(err)
	}

	info, err := a.EditObject("docs", "notes.md", false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	// start the watcher the successful editor launch would have started
	a.editorsMu.Lock()
	s := a.editors["docs\x00notes.md"]
	a.editorsMu.Unlock()
	go a.watchEditor(s)

	// an edit lands on disk
	if err := os.WriteFile(info.Local, []byte("v2 edited content"), 0o600); err != nil {
		t.Fatal(err)
	}

	waitFor := func(what string, ok func(EditInfo) bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			sv := a.EditingFiles()
			if len(sv) == 1 && ok(sv[0]) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("session never %s: %+v", what, sv)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// the push fails: dirty and pushFailed, visibly
	waitFor("turned dirty+pushFailed", func(e EditInfo) bool { return e.Dirty && e.PushFailed })
	// the endpoint recovers: the retry lands and clears both flags
	fault.Store(false)
	waitFor("recovered", func(e EditInfo) bool { return !e.Dirty && !e.PushFailed })
	f.mu.Lock()
	got := f.objects["docs"]["notes.md"]
	f.mu.Unlock()
	if got != "v2 edited content" {
		t.Fatalf("object content = %q, want the edited bytes", got)
	}
	// end the session (the watcher exits)
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
}

// "Stop & upload" is a save request, not a discard: a failing explicit
// upload must keep the session alive (still registered, dirty, flagged —
// the watcher retries it and the exit gate keeps guarding) instead of
// destroying it and stranding the edits on a staged file the next boot's
// workspace wipe deletes. A clean second stop settles the session.
func TestStopEditUploadFailureKeepsSession(t *testing.T) {
	oldPoll := watcherPoll
	watcherPoll = 10 * time.Millisecond
	defer func() { watcherPoll = oldPoll }()
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	defer func() { openInEditor = oldOpen }()

	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "v1")
	var fault atomic.Bool
	fault.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket, key := splitS3Path(r.URL.Path)
		if fault.Load() && r.Method == http.MethodPut && bucket == "docs" && key == "notes.md" {
			// 403, not 5xx: the SDK retries server errors, and the test
			// wants the attempt to fail fast, not ride the retry ladder
			http.Error(w, "injected fault", http.StatusForbidden)
			return
		}
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.mu.Lock()
	f.url = srv.URL
	f.mu.Unlock()
	if err := a.SaveSource(fakeS3Source("editstop", srv.URL)); err != nil {
		t.Fatal(err)
	}

	info, err := a.EditObject("docs", "notes.md", false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	// start the watcher the successful editor launch would have started
	a.editorsMu.Lock()
	s := a.editors["docs\x00notes.md"]
	a.editorsMu.Unlock()
	go a.watchEditor(s)

	if err := os.WriteFile(info.Local, []byte("v2 after failed stop"), 0o600); err != nil {
		t.Fatal(err)
	}

	// the explicit save fails (endpoint down) — and must not end the session
	if err := a.StopEdit("docs", "notes.md", true); err == nil {
		t.Fatal("StopEdit with a failing upload must return the error")
	}
	sessions := a.EditingFiles()
	if len(sessions) != 1 || !sessions[0].Dirty || !sessions[0].PushFailed {
		t.Fatalf("EditingFiles = %+v, want one dirty+pushFailed survivor", sessions)
	}
	f.mu.Lock()
	got := f.objects["docs"]["notes.md"]
	f.mu.Unlock()
	if got != "v1" {
		t.Fatalf("object content = %q, want the pre-edit bytes", got)
	}

	// the endpoint recovers: the watcher's retry lands the edit
	fault.Store(false)
	deadline := time.Now().Add(3 * time.Second)
	for {
		sv := a.EditingFiles()
		if len(sv) == 1 && !sv[0].Dirty && !sv[0].PushFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session never recovered: %+v", sv)
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.mu.Lock()
	got = f.objects["docs"]["notes.md"]
	f.mu.Unlock()
	if got != "v2 after failed stop" {
		t.Fatalf("object content = %q, want the edited bytes", got)
	}

	// the clean second stop settles the session
	if err := a.StopEdit("docs", "notes.md", true); err != nil {
		t.Fatal(err)
	}
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d session(s) after a clean stop, want 0", n)
	}
}

// A settled session retires its staged file after the grace (the editor
// process may still hold it open) instead of leaving the downloaded
// bytes in the edit workspace until the next boot's wipe.
func TestStopEditRetiresStagedFile(t *testing.T) {
	oldGrace := stageRetireGrace
	stageRetireGrace = 20 * time.Millisecond
	t.Cleanup(func() { stageRetireGrace = oldGrace })
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	defer func() { openInEditor = oldOpen }()

	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "staged v1")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("editret", url)); err != nil {
		t.Fatal(err)
	}

	info, err := a.EditObject("docs", "notes.md", false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	if _, err := os.Stat(info.Local); err != nil {
		t.Fatal(err)
	}
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d session(s) after stop, want 0", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(info.Local); os.IsNotExist(err) {
			return // retired
		}
		if time.Now().After(deadline) {
			t.Fatalf("staged file %s survived its retire grace", info.Local)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A second Edit on a live session re-focuses it: the staged file keeps
// its pending edits (a re-download would write the REMOTE bytes over
// them), no second watcher stacks on the file, and the call answers
// with the same session.
func TestEditObjectRefocusesExistingSession(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "remote v1")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("editrf", url)); err != nil {
		t.Fatal(err)
	}
	opened := ""
	oldOpen := openInEditor
	defer func() { openInEditor = oldOpen }()
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }

	info1, err := a.EditObject("docs", "notes.md", false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	// an edit lands on disk and is NOT yet uploaded (the pending edit a
	// re-download would destroy)
	if err := os.WriteFile(info1.Local, []byte("local v2 pending"), 0o600); err != nil {
		t.Fatal(err)
	}

	// the second Edit re-focuses: the editor opens on the SAME staged
	// file, whose pending edit survives
	openInEditor = func(_ *App, path string, _ bool) error { opened = path; return nil }
	info2, err := a.EditObject("docs", "notes.md", false)
	if err != nil {
		t.Fatalf("second EditObject err = %v, want a clean re-focus", err)
	}
	if opened != info1.Local {
		t.Fatalf("re-focus opened %q, want the existing staged file %q", opened, info1.Local)
	}
	if info2.Local != info1.Local {
		t.Fatalf("second EditObject local = %q, want the same session (%q)", info2.Local, info1.Local)
	}
	b, rerr := os.ReadFile(info1.Local)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(b) != "local v2 pending" {
		t.Fatalf("staged content = %q, want the pending edit (no re-download)", b)
	}
	if n := len(a.EditingFiles()); n != 1 {
		t.Fatalf("EditingFiles = %d session(s), want the one re-focused session", n)
	}
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
}

// A watcher whose staged file is deleted ends the session in the
// registry too — a zombie entry would pin the indicator (and, when
// dirty, the exit gate) on a file that no longer exists.
func TestWatcherEndsSessionWhenStagedFileRemoved(t *testing.T) {
	oldPoll := watcherPoll
	watcherPoll = 10 * time.Millisecond
	defer func() { watcherPoll = oldPoll }()
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	defer func() { openInEditor = oldOpen }()

	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "gone.md", "v1")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("editrm", url)); err != nil {
		t.Fatal(err)
	}

	info, err := a.EditObject("docs", "gone.md", false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	a.editorsMu.Lock()
	s := a.editors["docs\x00gone.md"]
	a.editorsMu.Unlock()
	go a.watchEditor(s)

	if err := os.Remove(info.Local); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if n := len(a.EditingFiles()); n == 0 {
			return // the session ended with its file
		}
		if time.Now().After(deadline) {
			t.Fatalf("zombie session survived its staged file's removal: %+v", a.EditingFiles())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A traversal-shaped key (legal in S3, hostile from a bad endpoint)
// downloads INSIDE the edit workspace — the pull must never resolve
// ".." out of it.
func TestEditObjectContainsTraversalKeys(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	const key = "../../escape.txt"
	f.seed("docs", key, "payload")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("editesc", url)); err != nil {
		t.Fatal(err)
	}
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	defer func() { openInEditor = oldOpen }()

	info, err := a.EditObject("docs", key, false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	base := editDir("docs")
	rel, rerr := filepath.Rel(base, info.Local)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("staged path %s (rel %q) escaped the edit workspace %s", info.Local, rel, base)
	}
	b, rerr := os.ReadFile(info.Local)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(b) != "payload" {
		t.Fatalf("staged content = %q, want the object bytes", b)
	}
	if err := a.StopEdit("docs", key, false); err != nil {
		t.Fatal(err)
	}
}

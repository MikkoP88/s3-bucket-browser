package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// The engine leg of the editor, ridden against the lab source (a
// TypeLocal source — the same hermetic engine the sync and compare rigs
// ride): EditFile pulls through the engine, the watcher pushes stable
// saves back, and the guard keeps a teammate's mid-edit overwrite from
// being silently clobbered — the size+mtime twin of the S3 If-Match law.

// labEditorApp builds a started app with a saved lab source whose root
// holds the given file, plus the hermetic-editor pin.
func labEditorApp(t *testing.T, name, key, content string) (*App, profile.Source, string) {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	src, root := emptyLocalSource(t, name)
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(strings.TrimPrefix(key, "/"))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, strings.TrimPrefix(key, "/")), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	t.Cleanup(func() { openInEditor = oldOpen })
	return a, src, root
}

// openLabEdit opens key of the lab source for editing and returns the
// live session (the open-editor error is the hermetic pin's own voice;
// the pull landed and the session registered).
func openLabEdit(t *testing.T, a *App, source, key string) *editSession {
	t.Helper()
	info, err := a.EditFile(EditTarget{Kind: "remote", Source: source, Key: key}, false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditFile err = %v, want the could-not-open-editor error", err)
	}
	if info.Kind != "remote" || info.Source != source || info.Key != key {
		t.Fatalf("EditFile info = %+v, want the remote %s:%s session", info, source, key)
	}
	a.editorsMu.Lock()
	defer a.editorsMu.Unlock()
	s := a.editors["remote\x00"+source+"\x00"+key]
	if s == nil {
		t.Fatal("remote session not registered")
	}
	return s
}

// The pull stages the engine file's bytes in the edit workspace under
// the source's own namespace (never colliding with a bucket's tree),
// and the watcher's auto-save pushes a stable edit back through the
// engine — the file on the engine's own disk changes.
func TestEditRemoteWatcherPushesStableSaves(t *testing.T) {
	oldPoll := watcherPoll
	watcherPoll = 10 * time.Millisecond
	defer func() { watcherPoll = oldPoll }()

	a, _, root := labEditorApp(t, "lab", "/notes.md", "engine v1")
	s := openLabEdit(t, a, "lab", "/notes.md")

	rel, err := filepath.Rel(editDir("remote", "lab"), s.Local)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("staged path %s (rel %q) escaped the remote edit workspace", s.Local, rel)
	}
	if b := read(t, s.Local); b != "engine v1" {
		t.Fatalf("staged content = %q, want the engine bytes", b)
	}

	go a.watchEditor(s)
	if err := os.WriteFile(s.Local, []byte("engine v2 auto"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if read(t, filepath.Join(root, "notes.md")) == "engine v2 auto" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auto-save never reached the engine: %q", read(t, filepath.Join(root, "notes.md")))
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, sv := range a.EditingFiles() {
		if sv.Dirty || sv.PushFailed || sv.Stale {
			t.Fatalf("session after a landed auto-save = %+v, want clean", sv)
		}
	}
	if err := a.StopEditFile(EditTarget{Kind: "remote", Source: "lab", Key: "/notes.md"}, false); err != nil {
		t.Fatal(err)
	}
}

// The guard: a teammate's overwrite of the engine file mid-edit (here
// the same size — only the mtime moved, the precise clock every engine
// reports) refuses the push with the conflict, marks the session stale,
// and keeps the teammate's bytes. force (the dialog's Push anyway)
// lands over them and rebases the baseline, so the next guarded save
// carries no false conflict — the mtime twin of the ETag rebase.
func TestEditRemoteGuardRefusesForceLandsAndRebases(t *testing.T) {
	a, _, root := labEditorApp(t, "lab", "/cfg.conf", "team A v1")
	s := openLabEdit(t, a, "lab", "/cfg.conf")

	if err := os.WriteFile(s.Local, []byte("team B v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	// the teammate lands the same-size overwrite while the edit is open
	time.Sleep(20 * time.Millisecond) // guarantee an mtime delta on every clock
	if err := os.WriteFile(filepath.Join(root, "cfg.conf"), []byte("team A v2"), 0o600); err != nil {
		t.Fatal(err)
	}

	tgt := EditTarget{Kind: "remote", Source: "lab", Key: "/cfg.conf"}
	if err := a.PushEditFile(tgt, false); !errors.Is(err, errEditConflict) {
		t.Fatalf("guarded push over a teammate's overwrite = %v, want the conflict", err)
	}
	if b := read(t, filepath.Join(root, "cfg.conf")); b != "team A v2" {
		t.Fatalf("engine content after the refused push = %q, want the teammate's bytes", b)
	}
	for _, sv := range a.EditingFiles() {
		if sv.Kind == "remote" && !sv.Stale {
			t.Fatalf("session after the refusal = %+v, want stale", sv)
		}
	}

	// Push anyway: the informed consent writes past the guard and rebases
	if err := a.PushEditFile(tgt, true); err != nil {
		t.Fatalf("PushEditFile force = %v, want nil", err)
	}
	if b := read(t, filepath.Join(root, "cfg.conf")); b != "team B v2" {
		t.Fatalf("engine content after the forced push = %q, want the edited bytes", b)
	}

	// the rebased guard: a second guarded save lands with no false conflict
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(s.Local, []byte("team B v3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.PushEditFile(tgt, false); err != nil {
		t.Fatalf("guarded push after the rebase = %v, want nil (no false conflict)", err)
	}
	if b := read(t, filepath.Join(root, "cfg.conf")); b != "team B v3" {
		t.Fatalf("engine content = %q, want the second edit's bytes", b)
	}
	if err := a.StopEditFile(tgt, false); err != nil {
		t.Fatal(err)
	}
}

// "Stop & upload" is a save request, not a discard — and a discard
// discards: the engine file keeps its own bytes.
func TestStopEditFileUploadAndDiscardLegs(t *testing.T) {
	a, _, root := labEditorApp(t, "lab", "/todo.txt", "one thing")
	tgt := EditTarget{Kind: "remote", Source: "lab", Key: "/todo.txt"}

	// the discard leg
	s := openLabEdit(t, a, "lab", "/todo.txt")
	if err := os.WriteFile(s.Local, []byte("one thing plus an unsaved idea"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.StopEditFile(tgt, false); err != nil {
		t.Fatal(err)
	}
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d after discard, want 0", n)
	}
	if b := read(t, filepath.Join(root, "todo.txt")); b != "one thing" {
		t.Fatalf("engine content after a discard = %q, want the untouched bytes", b)
	}

	// the upload leg
	s2 := openLabEdit(t, a, "lab", "/todo.txt")
	if b := read(t, s2.Local); b != "one thing" {
		t.Fatalf("re-opened staged content = %q, want a fresh pull", b)
	}
	if err := os.WriteFile(s2.Local, []byte("one thing done"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.StopEditFile(tgt, true); err != nil {
		t.Fatal(err)
	}
	if b := read(t, filepath.Join(root, "todo.txt")); b != "one thing done" {
		t.Fatalf("engine content after stop-and-upload = %q, want the saved bytes", b)
	}
}

// A second EditFile on a live remote session re-focuses it: the staged
// file keeps its pending edits (no re-download over them), and the
// typed call settles the session the historical grammar opened — one
// registry, one law.
func TestEditRemoteRefocusAndTypedParity(t *testing.T) {
	a, _, _ := labEditorApp(t, "lab", "/log.txt", "line one")
	s := openLabEdit(t, a, "lab", "/log.txt")
	if err := os.WriteFile(s.Local, []byte("line one pending"), 0o600); err != nil {
		t.Fatal(err)
	}

	opened := ""
	oldOpen := openInEditor
	openInEditor = func(_ *App, path string, _ bool) error { opened = path; return nil }
	defer func() { openInEditor = oldOpen }()

	info2, err := a.EditFile(EditTarget{Kind: "remote", Source: "lab", Key: "/log.txt"}, false)
	if err != nil {
		t.Fatalf("second EditFile err = %v, want a clean re-focus", err)
	}
	if info2.Local != s.Local || info2.Kind != "remote" {
		t.Fatalf("re-focus info = %+v, want the same staged session", info2)
	}
	if opened != s.Local {
		t.Fatalf("re-focus opened %q, want the staged file %q", opened, s.Local)
	}
	if b := read(t, s.Local); b != "line one pending" {
		t.Fatalf("staged content after re-focus = %q, want the pending edit (no re-download)", b)
	}
	if n := len(a.EditingFiles()); n != 1 {
		t.Fatalf("EditingFiles = %d sessions, want the one re-focused session", n)
	}

	// the S3 parity leg: a session opened by the historical EditObject
	// is settled by the typed StopEditFile — the same registry key
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "s3 v1")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("editparity", url)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditObject("docs", "notes.md", false); err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	if err := a.StopEditFile(EditTarget{Kind: "s3", Bucket: "docs", Key: "notes.md"}, false); err != nil {
		t.Fatalf("StopEditFile over an EditObject session = %v, want nil (one registry, one law)", err)
	}

	if err := a.StopEditFile(EditTarget{Kind: "remote", Source: "lab", Key: "/log.txt"}, false); err != nil {
		t.Fatal(err)
	}
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d after both stops, want 0", n)
	}
}

// The honest refusals: a directory path never pulls, an unknown source
// fails the resolve, and the local kind never crosses the bridge —
// local files open in place, they have no session to stage.
func TestEditFileRefusals(t *testing.T) {
	a, _, root := labEditorApp(t, "lab", "/present.txt", "here")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := a.EditFile(EditTarget{Kind: "remote", Source: "lab", Key: "/sub/"}, false); err == nil || !strings.Contains(err.Error(), "is a folder") {
		t.Fatalf("dir EditFile err = %v, want the folder refusal", err)
	}
	if _, err := a.EditFile(EditTarget{Kind: "remote", Source: "no-such-source", Key: "/x.txt"}, false); err == nil {
		t.Fatal("unknown-source EditFile err = nil, want the resolve failure")
	}
	if _, err := a.EditFile(EditTarget{Kind: "local", Key: `C:\somewhere\file.txt`}, false); err == nil || !strings.Contains(err.Error(), "local files open directly") {
		t.Fatalf("local-kind EditFile err = %v, want the local refusal", err)
	}
	if _, err := a.EditFile(EditTarget{Kind: "quantum", Key: "/x"}, false); err == nil {
		t.Fatal("unknown-kind EditFile err = nil, want the refusal")
	}
	if err := a.StopEditFile(EditTarget{Kind: "remote", Source: "lab", Key: "/absent.txt"}, false); err == nil || !strings.Contains(err.Error(), "not being edited") {
		t.Fatalf("unopened StopEditFile err = %v, want the not-being-edited refusal", err)
	}
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d after refusals, want 0", n)
	}
}

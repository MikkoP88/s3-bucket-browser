package api

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// The editor's lost-update guard. The pull records the remote's identity
// (its ETag); every push is conditional on it — If-Match — so a teammate
// (or any other writer) overwriting the object mid-edit turns the next
// auto-save into a loud refusal instead of a silent clobber. The session
// survives stale with its edits pending until the user decides: push
// anyway (the informed consent), reload from the server, or stop and
// discard.

// bootEditApp stages the standard editor-conflict rig: a fake S3 source
// with one seeded object, the editor launch faked off (hermetic), and the
// session registered by an EditObject pull. Returns the app, the store
// and the session's staged path.
func bootEditApp(t *testing.T, content string) (*App, *fakeS3, string) {
	t.Helper()
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return errors.New("no editor in tests") }
	t.Cleanup(func() { openInEditor = oldOpen })
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", content)
	if err := a.SaveSource(fakeS3Source("editconf", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	info, err := a.EditObject("docs", "notes.md", false)
	if err == nil || !strings.Contains(err.Error(), "could not open editor") {
		t.Fatalf("EditObject err = %v, want the could-not-open-editor error", err)
	}
	return a, f, info.Local
}

// waitForSession polls the session view until cond holds (the rigs' shape).
func waitForSession(t *testing.T, a *App, what string, cond func(EditInfo) bool) EditInfo {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sv := a.EditingFiles()
		if len(sv) == 1 && cond(sv[0]) {
			return sv[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("session never %s: %+v", what, sv)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func remoteContent(t *testing.T, f *fakeS3, key string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.objects["docs"][key]
}

// The headline verdict: a remote change under a pending edit refuses the
// auto-push, keeps the teammate's bytes on the wire, and flags the
// session stale — dirty, not lost, voiced.
func TestEditorPushGuardsAgainstRemoteChange(t *testing.T) {
	oldPoll := watcherPoll.Load()
	watcherPoll.Store(int64(10 * time.Millisecond))
	defer func() { watcherPoll.Store(oldPoll) }()
	a, f, local := bootEditApp(t, "v1 pulled")

	// the teammate overwrites while the edit is open (a new identity)
	f.seed("docs", "notes.md", "teammate overwrite")

	// the user saves a local edit; the watcher's push must refuse it
	if err := os.WriteFile(local, []byte("my edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.editorsMu.Lock()
	s := a.editors["docs\x00notes.md"]
	a.editorsMu.Unlock()
	go a.watchEditor(s)

	sv := waitForSession(t, a, "turned stale", func(e EditInfo) bool { return e.Dirty && e.Stale })
	if sv.PushFailed {
		t.Fatalf("a conflict is not endpoint trouble — pushFailed must stay clear: %+v", sv)
	}
	if got := remoteContent(t, f, "notes.md"); got != "teammate overwrite" {
		t.Fatalf("object content = %q — the guard let a clobber through", got)
	}
	// sticky: the watcher keeps refusing (no backoff ladder, no landing)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := remoteContent(t, f, "notes.md"); got != "teammate overwrite" {
			t.Fatalf("stale session wrote the object anyway: %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
}

// Push anyway is the informed consent: it writes over the changed remote,
// clears stale, and rebases the guard on what it wrote — proven by the
// next edit pushing guarded against the NEW identity and landing clean.
func TestEditorPushAnywayLandsOverConflict(t *testing.T) {
	a, f, local := bootEditApp(t, "v1 pulled")
	f.seed("docs", "notes.md", "teammate overwrite")
	if err := os.WriteFile(local, []byte("my edit"), 0o600); err != nil {
		t.Fatal(err)
	}

	// the guarded push refuses (the conflict verdict is errors.Is-able)
	if err := a.PushEdit("docs", "notes.md", false); !errors.Is(err, errEditConflict) {
		t.Fatalf("guarded PushEdit = %v, want the conflict", err)
	}
	if got := remoteContent(t, f, "notes.md"); got != "teammate overwrite" {
		t.Fatalf("object content = %q before the consent", got)
	}

	// the consent lands: stale and dirty settle, the remote is mine
	if err := a.PushEdit("docs", "notes.md", true); err != nil {
		t.Fatalf("PushEdit(force) err = %v", err)
	}
	sv := waitForSession(t, a, "settled after the consent", func(e EditInfo) bool {
		return !e.Dirty && !e.Stale && !e.PushFailed
	})
	_ = sv
	if got := remoteContent(t, f, "notes.md"); got != "my edit" {
		t.Fatalf("object content = %q, want the consented bytes", got)
	}

	// the rebase: a further edit pushes guarded and lands clean (the guard
	// must not false-positive on the identity the consent itself wrote)
	if err := os.WriteFile(local, []byte("my edit v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.PushEdit("docs", "notes.md", false); err != nil {
		t.Fatalf("rebased guarded PushEdit err = %v (the guard never rebased?)", err)
	}
	if got := remoteContent(t, f, "notes.md"); got != "my edit v2" {
		t.Fatalf("object content = %q, want the rebased push's bytes", got)
	}
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
}

// Stop & upload keeps the guard — never a side-door clobber past the very
// condition the auto-push refuses. The session survives the refusal.
func TestEditorStopUploadRefusesStaleClobber(t *testing.T) {
	a, f, local := bootEditApp(t, "v1 pulled")
	f.seed("docs", "notes.md", "teammate overwrite")
	if err := os.WriteFile(local, []byte("my edit"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.StopEdit("docs", "notes.md", true); !errors.Is(err, errEditConflict) {
		t.Fatalf("StopEdit(upload) on stale = %v, want the conflict refusal", err)
	}
	if got := remoteContent(t, f, "notes.md"); got != "teammate overwrite" {
		t.Fatalf("object content = %q — the explicit save clobbered", got)
	}
	sv := waitForSession(t, a, "survived the refused save", func(e EditInfo) bool {
		return e.Dirty && e.Stale
	})
	_ = sv
	// the honest exit stays open: stop and discard settles the session
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
	if n := len(a.EditingFiles()); n != 0 {
		t.Fatalf("EditingFiles = %d after discard, want 0", n)
	}
}

// The guard must not false-positive: with no remote change, the watched
// auto-push lands clean and the session settles exactly as before.
func TestEditorPushNoConflictLands(t *testing.T) {
	oldPoll := watcherPoll.Load()
	watcherPoll.Store(int64(10 * time.Millisecond))
	defer func() { watcherPoll.Store(oldPoll) }()
	a, f, local := bootEditApp(t, "v1 pulled")

	// size-differing on purpose: the pull staged 9 bytes, and a same-size
	// write that lands inside the pull's own millisecond is invisible to
	// the watcher's size-then-mtime change check on every filesystem
	if err := os.WriteFile(local, []byte("v2 edited clean"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.editorsMu.Lock()
	s := a.editors["docs\x00notes.md"]
	a.editorsMu.Unlock()
	go a.watchEditor(s)

	// the verdict is the wire first (the guard must not refuse its own
	// baseline), the session state second — waiting on the settle alone
	// would match the pristine session before the watcher ever saw the save
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := remoteContent(t, f, "notes.md"); got == "v2 edited clean" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("object content = %q, want the edited bytes (the guard false-positived?)",
				remoteContent(t, f, "notes.md"))
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitForSession(t, a, "settled clean", func(e EditInfo) bool {
		return !e.Dirty && !e.Stale && !e.PushFailed
	})
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
}

// The primitive's own contract, through the app's client: a matching
// guard lands and returns the new identity; a stale one refuses with the
// classifiable verdict; an empty guard writes unconditionally.
func TestUploadFileIfMatchLegs(t *testing.T) {
	a, f, local := bootEditApp(t, "base")
	_ = a
	c, err := a.client("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	f.mu.Lock()
	baseEtag := f.etags["docs"]["notes.md"]
	f.mu.Unlock()

	// match lands and returns the identity it wrote
	etag, err := transfer.UploadFileIfMatch(ctx, c.S3, local, "docs", "notes.md", baseEtag)
	if err != nil {
		t.Fatalf("matching If-Match: %v", err)
	}
	f.mu.Lock()
	cur := f.etags["docs"]["notes.md"]
	f.mu.Unlock()
	if etag != cur {
		t.Fatalf("returned etag %q, want the stored %q", etag, cur)
	}

	// stale guard refuses with the wire verdict the classifier knows
	if _, err := transfer.UploadFileIfMatch(ctx, c.S3, local, "docs", "notes.md", "\"deadbeef\""); !isPreconditionFailed(err) {
		t.Fatalf("stale If-Match = %v, want a precondition failure", err)
	}

	// empty guard writes unconditionally — the pre-guard shape
	if _, err := transfer.UploadFileIfMatch(ctx, c.S3, local, "docs", "notes.md", ""); err != nil {
		t.Fatalf("unconditional put: %v", err)
	}
	if err := a.StopEdit("docs", "notes.md", false); err != nil {
		t.Fatal(err)
	}
}

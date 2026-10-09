// edit_cmd_test.go pins the CLI edit face end to end: the engine round
// trip against the hermetic lab source (pull → editor → guarded push),
// the guard's two legs (a size move and an mtime-only move), --force as
// the informed consent, the create-on-exit shape for operands that do
// not exist yet, local in-place editing, the honest refusals, and the
// S3 leg's If-Match law on the wire against a stateful fake — the 412
// refused, the forced landing, the fresh create.
package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinEditor swaps the editor launch for an in-process hook that holds
// the staged copy — the mid-edit hand. The hook is also where a leg
// lands the teammate's overwrite, exactly between the pull and the
// push.
func pinEditor(t *testing.T, fn func(staged string) error) {
	t.Helper()
	old := launchEditor
	launchEditor = fn
	t.Cleanup(func() { launchEditor = old })
}

func readEngine(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The engine round trip: the pull stages the engine's bytes, the
// editor's save is the only hand on them, and the guarded push lands
// them back — while an editor that saves nothing pushes nothing.
func TestEditRemoteRoundTrip(t *testing.T) {
	root := srcEnv(t)

	pinEditor(t, func(staged string) error {
		if b := readEngine(t, staged); b != "alpha" {
			t.Errorf("staged content = %q, want the pulled alpha", b)
		}
		return os.WriteFile(staged, []byte("alpha edited"), 0o600)
	})
	out, code := captureOut(t, "edit", "lab://a.txt")
	if code != 0 {
		t.Fatalf("edit lab://a.txt: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "pushed lab://a.txt") {
		t.Errorf("edit output missing the push line: %s", out)
	}
	if got := readEngine(t, filepath.Join(root, "a.txt")); got != "alpha edited" {
		t.Errorf("engine content = %q, want the edited bytes", got)
	}

	// an editor that exits without saving pushes nothing
	pinEditor(t, func(staged string) error { return nil })
	out, code = captureOut(t, "edit", "lab://a.txt")
	if code != 0 {
		t.Fatalf("unchanged edit: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "no changes in lab://a.txt") {
		t.Errorf("unchanged edit output: %s", out)
	}
	if got := readEngine(t, filepath.Join(root, "a.txt")); got != "alpha edited" {
		t.Errorf("unchanged edit touched the engine: %q", got)
	}

	// an operand that does not exist yet is born on exit
	pinEditor(t, func(staged string) error {
		return os.WriteFile(staged, []byte("born editing"), 0o600)
	})
	out, code = captureOut(t, "edit", "lab://born.txt")
	if code != 0 {
		t.Fatalf("edit new file: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "created lab://born.txt") {
		t.Errorf("edit new file output: %s", out)
	}
	if got := readEngine(t, filepath.Join(root, "born.txt")); got != "born editing" {
		t.Errorf("created engine content = %q", got)
	}
}

// The guard's two legs: a teammate's mid-edit overwrite refuses the
// push with the conflict (the engine keeps their bytes, the edit stays
// in the kept staging copy), and --force is the informed consent that
// lands past it — the size move and the mtime-only move both refuse.
func TestEditRemoteConflictAndForce(t *testing.T) {
	root := srcEnv(t)

	// leg 1 — the size move
	pinEditor(t, func(staged string) error {
		if err := os.WriteFile(staged, []byte("team B draft"), 0o600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, "a.txt"), []byte("team A grew it"), 0o600)
	})
	err := runEdit(context.Background(), "lab://a.txt", false)
	if !errors.Is(err, editConflict) {
		t.Fatalf("guarded edit over a size move = %v, want the conflict", err)
	}
	if !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "s3b-edit-") {
		t.Errorf("conflict message misses the way out or the kept copy: %v", err)
	}
	if got := readEngine(t, filepath.Join(root, "a.txt")); got != "team A grew it" {
		t.Errorf("engine content after the refusal = %q, want the teammate's bytes", got)
	}

	// the exit-code contract: a conflict is an operation failure (1),
	// with the error label and the --force advice on stderr. The leg
	// stages a size-differing engine move — re-running leg 1's editor
	// would rewrite the same 12 bytes the refusal left behind, and a
	// same-size write whose clock lands in the same millisecond as the
	// pull's stamp is invisible to the guard's mtime half (the rig-clock
	// law: never lean on the ms).
	pinEditor(t, func(staged string) error {
		if err := os.WriteFile(staged, []byte("team B draft"), 0o600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, "a.txt"), []byte("team A grew it longer"), 0o600)
	})
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	code := Execute([]string{"edit", "lab://a.txt"})
	w.Close()
	os.Stderr = oldStderr
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	stderr := string(buf[:n])
	if code != exitOpFail {
		t.Errorf("conflict exit = %d, want %d", code, exitOpFail)
	}
	if !strings.Contains(stderr, "error:") || !strings.Contains(stderr, "--force") {
		t.Errorf("conflict stderr = %q, want the error label with --force", stderr)
	}

	// --force lands past the refusal
	pinEditor(t, func(staged string) error {
		return os.WriteFile(staged, []byte("team B final say"), 0o600)
	})
	if code := Execute([]string{"edit", "lab://a.txt", "--force"}); code != 0 {
		t.Fatalf("edit --force: exit %d", code)
	}
	if got := readEngine(t, filepath.Join(root, "a.txt")); got != "team B final say" {
		t.Errorf("engine content after --force = %q", got)
	}

	// leg 2 — the mtime-only move: same size on both sides, the engine
	// file's clock moved an hour back, so the mtime half of the law is
	// the only witness (and the staged edit differs in size from the
	// pull, never leaning on the staged clock to count as a change)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("same!"), 0o600); err != nil {
		t.Fatal(err)
	}
	pinEditor(t, func(staged string) error {
		if err := os.WriteFile(staged, []byte("same!!"), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("beta2"), 0o600); err != nil {
			return err
		}
		past := time.Now().Add(-time.Hour) // the pull happened moments ago
		return os.Chtimes(filepath.Join(root, "a.txt"), past, past)
	})
	if err := runEdit(context.Background(), "lab://a.txt", false); !errors.Is(err, editConflict) {
		t.Fatalf("guarded edit over an mtime move = %v, want the conflict", err)
	}
	if got := readEngine(t, filepath.Join(root, "a.txt")); got != "beta2" {
		t.Errorf("engine content after the mtime refusal = %q, want the teammate's bytes", got)
	}
}

// Local operands edit in place — the file itself is the store — a
// missing local file is born empty, and a folder is refused.
func TestEditLocalInPlace(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	pinEditor(t, func(staged string) error {
		if staged != note {
			t.Errorf("local edit opened %q, want the file itself", staged)
		}
		return os.WriteFile(staged, []byte("two lines"), 0o644)
	})
	out, code := captureOut(t, "edit", note)
	if code != 0 {
		t.Fatalf("edit local: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "edited") || !strings.Contains(out, "in place") {
		t.Errorf("local edit output: %s", out)
	}
	if got := readEngine(t, note); got != "two lines" {
		t.Errorf("local content = %q, want the edited bytes", got)
	}

	// a missing local file is born by the editor's save
	fresh := filepath.Join(dir, "fresh.txt")
	pinEditor(t, func(staged string) error {
		return os.WriteFile(staged, []byte("made"), 0o644)
	})
	if err := runEdit(context.Background(), fresh, false); err != nil {
		t.Fatalf("edit new local file: %v", err)
	}
	if got := readEngine(t, fresh); got != "made" {
		t.Errorf("new local content = %q", got)
	}

	// a folder is refused as a usage error
	err := runEdit(context.Background(), dir, false)
	var ee exitError
	if !errors.As(err, &ee) || ee.code != exitUsage {
		t.Fatalf("folder edit err = %v, want a usage refusal", err)
	}
	if !strings.Contains(err.Error(), "is a folder") {
		t.Errorf("folder refusal = %v", err)
	}
}

// The honest refusals: folder-shaped operands (an engine folder, the
// source root, a trailing-slash s3:// prefix, a bare bucket) and an
// unknown source name — all usage errors before any wire moves.
func TestEditRefusals(t *testing.T) {
	srcEnv(t)

	for _, c := range []struct{ arg, voice string }{
		{"lab://docs/", "is a folder"},
		{"lab://", "is a folder"},
		{"s3://some-bucket", "is a bucket or folder"},
		{"s3://some-bucket/prefix/", "is a bucket or folder"},
	} {
		err := runEdit(context.Background(), c.arg, false)
		var ee exitError
		if !errors.As(err, &ee) || ee.code != exitUsage {
			t.Errorf("edit %q err = %v, want a usage refusal", c.arg, err)
			continue
		}
		if !strings.Contains(err.Error(), c.voice) {
			t.Errorf("edit %q refusal = %v, want the %q voice", c.arg, err, c.voice)
		}
	}
	err := runEdit(context.Background(), "ghost://x.txt", false)
	var ee exitError
	if !errors.As(err, &ee) || ee.code != exitUsage {
		t.Fatalf("unknown-source edit err = %v, want a usage refusal", err)
	}
	if !strings.Contains(err.Error(), "not a saved data source") {
		t.Errorf("unknown-source refusal = %v", err)
	}
}

// The editor command resolution: the flag beats $VISUAL beats $EDITOR
// beats the platform default, and the command string splits on
// whitespace outside quotes.
func TestEditorCommandResolution(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := resolveEditor(""); got != defaultEditor() {
		t.Errorf("resolveEditor with nothing = %q, want the platform default %q", got, defaultEditor())
	}
	t.Setenv("EDITOR", "ed")
	if got := resolveEditor(""); got != "ed" {
		t.Errorf("resolveEditor with $EDITOR = %q", got)
	}
	t.Setenv("VISUAL", "vi")
	if got := resolveEditor(""); got != "vi" {
		t.Errorf("resolveEditor with $VISUAL = %q", got)
	}
	if got := resolveEditor("flagged"); got != "flagged" {
		t.Errorf("resolveEditor with the flag = %q", got)
	}

	cases := []struct {
		in   string
		want []string
	}{
		{"vim", []string{"vim"}},
		{"code -w", []string{"code", "-w"}},
		{`"C:\Program Files\e.exe" --wait`, []string{`C:\Program Files\e.exe`, "--wait"}},
		{"  nano   -w  ", []string{"nano", "-w"}},
	}
	for _, c := range cases {
		if got := splitCommand(c.in); strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitCommand(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// fakeEditS3 is a stateful one-bucket fake speaking just the three
// verbs the edit face rides: HEAD (the pull baseline), GET (the pull)
// and PUT (the guarded push — If-Match answered with the server's own
// 412 when the object moved).
type fakeEditS3 struct {
	mu    sync.Mutex
	objs  map[string]string
	etags map[string]string
	n     int
}

func (f *fakeEditS3) put(key, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.objs[key] = body
	f.etags[key] = "e" + strconv.Itoa(f.n)
}

func (f *fakeEditS3) handler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusBadRequest)
		return
	}
	key := parts[1]
	f.mu.Lock()
	body, has := f.objs[key]
	etag := f.etags[key]
	f.mu.Unlock()
	switch r.Method {
	case http.MethodHead:
		if !has {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"`+etag+`"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		if !has {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"`+etag+`"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write([]byte(body))
	case http.MethodPut:
		b := make([]byte, r.ContentLength)
		if _, err := io.ReadFull(r.Body, b); err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && im != etag {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusPreconditionFailed)
			w.Write([]byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
				"<Error><Code>PreconditionFailed</Code>" +
				"<Message>At least one of the pre-conditions you specified did not hold</Message>" +
				"</Error>"))
			return
		}
		f.put(key, string(b))
		w.Header().Set("ETag", `"next"`)
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "unexpected method "+r.Method, http.StatusBadRequest)
	}
}

// The S3 leg's If-Match law on the wire: the round trip carries the
// pulled ETag, a mid-edit teammate overwrite is refused with the
// server's 412 (kept staging copy, teammate's bytes standing), --force
// lands past it, and a missing object is created on exit.
func TestEditS3GuardRoundTrip(t *testing.T) {
	cliEnv(t)
	fake := &fakeEditS3{objs: map[string]string{}, etags: map[string]string{}}
	fake.put("a.txt", "alpha")
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)

	if code := Execute([]string{"source", "add", "store", "--type", "s3",
		"--endpoint", srv.URL, "--access-key", "k", "--secret-key", "s",
		"--bucket", "lab"}); code != 0 {
		t.Fatalf("source add: exit %d", code)
	}

	// the round trip: the pull stages the object, the push carries If-Match
	pinEditor(t, func(staged string) error {
		if b := readEngine(t, staged); b != "alpha" {
			t.Errorf("staged content = %q, want the pulled alpha", b)
		}
		return os.WriteFile(staged, []byte("alpha via s3"), 0o600)
	})
	out, code := captureOut(t, "edit", "store://a.txt")
	if code != 0 {
		t.Fatalf("edit store://a.txt: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "pushed store://a.txt") {
		t.Errorf("edit output missing the push line: %s", out)
	}
	fake.mu.Lock()
	got := fake.objs["a.txt"]
	fake.mu.Unlock()
	if got != "alpha via s3" {
		t.Errorf("object after the push = %q", got)
	}

	// the conflict: the teammate overwrites mid-edit, the guarded PUT
	// meets the 412 and the edit is refused with the kept copy
	pinEditor(t, func(staged string) error {
		if err := os.WriteFile(staged, []byte("team B via s3"), 0o600); err != nil {
			return err
		}
		fake.put("a.txt", "team A via s3")
		return nil
	})
	err := runEdit(context.Background(), "store://a.txt", false)
	if !errors.Is(err, editConflict) {
		t.Fatalf("guarded S3 edit over a teammate = %v, want the conflict", err)
	}
	fake.mu.Lock()
	got = fake.objs["a.txt"]
	fake.mu.Unlock()
	if got != "team A via s3" {
		t.Errorf("object after the refused push = %q, want the teammate's bytes", got)
	}

	// --force lands past the 412
	pinEditor(t, func(staged string) error {
		return os.WriteFile(staged, []byte("team B final s3"), 0o600)
	})
	if code := Execute([]string{"edit", "store://a.txt", "--force"}); code != 0 {
		t.Fatalf("edit --force: exit %d", code)
	}
	fake.mu.Lock()
	got = fake.objs["a.txt"]
	fake.mu.Unlock()
	if got != "team B final s3" {
		t.Errorf("object after --force = %q", got)
	}

	// a missing object is created on exit
	pinEditor(t, func(staged string) error {
		if b := readEngine(t, staged); b != "" {
			t.Errorf("new-file staged content = %q, want an empty buffer", b)
		}
		return os.WriteFile(staged, []byte("born s3"), 0o600)
	})
	out, code = captureOut(t, "edit", "store://born.txt")
	if code != 0 {
		t.Fatalf("edit new object: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "created store://born.txt") {
		t.Errorf("edit new object output: %s", out)
	}
	fake.mu.Lock()
	got = fake.objs["born.txt"]
	fake.mu.Unlock()
	if got != "born s3" {
		t.Errorf("created object = %q", got)
	}
}

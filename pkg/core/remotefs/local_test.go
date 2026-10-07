package remotefs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

func srcLocal(root string) profile.Source {
	return profile.Source{Name: "t", Type: profile.TypeLocal, LocalRoot: root}
}

func srcType(typ string) profile.Source {
	return profile.Source{Name: "t", Type: typ}
}

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"":             "/",
		"/":            "/",
		".":            "/",
		"..":           "/",
		"a":            "/a",
		"/a":           "/a",
		"a/":           "/a",
		"/a/b/":        "/a/b",
		"//a///b":      "/a/b",
		"../../etc":    "/etc",
		"/a/../../b":   "/b",
		"a\\b\\c":      "/a/b/c",
		"./a":          "/a",
		"/uploads/":    "/uploads",
		"/x/y/../z/..": "/x",
	}
	for in, want := range cases {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKey(t *testing.T) {
	cases := []struct {
		dir, name string
		isDir     bool
		want      string
	}{
		{"/", "sub", true, "/sub/"},
		{"/", "readme.md", false, "/readme.md"},
		{"/docs", "sub", true, "/docs/sub/"},
		{"/docs/", "f.txt", false, "/docs/f.txt"},
		{"docs", "f", true, "/docs/f/"},
	}
	for _, c := range cases {
		if got := Key(c.dir, c.name, c.isDir); got != c.want {
			t.Errorf("Key(%q, %q, %v) = %q, want %q", c.dir, c.name, c.isDir, got, c.want)
		}
	}
}

// newLocalTestBed builds a Local FS over a temp root with a small tree.
func newLocalTestBed(t *testing.T) *Local {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"docs", "docs/nested", "logs"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("hello remotefs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "spec.txt"), []byte("spec v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocal(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLocalListSortsAndKeys(t *testing.T) {
	l := newLocalTestBed(t)
	entries, err := l.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
		if e.IsDir && !strings.HasSuffix(e.Key, "/") {
			t.Errorf("dir entry key %q must end with /", e.Key)
		}
	}
	want := []string{"docs", "logs", "readme.md"} // folders first, sorted
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("List(/) = %v, want %v", names, want)
	}
	if entries[0].Key != "/docs/" || entries[2].Key != "/readme.md" {
		t.Errorf("keys wrong: %q %q", entries[0].Key, entries[2].Key)
	}

	nested, err := l.List(context.Background(), "/docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) != 2 || nested[0].Name != "nested" || !nested[0].IsDir || nested[1].Name != "spec.txt" {
		t.Errorf("List(/docs) = %+v", nested)
	}
}

func TestLocalListEscapeIsAnchored(t *testing.T) {
	l := newLocalTestBed(t)
	rootListing, err := l.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"..", "../..", "/../../..", "docs/../../.."} {
		got, err := l.List(context.Background(), p)
		if err != nil {
			t.Fatalf("List(%q): %v", p, err)
		}
		if len(got) != len(rootListing) {
			t.Errorf("List(%q) escaped the root: %d entries vs %d", p, len(got), len(rootListing))
		}
	}
}

func TestLocalStatOpenCreate(t *testing.T) {
	l := newLocalTestBed(t)
	ctx := context.Background()

	st, err := l.Stat(ctx, "/readme.md")
	if err != nil {
		t.Fatal(err)
	}
	if st.IsDir || st.Name != "readme.md" || st.Size != int64(len("hello remotefs")) {
		t.Errorf("Stat file = %+v", st)
	}
	st, err = l.Stat(ctx, "/docs")
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir || st.Name != "docs" {
		t.Errorf("Stat dir = %+v", st)
	}
	if _, err := l.Stat(ctx, "/missing.txt"); !os.IsNotExist(err) {
		t.Errorf("Stat missing: err = %v, want not-exist", err)
	}

	r, size, err := l.Open(ctx, "/readme.md")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	if string(b) != "hello remotefs" || size != int64(len(b)) {
		t.Errorf("Open = %q size %d", b, size)
	}

	if err := l.Create(ctx, "/docs/new.txt", bytes.NewReader([]byte("created"))); err != nil {
		t.Fatal(err)
	}
	r, _, err = l.Open(ctx, "/docs/new.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(r)
	r.Close()
	if string(b) != "created" {
		t.Errorf("Create round-trip = %q", b)
	}
	// Create replaces fully — a shorter rewrite leaves no tail.
	if err := l.Create(ctx, "/docs/new.txt", bytes.NewReader([]byte("v2"))); err != nil {
		t.Fatal(err)
	}
	r, _, _ = l.Open(ctx, "/docs/new.txt")
	b, _ = io.ReadAll(r)
	r.Close()
	if string(b) != "v2" {
		t.Errorf("Create must replace, got %q", b)
	}
	// A stream that dies mid-copy leaves the previous bytes intact —
	// the staged-and-committed contract, never a truncated file.
	if err := l.Create(ctx, "/docs/new.txt", iotest.ErrReader(errors.New("stream died"))); err == nil {
		t.Fatal("Create with a dying stream must fail")
	}
	r, _, err = l.Open(ctx, "/docs/new.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(r)
	r.Close()
	if string(b) != "v2" {
		t.Errorf("failed Create damaged the file: %q", b)
	}
}

func TestLocalMkdirRenameRemove(t *testing.T) {
	l := newLocalTestBed(t)
	ctx := context.Background()

	if err := l.MkdirAll(ctx, "/deep/er/still"); err != nil {
		t.Fatal(err)
	}
	entries, err := l.List(ctx, "/deep/er")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "still" || !entries[0].IsDir {
		t.Errorf("MkdirAll: /deep/er = %+v", entries)
	}

	if err := l.Rename(ctx, "/readme.md", "/docs/renamed.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Stat(ctx, "/readme.md"); !os.IsNotExist(err) {
		t.Error("rename must remove the old path")
	}
	if _, err := l.Stat(ctx, "/docs/renamed.md"); err != nil {
		t.Error("rename target missing")
	}

	if err := l.Rename(ctx, "/docs", "/archives"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Stat(ctx, "/archives/nested"); err != nil {
		t.Error("directory rename must move the tree")
	}

	if err := l.Remove(ctx, "/archives/nested"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Stat(ctx, "/archives/nested"); !os.IsNotExist(err) {
		t.Error("Remove dir failed")
	}
	if err := l.Remove(ctx, "/archives/spec.txt"); err != nil {
		t.Fatal(err)
	}

	if err := l.Remove(ctx, "/"); err == nil {
		t.Error("removing the root must be refused")
	}
}

// TestLocalListModesAndCreation pins the two optional attributes the local
// engine fills for the grid's Mode and Date created columns: Mode is a
// classic unix-style string on every platform ("d..."/"-..." by shape),
// Created is present on Windows (birth time in the Win32 attribute data)
// and honestly absent elsewhere.
func TestLocalListModesAndCreation(t *testing.T) {
	l := newLocalTestBed(t)
	entries, err := l.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	var file, dir listing.Entry
	for _, e := range entries {
		if e.Name == "readme.md" {
			file = e
		}
		if e.Name == "docs" {
			dir = e
		}
	}
	if file.Mode == "" || dir.Mode == "" {
		t.Fatalf("mode missing: file %+v dir %+v", file, dir)
	}
	if !strings.HasPrefix(dir.Mode, "d") {
		t.Errorf("dir mode %q must start with 'd'", dir.Mode)
	}
	if !strings.HasPrefix(file.Mode, "-") {
		t.Errorf("file mode %q must start with '-'", file.Mode)
	}
	if file.Created == nil {
		if runtime.GOOS == "windows" {
			t.Error("Created missing on Windows, which carries birth time")
		}
		return
	}
	// Where present, the only portable bound is sanity: after the epoch.
	if file.Created.Before(time.Unix(0, 0)) {
		t.Errorf("Created %v is before the epoch", file.Created)
	}
}

func TestNewLocalRejectsBadRoots(t *testing.T) {
	if _, err := NewLocal(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing root must error")
	}
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal(context.Background(), f); err == nil {
		t.Error("file root must error")
	}
}

func TestDialLocalAndUnknown(t *testing.T) {
	l, err := Dial(context.Background(), srcLocal(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.List(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(context.Background(), srcType("s3")); err == nil {
		t.Error("s3 has no remotefs engine (dedicated pipeline)")
	}
}

// stepCloser counts its own release, proving the abandon contract's
// resource discipline: a verdict nobody collects is released, not leaked.
type stepCloser struct{ done chan struct{} }

func (c *stepCloser) Close() error { close(c.done); return nil }

// TestLocalStepVerdicts pins the abandon contract itself: a step that
// never returns costs the caller one budget (never a park), the caller's
// own patience ends the wait even sooner, an abandoned verdict releases
// what it holds, and a fast step's verdict passes through verbatim — nil
// context included.
func TestLocalStepVerdicts(t *testing.T) {
	old := LocalOpBudget
	LocalOpBudget = 50 * time.Millisecond
	defer func() { LocalOpBudget = old }()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	// The budget breaks the silent wedge.
	start := time.Now()
	_, err := LocalStep(context.Background(), func() (struct{}, error) {
		<-release
		return struct{}{}, nil
	})
	if !errors.Is(err, ErrLocalDeadline) {
		t.Fatalf("parked step = %v, want ErrLocalDeadline", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the budget never fired", el)
	}

	// The caller's patience is honored first when it is shorter.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = LocalStep(ctx, func() (struct{}, error) {
		<-release
		return struct{}{}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shorter caller patience = %v, want the caller's deadline", err)
	}

	// An abandoned verdict releases what it holds.
	c := &stepCloser{done: make(chan struct{})}
	_, err = LocalStep(context.Background(), func() (*stepCloser, error) {
		time.Sleep(80 * time.Millisecond) // outlasts the budget
		return c, nil
	})
	if !errors.Is(err, ErrLocalDeadline) {
		t.Fatalf("slow-closer step = %v, want ErrLocalDeadline", err)
	}
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned verdict's closer was never released")
	}

	// A fast step's own verdict passes through verbatim.
	sentinel := errors.New("step's own error")
	if _, err := LocalStep(context.Background(), func() (struct{}, error) {
		return struct{}{}, sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("fast step error = %v, want the step's own", err)
	}
	if v, err := LocalStep(nil, func() (int, error) { return 7, nil }); err != nil || v != 7 {
		t.Fatalf("fast step = %d, %v; want 7, nil (nil context tolerated)", v, err)
	}
}

// TestLocalListWedgeBreaksSilentDeath pins the engine leg: a directory
// read whose backing vanished (dead UNC root — the read parks inside the
// kernel until the redirector gives up) must cost one budget and leave
// the engine unpoisoned — the very next listing works once the wire is
// back.
func TestLocalListWedgeBreaksSilentDeath(t *testing.T) {
	l := newLocalTestBed(t)
	oldBudget, oldReadDir := LocalOpBudget, localReadDir
	defer func() { LocalOpBudget, localReadDir = oldBudget, oldReadDir }()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	LocalOpBudget = 50 * time.Millisecond
	localReadDir = func(string) ([]os.DirEntry, error) {
		<-release // the wedge: take the request, answer nothing
		return nil, nil
	}
	start := time.Now()
	_, err := l.List(context.Background(), "/")
	if !errors.Is(err, ErrLocalDeadline) {
		t.Fatalf("wedged List = %v, want the local deadline verdict", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the budget never fired", el)
	}

	// Restored wire, same engine: the verdict is a verdict, not a scar.
	LocalOpBudget, localReadDir = oldBudget, oldReadDir
	entries, err := l.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("post-heal List = %d entries, want 3", len(entries))
	}
}

// TestNewLocalWedgeBreaksSilentDeath pins the dial leg: the root's stat
// is a bounded step, so in the GUI (where the dial runs under the
// engine-cache lock) a wedged local root costs one budget instead of
// freezing every source's engine resolution.
func TestNewLocalWedgeBreaksSilentDeath(t *testing.T) {
	oldBudget, oldStat := LocalOpBudget, localStat
	defer func() { LocalOpBudget, localStat = oldBudget, oldStat }()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	LocalOpBudget = 50 * time.Millisecond
	localStat = func(string) (os.FileInfo, error) {
		<-release
		return nil, nil
	}
	start := time.Now()
	_, err := NewLocal(context.Background(), t.TempDir())
	if !errors.Is(err, ErrLocalDeadline) {
		t.Fatalf("wedged dial = %v, want the local deadline verdict", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the budget never fired", el)
	}
}

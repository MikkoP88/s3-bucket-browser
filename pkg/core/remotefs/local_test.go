package remotefs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	l, err := NewLocal(root)
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
	// Create truncates.
	if err := l.Create(ctx, "/docs/new.txt", bytes.NewReader([]byte("v2"))); err != nil {
		t.Fatal(err)
	}
	r, _, _ = l.Open(ctx, "/docs/new.txt")
	b, _ = io.ReadAll(r)
	r.Close()
	if string(b) != "v2" {
		t.Errorf("Create must truncate, got %q", b)
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

func TestNewLocalRejectsBadRoots(t *testing.T) {
	if _, err := NewLocal(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing root must error")
	}
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal(f); err == nil {
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

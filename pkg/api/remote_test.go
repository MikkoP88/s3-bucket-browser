package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// localSource builds a TypeLocal source over a seeded temp directory.
func localSource(t *testing.T, name string) (profile.Source, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return profile.Source{Name: name, Type: profile.TypeLocal, LocalRoot: root}, root
}

// RemoteList: local engine rows match the S3 grid shape, unknown sources
// error, and S3 sources stay on their dedicated pipeline.
func TestRemoteListLocalEngine(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src, _ := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	entries, err := a.RemoteList("lab", "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].IsDir || entries[0].Name != "docs" || entries[0].Key != "/docs/" {
		t.Fatalf("RemoteList = %+v", entries)
	}
	if entries[1].Name != "readme.md" || entries[1].Key != "/readme.md" {
		t.Fatalf("file entry = %+v", entries[1])
	}

	// Nested view + engine cache reuse (second call hits the cache).
	nested, err := a.RemoteList("lab", "/docs")
	if err != nil || len(nested) != 0 {
		t.Fatalf("nested = %+v err = %v", nested, err)
	}

	// Unknown source.
	if _, err := a.RemoteList("nope", "/"); err == nil {
		t.Error("unknown source must error")
	}

	// S3 sources have no remotefs engine.
	if err := a.SaveSource(s3Source("cloud", "s")); err != nil {
		t.Fatal(err)
	}
	_, err = a.RemoteList("cloud", "/")
	if err == nil || !strings.Contains(err.Error(), "S3 pipeline") {
		t.Errorf("s3 via remotefs err = %v", err)
	}

	// SaveSource invalidates: the cache survives only until a source edit
	// (updates carry the source ID, like the editor does).
	st, err := a.loadStore()
	if err != nil {
		t.Fatal(err)
	}
	cur, err := st.GetSource("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSource(cur); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoteList("lab", "/"); err != nil {
		t.Fatalf("post-invalidate list: %v", err)
	}
}

// TestSource on a local source dials the engine end-to-end.
func TestTestSourceRemote(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src, _ := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	res := a.TestSource("lab")
	if !res.OK || !strings.Contains(res.Message, "connected") {
		t.Fatalf("TestSource(local) = %+v", res)
	}

	// A local source with a missing root fails the probe honestly.
	if err := a.SaveSource(profile.Source{Name: "gone", Type: profile.TypeLocal,
		LocalRoot: filepath.Join(t.TempDir(), "missing")}); err != nil {
		t.Fatal(err)
	}
	if res := a.TestSource("gone"); res.OK {
		t.Fatalf("missing root must fail: %+v", res)
	}
}

// ListSourceBuckets rejects non-S3 sources (typed guard before any dial).
func TestListSourceBucketsGuards(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src, _ := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListSourceBuckets("lab"); err == nil || !strings.Contains(err.Error(), "not an S3 source") {
		t.Errorf("non-s3 err = %v", err)
	}
	if _, err := a.ListSourceBuckets("nope"); err == nil {
		t.Error("unknown source must error")
	}
}

// RemoteMkdir / RemoteRename / RemoteStat / RemoteRemove over the local
// engine — the same contract every engine validates in remotefs.
func TestRemoteNativeOps(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src, root := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	// Mkdir with implicit parents.
	if err := a.RemoteMkdir("lab", "/docs/deep/inner"); err != nil {
		t.Fatalf("RemoteMkdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "deep", "inner")); err != nil {
		t.Fatalf("mkdir did not create parents: %v", err)
	}
	if err := a.RemoteMkdir("lab", "/"); err == nil {
		t.Error("root mkdir must be rejected")
	}

	// Rename a file (bare name, Explorer semantics).
	if err := a.RemoteRename("lab", "/readme.md", "intro.md"); err != nil {
		t.Fatalf("RemoteRename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "readme.md")); err == nil {
		t.Error("old name still present after rename")
	}
	if _, err := os.Stat(filepath.Join(root, "intro.md")); err != nil {
		t.Fatalf("new name missing after rename: %v", err)
	}
	// Rename a directory — key keeps the trailing-slash convention.
	if err := a.RemoteRename("lab", "/docs/", "manuals"); err != nil {
		t.Fatalf("RemoteRename(dir): %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "manuals")); err != nil {
		t.Fatalf("renamed dir missing: %v", err)
	}
	// Invalid names are rejected without touching the engine.
	if err := a.RemoteRename("lab", "/intro.md", "a/b"); err == nil {
		t.Error("slash in name must be rejected")
	}
	if err := a.RemoteRename("lab", "/", "root"); err == nil {
		t.Error("root rename must be rejected")
	}

	// Stat sees the renamed tree.
	st, err := a.RemoteStat("lab", "/manuals/deep/inner")
	if err != nil || !st.IsDir {
		t.Fatalf("RemoteStat = %+v err = %v", st, err)
	}

	// Count-then-act delete: preview reports files+folders+bytes, remove
	// deletes the tree, and the root is untouchable.
	seed := filepath.Join(root, "manuals", "deep", "inner")
	if err := os.WriteFile(filepath.Join(seed, "data.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	pv, err := a.RemoteDeletePreview("lab", []string{"/manuals/deep/", "/intro.md", "/missing"})
	if err != nil {
		t.Fatalf("RemoteDeletePreview: %v", err)
	}
	if pv.Files != 2 || pv.Folders != 2 || pv.Bytes != 6 {
		t.Fatalf("preview = %+v (want 2 files, 2 folders, 6 bytes)", pv)
	}
	if len(pv.Errors) != 1 || !strings.Contains(pv.Errors[0], "missing") {
		t.Fatalf("preview errors = %+v", pv.Errors)
	}
	if pv, err := a.RemoteDeletePreview("lab", []string{"/"}); err != nil || len(pv.Errors) != 1 {
		t.Fatalf("root preview must refuse: %+v err = %v", pv, err)
	}

	res, err := a.RemoteRemove("lab", []string{"/manuals/deep/", "/intro.md"})
	if err != nil {
		t.Fatalf("RemoteRemove: %v", err)
	}
	if res.Deleted != 2 || len(res.Errors) != 0 {
		t.Fatalf("remove = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "manuals", "deep")); err == nil {
		t.Error("deleted tree still present")
	}
	entries, err := a.RemoteList("lab", "/")
	if err != nil || len(entries) != 1 || entries[0].Name != "manuals" {
		t.Fatalf("post-delete listing = %+v err = %v", entries, err)
	}
	if res, err := a.RemoteRemove("lab", []string{"/"}); err != nil || res.Deleted != 0 || len(res.Errors) != 1 {
		t.Fatalf("root remove must refuse: %+v err = %v", res, err)
	}

	// Guards: unknown source, S3 source.
	if err := a.RemoteMkdir("nope", "/x"); err == nil {
		t.Error("unknown source must error")
	}
	if err := a.SaveSource(s3Source("cloud", "s")); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoteMkdir("cloud", "/x"); err == nil || !strings.Contains(err.Error(), "S3 pipeline") {
		t.Errorf("s3 via remote ops err = %v", err)
	}
}

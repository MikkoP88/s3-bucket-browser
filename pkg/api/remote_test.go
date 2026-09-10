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

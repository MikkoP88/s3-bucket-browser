package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStagedPathsLayout(t *testing.T) {
	dir := filepath.Join(string(filepath.Separator), "tmp", "s3b-clip-1")
	got := stagedPaths(dir, []DragItem{
		{Key: "readme.md"},
		{Key: "docs/notes.md"},
		{Key: "trailing/slash/"},
	})
	want := []string{
		filepath.Join(dir, "readme.md"),
		filepath.Join(dir, "notes.md"),
		filepath.Join(dir, "slash"),
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stagedPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDragOutFilesValidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	a := New("test")

	if err := a.DragOutFiles(nil); err == nil {
		t.Fatal("empty selection must be refused")
	}
	many := make([]DragItem, maxDragOutItems+1)
	for i := range many {
		many[i] = DragItem{Key: "f", Size: 1}
	}
	if err := a.DragOutFiles(many); err == nil {
		t.Fatalf("more than %d items must be refused", maxDragOutItems)
	}
	if err := a.DragOutFiles([]DragItem{{Key: "a/big.bin", Size: maxDragOutBytes + 1}}); err == nil {
		t.Fatal("oversized selection must be refused")
	}
	if err := a.DragOutFiles([]DragItem{{Key: "", Size: 1}}); err == nil {
		t.Fatal("missing key must be refused")
	}
}

// The staging half of a native drag, exercised without the OLE loop: the
// transfer engine materializes the selection into a fresh clip dir at the
// leaf-of-key layout the clipboard mirror uses, and wait() reports it.
func TestDragStageStagesSelection(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	src, _ := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	st := a.startDragStage([]DragItem{{Source: "lab", Key: "/readme.md", Name: "readme.md", Size: 1}})
	if err := st.wait(); err != nil {
		t.Fatalf("staging failed: %v", err)
	}
	if len(st.paths) != 1 {
		t.Fatalf("staged paths = %v", st.paths)
	}
	if filepath.Base(st.paths[0]) != "readme.md" {
		t.Fatalf("staged leaf = %q", filepath.Base(st.paths[0]))
	}
	b, err := os.ReadFile(st.paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "x" {
		t.Fatalf("staged content = %q", b)
	}
	st.cancel() // finished job: harmless no-op
}

// A drag for a source that does not exist must fail (not hang) the wait.
func TestDragStageFailsCleanly(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	st := a.startDragStage([]DragItem{{Source: "ghost", Key: "/nope.txt", Size: 1}})
	if err := st.wait(); err == nil {
		t.Fatal("staging a missing source must fail")
	}
}

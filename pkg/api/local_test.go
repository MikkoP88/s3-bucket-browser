package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
)

// Hermetic tests for the pane-to-pane directory compare. No S3 backend:
// compareFileMaps is the pure core; the walkers are exercised over local
// dirs and the local remotefs engine in compare_test.go.

func TestCompareFileMaps(t *testing.T) {
	now := time.Now().UnixMilli()
	left := map[string]localFile{
		"only-left.txt":   {size: 10, mtime: now},
		"same.txt":        {size: 100, mtime: now},
		"size.txt":        {size: 100, mtime: now},
		"newer-left.txt":  {size: 100, mtime: now},
		"newer-right.txt": {size: 100, mtime: now},
		"tolerance.txt":   {size: 100, mtime: now},
	}
	right := map[string]localFile{
		"only-right.txt":  {size: 10, mtime: now},
		"same.txt":        {size: 100, mtime: now},
		"size.txt":        {size: 101, mtime: now},
		"newer-left.txt":  {size: 100, mtime: now - 60_000},
		"newer-right.txt": {size: 100, mtime: now + 60_000},
		"tolerance.txt":   {size: 100, mtime: now + 1_000},
	}

	rows := compareFileMaps(left, right)
	got := map[string]string{}
	for _, r := range rows {
		got[r.Key] = r.Status
	}
	want := map[string]string{
		"only-left.txt":   CmpOnlyLocal,
		"only-right.txt":  CmpOnlyRemote,
		"same.txt":        CmpSame,
		"size.txt":        CmpSizeDiff,
		"newer-left.txt":  CmpNewerLocal,
		"newer-right.txt": CmpNewerRemote,
		"tolerance.txt":   CmpSame, // 1s apart is inside the 2s clock tolerance
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("compareFileMaps(%s) = %q, want %q", k, got[k], w)
		}
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Key >= rows[i].Key {
			t.Fatalf("rows not sorted by key: %q then %q", rows[i-1].Key, rows[i].Key)
		}
	}
	// size diff wins over mtime verdicts
	if got["size.txt"] != CmpSizeDiff {
		t.Errorf("size.txt = %q, want size-diff even with equal mtimes", got["size.txt"])
	}
}

func TestCompareFileMapsEmpty(t *testing.T) {
	if rows := compareFileMaps(nil, nil); len(rows) != 0 {
		t.Errorf("empty sides produced %d rows, want 0", len(rows))
	}
}

func TestWalkLocalFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("bb"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := walkLocalFiles(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("walked %v, want 2 files", files)
	}
	if files["a.txt"].size != 3 {
		t.Errorf("a.txt size = %d, want 3", files["a.txt"].size)
	}
	nested, ok := files["sub/b.txt"]
	if !ok {
		t.Fatalf("nested key missing: %v", files)
	}
	if nested.size != 2 {
		t.Errorf("sub/b.txt size = %d, want 2", nested.size)
	}
	if nested.mtime == 0 {
		t.Error("sub/b.txt mtime not captured")
	}
}

// TestWalkLocalTreeMatchesWalkDir pins the bounded walk's parity with the
// filepath.WalkDir semantics it replaced: the same set of entries (files
// and folders, relative slash keys) below the root, unreadable
// directories skipped with their subtrees.
func TestWalkLocalTreeMatchesWalkDir(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"docs/nested", "logs", "empty"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"readme.md":          "hello",
		"docs/spec.txt":      "spec",
		"docs/nested/deep.d": "deep",
		"logs/a.log":         "log",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var want []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		want = append(want, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	if err := walkLocalTree(context.Background(), dir, func(e localEntryInfo) {
		got = append(got, e.rel)
	}); err != nil {
		t.Fatal(err)
	}

	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("walkLocalTree = %v, want WalkDir's %v", got, want)
	}
}

// TestListLocalWedgeBreaksSilentDeath pins the dual-pane navigation leg:
// the pane's directory read parks inside the kernel when the backing
// vanished (dead UNC path, disconnected mapped drive — no FIN, no RST),
// and no caller context can reach into a syscall, so the budget must:
// one verdict, bounded time, and the pane navigates again the moment the
// wire is back.
func TestListLocalWedgeBreaksSilentDeath(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	dir := t.TempDir()

	oldBudget := remotefs.LocalOpBudget
	oldReadDir := localReadDir
	defer func() { remotefs.LocalOpBudget, localReadDir = oldBudget, oldReadDir }()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	remotefs.LocalOpBudget = 50 * time.Millisecond
	localReadDir = func(string) ([]os.DirEntry, error) {
		<-release // the wedge: take the request, answer nothing
		return nil, nil
	}
	start := time.Now()
	_, err := a.ListLocal(dir)
	if !errors.Is(err, remotefs.ErrLocalDeadline) {
		t.Fatalf("wedged ListLocal = %v, want the local deadline verdict", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the budget never fired", el)
	}

	// Restored wire: the next navigation lands, same as it ever was.
	remotefs.LocalOpBudget, localReadDir = oldBudget, oldReadDir
	ents, err := a.ListLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("post-heal ListLocal = %d entries, want 0", len(ents))
	}
}

// TestExpandUploadPathsWedgeBreaksSilentDeath pins the drop-expansion leg:
// a dropped folder on a wedged volume must fail the upload promise in
// bounded time (the expansion runs in the binding, before any job exists
// to carry a cancel), and the strict walk must complete honestly once the
// wire is back.
func TestExpandUploadPathsWedgeBreaksSilentDeath(t *testing.T) {
	dir := t.TempDir()
	dropped := filepath.Join(dir, "dropped")
	if err := os.MkdirAll(dropped, 0o755); err != nil {
		t.Fatal(err)
	}

	oldBudget := remotefs.LocalOpBudget
	oldReadDir := localReadDir
	defer func() { remotefs.LocalOpBudget, localReadDir = oldBudget, oldReadDir }()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	remotefs.LocalOpBudget = 50 * time.Millisecond
	localReadDir = func(string) ([]os.DirEntry, error) {
		<-release // the wedge: take the read, answer nothing
		return nil, nil
	}
	start := time.Now()
	_, err := expandUploadPaths(context.Background(), []string{dropped}, "up/")
	if !errors.Is(err, remotefs.ErrLocalDeadline) {
		t.Fatalf("wedged expansion = %v, want the local deadline verdict", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the budget never fired", el)
	}

	// Restored wire: the strict walk lands the whole drop.
	remotefs.LocalOpBudget, localReadDir = oldBudget, oldReadDir
	if err := os.WriteFile(filepath.Join(dropped, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	pairs, err := expandUploadPaths(context.Background(), []string{dropped}, "up/")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].key != "up/dropped/a.txt" {
		t.Fatalf("post-heal expansion = %+v, want the dropped file", pairs)
	}
}

// The compare walk is complete or nothing: an unreadable directory
// fails the walk instead of handing compare a partial map it would
// render as phantom remote-only rows — the sync decisions built on
// those rows delete data that is fine.
func TestWalkLocalFilesFailsClosedOnUnreadableDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "locked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locked", "hidden.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldReadDir := localReadDir
	localReadDir = func(p string) ([]os.DirEntry, error) {
		if filepath.Base(p) == "locked" {
			return nil, fmt.Errorf("access denied")
		}
		return os.ReadDir(p)
	}
	t.Cleanup(func() { localReadDir = oldReadDir })
	_, err := walkLocalFiles(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "walking") || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("walkLocalFiles = %v, want the honest walking error", err)
	}
}

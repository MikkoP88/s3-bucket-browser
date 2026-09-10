package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

	files, err := walkLocalFiles(dir)
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

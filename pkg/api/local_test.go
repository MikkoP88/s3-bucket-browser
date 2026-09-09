package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Hermetic tests for the dual-pane directory compare (M2.5). No S3 backend:
// CompareSides is the pure core; the remote side is plain s3types.Objects.

func TestCompareSides(t *testing.T) {
	now := time.Now().UnixMilli()
	local := map[string]localFile{
		"only-local.txt":   {size: 10, mtime: now},
		"same.txt":         {size: 100, mtime: now},
		"size.txt":         {size: 100, mtime: now},
		"newer-local.txt":  {size: 100, mtime: now},
		"newer-remote.txt": {size: 100, mtime: now},
		"tolerance.txt":    {size: 100, mtime: now},
	}
	remote := map[string]s3types.Object{
		"only-remote.txt":  {Size: aws.Int64(10), LastModified: aws.Time(time.UnixMilli(now))},
		"same.txt":         {Size: aws.Int64(100), LastModified: aws.Time(time.UnixMilli(now))},
		"size.txt":         {Size: aws.Int64(101), LastModified: aws.Time(time.UnixMilli(now))},
		"newer-local.txt":  {Size: aws.Int64(100), LastModified: aws.Time(time.UnixMilli(now - 60_000))},
		"newer-remote.txt": {Size: aws.Int64(100), LastModified: aws.Time(time.UnixMilli(now + 60_000))},
		"tolerance.txt":    {Size: aws.Int64(100), LastModified: aws.Time(time.UnixMilli(now + 1_000))},
	}

	rows := CompareSides(local, remote)
	got := map[string]string{}
	for _, r := range rows {
		got[r.Key] = r.Status
	}
	want := map[string]string{
		"only-local.txt":   CmpOnlyLocal,
		"only-remote.txt":  CmpOnlyRemote,
		"same.txt":         CmpSame,
		"size.txt":         CmpSizeDiff,
		"newer-local.txt":  CmpNewerLocal,
		"newer-remote.txt": CmpNewerRemote,
		"tolerance.txt":    CmpSame, // 1s apart is inside the 2s clock tolerance
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("CompareSides(%s) = %q, want %q", k, got[k], w)
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

func TestCompareSidesEmpty(t *testing.T) {
	if rows := CompareSides(nil, nil); len(rows) != 0 {
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

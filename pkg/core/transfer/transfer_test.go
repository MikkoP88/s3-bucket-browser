package transfer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestJoinKey(t *testing.T) {
	cases := map[string]string{
		JoinKey("photos", "2026"):        "photos/2026/",
		JoinKey("photos/", "", "/2026/"): "photos/2026/",
		JoinKey("a\\b", "c"):             "a/b/c/",
		JoinKey():                        "",
		JoinKey("", ""):                  "",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("JoinKey produced %q, want %q", got, want)
		}
	}
}

func TestProgressReader(t *testing.T) {
	var calls int64
	pr := &progressReader{
		r:        strings.NewReader("hello world"),
		fn:       func(sent, total int64) { calls++ },
		total:    11,
		reportOn: true,
	}
	buf := make([]byte, 5)
	pr.Read(buf) // hel
	pr.Read(buf) // lo w
	if calls != 2 {
		t.Errorf("progress calls = %d, want 2 (reportOn)", calls)
	}
}

func TestRateLimiter(t *testing.T) {
	if newRateLimiter(0) != nil || newRateLimiter(-100) != nil {
		t.Error("non-positive bps must mean unlimited (nil limiter)")
	}
	var nilLimiter *rateLimiter
	nilLimiter.wait(1 << 20) // must not panic
	r := newRateLimiter(1000)

	// First write fits in the one-second burst budget: no waiting.
	start := time.Now()
	r.wait(500)
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Errorf("first 500B waited %v, want ~0", el)
	}

	// Second write exceeds the budget (500 left, 1500 needed) → ~1s sleep.
	start = time.Now()
	r.wait(1500)
	if el := time.Since(start); el < 700*time.Millisecond {
		t.Errorf("throttled write waited only %v, want >= ~1s", el)
	}

	// Negative sizes are a no-op.
	r.wait(-5)
}

// fakeLister answers ListObjectsV2 with one fixed page of keys (the keys a
// real store would return for the request's prefix).
type fakeLister struct {
	keys map[string][]string // prefix -> listed keys
}

func (f *fakeLister) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(in.Prefix)
	objs := make([]s3types.Object, 0, len(f.keys[prefix]))
	for _, k := range f.keys[prefix] {
		objs = append(objs, s3types.Object{Key: aws.String(k)})
	}
	return &s3.ListObjectsV2Output{Contents: objs, IsTruncated: aws.Bool(false)}, nil
}

// CollectPrefixKeys must be a pass-through of exactly what the store lists,
// and IncludeFolderMarker is the delete-only fix for stores (MinIO) that
// omit "dir/" from a listing under "dir/" — a recursive delete that misses
// the marker leaves a ghost folder row behind. Non-delete callers (copies,
// storage-class conversion) must NOT get markers folded in, so the append
// lives in the helper, never in CollectPrefixKeys itself.
func TestCollectPrefixKeysIncludesFolderMarker(t *testing.T) {
	lister := &fakeLister{keys: map[string][]string{
		"zz/": {"zz/a.txt", "zz/sub/b.txt"}, // MinIO: marker omitted
		"aw/": {"aw/a.txt", "aw/"},          // AWS: marker listed
	}}
	keys, err := CollectPrefixKeys(context.Background(), lister, "b", "zz/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("collect is a pass-through = %v, want the 2 listed keys only", keys)
	}
	// The delete path appends the missing marker exactly once.
	keys = IncludeFolderMarker(keys, "zz/")
	if len(keys) != 3 || keys[len(keys)-1] != "zz/" {
		t.Fatalf("IncludeFolderMarker under MinIO-like store = %v, want marker zz/ appended", keys)
	}
	if dup := IncludeFolderMarker(keys, "zz/"); len(dup) != 3 {
		t.Fatalf("marker appended twice: %v", dup)
	}

	keys, err = CollectPrefixKeys(context.Background(), lister, "b", "aw/")
	if err != nil {
		t.Fatal(err)
	}
	keys = IncludeFolderMarker(keys, "aw/")
	if n := countKey(keys, "aw/"); n != 1 {
		t.Fatalf("marker listed by the store appears %d time(s), want exactly 1 (keys=%v)", n, keys)
	}

	// Non-prefix keys are left untouched (explicit file deletes).
	if got := IncludeFolderMarker([]string{"a.txt"}, "a.txt"); len(got) != 1 {
		t.Fatalf("non-folder key must pass through unchanged: %v", got)
	}
}

func countKey(keys []string, want string) int {
	n := 0
	for _, k := range keys {
		if k == want {
			n++
		}
	}
	return n
}

// stagingResidue reports any .s3b-part-* staging file left in dir — the
// write contract allows none after a settled (non-killed) outcome.
func stagingResidue(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".s3b-part-") {
			left = append(left, e.Name())
		}
	}
	return left
}

// The critical-data write contract: a failed or canceled write must leave
// the pre-existing destination file byte-identical and leave no staging
// residue behind.
func TestStageAndCommitFailureNeverTruncates(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(dst, []byte("ORIGINAL BYTES\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("connection reset mid-body")
	err := StageAndCommit(dst, func(f *os.File) error {
		if _, err := f.WriteString("PARTIAL"); err != nil {
			t.Fatal(err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the write error surfaced", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ORIGINAL BYTES\n" {
		t.Fatalf("failed write damaged the destination: %q", got)
	}
	if left := stagingResidue(t, dir); len(left) > 0 {
		t.Fatalf("staging residue after a settled failure: %v", left)
	}
}

// A successful write replaces the destination whole (or creates it), with
// no residue — and keeps the permissions of the file it replaced.
func TestStageAndCommitSuccessReplacesWhole(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(dst, []byte("OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StageAndCommit(dst, func(f *os.File) error {
		_, err := f.WriteString("NEW COMPLETE BYTES\n")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW COMPLETE BYTES\n" {
		t.Fatalf("committed content = %q", got)
	}
	if left := stagingResidue(t, dir); len(left) > 0 {
		t.Fatalf("staging residue after a clean commit: %v", left)
	}

	// A fresh destination (no predecessor) lands the same way.
	fresh := filepath.Join(dir, "sub", "new.txt")
	if err := StageAndCommit(fresh, func(f *os.File) error {
		_, err := f.WriteString("first\n")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(fresh); err != nil || string(b) != "first\n" {
		t.Fatalf("fresh file = %q (%v)", b, err)
	}
}

func TestStageAndCommitKeepsReplacedMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are advisory on Windows")
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "secret.key")
	if err := os.WriteFile(dst, []byte("k1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StageAndCommit(dst, func(f *os.File) error {
		_, err := f.WriteString("k2\n")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Fatalf("replaced file mode = %v, want the predecessor's 0600", perm)
	}
}

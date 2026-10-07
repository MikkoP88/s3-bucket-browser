package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempsIn counts leftover staging files in dir — zero is the contract
// on every exit path, success or failure.
func tempsIn(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".s3b-tmp-") {
			n++
		}
	}
	return n
}

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := Write(p, []byte("{\"a\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{\"a\":1}" {
		t.Fatalf("content = %q", b)
	}
	if runtime.GOOS != "windows" { // Windows perms via Stat are not meaningful
		if st, err := os.Stat(p); err != nil {
			t.Fatal(err)
		} else if st.Mode().Perm() != 0o600 {
			t.Fatalf("perm = %o, want 600", st.Mode().Perm())
		}
	}
	if n := tempsIn(t, dir); n != 0 {
		t.Fatalf("%d temp file(s) left after success", n)
	}
}

func TestWriteReplacesFully(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	long := strings.Repeat("x", 4096)
	if err := Write(p, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	// A shorter rewrite must leave no tail of the old bytes — the
	// truncation-residue class O_TRUNC writes only avoid by luck.
	if err := Write(p, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "short" {
		t.Fatalf("content = %q, want %q", b, "short")
	}
}

// TestWriteFailureKeepsPreviousFile pins the round's whole point: a
// write that fails at the replace step leaves the previous good file
// byte-for-byte intact and strands no staging file. The rename seam is
// the only step that touches the target, so breaking it simulates every
// late failure (target locked, disk quota, FS error) in one move.
func TestWriteFailureKeepsPreviousFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "store.json")
	if err := Write(p, []byte("GOOD BYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := rename
	rename = func(string, string) error { return errors.New("replace refused") }
	defer func() { rename = saved }()

	if err := Write(p, []byte("NEW BYTES"), 0o600); err == nil {
		t.Fatal("write with a failing rename must error")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "GOOD BYTES" {
		t.Fatalf("previous file damaged: %q", b)
	}
	if n := tempsIn(t, dir); n != 0 {
		t.Fatalf("%d temp file(s) left after failure", n)
	}
}

// TestWriteOntoDirectoryFailsClean targets the no-seam path: a
// directory squatting on the target name fails the rename for real, the
// error surfaces, and nothing is staged or left behind.
func TestWriteOntoDirectoryFailsClean(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("x"), 0o600); err == nil {
		t.Fatal("writing onto a directory must fail")
	}
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		t.Fatalf("target dir disturbed: %v %v", st, err)
	}
	if n := tempsIn(t, dir); n != 0 {
		t.Fatalf("%d temp file(s) left after failure", n)
	}
}

// TestWriteMissingDirErrors keeps the caller-owned-mkdir contract
// observable: the temp cannot be created and the helper says so rather
// than inventing the directory.
func TestWriteMissingDirErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nope", "state.json")
	if err := Write(p, []byte("x"), 0o600); err == nil {
		t.Fatal("write into a missing directory must fail")
	}
}

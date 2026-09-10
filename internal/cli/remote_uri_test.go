// remote_uri_test.go: hermetic coverage for source-URI commands (M10.5) —
// all through the local engine (source type "local" rooted at a temp
// dir), so no network or S3 backend is needed.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// appendTestEvent simulates GUI activity landing in the shared event log.
func appendTestEvent(t *testing.T, level, scope, msg string) {
	t.Helper()
	eventlog.Append(level, scope, msg)
}

// srcEnv sets up a throwaway config dir with one local source "lab"
// rooted at a populated temp tree.
func srcEnv(t *testing.T) string {
	t.Helper()
	cliEnv(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := Execute([]string{"source", "add", "lab", "--type", "local", "--root", root}); code != 0 {
		t.Fatalf("source add: exit %d", code)
	}
	return root
}

func TestRemoteLsTreeDuStat(t *testing.T) {
	root := srcEnv(t)

	out, code := captureOut(t, "ls", "lab://")
	if code != 0 {
		t.Fatalf("ls lab://: exit %d", code)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "docs") {
		t.Fatalf("ls lab:// missing entries: %s", out)
	}

	j, code := captureOut(t, "ls", "lab://", "--json")
	if code != 0 || !strings.Contains(j, `"name": "a.txt"`) {
		t.Fatalf("ls --json: exit %d, %s", code, j)
	}

	rec, code := captureOut(t, "ls", "lab://", "-r")
	if code != 0 || !strings.Contains(rec, "docs/b.txt") {
		t.Fatalf("ls -r lab://: exit %d, %s", code, rec)
	}

	tree, code := captureOut(t, "tree", "lab://docs")
	if code != 0 || !strings.Contains(tree, "b.txt") {
		t.Fatalf("tree lab://docs: exit %d, %s", code, tree)
	}

	du, code := captureOut(t, "du", "lab://")
	if code != 0 || !strings.Contains(du, "2 object(s)") {
		t.Fatalf("du lab://: exit %d, %s", code, du)
	}

	st, code := captureOut(t, "stat", "lab://a.txt")
	if code != 0 || !strings.Contains(st, "5 bytes") {
		t.Fatalf("stat lab://a.txt: exit %d, %s", code, st)
	}
	_ = root
}

func TestRemoteMkdirRm(t *testing.T) {
	srcEnv(t)

	if code := Execute([]string{"mkdir", "lab://new/dir"}); code != 0 {
		t.Fatalf("mkdir: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(reloadStoreRoot(t), "new", "dir")); err != nil {
		t.Fatalf("mkdir did not create the tree: %v", err)
	}

	// rm without --recursive refuses on a folder.
	if code := Execute([]string{"rm", "lab://docs"}); code != exitUsage {
		t.Fatalf("rm folder without -r: exit %d (want usage %d)", code, exitUsage)
	}
	if code := Execute([]string{"rm", "lab://docs", "-r", "--dry-run"}); code != 0 {
		t.Fatalf("rm dry-run: exit %d", code)
	}
	if code := Execute([]string{"rm", "lab://docs", "-r"}); code != 0 {
		t.Fatalf("rm -r: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(reloadStoreRoot(t), "docs")); !os.IsNotExist(err) {
		t.Fatalf("rm -r left the folder behind: %v", err)
	}

	// Single file rm without flags.
	if code := Execute([]string{"rm", "lab://a.txt"}); code != 0 {
		t.Fatalf("rm file: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(reloadStoreRoot(t), "a.txt")); !os.IsNotExist(err) {
		t.Fatal("rm file left it behind")
	}
}

func TestRemoteCopyLocal(t *testing.T) {
	srcEnv(t)
	dst := t.TempDir()

	// single file → local dir
	if code := Execute([]string{"cp", "lab://a.txt", dst}); code != 0 {
		t.Fatalf("cp file: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("cp file content: %q %v", b, err)
	}

	// tree → local dir
	if code := Execute([]string{"cp", "lab://", filepath.Join(dst, "out"), "-r"}); code != 0 {
		t.Fatalf("cp tree: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "out", "docs", "b.txt")); err != nil || string(b) != "beta" {
		t.Fatalf("cp tree content: %q %v", b, err)
	}

	// local file → remote leaf name
	if code := Execute([]string{"cp", filepath.Join(dst, "a.txt"), "lab://copy.txt"}); code != 0 {
		t.Fatalf("cp local->remote: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(reloadStoreRoot(t), "copy.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("cp local->remote content: %q %v", b, err)
	}

	// local tree → remote folder (trailing slash)
	if code := Execute([]string{"cp", dst, "lab://mirrored/", "-r"}); code != 0 {
		t.Fatalf("cp local tree->remote: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(reloadStoreRoot(t), "mirrored", "out", "docs", "b.txt")); err != nil {
		t.Fatalf("mirrored tree missing b.txt: %v", err)
	}
}

func TestRemoteCopyRemoteSameEngine(t *testing.T) {
	srcEnv(t)

	// lab:// → lab:// exercises the same-engine temp-file spool.
	if code := Execute([]string{"cp", "lab://docs", "lab://mirror", "-r"}); code != 0 {
		t.Fatalf("cp remote->remote: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(reloadStoreRoot(t), "mirror", "b.txt")); err != nil || string(b) != "beta" {
		t.Fatalf("remote->remote content: %q %v", b, err)
	}
}

func TestRemoteMvRemovesSource(t *testing.T) {
	srcEnv(t)
	dst := t.TempDir()

	if code := Execute([]string{"mv", "lab://a.txt", dst}); code != 0 {
		t.Fatalf("mv: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("mv content: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(reloadStoreRoot(t), "a.txt")); !os.IsNotExist(err) {
		t.Fatal("mv left the source behind")
	}
}

func TestSourceURIUnknownName(t *testing.T) {
	cliEnv(t)
	if code := Execute([]string{"ls", "ghost://"}); code != exitUsage {
		t.Fatalf("unknown source: exit %d (want usage %d)", code, exitUsage)
	}
}

func TestLogCommand(t *testing.T) {
	cliEnv(t)
	// The event log is populated through the shared package; simulate GUI
	// activity by appending directly, then read via the command.
	if code := Execute([]string{"log"}); code != 0 {
		t.Fatalf("log empty: exit %d", code)
	}
	appendTestEvent(t, "info", "transfer", "copied a.txt")
	appendTestEvent(t, "error", "delete", "boom")

	out, code := captureOut(t, "log", "--json")
	if code != 0 || !strings.Contains(out, "copied a.txt") || !strings.Contains(out, "boom") {
		t.Fatalf("log --json: exit %d, %s", code, out)
	}
	errOnly, code := captureOut(t, "log", "--json", "--level", "error")
	if code != 0 || strings.Contains(errOnly, "copied") || !strings.Contains(errOnly, "boom") {
		t.Fatalf("log --level error: exit %d, %s", code, errOnly)
	}
	if code := Execute([]string{"log", "--level", "bogus"}); code != exitUsage {
		t.Fatalf("bad level: exit %d", code)
	}
}

// reloadStoreRoot returns the local root of the "lab" source.
func reloadStoreRoot(t *testing.T) string {
	t.Helper()
	s := reloadStore(t)
	src, err := s.GetSource("lab")
	if err != nil {
		t.Fatal(err)
	}
	return src.LocalRoot
}

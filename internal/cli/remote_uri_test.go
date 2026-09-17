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
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// appendTestEvent simulates GUI activity landing in the shared event log.
func appendTestEvent(t *testing.T, level, scope, msg string) {
	t.Helper()
	eventlog.Append(level, scope, "", msg)
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

func TestRemoteCopySingleFileFolderDestinations(t *testing.T) {
	// Regression: a single file onto a folder-style remote destination
	// (trailing slash, or a URI naming an existing folder) must land at
	// dir/file.txt — not dir/file.txt/file.txt.
	srcEnv(t)
	root := reloadStoreRoot(t)

	if code := Execute([]string{"cp", filepath.Join(root, "a.txt"), "lab://docs/"}); code != 0 {
		t.Fatalf("cp file -> trailing-slash dir: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(root, "docs", "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("trailing-slash dst: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "a.txt", "a.txt")); err == nil {
		t.Fatal("trailing-slash dst nested the file inside a directory")
	}

	if code := Execute([]string{"cp", filepath.Join(root, "a.txt"), "lab://docs"}); code != 0 {
		t.Fatalf("cp file -> existing dir: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(root, "docs", "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("existing-dir dst: %q %v", b, err)
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

// ---- S3 source URIs (cp/mv operands) ----

func TestS3SourcePathSplit(t *testing.T) {
	scoped := profile.Source{Name: "lab", Type: profile.TypeS3, Bucket: "scoped"}
	for _, tc := range []struct {
		rest, bucket, key string
	}{
		{rest: "/", bucket: "scoped", key: ""},
		{rest: "docs", bucket: "scoped", key: "docs"},
		{rest: "docs/", bucket: "scoped", key: "docs"},
		{rest: "/docs/a.txt", bucket: "scoped", key: "docs/a.txt"},
	} {
		b, k, err := s3SourcePath(scoped, tc.rest)
		if err != nil || b != tc.bucket || k != tc.key {
			t.Fatalf("scoped %q: got (%q,%q,%v), want (%q,%q)", tc.rest, b, k, err, tc.bucket, tc.key)
		}
	}

	acct := profile.Source{Name: "lab", Type: profile.TypeS3}
	for _, tc := range []struct {
		rest, bucket, key string
	}{
		{rest: "b1", bucket: "b1", key: ""},
		{rest: "b1/", bucket: "b1", key: ""},
		{rest: "b1/a.txt", bucket: "b1", key: "a.txt"},
		{rest: "/b1/docs/b.txt", bucket: "b1", key: "docs/b.txt"},
	} {
		b, k, err := s3SourcePath(acct, tc.rest)
		if err != nil || b != tc.bucket || k != tc.key {
			t.Fatalf("account-wide %q: got (%q,%q,%v), want (%q,%q)", tc.rest, b, k, err, tc.bucket, tc.key)
		}
	}
	if _, _, err := s3SourcePath(acct, "/"); err == nil {
		t.Fatal("account-wide root must demand a bucket segment")
	}
}

func TestS3SourceRefURIForms(t *testing.T) {
	for _, tc := range []struct {
		ref      s3SourceRef
		bucket   string
		key      string
		isPrefix bool
		hasKey   bool
		uri      string
	}{
		{ref: s3SourceRef{bucket: "b"}, bucket: "b", uri: "s3://b"},                                                                             // bucket root
		{ref: s3SourceRef{bucket: "b", key: "docs", folder: true}, bucket: "b", key: "docs", isPrefix: true, hasKey: true, uri: "s3://b/docs/"}, // folder
		{ref: s3SourceRef{bucket: "b", key: "docs/a.txt"}, bucket: "b", key: "docs/a.txt", hasKey: true, uri: "s3://b/docs/a.txt"},              // object
		{ref: s3SourceRef{bucket: "b", folder: true}, bucket: "b", uri: "s3://b"},                                                               // root w/ slash == bucket root
	} {
		u := tc.ref.s3URI()
		if u.Bucket != tc.bucket || u.Key != tc.key || u.IsPrefix != tc.isPrefix || u.HasPrefix != tc.hasKey {
			t.Fatalf("s3URI(%+v): got %+v", tc.ref, u)
		}
		if got := tc.ref.uriStr(); got != tc.uri {
			t.Fatalf("uriStr(%+v): got %q want %q", tc.ref, got, tc.uri)
		}
	}
}

// addS3Source stores an S3 source without touching the network.
func addS3Source(t *testing.T, name, bucket string) {
	t.Helper()
	args := []string{"source", "add", name, "--type", "s3",
		"--endpoint", "http://127.0.0.1:1", "--access-key", "test", "--secret-key", "test"}
	if bucket != "" {
		args = append(args, "--bucket", bucket)
	}
	if code := Execute(args); code != 0 {
		t.Fatalf("source add %s: exit %d", name, code)
	}
}

func TestS3SourceURIReadCommandsRejected(t *testing.T) {
	cliEnv(t)
	addS3Source(t, "s3lab", "scoped")

	// Browsing stays on s3:// URIs — the error points both ways.
	if code := Execute([]string{"ls", "s3lab://"}); code != exitUsage {
		t.Fatalf("ls s3lab://: exit %d (want usage %d)", code, exitUsage)
	}
}

func TestS3SourceCopyGrammar(t *testing.T) {
	cliEnv(t)
	addS3Source(t, "pb", "my-bucket") // per-bucket
	addS3Source(t, "acct", "")        // account-wide
	dst := t.TempDir()

	if code := Execute([]string{"cp", "ghost://x", dst}); code != exitUsage {
		t.Fatalf("cp unknown source: exit %d (want usage %d)", code, exitUsage)
	}
	if code := Execute([]string{"cp", "acct://", dst}); code != exitUsage {
		t.Fatalf("cp account-wide root: exit %d (want usage %d)", code, exitUsage)
	}
	// Same source, same synthesized object: the server-side path's own
	// same-object gate fires before any network call.
	if code := Execute([]string{"cp", "pb://x.txt", "pb://x.txt"}); code != exitUsage {
		t.Fatalf("cp same object: exit %d (want usage %d)", code, exitUsage)
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

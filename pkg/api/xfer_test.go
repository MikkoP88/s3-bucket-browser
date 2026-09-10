package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// emptyLocalSource builds a TypeLocal source over an empty temp dir.
func emptyLocalSource(t *testing.T, name string) (profile.Source, string) {
	t.Helper()
	root := t.TempDir()
	return profile.Source{Name: name, Type: profile.TypeLocal, LocalRoot: root}, root
}

// waitXferJob polls the transfer manager until the job leaves "running".
func waitXferJob(t *testing.T, a *App, id string) JobInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, ji := range a.ActiveTransfers() {
			if ji.ID == id && ji.Status != JobRunning {
				return ji
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("transfer job %s did not finish in time", id)
	return JobInfo{}
}

func mustXfer(t *testing.T, a *App, items []XferItem, localPaths []string, dest XferDest, policy string, move bool) JobInfo {
	t.Helper()
	id, err := a.TransferCross(items, localPaths, dest, policy, 0, move)
	if err != nil {
		t.Fatalf("TransferCross: %v", err)
	}
	return waitXferJob(t, a, id)
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func mustExist(t *testing.T, p string, dir bool) {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	if dir != st.IsDir() {
		t.Fatalf("%s: isDir = %v, want %v", p, st.IsDir(), dir)
	}
}

func mustNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Fatalf("%s must not exist", p)
	}
}

// Remote → remote copies over two local engines: file, empty dir
// (created at the destination), nested destination, overwrite, rename
// and skip policies.
func TestTransferCrossRemoteToRemote(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, labRoot := localSource(t, "lab") // docs/ (empty) + readme.md ("x")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}
	vault := XferDest{Kind: "remote", Source: "vault", Dir: "/"}

	// Single file.
	ji := mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, vault, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.DoneFiles != 1 || ji.TotalFiles != 1 || ji.TotalBytes != 1 || ji.FailedFiles != 0 {
		t.Fatalf("job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "x" {
		t.Fatalf("vault/readme.md = %q", got)
	}

	// Empty directory is materialized at the destination.
	mustXfer(t, a, []XferItem{{Source: "lab", Key: "/docs/", IsDir: true}}, nil, vault, PolicyOverwrite, false)
	mustExist(t, filepath.Join(vaultRoot, "docs"), true)

	// Nested destination directory is created implicitly.
	mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil,
		XferDest{Kind: "remote", Source: "vault", Dir: "/sub"}, PolicyOverwrite, false)
	if got := read(t, filepath.Join(vaultRoot, "sub", "readme.md")); got != "x" {
		t.Fatalf("vault/sub/readme.md = %q", got)
	}

	// Overwrite replaces, skip leaves the destination alone.
	if err := os.WriteFile(filepath.Join(vaultRoot, "readme.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ji = mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, vault, PolicySkip, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("skip job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "old" {
		t.Fatalf("skip overwrote the destination: %q", got)
	}
	mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, vault, PolicyOverwrite, false)
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "x" {
		t.Fatalf("overwrite did not replace: %q", got)
	}

	// Rename finds a "name (n).ext" slot.
	mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, vault, PolicyRename, false)
	if got := read(t, filepath.Join(vaultRoot, "readme (1).md")); got != "x" {
		t.Fatalf("rename copy = %q", got)
	}

	_ = labRoot
}

// Move semantics: copy-then-delete where a skipped file is NOT a
// success — the source keeps the item — and a clean move deletes files,
// empty directories and the item root. Same-source copies stream
// through a temp file (one data connection per engine).
func TestTransferCrossMove(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, labRoot := localSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}
	vault := XferDest{Kind: "remote", Source: "vault", Dir: "/"}

	// Skip-policy move never deletes the source.
	if err := os.WriteFile(filepath.Join(vaultRoot, "readme.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ji := mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, vault, PolicySkip, true)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("skip-move job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "old" {
		t.Fatalf("skip-move changed the destination: %q", got)
	}
	mustExist(t, filepath.Join(labRoot, "readme.md"), false) // source kept

	// Clean move of an empty directory: created at dest, gone at source.
	mustXfer(t, a, []XferItem{{Source: "lab", Key: "/docs/", IsDir: true}}, nil, vault, PolicyOverwrite, true)
	mustExist(t, filepath.Join(vaultRoot, "docs"), true)
	mustNotExist(t, filepath.Join(labRoot, "docs"))

	// Clean move of a file.
	mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, vault, PolicyOverwrite, true)
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "x" {
		t.Fatalf("moved content = %q", got)
	}
	mustNotExist(t, filepath.Join(labRoot, "readme.md"))

	// Same-source copy (vault → vault) goes through the temp-file path.
	mustXfer(t, a, []XferItem{{Source: "vault", Key: "/readme.md", Size: 1}}, nil,
		XferDest{Kind: "remote", Source: "vault", Dir: "/copy"}, PolicyOverwrite, false)
	if got := read(t, filepath.Join(vaultRoot, "copy", "readme.md")); got != "x" {
		t.Fatalf("same-source copy = %q", got)
	}
}

// The local-pane sides: local paths as source (files, trees, empty
// dirs) into remote and local destinations, remote items into a local
// destination, and a local-pane move.
func TestTransferCrossLocalPane(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, _ := localSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}

	// Seed a local-pane tree: a.txt, sub/b.bin, empty/.
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "b.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Tree → remote dest keeps names, nesting and the empty directory.
	ji := mustXfer(t, a, nil, []string{srcDir},
		XferDest{Kind: "remote", Source: "vault", Dir: "/pane"}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.TotalFiles != 2 || ji.TotalBytes != 8 || ji.FailedFiles != 0 {
		t.Fatalf("tree job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "pane", filepath.Base(srcDir), "a.txt")); got != "aaa" {
		t.Fatalf("a.txt = %q", got)
	}
	if got := read(t, filepath.Join(vaultRoot, "pane", filepath.Base(srcDir), "sub", "b.bin")); got != "12345" {
		t.Fatalf("b.bin = %q", got)
	}
	mustExist(t, filepath.Join(vaultRoot, "pane", filepath.Base(srcDir), "empty"), true)

	// Remote → local destination.
	dl := t.TempDir()
	ji = mustXfer(t, a, []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil,
		XferDest{Kind: "local", Dir: dl}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("remote→local job = %+v", ji)
	}
	if got := read(t, filepath.Join(dl, "readme.md")); got != "x" {
		t.Fatalf("downloaded = %q", got)
	}

	// Local → local.
	dl2 := t.TempDir()
	mustXfer(t, a, nil, []string{filepath.Join(srcDir, "a.txt")}, XferDest{Kind: "local", Dir: dl2}, PolicyOverwrite, false)
	if got := read(t, filepath.Join(dl2, "a.txt")); got != "aaa" {
		t.Fatalf("local→local = %q", got)
	}

	// Local-pane move: copy then delete the source file.
	mustXfer(t, a, nil, []string{filepath.Join(srcDir, "a.txt")},
		XferDest{Kind: "remote", Source: "vault", Dir: "/moved"}, PolicyOverwrite, true)
	if got := read(t, filepath.Join(vaultRoot, "moved", "a.txt")); got != "aaa" {
		t.Fatalf("moved = %q", got)
	}
	mustNotExist(t, filepath.Join(srcDir, "a.txt"))
}

// Guards: everything invalid fails synchronously with a clear error.
func TestTransferCrossGuards(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, _ := localSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}

	if _, err := a.TransferCross([]XferItem{{Source: "nope", Key: "/x"}}, nil,
		XferDest{Kind: "remote", Source: "lab", Dir: "/"}, "", 0, false); err == nil {
		t.Error("unknown item source must error")
	}
	if _, err := a.TransferCross([]XferItem{{Source: "lab", Key: "/x"}}, nil,
		XferDest{Kind: "remote", Source: "nope", Dir: "/"}, "", 0, false); err == nil {
		t.Error("unknown dest source must error")
	}
	if _, err := a.TransferCross(nil, nil, XferDest{Kind: "remote", Source: "lab", Dir: "/"}, "", 0, false); err == nil {
		t.Error("nothing to transfer must error")
	}
	if _, err := a.TransferCross([]XferItem{{Source: "lab", Key: "/x", Size: 1}}, nil,
		XferDest{Kind: "remote", Source: "lab", Dir: "/"}, "bogus", 0, false); err == nil {
		t.Error("unknown policy must error")
	}
	if _, err := a.TransferCross([]XferItem{{Source: "lab", Key: "/x", Size: 1}}, nil,
		XferDest{Kind: "wat"}, "", 0, false); err == nil {
		t.Error("unknown dest kind must error")
	}
	if _, err := a.TransferCross(nil, []string{t.TempDir()},
		XferDest{Kind: "local", Dir: filepath.Join(t.TempDir(), "missing")}, "", 0, false); err == nil {
		t.Error("missing local dest must error")
	}
	if _, err := a.TransferCross([]XferItem{{Source: "", Bucket: "b", Key: "k", Size: 1}}, nil,
		XferDest{Kind: "s3", Bucket: ""}, "", 0, false); err == nil {
		t.Error("s3 dest without bucket must error")
	}
	// The source root is never transferable as one item.
	if _, err := a.TransferCross([]XferItem{{Source: "lab", Key: "/", IsDir: true}}, nil,
		XferDest{Kind: "remote", Source: "lab", Dir: "/"}, "", 0, false); err == nil {
		t.Error("source root must error")
	}
}

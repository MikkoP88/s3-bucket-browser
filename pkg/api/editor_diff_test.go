package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// The diff gate's own verdicts: EditDiff shows both sides of an open
// session — the staged edit versus the server's current bytes — so Push
// anyway and Reload from server consent against content. The S3 leg
// rides the one-bucket fake (the sampled texts, the full byte counts
// beside cut samples, the teammate-deleted miss, the binary verdict),
// the engine leg rides the lab source (the same laws through Open under
// the source lock, the mtime-only conflict whose contents are
// identical), and the refusals speak the typed grammar.

func TestEditDiffS3Leg(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "notes.md", "hello edit")
	url := f.serve(t)
	if err := a.SaveSource(fakeS3Source("editdiff", url)); err != nil {
		t.Fatal(err)
	}
	oldOpen := openInEditor
	openInEditor = func(*App, string, bool) error { return nil }
	t.Cleanup(func() { openInEditor = oldOpen })

	info, err := a.EditObject("docs", "notes.md", false)
	if err != nil {
		t.Fatal(err)
	}
	// stop the session like every editor rig: no watcher goroutine and no
	// registry entry outlives the test (the diff legs never shorten the
	// poll, so a leaked watcher would sit on the shipped 1.2s cadence).
	t.Cleanup(func() { _ = a.StopEdit("docs", "notes.md", false) })
	// the user edited; the teammate landed something else
	if err := os.WriteFile(info.Local, []byte("hello edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.seed("docs", "notes.md", "hello server")

	d, err := a.EditDiff(EditTarget{Bucket: "docs", Key: "notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "s3" || d.Bucket != "docs" || d.Key != "notes.md" || d.Source != "" {
		t.Fatalf("identity = %+v, want the docs/notes.md s3 session", d)
	}
	if d.Local != "hello edited" || d.Remote != "hello server" {
		t.Fatalf("samples = %q / %q, want the staged edit vs the server bytes", d.Local, d.Remote)
	}
	if d.LocalBytes != 12 || d.RemoteBytes != 12 {
		t.Fatalf("byte counts = %d/%d, want 12/12", d.LocalBytes, d.RemoteBytes)
	}
	if d.LocalTrunc || d.RemoteTrunc || d.RemoteMissing || d.Binary {
		t.Fatalf("flags = trunc(%v,%v) missing=%v binary=%v, want all clear", d.LocalTrunc, d.RemoteTrunc, d.RemoteMissing, d.Binary)
	}

	// the cap: a big object samples honestly — cut text, full counts
	oldCap := editDiffCap
	editDiffCap = 5
	defer func() { editDiffCap = oldCap }()
	d, err = a.EditDiff(EditTarget{Bucket: "docs", Key: "notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.LocalTrunc || !d.RemoteTrunc {
		t.Fatalf("trunc flags = %v/%v, want both cut", d.LocalTrunc, d.RemoteTrunc)
	}
	if d.Local != "hello" || d.Remote != "hello" {
		t.Fatalf("cut samples = %q / %q, want the first 5 bytes each", d.Local, d.Remote)
	}
	if d.LocalBytes != 12 || d.RemoteBytes != 12 {
		t.Fatalf("byte counts after the cut = %d/%d, want the full sizes 12/12", d.LocalBytes, d.RemoteBytes)
	}
	editDiffCap = oldCap

	// binary content: a NUL in either sample turns the line diff off
	if err := os.WriteFile(info.Local, []byte("bin\x00ry edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err = a.EditDiff(EditTarget{Bucket: "docs", Key: "notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Binary {
		t.Fatal("Binary = false for a NUL-carrying staged edit, want true")
	}

	// the teammate-deleted case: the diff says so instead of failing —
	// Push anyway then recreates, a reload fails honestly on its own leg
	c, err := a.client("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transfer.DeleteKeys(context.Background(), c.S3, "docs", []string{"notes.md"}); err != nil {
		t.Fatal(err)
	}
	d, err = a.EditDiff(EditTarget{Bucket: "docs", Key: "notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.RemoteMissing || d.Remote != "" || d.RemoteBytes != 0 {
		t.Fatalf("deleted-remote diff = missing(%v) remote(%q,%d), want the honest miss", d.RemoteMissing, d.Remote, d.RemoteBytes)
	}
	if d.Local != "bin\x00ry edited" {
		t.Fatalf("local side = %q, want the staged bytes to survive the miss", d.Local)
	}
}

// The engine leg: the same two-sided law through the lab source's Open
// under the source lock — including the mtime-only conflict, where the
// guard refuses a push whose contents are identical and the diff says
// exactly that (both sides the same bytes).
func TestEditDiffRemoteLeg(t *testing.T) {
	a, src, root := labEditorApp(t, "lab", "/cfg.conf", "base\n")
	s := openLabEdit(t, a, src.Name, "/cfg.conf")
	tgt := EditTarget{Kind: "remote", Source: src.Name, Key: "/cfg.conf"}
	t.Cleanup(func() { _ = a.StopEditFile(tgt, false) })

	// the user edited; the teammate landed something else
	if err := os.WriteFile(s.Local, []byte("base\nlocal edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engFile := filepath.Join(root, "cfg.conf")
	if err := os.WriteFile(engFile, []byte("base\nserver now\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := a.EditDiff(EditTarget{Kind: "remote", Source: src.Name, Key: "/cfg.conf"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "remote" || d.Source != src.Name || d.Key != "/cfg.conf" {
		t.Fatalf("identity = %+v, want the lab remote session", d)
	}
	if d.Local != "base\nlocal edit\n" || d.Remote != "base\nserver now\n" {
		t.Fatalf("samples = %q / %q, want the staged edit vs the engine bytes", d.Local, d.Remote)
	}
	if d.RemoteMissing || d.Binary {
		t.Fatalf("flags missing=%v binary=%v, want all clear", d.RemoteMissing, d.Binary)
	}

	// the mtime-only conflict: same bytes on both sides, the engine's
	// clock moved — the diff shows identical contents, no phantom lines
	if err := os.WriteFile(s.Local, []byte("base\nserver now\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(engFile, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	d, err = a.EditDiff(EditTarget{Kind: "remote", Source: src.Name, Key: "/cfg.conf"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Local != d.Remote {
		t.Fatalf("mtime-only conflict samples = %q / %q, want identical bytes", d.Local, d.Remote)
	}

	// the engine-side miss: the file removed under the session
	if err := os.Remove(engFile); err != nil {
		t.Fatal(err)
	}
	d, err = a.EditDiff(EditTarget{Kind: "remote", Source: src.Name, Key: "/cfg.conf"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.RemoteMissing || d.Remote != "" {
		t.Fatalf("deleted-engine diff = missing(%v) remote(%q), want the honest miss", d.RemoteMissing, d.Remote)
	}
}

// The refusals speak the typed grammar: kinds that never cross the
// bridge, and a target with no open session has nothing to diff.
func TestEditDiffRefusals(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	if _, err := a.EditDiff(EditTarget{Kind: "local", Key: "C:/x.txt"}); err == nil ||
		!strings.Contains(err.Error(), "cannot diff kind") {
		t.Fatalf("local-kind err = %v, want the cannot-diff-kind refusal", err)
	}
	if _, err := a.EditDiff(EditTarget{Kind: "ftp2", Key: "/x"}); err == nil ||
		!strings.Contains(err.Error(), "cannot diff kind") {
		t.Fatalf("unknown-kind err = %v, want the cannot-diff-kind refusal", err)
	}
	if _, err := a.EditDiff(EditTarget{Bucket: "docs", Key: "notes.md"}); err == nil ||
		!strings.Contains(err.Error(), "not being edited") {
		t.Fatalf("no-session err = %v, want the not-being-edited refusal", err)
	}
}

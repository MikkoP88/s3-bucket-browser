package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// CheckConflicts over two local engines: a clean destination reports
// nothing (the GUI then skips the conflict dialog entirely), a colliding
// file is reported with both sides' sizes, and the per-file decisions map
// drives skip/overwrite through TransferCross.
func TestCheckConflictsAndDecisions(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, _ := localSource(t, "lab") // readme.md ("x", 1 byte)
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}
	vault := XferDest{Kind: "remote", Source: "vault", Dir: "/"}
	items := []XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}

	// Clean destination: no conflicts, no dialog needed.
	rows, err := a.CheckConflicts(items, nil, vault)
	if err != nil {
		t.Fatalf("CheckConflicts (clean): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("clean destination reported %d conflict(s)", len(rows))
	}
	if rows == nil {
		t.Fatal("clean destination must return an empty slice, not nil — nil serializes to JSON null, which the GUI reads as a failed pre-check and answers with the classic dialog")
	}

	// Seed a collision at the destination.
	if err := os.WriteFile(filepath.Join(vaultRoot, "readme.md"), []byte("old-contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err = a.CheckConflicts(items, nil, vault)
	if err != nil {
		t.Fatalf("CheckConflicts (collision): %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("collision reported %d row(s), want 1", len(rows))
	}
	r := rows[0]
	if r.DecisionKey != "/readme.md" {
		t.Fatalf("decisionKey = %q, want /readme.md", r.DecisionKey)
	}
	if r.Key != "readme.md" {
		t.Fatalf("display key = %q, want readme.md", r.Key)
	}
	if r.SrcSize != 1 || r.DstSize != int64(len("old-contents")) {
		t.Fatalf("sizes = src %d / dst %d, want 1 / %d", r.SrcSize, r.DstSize, len("old-contents"))
	}

	// Job-wide skip policy (no decisions): the file is skipped, the job
	// still finishes done, and the destination keeps its content.
	ji := mustXfer(t, a, items, nil, vault, PolicySkip, false)
	if ji.SkippedFiles != 1 || ji.DoneFiles != 1 || ji.FailedFiles != 0 || ji.Status != JobDone {
		t.Fatalf("skip-policy job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "old-contents" {
		t.Fatalf("skip policy overwrote the destination: %q", got)
	}
	skip := map[string]string{"/readme.md": PolicySkip}
	id, err := a.TransferCross(items, nil, vault, PolicyOverwrite, 0, false, skip)
	if err != nil {
		t.Fatal(err)
	}
	ji = waitXferJob(t, a, id)
	if ji.SkippedFiles != 1 || ji.DoneFiles != 1 || ji.FailedFiles != 0 || ji.Status != JobDone {
		t.Fatalf("skip-decision job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "old-contents" {
		t.Fatalf("skip decision overwrote the destination: %q", got)
	}

	// Per-file overwrite: the destination is replaced.
	over := map[string]string{"/readme.md": PolicyOverwrite}
	id, err = a.TransferCross(items, nil, vault, PolicySkip, 0, false, over)
	if err != nil {
		t.Fatal(err)
	}
	ji = waitXferJob(t, a, id)
	if ji.SkippedFiles != 0 || ji.DoneFiles != 1 || ji.Status != JobDone {
		t.Fatalf("overwrite-decision job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme.md")); got != "x" {
		t.Fatalf("overwrite decision did not replace: %q", got)
	}

	// Per-file rename finds a free slot even under policy overwrite.
	ren := map[string]string{"/readme.md": PolicyRename}
	id, err = a.TransferCross(items, nil, vault, PolicyOverwrite, 0, false, ren)
	if err != nil {
		t.Fatal(err)
	}
	ji = waitXferJob(t, a, id)
	if ji.Status != JobDone || ji.DoneFiles != 1 || ji.SkippedFiles != 0 {
		t.Fatalf("rename-decision job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "readme (1).md")); got != "x" {
		t.Fatalf("rename decision did not find a free slot: %q", got)
	}
}

// Local destinations key decisions by the OS path (the same dstPath the
// planner produces), and stat errors never fake a conflict.
func TestCheckConflictsLocalDest(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, _ := localSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	dl := t.TempDir()

	// Clean local destination: no rows.
	rows, err := a.CheckConflicts([]XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, XferDest{Kind: "local", Dir: dl})
	if err != nil {
		t.Fatalf("CheckConflicts (local clean): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("clean local destination reported %d conflict(s)", len(rows))
	}
	if rows == nil {
		t.Fatal("clean local destination must return an empty slice, not nil (JSON null would re-open the classic dialog)")
	}

	// Seed a collision; the decision key is the OS path.
	dstPath := filepath.Join(dl, "readme.md")
	if err := os.WriteFile(dstPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err = a.CheckConflicts([]XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil, XferDest{Kind: "local", Dir: dl})
	if err != nil {
		t.Fatalf("CheckConflicts (local collision): %v", err)
	}
	if len(rows) != 1 || rows[0].DecisionKey != dstPath {
		t.Fatalf("rows = %+v, want one row keyed %q", rows, dstPath)
	}

	// Skip decision keeps the local file.
	id, err := a.TransferCross([]XferItem{{Source: "lab", Key: "/readme.md", Size: 1}}, nil,
		XferDest{Kind: "local", Dir: dl}, PolicyOverwrite, 0, false, map[string]string{dstPath: PolicySkip})
	if err != nil {
		t.Fatal(err)
	}
	ji := waitXferJob(t, a, id)
	if ji.SkippedFiles != 1 || ji.Status != JobDone {
		t.Fatalf("local skip job = %+v", ji)
	}
	if got := read(t, dstPath); got != "old" {
		t.Fatalf("local skip overwrote: %q", got)
	}
}

// filePolicy: per-file decisions win over the fallback, invalid values
// fall through to the fallback.
func TestFilePolicy(t *testing.T) {
	d := map[string]string{"a": PolicySkip, "b": "bogus"}
	if got := filePolicy(d, "a", PolicyOverwrite); got != PolicySkip {
		t.Fatalf("decision did not win: %q", got)
	}
	if got := filePolicy(d, "b", PolicyOverwrite); got != PolicyOverwrite {
		t.Fatalf("invalid decision did not fall back: %q", got)
	}
	if got := filePolicy(d, "c", PolicyRename); got != PolicyRename {
		t.Fatalf("missing decision did not fall back: %q", got)
	}
	if got := filePolicy(nil, "a", PolicySkip); got != PolicySkip {
		t.Fatalf("nil decisions did not fall back: %q", got)
	}
}

// Sources CRUD and profile-file events land in the (persistent) event log.
func TestSourcesAndProfileLogging(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src := profile.Source{Name: "lab", Type: profile.TypeLocal, LocalRoot: t.TempDir()}
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveSource("lab"); err != nil {
		t.Fatal(err)
	}
	if err := a.NewProfileFile("p", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.s3bprofile")
	if err := a.SaveProfileFileAs(path, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseProfileFile(false); err != nil {
		t.Fatal(err)
	}
}

// xfer_cycle_test.go pins the transfer family's last line of defense
// against silent total data loss: a destination inside the item's own
// source. Copying a folder beneath itself and then — on a move —
// deleting the sources takes the fresh copies with them, and a file
// dropped onto its own folder lands on its own key, which the move then
// deletes. Every engine that can nest is pinned: TransferCross (s3,
// remote and local destinations, an alias over one endpoint), copyMove
// (the same-bucket paste path, which must never trip on a rename) and
// CopySelectionVersions (the versioned twin) — all refusing before a
// single byte or version moves, while the sibling transfers that must
// keep working still run end to end. Hermetic: the httptest S3 from
// usage_test.go and local engines over temp dirs.
package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// wantRefusal asserts the synchronous error carries the refusal wording.
func wantRefusal(t *testing.T, label string, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("%s: want an error containing %q, got %v", label, substr, err)
	}
}

// wantResultError asserts one of the per-item errors carries substr.
func wantResultError(t *testing.T, label string, res CopyResult, substr string) {
	t.Helper()
	for _, e := range res.Errors {
		if strings.Contains(e, substr) {
			return
		}
	}
	t.Fatalf("%s: result errors %v, want one containing %q", label, res.Errors, substr)
}

// hasCycleError reports whether any per-item error is a cycle refusal.
func hasCycleError(res CopyResult) bool {
	for _, e := range res.Errors {
		if strings.Contains(e, "into itself") || strings.Contains(e, "are the same") {
			return true
		}
	}
	return false
}

// TestTransferCrossCycleS3 pins the s3 destination side: a folder into
// its own subtree, onto its own spot, and a file onto its own folder are
// refused synchronously — before any server call — for a move and a
// copy alike, through a second name over the same endpoint, while a
// different bucket and a disjoint prefix in the same bucket still plan
// and start.
func TestTransferCrossCycleS3(t *testing.T) {
	ep := newUsageServer(t, labBucket())
	a := usageApp(t, ep)
	// A second name over the same endpoint: an alias of one store.
	if err := a.SaveSource(profile.Source{
		Name: "alias", Type: profile.TypeS3,
		S3: &profile.Profile{
			Name: "alias", Endpoint: ep, Region: "us-east-1",
			PathStyle: true, AccessKeyID: "k", SecretKey: "s",
		},
	}); err != nil {
		t.Fatal(err)
	}

	dir := XferItem{Source: "", Bucket: "lab", Key: "docs/", IsDir: true}
	file := XferItem{Source: "", Bucket: "lab", Key: "docs/notes.md", Size: 900}
	inLab := func(d string) XferDest {
		return XferDest{Kind: "s3", Source: "", Bucket: "lab", Dir: d}
	}

	_, err := a.TransferCross([]XferItem{dir}, nil, inLab("docs/sub/"), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "dir into itself (move)", err, "a folder cannot be moved or copied into itself")
	_, err = a.TransferCross([]XferItem{dir}, nil, inLab(""), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "dir onto its own spot", err, "a folder cannot be moved or copied into itself")
	_, err = a.TransferCross([]XferItem{file}, nil, inLab("docs/"), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "file onto its own folder", err, "source and destination are the same")

	// The copy verb refuses the same shape, and the alias resolves to
	// the same endpoint, so spelling the store differently changes
	// nothing.
	aliased := dir
	aliased.Source = "alias"
	_, err = a.TransferCross([]XferItem{aliased}, nil, inLab("docs/sub/"), PolicyOverwrite, 0, false, nil, false)
	wantRefusal(t, "alias over one endpoint", err, "a folder cannot be moved or copied into itself")

	// A different bucket never nests inside the source: the guard lets
	// it through and planning starts (the fake serves no such bucket, so
	// the walk fails — with a non-cycle error).
	other := dir
	other.Bucket = "other"
	if _, err := a.TransferCross([]XferItem{other}, nil, inLab("docs/sub/"), PolicyOverwrite, 0, false, nil, false); err == nil ||
		strings.Contains(err.Error(), "into itself") || strings.Contains(err.Error(), "are the same") {
		t.Fatalf("different bucket: want a planning error, not a cycle refusal, got %v", err)
	}

	// The same-bucket sibling transfers that must keep working: a file
	// and a folder into a disjoint prefix both plan (the folder's walk
	// runs against the fake's listing) and start a job.
	for _, it := range []XferItem{file, dir} {
		id, err := a.TransferCross([]XferItem{it}, nil, inLab("photos/"), PolicyOverwrite, 0, false, nil, false)
		if err != nil || id == "" {
			t.Fatalf("sibling copy of %s: id=%q err=%v", it.Key, id, err)
		}
	}
}

// TestTransferCrossCycleRemote pins the remote destination side on one
// engine (the local engine over a temp tree): the folder and file loss
// shapes are refused, while a same-engine sibling move runs end to end —
// the fresh copy in place, the source gone, never the other way around.
func TestTransferCrossCycleRemote(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, labRoot := localSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}

	dir := XferItem{Source: "lab", Key: "/docs/", IsDir: true}
	file := XferItem{Source: "lab", Key: "/readme.md", Size: 1}
	inLab := func(d string) XferDest {
		return XferDest{Kind: "remote", Source: "lab", Dir: d}
	}

	_, err := a.TransferCross([]XferItem{dir}, nil, inLab("/docs/sub"), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "dir into itself", err, "a folder cannot be moved or copied into itself")
	_, err = a.TransferCross([]XferItem{dir}, nil, inLab("/docs"), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "dir onto its own spot", err, "a folder cannot be moved or copied into itself")
	_, err = a.TransferCross([]XferItem{file}, nil, inLab("/"), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "file onto its own root", err, "source and destination are the same")

	// Same-engine sibling move: content lands, only the source goes.
	ji := mustXfer(t, a, []XferItem{file}, nil, inLab("/backup"), PolicyOverwrite, true)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("sibling move job = %+v", ji)
	}
	if got := read(t, filepath.Join(labRoot, "backup", "readme.md")); got != "x" {
		t.Fatalf("backup/readme.md = %q", got)
	}
	mustNotExist(t, filepath.Join(labRoot, "readme.md"))
	// A same-engine folder copy into a sibling still works.
	mustXfer(t, a, []XferItem{dir}, nil, inLab("/backup"), PolicyOverwrite, false)
	mustExist(t, filepath.Join(labRoot, "backup", "docs"), true)
}

// TestTransferCrossCycleLocal pins the local destination side: a local
// pane file pasted into its own folder, a folder onto itself, and the
// whole tree into its own subfolder are refused, while a sibling move
// runs end to end.
func TestTransferCrossCycleLocal(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "docs", "b.txt"), []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := func(d string) XferDest { return XferDest{Kind: "local", Dir: d} }

	_, err := a.TransferCross(nil, []string{filepath.Join(src, "a.txt")}, at(src), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "file into its own folder", err, "source and destination are the same")
	_, err = a.TransferCross(nil, []string{filepath.Join(src, "docs")}, at(src), PolicyOverwrite, 0, true, nil, false)
	wantRefusal(t, "dir onto its own spot", err, "a folder cannot be moved or copied into itself")
	_, err = a.TransferCross(nil, []string{src}, at(filepath.Join(src, "docs")), PolicyOverwrite, 0, false, nil, false)
	wantRefusal(t, "tree into its own subfolder", err, "a folder cannot be moved or copied into itself")

	// Sibling move: the copy lands, the source folder goes.
	other := t.TempDir()
	ji := mustXfer(t, a, nil, []string{filepath.Join(src, "docs")}, at(other), PolicyOverwrite, true)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("sibling move job = %+v", ji)
	}
	if got := read(t, filepath.Join(other, "docs", "b.txt")); got != "bbb" {
		t.Fatalf("other/docs/b.txt = %q", got)
	}
	mustNotExist(t, filepath.Join(src, "docs"))
}

// TestCopyMoveCycleGuard pins the same-bucket paste path's server-side
// twin: folder-into-itself and onto-its-own-spot are refused as per-item
// errors before the walk, a file onto its own folder joins back to its
// own key, and neither a folder rename (which lands at its parent) nor a
// sibling copy is refused — their only errors are the fake's refused
// writes.
func TestCopyMoveCycleGuard(t *testing.T) {
	a := usageApp(t, newUsageServer(t, labBucket()))
	c, err := a.client("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	res, err := a.copyMove(ctx, c, "lab", []string{"docs/"}, "lab", "docs/sub/", false, nil, "", PolicyOverwrite, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantResultError(t, "dir into itself", res, "docs: a folder cannot be moved or copied into itself")
	res, err = a.copyMove(ctx, c, "lab", []string{"docs/"}, "lab", "", false, nil, "", PolicyOverwrite, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantResultError(t, "dir onto its own spot", res, "docs: source and destination are the same")
	res, err = a.copyMove(ctx, c, "lab", []string{"docs/notes.md"}, "lab", "docs/", false, nil, "", PolicyOverwrite, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantResultError(t, "file onto its own folder", res, "docs/notes.md: source and destination are the same")

	// An F2-style rename lands at its parent under the NEW name — the
	// guard must not trip; the error that lands is the fake's refused
	// marker write, never a cycle refusal.
	res, err = a.copyMove(ctx, c, "lab", []string{"docs/"}, "lab", "", false, nil, "docs2", PolicyOverwrite, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) == 0 || hasCycleError(res) {
		t.Fatalf("rename must not be refused as a cycle: %+v", res)
	}
	// A sibling file copy proceeds past the guard (the fake refuses the
	// copy itself).
	res, err = a.copyMove(ctx, c, "lab", []string{"docs/notes.md"}, "lab", "photos/", false, nil, "", PolicyOverwrite, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) == 0 || hasCycleError(res) {
		t.Fatalf("sibling copy must not be refused as a cycle: %+v", res)
	}
}

// TestCopySelectionVersionsCycle pins the versioned twin: after the
// destination versioning check and before a single version is planned,
// a folder beneath itself and an exact key onto itself are refused,
// while a disjoint prefix still plans and starts its job.
func TestCopySelectionVersionsCycle(t *testing.T) {
	a := usageApp(t, newUsageServer(t, labBucket()))

	_, err := a.CopySelectionVersions("", "lab", []string{"docs/"}, "", "lab", "docs/sub/", true)
	wantRefusal(t, "folder into itself", err, "a folder cannot be moved or copied into itself")
	_, err = a.CopySelectionVersions("", "lab", []string{"docs/notes.md"}, "", "lab", "docs/", true)
	wantRefusal(t, "file onto its own key", err, "source and destination are the same")

	id, err := a.CopySelectionVersions("", "lab", []string{"docs/"}, "", "lab", "photos/", false)
	if err != nil || id == "" {
		t.Fatalf("sibling versioned copy: id=%q err=%v", id, err)
	}
}

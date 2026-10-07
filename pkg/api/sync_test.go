package api

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/syncplan"
)

// The Synchronize dialog's planner. SyncPreview rides the compare walkers
// (any local dir vs any bucket prefix) through the same shared predicate
// the CLI's sync command computes with, so these rigs pin the field shape:
// both copy vectors, both delete vectors, sizes from the right side, and
// the honest refusal for pairs the contract does not serve.

// syncRels strips a plan vector to its rel list.
func syncRels(v []SyncFile) []string {
	out := make([]string, len(v))
	for i, f := range v {
		out[i] = f.Rel
	}
	return out
}

// sizeOf returns the named rel's size from a plan vector (-1 when absent).
func sizeOf(v []SyncFile, rel string) int64 {
	for _, f := range v {
		if f.Rel == rel {
			return f.Size
		}
	}
	return -1
}

func TestSyncPreviewPlansBothDirections(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "same.txt", "0123456789")      // 10 — size-equal: skipped
	f.seed("docs", "grew.txt", "012345678901234") // 15 — local grew to 20: upload
	f.seed("docs", "stale.txt", "0123456")        // 7 — absent locally
	f.seed("docs", "gone.txt", "012345678901")    // 12 — absent locally
	if err := a.SaveSource(fakeS3Source("syncsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	root := t.TempDir()
	cmpTree(t, root, "same.txt", 10, base)
	cmpTree(t, root, "grew.txt", 20, base)
	cmpTree(t, root, "new.txt", 5, base)
	cmpTree(t, root, "extra-local.txt", 9, base)

	info, err := a.SyncPreview(
		CompareRef{Kind: "local", Dir: root},
		CompareRef{Kind: "s3", Source: "syncsrc", Bucket: "docs"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"extra-local.txt", "grew.txt", "new.txt"}; !reflect.DeepEqual(syncRels(info.Uploads), want) {
		t.Errorf("uploads = %v, want %v", syncRels(info.Uploads), want)
	}
	if want := []string{"gone.txt", "grew.txt", "stale.txt"}; !reflect.DeepEqual(syncRels(info.Downloads), want) {
		t.Errorf("downloads = %v, want %v", syncRels(info.Downloads), want)
	}
	if want := []string{"gone.txt", "stale.txt"}; !reflect.DeepEqual(syncRels(info.DelRemote), want) {
		t.Errorf("delRemote = %v, want %v", syncRels(info.DelRemote), want)
	}
	if want := []string{"extra-local.txt", "new.txt"}; !reflect.DeepEqual(syncRels(info.DelLocal), want) {
		t.Errorf("delLocal = %v, want %v", syncRels(info.DelLocal), want)
	}
	if info.Skipped != 1 {
		t.Errorf("skipped = %d, want 1 (same.txt)", info.Skipped)
	}
	// sizes come from the plan's own side: uploads carry the local size,
	// downloads the remote one
	if got := sizeOf(info.Uploads, "grew.txt"); got != 20 {
		t.Errorf("upload grew.txt size = %d, want the local 20", got)
	}
	if got := sizeOf(info.Downloads, "grew.txt"); got != 15 {
		t.Errorf("download grew.txt size = %d, want the remote 15", got)
	}
	// the identity fields the run legs need
	if info.Bucket != "docs" || info.Prefix != "" || info.LocalDir != root || info.Source != "syncsrc" {
		t.Errorf("identity = %s/%s/%s/%s, want docs//%s/syncsrc", info.Bucket, info.Prefix, info.LocalDir, info.Source, root)
	}

	// the sides may arrive in either order — the planner seats them
	info2, err := a.SyncPreview(
		CompareRef{Kind: "s3", Source: "syncsrc", Bucket: "docs"},
		CompareRef{Kind: "local", Dir: root},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(syncRels(info2.Uploads), syncRels(info.Uploads)) ||
		!reflect.DeepEqual(syncRels(info2.DelLocal), syncRels(info.DelLocal)) {
		t.Error("reversed pair changed the plan")
	}
}

// A prefixed s3 side trims its prefix exactly like the compare walk, and
// the returned Prefix rides dirPrefix form so the run legs build keys.
func TestSyncPreviewUnderPrefix(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("photos")
	f.seed("photos", "raw/keep.txt", "12345") // 5 — size-equal
	f.seed("photos", "raw/old.txt", "123")    // 3 — absent locally
	if err := a.SaveSource(fakeS3Source("syncpre", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	root := t.TempDir()
	cmpTree(t, root, "keep.txt", 5, base)
	cmpTree(t, root, "fresh.txt", 2, base)

	info, err := a.SyncPreview(
		CompareRef{Kind: "local", Dir: root},
		CompareRef{Kind: "s3", Source: "syncpre", Bucket: "photos", Prefix: "raw"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fresh.txt"}; !reflect.DeepEqual(syncRels(info.Uploads), want) {
		t.Errorf("uploads = %v, want %v", syncRels(info.Uploads), want)
	}
	if want := []string{"old.txt"}; !reflect.DeepEqual(syncRels(info.DelRemote), want) {
		t.Errorf("delRemote = %v, want %v", syncRels(info.DelRemote), want)
	}
	if info.Prefix != "raw/" {
		t.Errorf("prefix = %q, want raw/", info.Prefix)
	}
}

// The v1 contract: one local folder and one S3 prefix — anything else is
// refused honestly, pointing at the surfaces that serve it.
func TestSyncPreviewRefusesNonSyncPairs(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "x.txt", "x")
	if err := a.SaveSource(fakeS3Source("syncref", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	labSrc, labRoot := emptyLocalSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	s3 := CompareRef{Kind: "s3", Source: "syncref", Bucket: "docs"}
	loc := CompareRef{Kind: "local", Dir: labRoot}
	rem := CompareRef{Kind: "remote", Source: "lab", Dir: "/"}
	for _, pair := range [][2]CompareRef{{loc, loc}, {s3, s3}, {loc, rem}, {rem, s3}} {
		_, err := a.SyncPreview(pair[0], pair[1])
		if err == nil {
			t.Errorf("SyncPreview(%s↔%s) accepted a non-sync pair", pair[0].Kind, pair[1].Kind)
			continue
		}
		if want := "one local folder and one S3 prefix"; !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to name %q", err.Error(), want)
		}
	}
}

// The parity pin: the shared predicate agrees with the CLI's historical
// inline loops (skip when size-equal, copy when missing or size-diff,
// delete target files absent at the source) on the same size maps — the
// guarantee that the two faces can never drift.
func TestSyncPredicateMatchesHistoricalCLILoops(t *testing.T) {
	local := map[string]int64{"a.txt": 1, "same.txt": 10, "grew.txt": 20, "deep/n.txt": 3}
	remote := map[string]int64{"b.txt": 2, "same.txt": 10, "grew.txt": 15, "deep/o.txt": 4}

	historical := func(source, target map[string]int64, del bool) (copies, dels []string, skipped int) {
		for rel, size := range source {
			if ts, ok := target[rel]; ok && ts == size {
				skipped++
				continue
			}
			copies = append(copies, rel)
		}
		if del {
			for rel := range target {
				if _, ok := source[rel]; !ok {
					dels = append(dels, rel)
				}
			}
		}
		return copies, dels, skipped
	}
	for _, del := range []bool{false, true} {
		for _, pair := range [][2]map[string]int64{{local, remote}, {remote, local}} {
			hc, hd, hs := historical(pair[0], pair[1], del)
			pc, pd, ps := syncplan.Plan(pair[0], pair[1], del)
			sort.Strings(hc)
			sort.Strings(hd)
			if !reflect.DeepEqual(pc, hc) {
				t.Errorf("del=%v copies = %v, historical %v", del, pc, hc)
			}
			if !reflect.DeepEqual(pd, hd) {
				t.Errorf("del=%v dels = %v, historical %v", del, pd, hd)
			}
			if ps != hs {
				t.Errorf("del=%v skipped = %d, historical %d", del, ps, hs)
			}
		}
	}
}

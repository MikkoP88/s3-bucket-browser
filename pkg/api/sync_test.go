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
// (any two sides: local dir, remote source dir, bucket prefix) through the
// same shared predicate the CLI's sync command computes with, so these
// rigs pin the field shape: both copy vectors in the call's own seating
// order, both delete vectors, sizes from the right side, and the honest
// refusal for the degenerate same-location pair.

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
	if want := []string{"extra-local.txt", "grew.txt", "new.txt"}; !reflect.DeepEqual(syncRels(info.CopiesXY), want) {
		t.Errorf("copiesXY = %v, want %v", syncRels(info.CopiesXY), want)
	}
	if want := []string{"gone.txt", "grew.txt", "stale.txt"}; !reflect.DeepEqual(syncRels(info.CopiesYX), want) {
		t.Errorf("copiesYX = %v, want %v", syncRels(info.CopiesYX), want)
	}
	if want := []string{"gone.txt", "stale.txt"}; !reflect.DeepEqual(syncRels(info.DelY), want) {
		t.Errorf("delY = %v, want %v", syncRels(info.DelY), want)
	}
	if want := []string{"extra-local.txt", "new.txt"}; !reflect.DeepEqual(syncRels(info.DelX), want) {
		t.Errorf("delX = %v, want %v", syncRels(info.DelX), want)
	}
	if info.Skipped != 1 {
		t.Errorf("skipped = %d, want 1 (same.txt)", info.Skipped)
	}
	// sizes come from the plan's own side: copiesXY carry the local size,
	// copiesYX the remote one
	if got := sizeOf(info.CopiesXY, "grew.txt"); got != 20 {
		t.Errorf("copyXY grew.txt size = %d, want the local 20", got)
	}
	if got := sizeOf(info.CopiesYX, "grew.txt"); got != 15 {
		t.Errorf("copyYX grew.txt size = %d, want the remote 15", got)
	}

	// the call's own order seats the sides (the CompareAny grammar):
	// reversing the pair mirrors the vectors, x→y stays the first leg
	info2, err := a.SyncPreview(
		CompareRef{Kind: "s3", Source: "syncsrc", Bucket: "docs"},
		CompareRef{Kind: "local", Dir: root},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(syncRels(info2.CopiesXY), syncRels(info.CopiesYX)) ||
		!reflect.DeepEqual(syncRels(info2.CopiesYX), syncRels(info.CopiesXY)) ||
		!reflect.DeepEqual(syncRels(info2.DelY), syncRels(info.DelX)) {
		t.Error("reversed pair did not mirror the plan")
	}
}

// A prefixed s3 side trims its prefix exactly like the compare walk, so
// the vectors speak the prefix-relative grammar the run legs rebuild keys
// from.
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
	if want := []string{"fresh.txt"}; !reflect.DeepEqual(syncRels(info.CopiesXY), want) {
		t.Errorf("copiesXY = %v, want %v", syncRels(info.CopiesXY), want)
	}
	if want := []string{"old.txt"}; !reflect.DeepEqual(syncRels(info.DelY), want) {
		t.Errorf("delY = %v, want %v", syncRels(info.DelY), want)
	}
}

// The matrix: any two sides plan — a local folder against a remote source
// directory, a remote source against an S3 prefix — through the same
// walkers and the same predicate. Only the degenerate pair (both sides
// naming the same location) is refused.
func TestSyncPreviewAnyPairPlans(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	f.seed("docs", "shared.txt", "0123456789") // 10
	f.seed("docs", "srv-grew.txt", "01234")    // 5 — engine side grew to 8
	if err := a.SaveSource(fakeS3Source("syncmix", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	engSrc, engRoot := emptyLocalSource(t, "eng")
	if err := a.SaveSource(engSrc); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	cmpTree(t, engRoot, "shared.txt", 10, base)
	cmpTree(t, engRoot, "srv-grew.txt", 8, base)
	cmpTree(t, engRoot, "new-on-engine.txt", 3, base)

	// remote engine ↔ s3
	info, err := a.SyncPreview(
		CompareRef{Kind: "remote", Source: "eng", Dir: "/"},
		CompareRef{Kind: "s3", Source: "syncmix", Bucket: "docs"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"new-on-engine.txt", "srv-grew.txt"}; !reflect.DeepEqual(syncRels(info.CopiesXY), want) {
		t.Errorf("engine→s3 copies = %v, want %v", syncRels(info.CopiesXY), want)
	}
	if got := sizeOf(info.CopiesXY, "srv-grew.txt"); got != 8 {
		t.Errorf("engine→s3 srv-grew.txt size = %d, want the engine's 8", got)
	}
	if want := []string{"srv-grew.txt"}; !reflect.DeepEqual(syncRels(info.CopiesYX), want) {
		t.Errorf("s3→engine copies = %v, want %v", syncRels(info.CopiesYX), want)
	}
	if len(info.DelY) != 0 {
		t.Errorf("delY = %v, want none (every s3 file lives at the engine)", syncRels(info.DelY))
	}
	if want := []string{"new-on-engine.txt"}; !reflect.DeepEqual(syncRels(info.DelX), want) {
		t.Errorf("delX = %v, want %v", syncRels(info.DelX), want)
	}
	if info.Skipped != 1 {
		t.Errorf("skipped = %d, want 1 (shared.txt)", info.Skipped)
	}

	// local ↔ remote engine
	loc := t.TempDir()
	cmpTree(t, loc, "shared.txt", 10, base)
	cmpTree(t, loc, "loc-new.txt", 4, base)
	info2, err := a.SyncPreview(
		CompareRef{Kind: "local", Dir: loc},
		CompareRef{Kind: "remote", Source: "eng", Dir: "/"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"loc-new.txt"}; !reflect.DeepEqual(syncRels(info2.CopiesXY), want) {
		t.Errorf("local→engine copies = %v, want %v", syncRels(info2.CopiesXY), want)
	}
	if want := []string{"new-on-engine.txt", "srv-grew.txt"}; !reflect.DeepEqual(syncRels(info2.CopiesYX), want) {
		t.Errorf("engine→local copies = %v, want %v", syncRels(info2.CopiesYX), want)
	}
	if want := []string{"new-on-engine.txt", "srv-grew.txt"}; !reflect.DeepEqual(syncRels(info2.DelY), want) {
		t.Errorf("delY = %v, want %v", syncRels(info2.DelY), want)
	}
}

// The one refusal: both sides naming the same location — a two-way plan
// against itself. Different locations of the same kinds always plan.
func TestSyncPreviewRefusesSameSide(t *testing.T) {
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
	for _, pair := range [][2]CompareRef{
		{loc, loc},
		{s3, s3},
		{rem, rem},
		{s3, {Kind: "s3", Source: "syncref", Bucket: "docs", Prefix: "/"}},
	} {
		_, err := a.SyncPreview(pair[0], pair[1])
		if err == nil {
			t.Errorf("SyncPreview(%s↔%s) accepted the same location twice", pair[0].Kind, pair[1].Kind)
			continue
		}
		if want := "the same location"; !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to name %q", err.Error(), want)
		}
	}
	// different locations of the same kinds always plan (the remote
	// acceptance legs live in the matrix rig above)
	other := t.TempDir()
	for _, pair := range [][2]CompareRef{
		{loc, {Kind: "local", Dir: other}},
		{s3, {Kind: "s3", Source: "syncref", Bucket: "docs", Prefix: "sub"}},
	} {
		if _, err := a.SyncPreview(pair[0], pair[1]); err != nil {
			t.Errorf("SyncPreview(%s↔%s) refused a legitimate pair: %v", pair[0].Kind, pair[1].Kind, err)
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
			if len(pc) != len(hc) || len(pd) != len(hd) {
				t.Fatalf("del=%v vector lengths differ", del)
			}
			if ps != hs {
				t.Errorf("del=%v skipped = %d, historical %d", del, ps, hs)
			}
			sort.Strings(hc)
			sort.Strings(hd)
			if !reflect.DeepEqual(pc, hc) {
				t.Errorf("del=%v copies = %v, historical %v", del, pc, hc)
			}
			if !reflect.DeepEqual(pd, hd) {
				t.Errorf("del=%v dels = %v, historical %v", del, pd, hd)
			}
		}
	}
}

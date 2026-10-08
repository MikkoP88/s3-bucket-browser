// sync_matrix_test.go pins the sync matrix on the engine side: any two
// locations — a local directory and a NAME:// source path, engine ↔
// engine through the temp spool, local ↔ local — plan and run through
// the one shared predicate (missing or size-diff copies, opt-in target
// deletes under the L1 gate), with --dry-run transferring nothing and
// the same-location pair refused before a byte moves. The hermetic
// local "lab" engine carries both sides; the s3 legs of the matrix ride
// the same primitives cp's cross-engine tests already walk in CI.
package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// syncStage seeds the lab engine and a local folder in the shape the
// predicate speaks: one size-equal pair, one the target holds longer, one
// missing at the target, one target-only extra.
func syncStage(t *testing.T) (engRoot, locDir string) {
	t.Helper()
	engRoot = srcEnv(t) // lab:// seeded with a.txt and docs/b.txt
	if err := os.Remove(filepath.Join(engRoot, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(engRoot, "docs")); err != nil {
		t.Fatal(err)
	}
	locDir = t.TempDir()
	write := func(dir, rel string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(engRoot, "same.txt", []byte("0123456789"))       // 10 — size-equal with local
	write(engRoot, "target-grew.txt", []byte("012345678")) // 9 — local holds 4
	write(engRoot, "extra-at-target.txt", []byte("xx"))    // engine-only extra
	write(locDir, "same.txt", []byte("9876543210"))        // 10 — size-equal with engine
	write(locDir, "target-grew.txt", []byte("1234"))       // 4 — engine holds 9
	write(locDir, "fresh.txt", []byte("mm"))               // local-only, missing at the engine
	return engRoot, locDir
}

func TestSyncLocalToEnginePlansAndRuns(t *testing.T) {
	engRoot, locDir := syncStage(t)

	// upload leg: local → lab:// — copies the size-diff and the missing,
	// skips the size-equal, and never deletes the engine-only extra
	if code := Execute([]string{"sync", locDir, "lab://"}); code != 0 {
		t.Fatalf("sync local → lab: exit %d", code)
	}
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := read(filepath.Join(engRoot, "target-grew.txt")); got != "1234" {
		t.Errorf("size-diff copy landed %q, want the local 1234", got)
	}
	if got := read(filepath.Join(engRoot, "fresh.txt")); got != "mm" {
		t.Errorf("missing copy landed %q, want mm", got)
	}
	if b, err := os.ReadFile(filepath.Join(engRoot, "same.txt")); err != nil || string(b) != "0123456789" {
		t.Error("size-equal pair was re-copied")
	}
	if _, err := os.Stat(filepath.Join(engRoot, "extra-at-target.txt")); err != nil {
		t.Fatal("sync without --delete removed the engine-only extra")
	}

	// download leg: lab:// → local — the same predicate mirrored. Every
	// pair is size-equal by now (the upload leg settled them), so nothing
	// turns around and the engine's extra lands as a copy, not a delete
	if code := Execute([]string{"sync", "lab://", locDir}); code != 0 {
		t.Fatalf("sync lab → local: exit %d", code)
	}
	if got := read(filepath.Join(locDir, "target-grew.txt")); got != "1234" {
		t.Errorf("size-equal pair was re-copied on the mirror leg: %q", got)
	}
	if got := read(filepath.Join(locDir, "extra-at-target.txt")); got != "xx" {
		t.Errorf("engine extra landed %q, want the copy xx (never a delete)", got)
	}
}

func TestSyncDeleteRemovesTargetExtras(t *testing.T) {
	engRoot, locDir := syncStage(t)

	if code := Execute([]string{"sync", locDir, "lab://", "--delete"}); code != 0 {
		t.Fatalf("sync --delete local → lab: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(engRoot, "extra-at-target.txt")); !os.IsNotExist(err) {
		t.Error("--delete kept the engine-only extra")
	}
	// the source side stands whole — the delete leg names target files only
	for _, rel := range []string{"same.txt", "target-grew.txt", "fresh.txt"} {
		if _, err := os.Stat(filepath.Join(locDir, rel)); err != nil {
			t.Errorf("--delete touched the source side's %s", rel)
		}
	}
}

func TestSyncDryRunTransfersNothing(t *testing.T) {
	engRoot, locDir := syncStage(t)

	outTxt, code := captureOut(t, "sync", locDir, "lab://", "--dry-run", "--delete")
	if code != 0 {
		t.Fatalf("sync --dry-run: exit %d", code)
	}
	for _, want := range []string{"target-grew.txt", "fresh.txt"} {
		if !strings.Contains(outTxt, want) {
			t.Errorf("dry-run plan missing %q in:\n%s", want, outTxt)
		}
	}
	if !strings.Contains(outTxt, "delete lab://extra-at-target.txt") {
		t.Errorf("dry-run plan missing the delete line in:\n%s", outTxt)
	}
	if _, err := os.Stat(filepath.Join(engRoot, "extra-at-target.txt")); err != nil {
		t.Fatal("dry-run deleted the extra")
	}
	if b, err := os.ReadFile(filepath.Join(engRoot, "same.txt")); err != nil || len(b) != 10 {
		t.Fatal("dry-run touched a file")
	}
}

func TestSyncSameLocationRefused(t *testing.T) {
	srcEnv(t)
	if code := Execute([]string{"sync", "lab://docs", "lab://docs"}); code != exitUsage {
		t.Fatalf("same engine location: exit %d (want usage %d)", code, exitUsage)
	}
	if code := Execute([]string{"sync", "lab://", "lab://"}); code != exitUsage {
		t.Fatalf("same engine root: exit %d (want usage %d)", code, exitUsage)
	}
}

func TestSyncEngineToEngineThroughSpool(t *testing.T) {
	engRoot := srcEnv(t) // a.txt ("alpha"), docs/b.txt ("beta")
	sub := filepath.Join(engRoot, "mirror")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if code := Execute([]string{"sync", "lab://", "lab://mirror"}); code != 0 {
		t.Fatalf("same-engine sync through the temp spool: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(sub, "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("spooled copy content: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(sub, "docs", "b.txt")); err != nil || string(b) != "beta" {
		t.Fatalf("spooled nested copy content: %q %v", b, err)
	}
	// the sources stand — sync is not mv
	if b, err := os.ReadFile(filepath.Join(engRoot, "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("sync moved the source: %q %v", b, err)
	}
}

func TestSyncLocalToLocalMirrors(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := Execute([]string{"sync", src, dst}); code != 0 {
		t.Fatalf("sync local → local: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "f.txt")); err != nil || string(b) != "body" {
		t.Fatalf("local mirror content: %q %v", b, err)
	}
	if code := Execute([]string{"sync", src, src}); code != exitUsage {
		t.Fatalf("same local dir: exit %d (want usage %d)", code, exitUsage)
	}
}

// --watch is the standing mirror: each pass re-plans and lands what
// drifted — copies and, with the opt-in, deletes — quietly when nothing
// moved, until the loop is stopped (here the context; in the field
// Ctrl+C). The rig drops and removes files at the near side mid-flight
// and waits for the mirror to catch each one, then proves a stopped
// watch moves nothing further.
func TestSyncWatchKeepsPairInStep(t *testing.T) {
	engRoot, locDir := syncStage(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- watchSync(ctx, nil, []string{locDir, "lab://"}, 40*time.Millisecond, true, false)
	}()

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("%s never landed", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	// the first pass settles the stage: the size-diff and missing copies
	// land, the target-only extra falls to the delete leg
	waitFor("the first pass", func() bool {
		if b, err := os.ReadFile(filepath.Join(engRoot, "target-grew.txt")); err != nil || string(b) != "1234" {
			return false
		}
		if b, err := os.ReadFile(filepath.Join(engRoot, "fresh.txt")); err != nil || string(b) != "mm" {
			return false
		}
		_, err := os.Stat(filepath.Join(engRoot, "extra-at-target.txt"))
		return os.IsNotExist(err)
	})

	// a file dropped at the source after the first pass lands on the next
	if err := os.WriteFile(filepath.Join(locDir, "late.txt"), []byte("just landed"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor("the late file's copy", func() bool {
		b, err := os.ReadFile(filepath.Join(engRoot, "late.txt"))
		return err == nil && string(b) == "just landed"
	})

	// a file removed at the source falls at the target on the next pass
	if err := os.Remove(filepath.Join(locDir, "fresh.txt")); err != nil {
		t.Fatal(err)
	}
	waitFor("the delete leg", func() bool {
		_, err := os.Stat(filepath.Join(engRoot, "fresh.txt"))
		return os.IsNotExist(err)
	})

	// stopping the watch is clean — and final
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watchSync returned %v, want nil on stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchSync never stopped after the context ended")
	}
	if err := os.WriteFile(filepath.Join(locDir, "after-stop.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(engRoot, "after-stop.txt")); !os.IsNotExist(err) {
		t.Error("a stopped watch kept syncing")
	}
}

// The watch refusals: --watch is interactive (no --json) and it syncs for
// real (no --dry-run) — both usage errors before the loop ever ticks.
func TestSyncWatchRefusals(t *testing.T) {
	syncStage(t)
	for _, args := range [][]string{
		{"sync", "lab://", "lab://mirror", "--watch", "--json"},
		{"sync", "lab://", "lab://mirror", "--watch", "--dry-run"},
	} {
		if code := Execute(args); code != exitUsage {
			t.Errorf("%v exit = %d, want the usage refusal %d", args, code, exitUsage)
		}
	}
}

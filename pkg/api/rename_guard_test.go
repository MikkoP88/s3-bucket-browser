package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fail-closed round: every existence probe that gates a destructive
// or skip decision learns to say "I don't know". A rename whose
// destination cannot be probed refuses and leaves the original standing;
// a skip/rename policy whose destination is illegible fails the item
// instead of overwriting (or silently dropping) it; and the fault legs
// ride the fake's HEAD and LIST fault arms, with n high enough to
// outlast the SDK retryer's attempts.

// TestRenameRefusesWhenProbeFails — the file-form guard's own comment
// ("renames must refuse rather than clobber") finally covers the probe
// itself failing: the wire's silence is not an empty destination.
func TestRenameRefusesWhenProbeFails(t *testing.T) {
	a, f := adminLogRig(t)
	f.seed("docs", "old.txt", "body")
	f.fault("HEAD", "docs", "new.txt", 5) // outlasts the SDK retryer's attempts
	mu, lines := captureLogLines(t)

	if err := a.RenameObject("docs", "old.txt", "new.txt"); err == nil ||
		!strings.Contains(err.Error(), "could not check whether new.txt exists") {
		t.Fatalf("rename onto an unprobeable key = %v, want the refusal", err)
	}
	if got := f.keys("docs"); len(got) != 1 || got[0] != "old.txt" {
		t.Fatalf("store after the refused rename: %v, want old.txt only", got)
	}
	mu.Lock()
	defer mu.Unlock()
	var spoke bool
	for _, l := range *lines {
		if l.Level == LogError && l.Scope == "rename" && l.Source == "docs" &&
			strings.HasPrefix(l.Message, "renaming old.txt to") && strings.Contains(l.Message, "failed:") {
			spoke = true
		}
	}
	if !spoke {
		t.Fatalf("the refused rename never logged its error: %+v", *lines)
	}
}

// TestRenameFolderRefusesWhenWalkFails — the folder form's third probe is
// the prefix walk (content without a marker); a listing the wire cannot
// answer refuses the rename the same way.
func TestRenameFolderRefusesWhenWalkFails(t *testing.T) {
	a, f := adminLogRig(t)
	f.seed("docs", "old/inner.txt", "body")
	f.fault("LIST", "docs", "new/", 5) // the walk leg, same retry arithmetic

	if err := a.RenameObject("docs", "old/", "new"); err == nil ||
		!strings.Contains(err.Error(), "could not check what lives under new/") {
		t.Fatalf("folder rename with an unanswerable walk = %v, want the refusal", err)
	}
	if got := f.keys("docs"); len(got) != 1 || got[0] != "old/inner.txt" {
		t.Fatalf("store after the refused folder rename: %v, want old/inner.txt only", got)
	}
}

// TestRenameOntoExistingStillRefuses — the occupant refusal itself is
// unchanged by the honest signature: a probe that answers "taken" still
// says "already exists" and nothing moves.
func TestRenameOntoExistingStillRefuses(t *testing.T) {
	a, f := adminLogRig(t)
	f.seed("docs", "old.txt", "body")
	f.seed("docs", "new.txt", "resident")

	if err := a.RenameObject("docs", "old.txt", "new.txt"); err == nil ||
		!strings.Contains(err.Error(), "new.txt already exists") {
		t.Fatalf("rename onto a resident = %v, want the refusal", err)
	}
	if got := f.keys("docs"); len(got) != 2 {
		t.Fatalf("store after the refused rename: %v, want both residents", got)
	}
}

// TestCopySelectionSkipFailsClosed — the copy/move skip policy reads the
// destination before deciding; a destination it cannot read is an error
// for that item, not permission to overwrite.
func TestCopySelectionSkipFailsClosed(t *testing.T) {
	a, f := adminLogRig(t)
	f.seed("docs", "old.txt", "body")
	f.fault("HEAD", "docs", "copy/old.txt", 5) // the destination the skip gate reads

	res, err := a.CopySelection("docs", []string{"old.txt"}, "docs", "copy/", false, PolicySkip, nil)
	if err != nil {
		t.Fatalf("per-item failures are the result's, not the call's: %v", err)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "could not check whether s3://docs/copy/old.txt is taken") {
		t.Fatalf("res.Errors = %v, want the illegible-destination refusal", res.Errors)
	}
	if res.Copied != 0 || res.Skipped != 0 {
		t.Fatalf("nothing may move on a failed probe: copied=%d skipped=%d", res.Copied, res.Skipped)
	}
	if got := f.keys("docs"); len(got) != 1 || got[0] != "old.txt" {
		t.Fatalf("store after the failed copy: %v, want old.txt only", got)
	}
}

// TestCopySelectionRenameFailsClosed — the rename policy's probe leg
// fails the item too: the alternative name must be vouched for, not
// assumed.
func TestCopySelectionRenameFailsClosed(t *testing.T) {
	a, f := adminLogRig(t)
	f.seed("docs", "old.txt", "body")
	f.fault("HEAD", "docs", "copy/old.txt", 5)

	res, err := a.CopySelection("docs", []string{"old.txt"}, "docs", "copy/", false, PolicyRename, nil)
	if err != nil {
		t.Fatalf("per-item failures are the result's, not the call's: %v", err)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "could not check whether s3://docs/copy/old.txt is taken") {
		t.Fatalf("res.Errors = %v, want the illegible-destination refusal", res.Errors)
	}
	if res.Copied != 0 {
		t.Fatalf("nothing may move on a failed probe: copied=%d", res.Copied)
	}
}

// TestRenameRootFileLandsCleanKey — path.Dir's root artifact once rode
// into the destination: a root file renamed to "./name" while the guard
// probed "name" — the wrong key on both ends of the rename. The rig pins
// the clean key and the guard's real destination.
func TestRenameRootFileLandsCleanKey(t *testing.T) {
	a, f := adminLogRig(t)
	f.seed("docs", "old.txt", "body")

	if err := a.RenameObject("docs", "old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	if got := f.keys("docs"); len(got) != 1 || got[0] != "new.txt" {
		t.Fatalf("root rename landed at %v, want the clean key new.txt", got)
	}
}

// guardFiles stages two local files the upload rigs can fault one of.
func guardFiles(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for _, fc := range []struct{ name, body string }{
		{"a.txt", "alpha"}, {"b.txt", "bravo"},
	} {
		p := filepath.Join(dir, fc.name)
		if err := os.WriteFile(p, []byte(fc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// TestUploadSkipFailsClosed — the upload loop's skip gate: a probe the
// wire could not answer fails the file (the job settles JobError with
// the honest message) instead of uploading over what may be there, while
// the files with legible destinations land normally.
func TestUploadSkipFailsClosed(t *testing.T) {
	a, f := adminLogRig(t)
	paths := guardFiles(t)
	f.fault("HEAD", "docs", "a.txt", 5)

	id, err := a.Upload(paths, "docs", "", PolicySkip, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitJobStatus(t, a, id, JobError, 20*time.Second) // the faulted probe pays the SDK retryer's backoff
	if !strings.Contains(ji.Error, "could not check whether s3://docs/a.txt is taken") {
		t.Fatalf("job error = %q, want the illegible-destination failure", ji.Error)
	}
	if got := f.keys("docs"); len(got) != 1 || got[0] != "b.txt" {
		t.Fatalf("store after the failed upload: %v, want b.txt only", got)
	}
}

// TestUploadRenameFailsClosed — the upload loop's rename gate: a key the
// wire cannot vouch for is not the original key by default; the file
// fails rather than overwrite.
func TestUploadRenameFailsClosed(t *testing.T) {
	a, f := adminLogRig(t)
	paths := guardFiles(t)
	f.fault("HEAD", "docs", "a.txt", 5)

	id, err := a.Upload(paths, "docs", "", PolicyRename, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitJobStatus(t, a, id, JobError, 20*time.Second) // same backoff the skip leg pays
	if !strings.Contains(ji.Error, "could not check whether a.txt is free") {
		t.Fatalf("job error = %q, want the unvouched-name failure", ji.Error)
	}
	if got := f.keys("docs"); len(got) != 1 || got[0] != "b.txt" {
		t.Fatalf("store after the failed upload: %v, want b.txt only", got)
	}
}

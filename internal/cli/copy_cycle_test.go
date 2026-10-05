// copy_cycle_test.go pins the CLI copy engines' cycle refusals: a
// destination inside the source — the tree beneath itself, a folder
// onto its own spot, a file onto its own path — never starts, on mv
// (where the source delete would take the fresh copies along) and cp
// alike. The remote engine runs hermetically over the local "lab"
// source; the s3-to-s3 refusals sit ahead of the first S3 call, so a
// nil client proves they need none.
package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyCycleRemoteRefused(t *testing.T) {
	srcEnv(t)

	// A folder into its own subtree, or onto its own spot: usage error,
	// and the tree untouched.
	if code := Execute([]string{"mv", "lab://docs", "lab://docs/sub"}); code != exitUsage {
		t.Fatalf("mv docs into itself: exit %d (want usage %d)", code, exitUsage)
	}
	if code := Execute([]string{"cp", "lab://docs", "lab://docs/sub", "-r"}); code != exitUsage {
		t.Fatalf("cp docs into itself: exit %d (want usage %d)", code, exitUsage)
	}
	if code := Execute([]string{"mv", "lab://docs", "lab://docs"}); code != exitUsage {
		t.Fatalf("mv docs onto itself: exit %d (want usage %d)", code, exitUsage)
	}
	if b, err := os.ReadFile(filepath.Join(reloadStoreRoot(t), "docs", "b.txt")); err != nil || string(b) != "beta" {
		t.Fatalf("refused transfer must leave the tree intact: %q %v", b, err)
	}

	// A file onto its own root: the landing path is the source itself —
	// a move would copy it onto itself and then delete it.
	if code := Execute([]string{"mv", "lab://a.txt", "lab://"}); code != exitUsage {
		t.Fatalf("mv file onto its own path: exit %d (want usage %d)", code, exitUsage)
	}
	if _, err := os.Stat(filepath.Join(reloadStoreRoot(t), "a.txt")); err != nil {
		t.Fatalf("refused move lost the source: %v", err)
	}

	// The sibling shapes that must keep working: a file into its own
	// subfolder (a different path, a real move) and a folder to a fresh
	// name.
	if code := Execute([]string{"mv", "lab://a.txt", "lab://docs"}); code != 0 {
		t.Fatalf("mv file into subfolder: exit %d", code)
	}
	root := reloadStoreRoot(t)
	if b, err := os.ReadFile(filepath.Join(root, "docs", "a.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("moved file content: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("mv left the source behind")
	}
	if code := Execute([]string{"cp", "lab://docs", "lab://d2", "-r"}); code != 0 {
		t.Fatalf("cp folder to a sibling: exit %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(reloadStoreRoot(t), "d2", "b.txt")); err != nil || string(b) != "beta" {
		t.Fatalf("sibling copy content: %q %v", b, err)
	}
}

// TestCopyS3ToS3CycleRefused pins the s3-to-s3 guards in isolation: both
// refusals run before the first S3 call, so a nil client is never dialed.
func TestCopyS3ToS3CycleRefused(t *testing.T) {
	if _, err := copyS3ToS3(context.Background(), nil,
		"s3://b/docs/", "s3://b/docs/sub/", copyOptions{Recursive: true}); err == nil ||
		!strings.Contains(err.Error(), "inside the source") {
		t.Fatalf("folder into itself: want an inside-the-source refusal, got %v", err)
	}
	if _, err := copyS3ToS3(context.Background(), nil,
		"s3://b/docs/", "s3://b/docs", copyOptions{Recursive: true}); err == nil ||
		!strings.Contains(err.Error(), "inside the source") {
		t.Fatalf("folder onto its own spot: want an inside-the-source refusal, got %v", err)
	}
	if _, err := copyS3ToS3(context.Background(), nil,
		"s3://b/readme.md", "s3://b/", copyOptions{}); err == nil ||
		!strings.Contains(err.Error(), "the same object") {
		t.Fatalf("file onto its own root: want a same-object refusal, got %v", err)
	}
}

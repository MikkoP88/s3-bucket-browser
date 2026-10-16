package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTryLockExcludesAndReleases(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "container.dat")

	release, err := TryLock(a + ".lock")
	if err != nil {
		t.Fatalf("First TryLock: %v", err)
	}
	if _, err := TryLock(a + ".lock"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Second TryLock on a held lock must refuse with ErrLocked, got %v", err)
	}
	// A different name is a different lock — independence, not a global gate.
	otherRelease, err := TryLock(filepath.Join(dir, "other.dat.lock"))
	if err != nil {
		t.Fatalf("TryLock on an independent name while one is held: %v", err)
	}
	otherRelease()

	release()
	relaunch, err := TryLock(a + ".lock")
	if err != nil {
		t.Fatalf("TryLock after release must succeed, got %v", err)
	}
	relaunch()
}

func TestTryLockOverLeftoverLockFile(t *testing.T) {
	// A lock file that lost its race with a waiter's remove (or outlived a
	// killed holder before the OS swept it) is a zero-byte leftover, not a
	// held lock: TryLock must take it and succeed.
	dir := t.TempDir()
	name := filepath.Join(dir, "leftover.lock")
	if err := os.WriteFile(name, nil, 0o600); err != nil {
		t.Fatalf("Seeding leftover lock file: %v", err)
	}
	release, err := TryLock(name)
	if err != nil {
		t.Fatalf("TryLock over a leftover lock file: %v", err)
	}
	if _, err := TryLock(name); !errors.Is(err, ErrLocked) {
		t.Fatalf("Held-over-leftover must still refuse a second taker, got %v", err)
	}
	release()
}

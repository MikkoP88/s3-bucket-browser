package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestTryLockBounded(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "shared.log.lock")

	// free: answers at once with the lock in hand
	release, err := TryLockBounded(name, time.Second)
	if err != nil {
		t.Fatalf("TryLockBounded on a free lock: %v", err)
	}
	release()

	// held for the whole budget (a sibling app window mid-append): the
	// answer is ErrLocked at the deadline, not a hang
	held, err := TryLock(name)
	if err != nil {
		t.Fatalf("seeding holder: %v", err)
	}
	start := time.Now()
	if _, err := TryLockBounded(name, 60*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("bounded wait on a held lock: got %v, want ErrLocked", err)
	}
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("budget not honored: returned after %v", d)
	}
	held()

	// held then released mid-budget: the wait outlasts the holder and
	// wins the lock
	mid, err := TryLock(name)
	if err != nil {
		t.Fatalf("seeding mid holder: %v", err)
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		mid()
	}()
	won, err := TryLockBounded(name, 5*time.Second)
	if err != nil {
		t.Fatalf("TryLockBounded after the holder released: %v", err)
	}
	won()
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

//go:build !windows

package lockfile

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes the exclusive flock on name's lock file without waiting.
// The returned release drops the lock; it is safe to call once, and the
// kernel drops it by itself if the process dies first. Removing the lock
// file on release is best-effort (a waiter may re-lock it mid-remove); a
// leftover zero-byte file is inert and reused by the next TryLock.
func TryLock(name string) (release func(), err error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file %s: %w", name, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("locking %s: %w", name, err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
		_ = os.Remove(name)
	}, nil
}

//go:build windows

package lockfile

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes the exclusive lock on name's lock file without waiting.
// The returned release drops the lock; it is safe to call once, and the
// kernel drops the lock by itself if the process dies first.
//
// Removing the lock file on release is best-effort and deliberately last:
// another waiter may re-lock the file between our CloseHandle and the
// remove, in which case the remove fails and one zero-byte file stays
// behind — inert, reused by the next TryLock.
func TryLock(name string) (release func(), err error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("lock path %s: %w", name, err)
	}
	h, err := windows.CreateFile(
		path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		// Sharing violations (another handle without share-delete is open)
		// or a locked range both mean "someone else holds it right now";
		// LockFileEx below decides the honest verdict for the normal path.
		return nil, fmt.Errorf("opening lock file %s: %w", name, err)
	}
	var ov windows.Overlapped
	err = windows.LockFileEx(
		h,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1, 0, // one byte at offset 0 — the lock's size is nominal
		&ov,
	)
	if err != nil {
		windows.CloseHandle(h)
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("locking %s: %w", name, err)
	}
	return func() {
		var un windows.Overlapped
		_ = windows.UnlockFileEx(h, 0, 1, 0, &un)
		windows.CloseHandle(h)
		_ = os.Remove(name)
	}, nil
}

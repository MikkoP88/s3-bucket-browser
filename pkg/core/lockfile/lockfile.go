// Package lockfile takes exclusive cross-process advisory locks without
// waiting. The profile container lock (pkg/api) uses it to keep one
// *.s3bprofile openable in exactly one app window at a time: the whole
// file is rewritten on every save, so two writers would clobber each
// other's secrets, and a lock answered in microseconds is kinder than a
// merge after the fact.
//
// The lock lives in a zero-byte file beside the guarded name ("<name>.lock").
// It is held by a kernel object (a locked byte range on Windows, a flock on
// unix), so the OS releases it the instant the holder dies — a crashed or
// killed app never wedges the file. Lock files may outlive their locks by
// instants (see the release note); they are inert and get reused.
package lockfile

import (
	"errors"
	"time"
)

// ErrLocked reports that another process (or another handle in this one)
// holds the lock right now.
var ErrLocked = errors.New("locked by another process")

// TryLockBounded retries a contended TryLock for up to budget. The shared
// log sinks use it (two app windows appending to one file): a sibling
// instance holding the lock costs a bounded wait, never a blocked app.
// ErrLocked comes back when the budget runs out; any other error (an
// unusable directory, permissions) is the lock's own and returns at once.
func TryLockBounded(name string, budget time.Duration) (func(), error) {
	deadline := time.Now().Add(budget)
	for {
		release, err := TryLock(name)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(2 * time.Millisecond)
	}
}

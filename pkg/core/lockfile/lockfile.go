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

import "errors"

// ErrLocked reports that another process (or another handle in this one)
// holds the lock right now.
var ErrLocked = errors.New("locked by another process")

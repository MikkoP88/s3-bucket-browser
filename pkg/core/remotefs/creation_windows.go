//go:build windows

package remotefs

import (
	"io/fs"
	"syscall"
	"time"
)

// CreationTimeOf returns the file's creation (birth) time. Windows carries
// it in the Win32 attribute data behind os.FileInfo, so the local engine
// can feed the grid's "Date created" column; other platforms build-tag to
// a no-op (see creation_other.go).
func CreationTimeOf(info fs.FileInfo) (time.Time, bool) {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}, false
	}
	ft := d.CreationTime
	// 100-ns ticks since 1601-01-01 → Unix epoch (same arithmetic the os
	// package uses for ModTime).
	n := int64(ft.HighDateTime)<<32 + int64(ft.LowDateTime) - 116444736000000000
	// An absent/zero filetime decodes to 1601-01-01 — before the epoch —
	// which reads as "unknown" rather than a real birth date.
	if n <= 0 {
		return time.Time{}, false
	}
	return time.Unix(n/1e7, (n%1e7)*100), true
}

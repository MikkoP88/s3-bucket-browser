//go:build !windows

package remotefs

import (
	"io/fs"
	"time"
)

// CreationTimeOf returns the file's creation (birth) time. Linux and the
// BSDs do not expose birth time through os.FileInfo (statx birth time is
// neither portable nor surfaced by the os package), so non-Windows builds
// report "unknown" and the grid renders the Date created cell empty — the
// same honest-absence contract folders follow for size and dates.
func CreationTimeOf(info fs.FileInfo) (time.Time, bool) {
	return time.Time{}, false
}

//go:build windows

package procprobe

import (
	"golang.org/x/sys/windows"
)

// alive asks the loader's own process table: OpenProcess fails with
// ERROR_INVALID_PARAMETER the moment no process carries the PID.
// ERROR_ACCESS_DENIED means the process exists but belongs to another
// user (or is protected) — alive, per the fail-safe law in the package
// comment.
func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	windows.CloseHandle(h)
	return true
}

//go:build !windows

package procprobe

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// alive asks with signal 0, which the kernel answers with existence and
// nothing else. EPERM means the process exists but belongs to another
// user — alive, per the fail-safe law in the package comment.
func alive(pid int) bool {
	err := unix.Kill(pid, syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

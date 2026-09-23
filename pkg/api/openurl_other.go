//go:build !windows

package api

import (
	"os/exec"
	"runtime"
)

// openExternal uses the platform opener — `open` on macOS, `xdg-open`
// on the Linux desktops. Started, not waited: the browser is not our
// child process to babysit.
func openExternal(u string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", u)
	} else {
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

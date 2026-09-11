//go:build windows

package cli

import (
	"os"
	"syscall"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// attachParentConsole makes CLI output visible when the binary is built
// with the windowsgui subsystem (no console flash on GUI launch — see
// release.yml).
//
// A GUI-subsystem process gets no console of its own, and terminals differ
// in what they hand it:
//
//   - cmd.exe / PowerShell without redirection pass no usable std handles —
//     attach to the parent's console and reopen CONOUT$/CONIN$.
//   - Git-Bash/mintty and any redirection (`> file`, `| grep`) pass real
//     inherited handles — use them as-is; attaching a hidden console here
//     and switching to CONOUT$ would swallow the output (verified the hard
//     way on this very code).
func attachParentConsole() {
	// Already have a console (console-subsystem build): nothing to do.
	if hwnd, _, _ := kernel32.NewProc("GetConsoleWindow").Call(); hwnd != 0 {
		return
	}
	outOK := usableHandle(os.Stdout)
	errOK := usableHandle(os.Stderr)
	if outOK && errOK {
		return // inherited pipes/files already work
	}
	r1, _, _ := kernel32.NewProc("AttachConsole").Call(^uintptr(0)) // ATTACH_PARENT_PROCESS
	if r1 == 0 {
		return
	}
	if !outOK {
		if con, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = con
		}
	}
	if !errOK {
		if con, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stderr = con
		}
	}
	if !usableHandle(os.Stdin) {
		if con, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			os.Stdin = con
		}
	}
}

// usableHandle reports whether f is backed by a working handle (pipe, file,
// ...). The zero/invalid std handles of a GUI-subsystem launch fail here.
func usableHandle(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := f.Stat()
	return err == nil
}

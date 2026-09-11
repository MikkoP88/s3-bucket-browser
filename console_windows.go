//go:build windows && !s3b_headless

package gui

import "syscall"

// detachConsole frees the console Windows allocated for this process.
//
// Release builds link with -H windowsgui (no console at all — see
// release.yml); this stays as belt-and-braces for console-subsystem builds
// (plain `go build`, some dev flows), where a GUI launch would otherwise
// show an empty console popup behind the window for the whole session.
// Detaching affects only this process: a GUI started from a terminal leaves
// the terminal itself untouched.
func detachConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	kernel32.NewProc("FreeConsole").Call()
}

//go:build windows && !s3b_headless

package gui

import "syscall"

// detachConsole frees the console Windows allocated for this process.
//
// s3b.exe is a console-subsystem binary (the CLI needs a console), so a GUI
// launch (no arguments) would otherwise show an empty console popup behind
// the window for the whole session. Detaching affects only this process:
// a GUI started from a terminal leaves the terminal itself untouched.
func detachConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	kernel32.NewProc("FreeConsole").Call()
}

//go:build windows

package guihealth

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

// notifyUser puts the message where a user with no app window cannot miss
// it: a plain modal MessageBox owned by the desktop.
func notifyUser(text string) {
	title, _ := windows.UTF16PtrFromString("S3 Bucket Browser")
	body, _ := windows.UTF16PtrFromString(text)
	// MB_ICONERROR | MB_SETFOREGROUND | MB_TOPMOST — be seen.
	procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(body)),
		uintptr(unsafe.Pointer(title)),
		0x10|0x00010000|0x00040000)
}

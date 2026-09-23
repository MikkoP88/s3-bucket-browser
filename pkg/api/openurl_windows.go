//go:build windows

package api

import (
	"syscall"
	"unsafe"
)

var pShellExecuteW = shell32.NewProc("ShellExecuteW")

// openExternal hands the URL to the shell, which routes it to the
// default browser. Anything but a success code comes back as <= 32
// (the SE_ERR_* range) with the last error set.
func openExternal(u string) error {
	p, err := syscall.UTF16PtrFromString(u)
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	r1, _, callErr := pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(p)), 0, 0, 1) // SW_SHOWNORMAL
	if r1 <= 32 {
		return callErr
	}
	return nil
}

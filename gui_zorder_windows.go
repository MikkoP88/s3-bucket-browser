//go:build windows && !s3b_headless

package gui

import "syscall"

// The window-group raise (see installGroupRaise in gui.go) is Win32-only:
// the z-order call that lifts the siblings of a clicked window without
// stealing its focus. Kept beside gui_screen_windows.go with the same
// build tag; pkg/api stays framework-free and cannot own window handles.

var pSetWindowPos = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")

// Win32 constants for the raise (MSDN, SetWindowPos): HWND_TOP inserts at
// the top of the non-topmost band, and the flags say "z-order only — no
// move, no resize, no activation, the owner's band untouched".
const (
	zHwndTop          = 0
	zSWPNosize        = 0x0001
	zSWPNomove        = 0x0002
	zSWPNoactivate    = 0x0010
	zSWPNoownerzorder = 0x0200
)

// raiseNoActivate lifts hwnd to the top of the z-order WITHOUT activating
// it — the clicked window keeps the focus while its siblings come forward.
func raiseNoActivate(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	pSetWindowPos.Call(hwnd, zHwndTop, 0, 0, 0, 0,
		zSWPNosize|zSWPNomove|zSWPNoactivate|zSWPNoownerzorder)
}

// The picker-modality ring (see pickerModalityRing in gui.go) is Win32
// too: EnableWindow is not in golang.org/x/sys/windows, so it rides the
// same LazyDLL door as the raise above.
var pEnableWindow = syscall.NewLazyDLL("user32.dll").NewProc("EnableWindow")

// disableWindow marks hwnd unable to take input while a native OS picker
// is up (its owner is disabled by the picker's own modal loop). It reports
// whether THIS call did the disabling — a window already disabled by
// someone else must not be re-enabled by the ring's restore.
func disableWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	// EnableWindow returns nonzero when the window WAS enabled — exactly
	// the "we disabled it, remember to re-enable" answer.
	r, _, _ := pEnableWindow.Call(hwnd, 0)
	return r != 0
}

// enableWindow gives a ring-disabled window its input back (picker closed).
func enableWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	pEnableWindow.Call(hwnd, 1)
}

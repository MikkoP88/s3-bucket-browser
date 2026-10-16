//go:build !windows && !s3b_headless

package gui

// raiseNoActivate has no non-Windows implementation: the window-group
// raise is a Win32 z-order dance, so on other desktop platforms the hook
// installs but every raise is a no-op (see installGroupRaise in gui.go).
func raiseNoActivate(hwnd uintptr) {
	_ = hwnd
}

// The picker-modality ring (see pickerModalityRing in gui.go) is Win32
// EnableWindow; elsewhere no window is ever ringed, so disableWindow
// answers false (nothing to remember) and enableWindow is a no-op.
func disableWindow(hwnd uintptr) bool {
	_ = hwnd
	return false
}

func enableWindow(hwnd uintptr) {
	_ = hwnd
}

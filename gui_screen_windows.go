//go:build windows && !s3b_headless

package gui

import (
	"syscall"
	"unsafe"
)

// Screen-point mapping for the native drag-out's self-drop detection
// (pkg/api's ScreenToClient shell hook). Kept beside gui.go with the same
// build tag; pkg/api stays framework-free and cannot own window handles.

var (
	pScreenToClient  = syscall.NewLazyDLL("user32.dll").NewProc("ScreenToClient")
	pGetClientRect   = syscall.NewLazyDLL("user32.dll").NewProc("GetClientRect")
	pGetDpiForWindow = syscall.NewLazyDLL("user32.dll").NewProc("GetDpiForWindow")
)

// clientPoint maps a screen point into hwnd's client area as CSS pixels
// (device pixels over the window's DPI scale — the same space Wails'
// file-drop coordinates live in) and reports whether the point is inside
// the client rectangle at all.
func clientPoint(hwnd uintptr, x, y int) (int, int, bool) {
	pt := struct{ X, Y int32 }{int32(x), int32(y)}
	if r, _, _ := pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, false
	}
	var rc struct{ L, T, R, B int32 }
	if r, _, _ := pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return 0, 0, false
	}
	inside := pt.X >= rc.L && pt.X < rc.R && pt.Y >= rc.T && pt.Y < rc.B
	scale := 1.0
	if dpi, _, _ := pGetDpiForWindow.Call(hwnd); dpi != 0 {
		scale = float64(dpi) / 96.0
	}
	return int(float64(pt.X) / scale), int(float64(pt.Y) / scale), inside
}

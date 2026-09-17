//go:build !windows && !s3b_headless

package gui

// clientPoint has no non-Windows implementation yet: the native drag-out
// is Windows-only, so the shell hook never installs elsewhere and the
// answer stays "not over self".
func clientPoint(hwnd uintptr, x, y int) (int, int, bool) {
	_ = hwnd
	_ = x
	_ = y
	return 0, 0, false
}

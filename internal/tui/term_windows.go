//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// makeRaw switches the console into VT-input mode: unprocessed (Ctrl+C
// arrives as a key for the double-press quit grammar, not a signal),
// unechoed, and with quick-edit off so the mouse reports to the app
// instead of selecting console text. Clearing quick-edit requires
// EXTENDED_FLAGS to take effect.
func makeRaw(in *os.File) (func(), error) {
	h := windows.Handle(in.Fd())
	var old uint32
	if err := windows.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	mode := old
	mode &^= windows.ENABLE_PROCESSED_INPUT |
		windows.ENABLE_LINE_INPUT |
		windows.ENABLE_ECHO_INPUT |
		windows.ENABLE_QUICK_EDIT_MODE
	mode |= windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(h, mode); err != nil {
		return nil, err
	}
	return func() { _ = windows.SetConsoleMode(h, old) }, nil
}

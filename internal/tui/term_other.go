//go:build !windows

package tui

import (
	"os"

	"golang.org/x/term"
)

// makeRaw drops the tty into raw mode via x/term (unprocessed, unechoed
// — Ctrl+C arrives as a key for the double-press quit grammar).
func makeRaw(in *os.File) (func(), error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(int(in.Fd()), state) }, nil
}

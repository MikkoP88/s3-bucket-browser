package tui

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Terminal owns stdin and its raw mode. When stdin is not a TTY (a
// pipe — the battery, or `echo keys | s3b tui`) raw mode is skipped
// entirely and the size comes from the S3B_TUI_SIZE override, because
// there is no real terminal to ask.
type Terminal struct {
	in      *os.File
	restore func()
	raw     bool
	w, h    int
}

func openTerminal() *Terminal {
	t := &Terminal{in: os.Stdin}
	t.w, t.h = t.probeSize()
	if term.IsTerminal(int(t.in.Fd())) {
		if restore, err := makeRaw(t.in); err == nil {
			t.restore, t.raw = restore, true
		}
	}
	return t
}

// Close restores the console mode (idempotent).
func (t *Terminal) Close() {
	if t.restore != nil {
		t.restore()
		t.restore = nil
	}
}

// Size returns the last known geometry.
func (t *Terminal) Size() (int, int) { return t.w, t.h }

// sizeOverride names a pipe-run geometry: S3B_TUI_SIZE=WxH.
func sizeOverride() (int, int, bool) {
	v := os.Getenv("S3B_TUI_SIZE")
	if v == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(v, "x", 2)
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w < 20 || h < 5 || len(parts) != 2 {
		return 0, 0, false
	}
	return w, h, true
}

// probeSize asks the terminal, falling back to the override then a
// sane default for redirected runs.
func (t *Terminal) probeSize() (int, int) {
	if w, h, ok := sizeOverride(); ok {
		return w, h
	}
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 120, 30
	}
	return w, h
}

// pollSize re-probes and reports whether the geometry changed.
func (t *Terminal) pollSize() bool {
	w, h := t.probeSize()
	if w == t.w && h == t.h {
		return false
	}
	t.w, t.h = w, h
	return true
}

package tui

import (
	"os"
	"strings"
	"testing"
)

func TestRuneWidthTable(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1},
		{'Z', 1},
		{'0', 1},
		{' ', 1},
		{'é', 1},     // precomposed Latin
		{0x0301, 0},  // combining acute
		{0x00AD, 0},  // soft hyphen
		{0x200B, 0},  // zero-width space
		{0xFE0F, 0},  // variation selector 16
		{0xFEFF, 0},  // BOM
		{0x4E2D, 2},  // CJK 中
		{0xAC00, 2},  // Hangul syllable
		{0x3042, 2},  // Hiragana あ
		{0xFF01, 2},  // fullwidth ！
		{0x1F4C1, 2}, // 📁
		{0x1F9E1, 2}, // 🧡 (orange heart, 1F9E1)
		{0x2E80, 2},  // CJK radicals start
		{0, 0},
		{0x10FFFF, 1}, // unassigned plane 16: degrade to 1, never corrupt
		{0x07FF, 1},   // unassigned-but-narrow
	}
	for _, c := range cases {
		if got := runeWidth(c.r); got != c.want {
			t.Errorf("runeWidth(U+%04X): want %d, got %d", c.r, c.want, got)
		}
	}
}

func TestStringWidthMixed(t *testing.T) {
	// a + 中 + ZWSP + 📁 + combining mark = 1+2+0+2+0
	if got := StringWidth("a中\u200b📁́"); got != 5 {
		t.Fatalf("StringWidth mixed: want 5, got %d", got)
	}
	if StringWidth("") != 0 {
		t.Fatal("empty string width must be 0")
	}
}

func TestDetectColorDepthMatrix(t *testing.T) {
	// Each case runs with a controlled environment: keys in kv are set,
	// every other probe key is truly unset (an empty NO_COLOR still
	// counts as set for LookupEnv).
	set := func(kv map[string]string) func(*testing.T) {
		return func(t *testing.T) {
			for _, k := range []string{"NO_COLOR", "COLORTERM", "TERM", "WT_SESSION", "ANSICON"} {
				if v, ok := kv[k]; ok {
					t.Setenv(k, v)
					continue
				}
				old, had := os.LookupEnv(k)
				os.Unsetenv(k)
				t.Cleanup(func() {
					if had {
						os.Setenv(k, old)
					} else {
						os.Unsetenv(k)
					}
				})
			}
		}
	}
	cases := []struct {
		name string
		env  map[string]string
		want colorDepth
	}{
		{"NO_COLOR wins over everything", map[string]string{"NO_COLOR": "1", "COLORTERM": "truecolor", "TERM": "xterm-256color"}, depthMono},
		{"COLORTERM truecolor", map[string]string{"COLORTERM": "truecolor", "TERM": "xterm"}, depthTrue},
		{"COLORTERM 24bit", map[string]string{"COLORTERM": "24bit"}, depthTrue},
		{"TERM 256color", map[string]string{"TERM": "screen-256color"}, depth256},
		{"any COLORTERM beats bare TERM", map[string]string{"COLORTERM": "yes", "TERM": "xterm"}, depth256},
		{"WT_SESSION is truecolor", map[string]string{"WT_SESSION": "abc"}, depthTrue},
		{"ANSICON is truecolor", map[string]string{"ANSICON": "1"}, depthTrue},
		{"dumb terminal", map[string]string{"TERM": "dumb"}, depthMono},
		{"no TERM at all", map[string]string{}, depthMono},
		{"plain xterm", map[string]string{"TERM": "xterm"}, depth16},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set(c.env)(t)
			if got := detectColorDepth(); got != c.want {
				t.Fatalf("want depth %d, got %d", c.want, got)
			}
		})
	}
}

func sgr(t *testing.T, from, to Style, depth colorDepth) string {
	t.Helper()
	var sb strings.Builder
	sgrSequence(from, to, depth, &sb)
	return sb.String()
}

func TestSGRTrueColor(t *testing.T) {
	// RGB fg + bold, from default.
	st := Style{Fg: RGB(1, 2, 3)}.Bold()
	if got := sgr(t, Style{}, st, depthTrue); got != "\x1b[38;2;1;2;3;1m" {
		t.Fatalf("truecolor fg+bold: got %q", got)
	}
	// Back to default: fg cleared with 39, bold dropped with 22.
	if got := sgr(t, st, Style{}, depthTrue); got != "\x1b[39;22m" {
		t.Fatalf("truecolor reset: got %q", got)
	}
	// A C256 color resolves through the palette, not its empty RGB
	// fields (index 255 is the grayscale ramp's end, 238,238,238).
	if got := sgr(t, Style{}, Style{Bg: C256(255)}, depthTrue); got != "\x1b[48;2;238;238;238m" {
		t.Fatalf("truecolor C256 bg: got %q", got)
	}
}

func TestSGR256(t *testing.T) {
	// An exact cube entry maps to its index: (0,95,135) is cube slot 24.
	if got := sgr(t, Style{}, Style{Fg: RGB(0, 95, 135)}, depth256); got != "\x1b[38;5;24m" {
		t.Fatalf("256 fg exact cube: got %q", got)
	}
	// A C256 passes through unchanged.
	if got := sgr(t, Style{}, Style{Fg: C256(42)}, depth256); got != "\x1b[38;5;42m" {
		t.Fatalf("256 passthrough: got %q", got)
	}
	// Default fg is cleared with 39.
	if got := sgr(t, Style{Fg: C256(42)}, Style{}, depth256); got != "\x1b[39m" {
		t.Fatalf("256 default: got %q", got)
	}
}

func TestSGR16(t *testing.T) {
	// Bright red fg (index 9 → 90+9-8=91) and blue bg (4 → 44).
	if got := sgr(t, Style{}, Style{Fg: RGB(255, 0, 0), Bg: RGB(0, 0, 128)}, depth16); got != "\x1b[91;44m" {
		t.Fatalf("16 fg+bg: got %q", got)
	}
	// Normal red (index 1 → 31).
	if got := sgr(t, Style{}, Style{Fg: RGB(128, 0, 0)}, depth16); got != "\x1b[31m" {
		t.Fatalf("16 normal red: got %q", got)
	}
}

func TestSGRMonoDropsColors(t *testing.T) {
	// Colors vanish; only attribute changes reach the wire.
	if got := sgr(t, Style{}, Style{Fg: RGB(255, 0, 0), Bg: C256(9)}, depthMono); got != "" {
		t.Fatalf("mono pure color change must emit nothing, got %q", got)
	}
	if got := sgr(t, Style{}, Style{Fg: RGB(255, 0, 0)}.Bold(), depthMono); got != "\x1b[0;1m" {
		t.Fatalf("mono bold add: got %q", got)
	}
	if got := sgr(t, Style{}.Bold().Under(), Style{}.Bold(), depthMono); got != "\x1b[0;1m" {
		t.Fatalf("mono underline drop keeps bold: got %q", got)
	}
}

func TestSGRAttrToggles(t *testing.T) {
	// Identical styles emit nothing.
	if got := sgr(t, Style{Fg: RGB(1, 2, 3)}.Bold(), Style{Fg: RGB(1, 2, 3)}.Bold(), depthTrue); got != "" {
		t.Fatalf("same style must emit nothing, got %q", got)
	}
	// Dropping dim while keeping bold re-applies bold after the 22.
	if got := sgr(t, Style{}.Bold().Dim(), Style{}.Bold(), depthTrue); got != "\x1b[22;1m" {
		t.Fatalf("dim drop keeps bold: got %q", got)
	}
	// Dropping bold while keeping dim re-applies dim.
	if got := sgr(t, Style{}.Bold().Dim(), Style{}.Dim(), depthTrue); got != "\x1b[22;2m" {
		t.Fatalf("bold drop keeps dim: got %q", got)
	}
	// Dropping both: bare 22.
	if got := sgr(t, Style{}.Bold().Dim(), Style{}, depthTrue); got != "\x1b[22m" {
		t.Fatalf("both dropped: got %q", got)
	}
	// Underline off, italic on — independent bits, no reset needed.
	if got := sgr(t, Style{}.Under(), Style{}.Italic(), depthTrue); got != "\x1b[24;3m" {
		t.Fatalf("underline→italic: got %q", got)
	}
}

func TestPalette256Anchors(t *testing.T) {
	anchors := []struct {
		idx     int
		r, g, b uint8
	}{
		{0, 0, 0, 0},         // xterm black
		{1, 205, 0, 0},       // xterm red
		{16, 0, 0, 0},        // cube corner
		{21, 0, 0, 255},      // cube (0,0,5)
		{196, 255, 0, 0},     // cube (5,0,0)
		{231, 255, 255, 255}, // cube (5,5,5)
		{232, 8, 8, 8},       // grayscale start
		{255, 238, 238, 238}, // grayscale end
	}
	for _, a := range anchors {
		p := palette256[a.idx]
		if p.Kind != ColorRGB || p.R != a.r || p.G != a.g || p.B != a.b {
			t.Errorf("palette256[%d]: want %d,%d,%d got %d,%d,%d", a.idx, a.r, a.g, a.b, p.R, p.G, p.B)
		}
	}
	// ansi256 round-trips an exact palette entry and maps near-misses
	// to the nearest cube slot.
	if got := ansi256(C256(77)); got != 77 {
		t.Errorf("ansi256 passthrough: want 77, got %d", got)
	}
	// The basic-16 slot 9 and cube slot 196 are both pure red, so a
	// red-ish color ties there; a cube-only level (95,0,0) proves the
	// search reaches past the basics.
	if got := ansi256(RGB(95, 0, 0)); got != 52 {
		t.Errorf("ansi256 nearest cube: want 52, got %d", got)
	}
	// ansi16 resolves through the palette: index 196 is pure red → 9.
	if got := ansi16(C256(196)); got != 9 {
		t.Errorf("ansi16 via palette: want 9, got %d", got)
	}
}

func TestNearest16(t *testing.T) {
	cases := []struct {
		r, g, b uint8
		want    uint8
	}{
		{0, 0, 0, 0},
		{255, 0, 0, 9},
		{0, 255, 0, 10},
		{255, 255, 255, 15},
		{127, 127, 127, 8},
		{192, 192, 192, 7},
	}
	for _, c := range cases {
		if got := nearest16(c.r, c.g, c.b); got != c.want {
			t.Errorf("nearest16(%d,%d,%d): want %d, got %d", c.r, c.g, c.b, c.want, got)
		}
	}
}

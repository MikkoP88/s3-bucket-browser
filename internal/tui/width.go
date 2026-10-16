package tui

import (
	"os"
	"strings"
)

// runeWidth returns the terminal display width of r: 0 for combining
// marks and other zero-width runes, 2 for East-Asian wide and fullwidth
// and the square emoji blocks, 1 for everything else. Hand-rolled (the
// module's dependency law: no new edges) over the ranges that actually
// appear in file names and UI chrome; unknown runes degrade to width 1,
// which renders as a normal glyph — never as grid corruption.
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if r < 0x1100 {
		// ASCII and Latin: width 1 except the soft hyphen and the
		// combining diacritics (0x300-0x36F — the one zero-width
		// family that lives below the CJK ranges).
		if r == 0x00AD || (r >= 0x0300 && r <= 0x036F) {
			return 0
		}
		return 1
	}
	// Zero-width families: combining marks, variation selectors, ZWJ and
	// friends, most flag/skin-tone modifiers.
	if inRanges(r, zeroWidth) {
		return 0
	}
	// Wide families: CJK ideographs and extensions, Hangul, Kana,
	// fullwidth forms, and the emoji planes/blocks terminals render
	// double-width.
	if inRanges(r, wideRanges) {
		return 2
	}
	return 1
}

// StringWidth sums the display width of a string.
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

type rng struct{ lo, hi rune }

func inRanges(r rune, rs []rng) bool {
	lo, hi := 0, len(rs)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case r < rs[mid].lo:
			hi = mid - 1
		case r > rs[mid].hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// zeroWidth holds the combining/mark ranges (sorted; binary-searched).
var zeroWidth = []rng{
	{0x0300, 0x036F}, // combining diacritical marks
	{0x0483, 0x0489},
	{0x0591, 0x05BD},
	{0x0610, 0x061A},
	{0x064B, 0x065F},
	{0x0670, 0x0670},
	{0x06D6, 0x06DC},
	{0x0730, 0x074A},
	{0x07A6, 0x07B0},
	{0x0900, 0x0903},
	{0x093A, 0x093C},
	{0x0941, 0x0948},
	{0x0951, 0x0957},
	{0x0E31, 0x0E31},
	{0x0E34, 0x0E3A},
	{0x0E47, 0x0E4E},
	{0x200B, 0x200F}, // zero-width space .. RLM
	{0x202A, 0x202E},
	{0x2060, 0x2064},
	{0x20D0, 0x20F0}, // combining marks for symbols
	{0xFE00, 0xFE0F}, // variation selectors
	{0xFE20, 0xFE2F},
	{0xFEFF, 0xFEFF}, // BOM / zero-width no-break space
	{0x1D167, 0x1D169},
	{0xE0100, 0xE01EF}, // variation selectors supplement
}

// wideRanges holds the double-width families (sorted; binary-searched).
var wideRanges = []rng{
	{0x1100, 0x115F}, // Hangul Jamo
	{0x2E80, 0x303E}, // CJK radicals, Kangxi, CJK symbols
	{0x3041, 0x33FF}, // Kana, Bopomofo, Katakana extensions, CJK compat
	{0x3400, 0x4DBF}, // CJK extension A
	{0x4E00, 0x9FFF}, // CJK unified
	{0xA000, 0xA4CF}, // Yi
	{0xA960, 0xA97F}, // Hangul Jamo extended-A
	{0xAC00, 0xD7A3}, // Hangul syllables
	{0xF900, 0xFAFF}, // CJK compatibility ideographs
	{0xFE10, 0xFE19}, // vertical forms
	{0xFE30, 0xFE6F}, // CJK compatibility forms + small forms
	{0xFF00, 0xFF60}, // fullwidth forms
	{0xFFE0, 0xFFE6},
	{0x16FE0, 0x16FE4},
	{0x17000, 0x18AFF},
	{0x1B000, 0x1B2FF},
	{0x1F004, 0x1F004},
	{0x1F0CF, 0x1F0CF},
	{0x1F18E, 0x1F18E},
	{0x1F191, 0x1F19A},
	{0x1F200, 0x1F320}, // enclosed ideographs + many emoji
	{0x1F32D, 0x1F335},
	{0x1F337, 0x1F37C},
	{0x1F37E, 0x1F393},
	{0x1F3A0, 0x1F3CA},
	{0x1F3CF, 0x1F3D3},
	{0x1F3E0, 0x1F3F0},
	{0x1F3F4, 0x1F3F4},
	{0x1F3F8, 0x1F43E},
	{0x1F440, 0x1F440},
	{0x1F442, 0x1F4FC},
	{0x1F4FF, 0x1F53D},
	{0x1F54B, 0x1F54E},
	{0x1F550, 0x1F567},
	{0x1F57A, 0x1F57A},
	{0x1F595, 0x1F596},
	{0x1F5A4, 0x1F5A4},
	{0x1F5FB, 0x1F64F},
	{0x1F680, 0x1F6C5},
	{0x1F6CC, 0x1F6CC},
	{0x1F6D0, 0x1F6D2},
	{0x1F6EB, 0x1F6EC},
	{0x1F6F4, 0x1F6FC},
	{0x1F7E0, 0x1F7EB},
	{0x1F90C, 0x1F93A},
	{0x1F93C, 0x1F945},
	{0x1F947, 0x1F978},
	{0x1F97A, 0x1F9CB},
	{0x1F9CD, 0x1F9FF},
	{0x1FA70, 0x1FAF6},
	{0x20000, 0x2FFFD}, // CJK extension B..
	{0x30000, 0x3FFFD},
}

// detectColorDepth negotiates the palette: truecolor when COLORTERM says
// so (Windows Terminal, WezTerm, kitty, iTerm, modern conhost), else
// 256 for TERM entries that have it, else 16. NO_COLOR collapses to mono
// (attributes survive; colors are dropped by the SGR emitter).
func detectColorDepth() colorDepth {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return depthMono
	}
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return depthTrue
	}
	if strings.Contains(os.Getenv("TERM"), "256color") {
		return depth256
	}
	if ct != "" { // any COLORTERM beats a bare TERM
		return depth256
	}
	// Windows Terminal and ConPTY announce themselves through WT_SESSION;
	// they are truecolor even with TERM unset.
	if os.Getenv("WT_SESSION") != "" || os.Getenv("ANSICON") != "" {
		return depthTrue
	}
	if os.Getenv("TERM") == "" || os.Getenv("TERM") == "dumb" {
		return depthMono
	}
	return depth16
}

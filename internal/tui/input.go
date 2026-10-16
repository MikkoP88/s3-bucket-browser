package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// InputParser turns terminal input bytes into events. It is a pure
// byte-in/events-out state machine over the ANSI/VT grammar — CSI and
// SS3 sequences, SGR-1006 mouse, focus reports, bracketed paste — which
// is what makes the whole face drivable over plain pipes: the battery
// writes the same bytes a terminal would.
//
// ESC disambiguation uses the per-chunk convention: a lone ESC at the
// end of a chunk is the Esc key (a human press reads as one byte);
// sequences arrive whole in one chunk and parse as such.
type InputParser struct {
	buf     []byte
	pasting bool
}

// Feed appends bytes and drains every complete event they complete.
// Events returned are valid until the next Feed call.
func (p *InputParser) Feed(b []byte) []Event {
	p.buf = append(p.buf, b...)
	var evs []Event
	for {
		n, ev, ok := p.decodeOne()
		if n > 0 {
			p.buf = p.buf[n:]
		}
		if !ok {
			break
		}
		if ev != nil {
			evs = append(evs, ev)
		}
	}
	return evs
}

// Pending reports buffered bytes still awaiting completion.
func (p *InputParser) Pending() int { return len(p.buf) }

func (p *InputParser) decodeOne() (n int, ev Event, ok bool) {
	if p.pasting {
		return p.decodePaste()
	}
	if len(p.buf) == 0 {
		return 0, nil, false
	}
	b := p.buf[0]
	switch {
	case b == 0x1B:
		n, ev, ok := p.decodeEscape()
		if ok {
			if _, isStart := ev.(pasteStart); isStart {
				p.pasting = true
				return n, nil, true // mode switch, not an event
			}
		}
		return n, ev, ok
	case b == 0x0D, b == 0x0A:
		return 1, KeyEvent{Key: KeyEnter}, true
	case b == 0x09:
		return 1, KeyEvent{Key: KeyTab}, true
	case b == 0x7F:
		return 1, KeyEvent{Key: KeyBackspace}, true
	case b == 0x08:
		return 1, KeyEvent{Key: KeyBackspace, Mod: ModCtrl}, true
	case b >= 0x01 && b <= 0x1A: // Ctrl+letter (CR/LF/Tab handled above)
		return 1, KeyEvent{Rune: rune(b + 'a' - 1), Key: KeyRune, Mod: ModCtrl}, true
	case b >= 0x1C && b <= 0x1F:
		r := map[byte]rune{0x1C: '\\', 0x1D: ']', 0x1E: '^', 0x1F: '_'}[b]
		return 1, KeyEvent{Rune: r, Key: KeyRune, Mod: ModCtrl}, true
	case b == 0:
		return 1, nil, true
	default:
		r, size := utf8.DecodeRune(p.buf)
		if r == utf8.RuneError && size <= 1 {
			return 1, nil, true // skip invalid byte
		}
		return size, KeyEvent{Rune: r, Key: KeyRune}, true
	}
}

func (p *InputParser) decodeEscape() (n int, ev Event, ok bool) {
	if len(p.buf) == 1 {
		return 1, KeyEvent{Key: KeyEsc}, true
	}
	switch p.buf[1] {
	case '[':
		return p.decodeCSI()
	case 'O':
		if len(p.buf) < 3 {
			return 0, nil, false
		}
		key, isKey := ss3Keys[p.buf[2]]
		if !isKey {
			return 3, nil, true
		}
		return 3, KeyEvent{Key: key}, true
	default:
		// ESC + printable: Alt+key.
		r, size := utf8.DecodeRune(p.buf[1:])
		if r == utf8.RuneError && size <= 1 {
			return 2, nil, true
		}
		switch r {
		case '\r':
			return 1 + size, KeyEvent{Key: KeyEnter, Mod: ModAlt}, true
		case '\t':
			return 1 + size, KeyEvent{Key: KeyBacktab}, true
		case 0x7F:
			return 1 + size, KeyEvent{Key: KeyBackspace, Mod: ModAlt}, true
		}
		return 1 + size, KeyEvent{Rune: r, Key: KeyRune, Mod: ModAlt}, true
	}
}

var ss3Keys = map[byte]KeyCode{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft,
	'H': KeyHome, 'F': KeyEnd,
	'P': KeyF1, 'Q': KeyF2, 'R': KeyF3, 'S': KeyF4,
}

// decodeCSI parses ESC [ params final. Parameter bytes are 0x30-0x3F;
// the final byte is 0x40-0x7E.
func (p *InputParser) decodeCSI() (n int, ev Event, ok bool) {
	i := 2
	for i < len(p.buf) && p.buf[i] >= 0x30 && p.buf[i] <= 0x3F {
		i++
	}
	if i >= len(p.buf) {
		return 0, nil, false // sequence still arriving
	}
	final := p.buf[i]
	body := string(p.buf[2:i])
	n = i + 1

	private := strings.HasPrefix(body, "?")
	mouse := strings.HasPrefix(body, "<")
	switch {
	case private:
		return n, nil, true // mode report — not ours
	case mouse:
		return n, parseMouse(strings.TrimPrefix(body, "<"), final), true
	}
	params := parseParams(body)

	switch final {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		key := map[byte]KeyCode{
			'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft,
			'H': KeyHome, 'F': KeyEnd,
		}[final]
		return n, KeyEvent{Key: key, Mod: modOf(params, 1)}, true
	case 'Z':
		return n, KeyEvent{Key: KeyBacktab}, true
	case 'P', 'Q', 'R', 'S':
		key := map[byte]KeyCode{
			'P': KeyF1, 'Q': KeyF2, 'R': KeyF3, 'S': KeyF4,
		}[final]
		return n, KeyEvent{Key: key, Mod: modOf(params, 1)}, true
	case 'I':
		return n, FocusEvent{Focused: true}, true
	case 'O':
		return n, FocusEvent{Focused: false}, true
	case 'u':
		return n, parseKitty(params), true
	case '~':
		return n, parseTilde(params), true
	}
	return n, nil, true // unknown sequence — swallow whole
}

// parseMouse decodes an SGR-1006 report: b;x;y with final M (press,
// motion, wheel) or m (release).
func parseMouse(body string, final byte) Event {
	parts := parseParams(body)
	if len(parts) < 3 {
		return nil
	}
	b, x, y := parts[0], parts[1], parts[2]
	mod := Mod(b & 0x1C >> 2) // shift 4, alt 8, ctrl 16 → our bit layout
	if b&64 != 0 {
		t := MouseWheelUp
		if b&1 != 0 {
			t = MouseWheelDown
		}
		return MouseEvent{X: x - 1, Y: y - 1, Type: t, Mod: mod}
	}
	btn := MouseButton(b & 3)
	if final == 'm' {
		return MouseEvent{X: x - 1, Y: y - 1, Button: btn, Type: MouseRelease, Mod: mod}
	}
	if b&32 != 0 {
		return MouseEvent{X: x - 1, Y: y - 1, Button: btn, Type: MouseMotion, Mod: mod}
	}
	return MouseEvent{X: x - 1, Y: y - 1, Button: btn, Type: MousePress, Mod: mod}
}

// parseKitty decodes CSI codepoint;mods u.
func parseKitty(params []int) Event {
	if len(params) == 0 {
		return nil
	}
	ev := KeyEvent{Mod: modOf(params, 1)}
	switch params[0] {
	case 27:
		ev.Key = KeyEsc
	case 13:
		ev.Key = KeyEnter
	case 9:
		ev.Key = KeyTab
	case 127:
		ev.Key = KeyBackspace
	default:
		ev.Key = KeyRune
		ev.Rune = rune(params[0])
	}
	return ev
}

// parseTilde decodes CSI n~ (with optional modifier as param 2).
func parseTilde(params []int) Event {
	if len(params) == 0 {
		return nil
	}
	mod := modOf(params, 1)
	switch params[0] {
	case 1, 7:
		return KeyEvent{Key: KeyHome, Mod: mod}
	case 2:
		return KeyEvent{Key: KeyInsert, Mod: mod}
	case 3:
		return KeyEvent{Key: KeyDelete, Mod: mod}
	case 4, 8:
		return KeyEvent{Key: KeyEnd, Mod: mod}
	case 5:
		return KeyEvent{Key: KeyPgUp, Mod: mod}
	case 6:
		return KeyEvent{Key: KeyPgDn, Mod: mod}
	case 11, 12, 13, 14, 15:
		return KeyEvent{Key: KeyCode(int(KeyF1) + params[0] - 11), Mod: mod}
	case 17, 18, 19, 20, 21:
		return KeyEvent{Key: KeyCode(int(KeyF6) + params[0] - 17), Mod: mod}
	case 23, 24, 25, 26:
		return KeyEvent{Key: KeyCode(int(KeyF11) + params[0] - 23), Mod: mod}
	case 200:
		return pasteStart{}
	case 201:
		return nil
	}
	return nil
}

// pasteStart is an internal marker; Feed switches modes on it.
type pasteStart struct{}

func (pasteStart) isEvent() {}

// decodePaste buffers pasted text until the 201~ terminator.
func (p *InputParser) decodePaste() (n int, ev Event, ok bool) {
	if len(p.buf) == 0 {
		return 0, nil, false // empty: wait for the paste body
	}
	const end = "\x1b[201~"
	idx := strings.Index(string(p.buf), end)
	if idx < 0 {
		if hasPartialSuffix(string(p.buf), end) {
			return 0, nil, false
		}
		// Unterminated: deliver what we have and end the mode —
		// a later terminator arrives as a swallowed CSI 201~ and
		// any trailing body as plain keys, so the inserted text
		// is the same either way (Feed's n-slicing drains).
		p.pasting = false
		return len(p.buf), PasteEvent{Text: string(p.buf)}, true
	}
	text := string(p.buf[:idx])
	p.pasting = false
	return idx + len(end), PasteEvent{Text: text}, true
}

// hasPartialSuffix reports whether s ends with a proper prefix of term
// (so a split terminator waits for its remainder).
func hasPartialSuffix(s, term string) bool {
	for k := len(term) - 1; k > 0 && k <= len(s); k-- {
		if s[len(s)-k:] == term[:k] {
			return true
		}
	}
	return false
}

// parseParams splits a CSI body into integers; empty/odd fields read 0.
func parseParams(body string) []int {
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	out := make([]int, len(parts))
	for i, s := range parts {
		v, _ := strconv.Atoi(strings.TrimSpace(s))
		out[i] = v
	}
	return out
}

// modOf extracts the SGR modifier param at index i (1-based value:
// 1 + shift|alt|ctrl|meta bits, laid out exactly like Mod).
func modOf(params []int, i int) Mod {
	if i >= len(params) || params[i] <= 1 {
		return 0
	}
	return Mod(params[i] - 1)
}

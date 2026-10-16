package tui

import (
	"reflect"
	"testing"
)

func feedAll(t *testing.T, chunks ...string) []Event {
	t.Helper()
	var p InputParser
	var evs []Event
	for _, c := range chunks {
		evs = append(evs, p.Feed([]byte(c))...)
	}
	return evs
}

func TestInputPlainRunes(t *testing.T) {
	evs := feedAll(t, "hi")
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	for i, want := range []rune{'h', 'i'} {
		k, ok := evs[i].(KeyEvent)
		if !ok || k.Key != KeyRune || k.Rune != want || k.Mod != 0 {
			t.Fatalf("event %d: want rune %q, got %#v", i, want, evs[i])
		}
	}
}

func TestInputControlBytes(t *testing.T) {
	cases := []struct {
		in   byte
		want KeyEvent
	}{
		{0x0D, KeyEvent{Key: KeyEnter}},
		{0x0A, KeyEvent{Key: KeyEnter}},
		{0x09, KeyEvent{Key: KeyTab}},
		{0x7F, KeyEvent{Key: KeyBackspace}},
		{0x08, KeyEvent{Key: KeyBackspace, Mod: ModCtrl}},
		{0x01, KeyEvent{Rune: 'a', Key: KeyRune, Mod: ModCtrl}},
		{0x1A, KeyEvent{Rune: 'z', Key: KeyRune, Mod: ModCtrl}},
		{0x1C, KeyEvent{Rune: '\\', Key: KeyRune, Mod: ModCtrl}},
	}
	for _, c := range cases {
		evs := feedAll(t, string(c.in))
		if len(evs) != 1 || evs[0] != Event(c.want) {
			t.Fatalf("byte %02x: want %#v, got %#v", c.in, c.want, evs)
		}
	}
	// NUL is swallowed whole.
	if evs := feedAll(t, "\x00a"); len(evs) != 1 {
		t.Fatalf("NUL should be skipped, got %d events", len(evs))
	}
}

func TestInputLoneEscPerChunk(t *testing.T) {
	// A lone ESC in its own chunk is the Esc key; a longer chunk
	// starting with ESC is a sequence (or Alt+key).
	evs := feedAll(t, "\x1b")
	if len(evs) != 1 || evs[0] != Event(KeyEvent{Key: KeyEsc}) {
		t.Fatalf("lone ESC: got %#v", evs)
	}
}

func TestInputCSIArrowsAndModifiers(t *testing.T) {
	pairs := []struct {
		in   string
		want KeyEvent
	}{
		{"\x1b[A", KeyEvent{Key: KeyUp}},
		{"\x1b[B", KeyEvent{Key: KeyDown}},
		{"\x1b[C", KeyEvent{Key: KeyRight}},
		{"\x1b[D", KeyEvent{Key: KeyLeft}},
		{"\x1b[H", KeyEvent{Key: KeyHome}},
		{"\x1b[F", KeyEvent{Key: KeyEnd}},
		{"\x1b[1;5A", KeyEvent{Key: KeyUp, Mod: ModCtrl}},
		{"\x1b[1;3D", KeyEvent{Key: KeyLeft, Mod: ModAlt}},
		{"\x1b[Z", KeyEvent{Key: KeyBacktab}},
		{"\x1bOP", KeyEvent{Key: KeyF1}},
		{"\x1bOQ", KeyEvent{Key: KeyF2}},
	}
	for _, c := range pairs {
		evs := feedAll(t, c.in)
		if len(evs) != 1 || evs[0] != Event(c.want) {
			t.Fatalf("%q: want %#v, got %#v", c.in, c.want, evs)
		}
	}
}

func TestInputTildeSpecials(t *testing.T) {
	pairs := []struct {
		in   string
		want Event
	}{
		{"\x1b[1~", KeyEvent{Key: KeyHome}},
		{"\x1b[7~", KeyEvent{Key: KeyHome}},
		{"\x1b[3~", KeyEvent{Key: KeyDelete}},
		{"\x1b[4~", KeyEvent{Key: KeyEnd}},
		{"\x1b[5~", KeyEvent{Key: KeyPgUp}},
		{"\x1b[6~", KeyEvent{Key: KeyPgDn}},
		{"\x1b[11~", KeyEvent{Key: KeyF1}},
		{"\x1b[15~", KeyEvent{Key: KeyF5}},
		{"\x1b[17~", KeyEvent{Key: KeyF6}},
		{"\x1b[21~", KeyEvent{Key: KeyF10}},
		{"\x1b[23~", KeyEvent{Key: KeyF11}},
		{"\x1b[24~", KeyEvent{Key: KeyF12}},
		{"\x1b[3;5~", KeyEvent{Key: KeyDelete, Mod: ModCtrl}},
		{"\x1b[I", FocusEvent{Focused: true}},
		{"\x1b[O", FocusEvent{Focused: false}},
	}
	for _, c := range pairs {
		evs := feedAll(t, c.in)
		if len(evs) != 1 || evs[0] != c.want {
			t.Fatalf("%q: want %#v, got %#v", c.in, c.want, evs)
		}
	}
}

func TestInputKitty(t *testing.T) {
	pairs := []struct {
		in   string
		want Event
	}{
		{"\x1b[97u", KeyEvent{Rune: 'a', Key: KeyRune}},
		{"\x1b[97;5u", KeyEvent{Rune: 'a', Key: KeyRune, Mod: ModCtrl}},
		{"\x1b[13u", KeyEvent{Key: KeyEnter}},
		{"\x1b[27u", KeyEvent{Key: KeyEsc}},
		{"\x1b[9u", KeyEvent{Key: KeyTab}},
	}
	for _, c := range pairs {
		evs := feedAll(t, c.in)
		if len(evs) != 1 || evs[0] != c.want {
			t.Fatalf("%q: want %#v, got %#v", c.in, c.want, evs)
		}
	}
}

func TestInputAltCombos(t *testing.T) {
	pairs := []struct {
		in   string
		want Event
	}{
		{"\x1bx", KeyEvent{Rune: 'x', Key: KeyRune, Mod: ModAlt}},
		{"\x1b\r", KeyEvent{Key: KeyEnter, Mod: ModAlt}},
		{"\x1b\t", KeyEvent{Key: KeyBacktab}},
		{"\x1b\x7f", KeyEvent{Key: KeyBackspace, Mod: ModAlt}},
	}
	for _, c := range pairs {
		evs := feedAll(t, c.in)
		if len(evs) != 1 || evs[0] != c.want {
			t.Fatalf("%q: want %#v, got %#v", c.in, c.want, evs)
		}
	}
}

func TestInputMouseSGR(t *testing.T) {
	pairs := []struct {
		in   string
		want Event
	}{
		// Left press at (10,5) — wire is 1-based, events are 0-based.
		{"\x1b[<0;11;6M", MouseEvent{X: 10, Y: 5, Button: ButtonLeft, Type: MousePress}},
		// Release of the same button.
		{"\x1b[<0;11;6m", MouseEvent{X: 10, Y: 5, Button: ButtonLeft, Type: MouseRelease}},
		// Right press (button 2).
		{"\x1b[<2;1;1M", MouseEvent{X: 0, Y: 0, Button: ButtonRight, Type: MousePress}},
		// Motion with no button (b&32).
		{"\x1b[<35;4;3M", MouseEvent{X: 3, Y: 2, Button: MouseButton(3), Type: MouseMotion}},
		// Wheel up / down (b&64; bit 0 flips direction).
		{"\x1b[<64;2;2M", MouseEvent{X: 1, Y: 1, Type: MouseWheelUp}},
		{"\x1b[<65;2;2M", MouseEvent{X: 1, Y: 1, Type: MouseWheelDown}},
		// Ctrl held: modifier bits 4/8/16 arrive as b's low bits.
		{"\x1b[<16;3;3M", MouseEvent{X: 2, Y: 2, Button: ButtonLeft, Type: MousePress, Mod: ModCtrl}},
	}
	for _, c := range pairs {
		evs := feedAll(t, c.in)
		if len(evs) != 1 || evs[0] != c.want {
			t.Fatalf("%q: want %#v, got %#v", c.in, c.want, evs)
		}
	}
}

func TestInputMouseSplitAcrossChunks(t *testing.T) {
	// Terminals deliver sequences whole, but pipes may split them; the
	// parser must buffer until the final byte.
	evs := feedAll(t, "\x1b[<0;", "11;6", "M")
	if len(evs) != 1 {
		t.Fatalf("split mouse: want 1 event, got %d", len(evs))
	}
	m, ok := evs[0].(MouseEvent)
	if !ok || m.X != 10 || m.Y != 5 || m.Type != MousePress {
		t.Fatalf("split mouse: got %#v", evs[0])
	}
}

func TestInputBracketedPaste(t *testing.T) {
	// Whole paste in one chunk.
	evs := feedAll(t, "\x1b[200~hello world\x1b[201~")
	if len(evs) != 1 {
		t.Fatalf("paste: want 1 event, got %d", len(evs))
	}
	if p, ok := evs[0].(PasteEvent); !ok || p.Text != "hello world" {
		t.Fatalf("paste: got %#v", evs[0])
	}

	// Terminator split across chunks waits for its remainder (the
	// partial-suffix guard).
	evs = feedAll(t, "\x1b[200~data\x1b[2", "01~", "\x1b[A")
	if len(evs) != 2 {
		t.Fatalf("split paste: want paste+arrow, got %d events", len(evs))
	}
	if p, ok := evs[0].(PasteEvent); !ok || p.Text != "data" {
		t.Fatalf("split paste text: got %#v", evs[0])
	}
	if _, ok := evs[1].(KeyEvent); !ok {
		t.Fatalf("event after paste should be a key, got %#v", evs[1])
	}

	// Unterminated paste delivers what it has (a drained pipe is the
	// only honest end).
	evs = feedAll(t, "\x1b[200~tail without end")
	if len(evs) != 1 {
		t.Fatalf("unterminated paste: want 1 event, got %d", len(evs))
	}
	if p, ok := evs[0].(PasteEvent); !ok || p.Text != "tail without end" {
		t.Fatalf("unterminated paste: got %#v", evs[0])
	}
}

func TestInputUnknownSequencesSwallowed(t *testing.T) {
	// Mode reports and unknown finals are consumed silently, and the
	// events on either side still arrive.
	evs := feedAll(t, "a\x1b[?2026h\x1b[10nb")
	if !reflect.DeepEqual(evs, []Event{
		KeyEvent{Rune: 'a', Key: KeyRune},
		KeyEvent{Rune: 'b', Key: KeyRune},
	}) {
		t.Fatalf("unknown sequences: got %#v", evs)
	}
}

func TestInputInvalidUTF8Skipped(t *testing.T) {
	evs := feedAll(t, "\xffz")
	if len(evs) != 1 {
		t.Fatalf("invalid byte should be skipped, got %d events", len(evs))
	}
	if k, ok := evs[0].(KeyEvent); !ok || k.Rune != 'z' {
		t.Fatalf("want 'z' after the bad byte, got %#v", evs[0])
	}
}

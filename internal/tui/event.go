package tui

// KeyCode identifies a non-rune key. KeyRune means the event's Rune
// field carries the character (with modifiers in Mod).
type KeyCode uint8

const (
	KeyRune KeyCode = iota
	KeyEnter
	KeyTab
	KeyBacktab
	KeyEsc
	KeyBackspace
	KeyDelete
	KeyInsert
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Mod is the modifier bitset for key and mouse events.
type Mod uint8

const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModMeta
)

// KeyEvent is one key press. For KeyRune, Rune holds the character
// (already lowercased by the terminal for Ctrl combos — Mod carries the
// meaning); for named keys Rune is 0.
type KeyEvent struct {
	Rune rune
	Key  KeyCode
	Mod  Mod
}

// MouseEvent kinds.
type MouseType uint8

const (
	MousePress MouseType = iota
	MouseRelease
	MouseMotion // hover (no button held) or drag (button held)
	MouseWheelUp
	MouseWheelDown
)

// MouseButton is the SGR button number (0 left, 1 middle, 2 right).
type MouseButton uint8

const (
	ButtonLeft MouseButton = iota
	ButtonMiddle
	ButtonRight
)

// MouseEvent is one SGR-1006 report. X/Y are 0-based cell coordinates
// (the wire is 1-based; the parser normalizes).
type MouseEvent struct {
	X, Y   int
	Button MouseButton
	Type   MouseType
	Mod    Mod
}

// FocusEvent reports terminal window focus (CSI I/O, enabled in Begin).
type FocusEvent struct{ Focused bool }

// ResizeEvent carries the new terminal size.
type ResizeEvent struct{ W, H int }

// PasteEvent carries bracketed-paste text as one unit.
type PasteEvent struct{ Text string }

// Event is anything the event loop consumes. AppMsg wraps engine-side
// messages (listings landed, transfer progress, watch ticks) so async
// work and input share one queue.
type Event interface{ isEvent() }

func (KeyEvent) isEvent()    {}
func (MouseEvent) isEvent()  {}
func (FocusEvent) isEvent()  {}
func (ResizeEvent) isEvent() {}
func (PasteEvent) isEvent()  {}

// AppMsg is the engine-to-UI message envelope. Data is owned by the
// sender and read-only on the loop goroutine — no locks needed.
type AppMsg struct {
	Kind string // "list", "progress", "done", "error", "tick", ...
	Data any
}

func (AppMsg) isEvent() {}

// Event strings the browser face uses (one place, no scattered literals).
const (
	MsgList     = "list"     // listing landed: Data = listResult
	MsgProgress = "progress" // transfer tick: Data = *TransferTick
	MsgDone     = "done"     // transfer finished: Data = *TransferTick (final)
	MsgError    = "error"    // background failure: Data = string
	MsgTick     = "tick"     // clock/EMA settle tick
)

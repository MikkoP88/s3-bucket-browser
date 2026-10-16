package tui

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// App is the event loop: it owns the screen and the terminal, reads
// input on a goroutine, coalesces redraws, routes events through the
// modal stack → popup menu → root chain, and restores the terminal no
// matter how it exits.
type App struct {
	screen  *Screen
	term    *Terminal
	events  chan Event
	quit    chan struct{}
	quitOne sync.Once

	root  Widget
	onMsg func(AppMsg)

	modals []*Dialog
	menu   *Menu

	ctrlCAt time.Time // first Ctrl+C press (double-press quits)

	OnQuitHint func()               // face paints "Ctrl+C again to quit"
	onPasteTgt func() pasteReceiver // face answers "who gets pasted text now"

	dirty  bool
	frames *frameSink
}

// NewApp wires the terminal, the screen, and (when asked by env) the
// frame-capture sink the battery reads.
func NewApp() (*App, error) {
	term := openTerminal()
	w, h := term.Size()
	a := &App{
		term:   term,
		screen: NewScreen(w, h, os.Stdout),
		events: make(chan Event, 256),
		quit:   make(chan struct{}),
	}
	if path := os.Getenv("S3B_TUI_FRAMES"); path != "" {
		sink, err := openFrameSink(path)
		if err != nil {
			term.Close()
			return nil, err
		}
		a.frames = sink
		a.screen.FrameCapture = sink.write
	}
	return a, nil
}

// SetRoot seats the face.
func (a *App) SetRoot(w Widget) { a.root = w }

// OnMsg registers the engine-message handler.
func (a *App) OnMsg(fn func(AppMsg)) { a.onMsg = fn }

// Screen exposes the draw surface (face layout + tests).
func (a *App) Screen() *Screen { return a.screen }

// MarkDirty asks for a redraw at the end of the event batch.
func (a *App) MarkDirty() { a.dirty = true }

// Post delivers an engine-side event from any goroutine.
func (a *App) Post(ev Event) {
	select {
	case a.events <- ev:
	case <-a.quit:
	}
}

// Quit stops the loop (idempotent).
func (a *App) Quit() { a.quitOne.Do(func() { close(a.quit) }) }

// PushModal stacks a dialog (centered; laid out on the next draw).
func (a *App) PushModal(d *Dialog) { a.modals = append(a.modals, d) }

// PopModal removes the top dialog.
func (a *App) PopModal() {
	if n := len(a.modals); n > 0 {
		a.modals = a.modals[:n-1]
	}
}

// TopModal returns the top dialog, if any.
func (a *App) TopModal() *Dialog {
	if n := len(a.modals); n > 0 {
		return a.modals[n-1]
	}
	return nil
}

// OpenMenu seats a context popup; CloseMenu removes it.
func (a *App) OpenMenu(m *Menu) { a.menu = m }
func (a *App) CloseMenu()       { a.menu = nil }

// Run enters the alternate screen and pumps events until Quit. Input
// EOF (the pipe writer closed) also quits — a drained pipe means the
// driving side is done.
func (a *App) Run() error {
	a.screen.Begin()
	defer func() {
		a.screen.End()
		a.term.Close()
		if a.frames != nil {
			a.frames.Close()
		}
	}()

	go a.readInput()

	a.resizeRoot()
	a.dirty = true
	a.redraw()

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-a.quit:
			return nil
		case ev := <-a.events:
			a.handle(ev)
			// Coalesce: drain whatever else is already queued, draw once.
		drain:
			for {
				select {
				case ev := <-a.events:
					a.handle(ev)
				default:
					break drain
				}
			}
			if a.dirty {
				a.redraw()
			}
		case <-tick.C:
			if a.term.pollSize() {
				w, h := a.term.Size()
				a.screen.Resize(w, h)
				a.resizeRoot()
			}
			if a.onMsg != nil {
				a.onMsg(AppMsg{Kind: MsgTick})
			}
			if a.dirty {
				a.redraw()
			}
		}
	}
}

// readInput pumps stdin bytes through the parser into the event queue.
func (a *App) readInput() {
	p := &InputParser{}
	buf := make([]byte, 4096)
	for {
		n, err := a.term.in.Read(buf)
		if n > 0 {
			for _, ev := range p.Feed(buf[:n]) {
				if _, isPasteStart := ev.(pasteStart); isPasteStart {
					p.pasting = true
					continue
				}
				a.Post(ev)
			}
		}
		if err != nil {
			a.Quit()
			return
		}
	}
}

// handle routes one event through the chrome chain.
func (a *App) handle(ev Event) {
	switch e := ev.(type) {
	case KeyEvent:
		if a.handleKeyChain(e) {
			a.dirty = true
		}
	case MouseEvent:
		if a.handleMouseChain(e) {
			a.dirty = true
		}
	case PasteEvent:
		if ti := a.pasteTarget(); ti != nil {
			ti.HandlePaste(e.Text)
			a.dirty = true
		}
	case ResizeEvent:
		a.screen.Resize(e.W, e.H)
		a.resizeRoot()
		a.dirty = true
	case FocusEvent:
		// focus loss/gain: no-op for now (future: pause redraws)
	case AppMsg:
		if a.onMsg != nil {
			a.onMsg(e)
		}
		a.dirty = true
	}
}

// pasteReceiver is the widget that accepts pasted text (TextInput).
type pasteReceiver interface{ HandlePaste(string) }

// OnPasteTarget registers the face's callback answering "who gets
// pasted text right now" (the modal input, else the command bar).
func (a *App) OnPasteTarget(fn func() pasteReceiver) { a.onPasteTgt = fn }

// pasteTarget answers the current paste receiver, if any.
func (a *App) pasteTarget() pasteReceiver {
	if a.onPasteTgt != nil {
		return a.onPasteTgt()
	}
	return nil
}

func (a *App) handleKeyChain(k KeyEvent) bool {
	if m := a.TopModal(); m != nil {
		return m.HandleKey(k)
	}
	if a.menu != nil {
		if k.Key == KeyEsc {
			a.CloseMenu()
			return true
		}
		if a.menu.HandleKey(k) {
			return true
		}
		a.CloseMenu()
		return true
	}
	if a.root != nil && a.root.HandleKey(k) {
		return true
	}
	return a.appKey(k)
}

func (a *App) handleMouseChain(m MouseEvent) bool {
	if d := a.TopModal(); d != nil {
		return d.HandleMouse(m)
	}
	if a.menu != nil {
		x, y, w, h := a.menu.Rect()
		if m.X >= x && m.X < x+w && m.Y >= y && m.Y < y+h {
			return a.menu.HandleMouse(m)
		}
		if m.Type == MousePress {
			a.CloseMenu()
			return true
		}
		return true // motions outside the popup stay popup-owned while open
	}
	if a.root != nil {
		return a.root.HandleMouse(m)
	}
	return false
}

// appKey is the global grammar: Ctrl+C twice quits, Ctrl+Q quits now,
// Ctrl+L repaints.
func (a *App) appKey(k KeyEvent) bool {
	if k.Key == KeyRune && k.Rune == 'q' && k.Mod == ModCtrl {
		a.Quit()
		return true
	}
	if k.Key == KeyRune && k.Rune == 'c' && k.Mod == ModCtrl {
		now := time.Now()
		if now.Sub(a.ctrlCAt) < 1500*time.Millisecond {
			a.Quit()
			return true
		}
		a.ctrlCAt = now
		if a.OnQuitHint != nil {
			a.OnQuitHint()
		}
		return true
	}
	if k.Key == KeyRune && k.Rune == 'l' && k.Mod == ModCtrl {
		w, h := a.term.Size()
		a.screen.Resize(w, h) // forces a full repaint
		return true
	}
	return false
}

// resizeRoot stretches the root to the full screen.
func (a *App) resizeRoot() {
	w, h := a.screen.Size()
	if a.root != nil {
		a.root.SetRect(0, 0, w, h)
	}
}

// redraw paints root → menu → modal stack over the dimmed backdrop.
func (a *App) redraw() {
	a.screen.Clear()
	if a.root != nil {
		a.root.Draw(a.screen)
	}
	w, h := a.screen.Size()
	if a.menu != nil {
		a.menu.Draw(a.screen)
	}
	for _, m := range a.modals {
		a.screen.FillRect(0, 0, w, h, Style{Bg: theme.Overlay})
		m.Layout(w, h)
		m.Draw(a.screen)
	}
	a.screen.Flush()
	a.dirty = false
}

// ── frame sink ────────────────────────────────────────────────────────────

// frameSink appends every flushed frame as one JSONL record — the
// battery's oracle: write keys to the pipe, read frames as text.
type frameSink struct {
	f *os.File
}

type frameRecord struct {
	Frame int      `json:"frame"`
	W     int      `json:"w"`
	H     int      `json:"h"`
	Lines []string `json:"lines"`
}

func openFrameSink(path string) (*frameSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &frameSink{f: f}, nil
}

func (fs *frameSink) write(frame, w, h int, lines []string) {
	b, err := json.Marshal(frameRecord{Frame: frame, W: w, H: h, Lines: lines})
	if err != nil {
		return
	}
	fs.f.Write(append(b, '\n'))
}

func (fs *frameSink) Close() { fs.f.Close() }

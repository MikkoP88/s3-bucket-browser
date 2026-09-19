package api

import "errors"

// Desktop shell seam. pkg/api is framework-free: every OS capability the
// frontend can reach (events, clipboard, native dialogs, quit, popout
// windows) is a function on DesktopShell, installed once by the GUI (gui.go)
// before the app runs. The dev/test harnesses never install it — they keep
// their narrower per-feature seams (SetEventSink, SetPickers,
// SetProfileDialogs, SetClipboardTextSink) which take precedence.

// EventDragSelfDrop announces a native drag-out released back over the app's
// own window. Payload: positional (x, y, shift, ctrl) where x/y are CSS
// pixels relative to the webview (same space as wails:file-drop) and
// shift/ctrl carry the modifier keys held at release — the frontend routes
// it exactly like an internal HTML5 drop.
const EventDragSelfDrop = "drag:self-drop"

// errNoShell is returned by shell-backed methods when no desktop shell is
// attached (headless harness without a scripted seam).
var errNoShell = errors.New("no desktop shell attached")

// ShellFilter is one native file-dialog filter.
type ShellFilter struct {
	DisplayName string // e.g. "s3b profile files (*.s3bprofile)"
	Pattern     string // semicolon-separated, e.g. "*.jpg;*.png"; empty = all
}

// ShellDialog describes a native file dialog. DefaultName seeds the save
// dialog's filename; ShowHidden reveals hidden files (uploads picker).
type ShellDialog struct {
	Title       string
	Filters     []ShellFilter
	DefaultName string
	ShowHidden  bool
}

// ShellGeometry sizes and places a native popout window. Zero W/H fall
// back to the dialog's preferred size. Center picks the opening anchor:
// "display" (the default) centers the popout on the display carrying the
// app's main window — multi-monitor aware, inside the display's work
// area; "app" centers it on the main window's own rect.
//
// MinW/MinH/MaxH carry optional resize bounds (zero = the shell's own
// defaults). MinH > 0 also marks the auto-height windows (transfers,
// running tasks — the Windows file-transfer footprint): their height
// tracks the content between MinH and MaxH and their width is a fixed
// footprint, so the shell makes them non-user-resizable and a remembered
// session size (either dimension) must not pin them on reopen — only
// placement is honored.
type ShellGeometry struct {
	W      int
	H      int
	Center string
	MinW   int
	MinH   int
	MaxH   int
}

// DesktopShell is the full desktop capability set. InstallDesktopShell
// requires every field — gui.go builds it once, complete.
type DesktopShell struct {
	// NativeWindows reports whether real OS windows exist for popouts.
	// Desktop builds set it true; Wails server builds (-tags server) run
	// the same app behind HTTP with no windows at all and must set false
	// so the frontend floats popouts in-page instead of into nothing.
	NativeWindows bool
	Emit          func(event string, data ...any)
	ClipGetText   func() (string, error)
	ClipSetText   func(text string) error
	OpenFiles     func(d ShellDialog) ([]string, error) // multi-select open
	OpenFile      func(d ShellDialog) (string, error)   // single-select open
	SaveFile      func(d ShellDialog) (string, error)
	OpenDir       func(title string) (string, error)
	Quit          func()
	// OpenPopout creates the native popout window for id (or focuses the
	// existing one) and reports whether a window was created. query is the
	// URL query after the app root ("/?<query>") without the leading '?'.
	OpenPopout  func(id, title, query string, geo ShellGeometry) bool
	ClosePopout func(id string)
	FocusPopout func(id string)
	// ResizePopout resizes the popout window for id (no-op when none).
	// The auto-height windows call it as their content grows and
	// shrinks; w <= 0 means "keep the current width".
	ResizePopout func(id string, w, h int)
	// PopoutOpen reports whether a live popout window exists for id.
	// The frontend heals its bookkeeping with it when a window died
	// without the popout:closed event. Optional: nil reads as false.
	PopoutOpen func(id string) bool
	// ScreenToClient maps a screen point into the main window's webview
	// CSS coordinates and reports whether the point is over the window's
	// client area. The native drag-out uses it to detect a drop landing
	// back on the app itself (self-drop) and to give the frontend drop
	// coordinates Explorer never provides. Optional: nil means "never over
	// self" (server builds, tests, non-Windows).
	ScreenToClient func(screenX, screenY int) (clientX, clientY int, over bool)
	// InvokeMain runs fn on the application's UI thread, blocking until it
	// returns. An OLE DoDragDrop must run on the thread that owns the
	// source window and its message pump — the drag-out marshals its whole
	// modal loop through here. Optional: nil (or any non-GUI build) sends
	// the drag to a dedicated STA thread instead.
	InvokeMain func(fn func() error) error
}

// shell is the installed desktop shell (nil outside the GUI).
var shell *DesktopShell

// InstallDesktopShell attaches the desktop shell capabilities. Only gui.go
// calls this, once, before the application runs.
func InstallDesktopShell(s *DesktopShell) { shell = s }

// IsDesktopShell reports whether the app runs with real desktop windows —
// the frontend uses it to decide between native popout windows and the
// in-page fallback (headless/server embedders have no windows to float).
func (a *App) IsDesktopShell() bool {
	return shell != nil && shell.NativeWindows
}

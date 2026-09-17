package api

import "errors"

// Desktop shell seam. pkg/api is framework-free: every OS capability the
// frontend can reach (events, clipboard, native dialogs, quit, popout
// windows) is a function on DesktopShell, installed once by the GUI (gui.go)
// before the app runs. The dev/test harnesses never install it — they keep
// their narrower per-feature seams (SetEventSink, SetPickers,
// SetProfileDialogs, SetClipboardTextSink) which take precedence.

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

// ShellGeometry sizes a native popout window. Zero W/H fall back to the
// dialog's preferred size. Placement is not part of the geometry: every
// popout opens centered on the app's main window.
type ShellGeometry struct {
	W int
	H int
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

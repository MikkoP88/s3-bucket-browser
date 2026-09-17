package api

import "errors"

// Native popout windows (Wails v3 multi-window). The frontend's openPopout
// calls OpenPopout when the desktop shell offers native windows; the created
// window loads the app with ?popout=<id> and renders the single view in DOM
// mode. The main window learns about a closed popout through the
// popout:closed event ({id}) so its bookkeeping (per-id singletons,
// onClose callbacks) stays correct.

// EventPopoutClosed announces a closed native popout window. Payload: {id}.
const EventPopoutClosed = "popout:closed"

// PopoutSpec is one native popout window request (frontend/js/dialogs.js).
type PopoutSpec struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Query is the URL query of the popout window without the leading '?'
	// (e.g. "popout=doctor&bucket=demo").
	Query string `json:"query"`
	// W/H size the window (zero falls back to the dialog's preferred
	// size). Placement is the backend's: every popout opens centered
	// on the app's main window.
	W int `json:"w"`
	H int `json:"h"`
}

// OpenPopout creates the native popout window for spec.ID, or focuses the
// existing one. Returns true when a window was created, false when one
// already floated (it was focused instead).
func (a *App) OpenPopout(spec PopoutSpec) (bool, error) {
	if shell == nil {
		return false, errNoShell
	}
	if spec.ID == "" {
		return false, errors.New("popout id required")
	}
	return shell.OpenPopout(spec.ID, spec.Title, spec.Query, ShellGeometry{
		W: spec.W, H: spec.H,
	}), nil
}

// ClosePopout closes the popout window for id (no-op when none).
func (a *App) ClosePopout(id string) {
	if shell != nil {
		shell.ClosePopout(id)
	}
}

// FocusPopout raises the popout window for id (no-op when none).
func (a *App) FocusPopout(id string) {
	if shell != nil {
		shell.FocusPopout(id)
	}
}

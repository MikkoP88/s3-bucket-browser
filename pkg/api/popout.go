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
	// size). Placement is the backend's; Center picks the anchor — see
	// ShellGeometry.Center.
	W int `json:"w"`
	H int `json:"h"`
	// Center picks where the popout opens: "display" (default) centers it
	// on the display carrying the app's main window; "app" centers it on
	// the main window itself. Anything but "app" means "display".
	Center string `json:"center"`
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
	center := spec.Center
	if center != "app" {
		center = "display" // the default; also swallows bogus values
	}
	return shell.OpenPopout(spec.ID, spec.Title, spec.Query, ShellGeometry{
		W: spec.W, H: spec.H,
		Center: center,
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

// PopoutOpen reports whether a live popout window exists for id. The
// frontend verifies with it that OpenPopout really produced a window
// before it gives up on the DOM fallback — and heals its per-id flags
// when a window died without the popout:closed event.
func (a *App) PopoutOpen(id string) bool {
	if shell == nil || shell.PopoutOpen == nil {
		return false
	}
	return shell.PopoutOpen(id)
}

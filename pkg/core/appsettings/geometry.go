// geometry.go: window placement memory (windows.json in the config
// dir). The app reopens where the user left it — the main window's
// rectangle and state, the popouts' rectangles — instead of the fixed
// 1280x800 every boot. Values are DIPs, the coordinate space the
// window APIs speak. A missing or corrupt file reads as zero values
// and the caller falls back to its defaults: placement is cosmetic and
// must never block a boot. The write is atomic (atomicfile), so a
// crash mid-save cannot trade a good memory for a truncated one.
package appsettings

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/atomicfile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// WindowRect is one remembered window rectangle (DIPs). Max marks a
// window that quit maximized; X/Y/W/H hold its NORMAL rect, so
// un-maximizing lands where the user left it, not at the maximized
// frame the OS would otherwise report on the way out.
type WindowRect struct {
	X, Y, W, H int
	Max        bool
}

// ScreenRect is a neutral screen rectangle (the wails Screen shape),
// so the sanity rules stay testable without a windowing system.
type ScreenRect struct{ X, Y, W, H int }

// WindowState is everything remembered about the app's windows.
type WindowState struct {
	Main    WindowRect            `json:"main"`
	Popouts map[string]WindowRect `json:"popouts,omitempty"`
}

func windowsPath() (string, error) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "windows.json"), nil
}

// LoadWindowState reads the memory; anything unreadable reads as the
// zero value (the caller's defaults).
func LoadWindowState() WindowState {
	p, err := windowsPath()
	if err != nil {
		return WindowState{}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return WindowState{}
	}
	var ws WindowState
	if json.Unmarshal(b, &ws) != nil {
		return WindowState{}
	}
	return ws
}

// SaveWindowState persists the memory atomically (0600).
func SaveWindowState(ws WindowState) error {
	p, err := windowsPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(p, b, 0o600)
}

// Sane validates a remembered rect for restoration and clamps it into
// range. ok=false means the memory cannot be trusted and the caller
// should place the window by its defaults. The rules: dimensions clamp
// up to the minimums (a remembered 900x500 from an older, smaller
// minimum clamps — it does not discard the placement); with no screens
// to check against the placement is untrustable; and the clamped rect
// must overlap some screen by enough to grab the title bar (100x30
// DIPs) — a window remembered on a monitor that is no longer attached
// must not reopen stranded off-screen where no drag can reach it.
func (r WindowRect) Sane(minW, minH int, screens []ScreenRect) (WindowRect, bool) {
	if r.W <= 0 || r.H <= 0 {
		return r, false
	}
	if len(screens) == 0 {
		return r, false
	}
	if r.W < minW {
		r.W = minW
	}
	if r.H < minH {
		r.H = minH
	}
	for _, s := range screens {
		ow := min(r.X+r.W, s.X+s.W) - max(r.X, s.X)
		oh := min(r.Y+r.H, s.Y+s.H) - max(r.Y, s.Y)
		if ow >= 100 && oh >= 30 {
			return r, true
		}
	}
	return r, false
}

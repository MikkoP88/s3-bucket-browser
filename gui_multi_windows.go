//go:build windows && !s3b_headless && !server

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/api"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/procprobe"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// Per-instance WebView2 profiles — the other half of multi-instance
// beside api.EnvMultiInstance's guard opt-out. The single-instance guard
// exists because two webview trees on one user-data folder deadlock (the
// loser hangs into the startup watchdog); a second window therefore gets
// its OWN folder, <config>/webview/<pid>, and never touches the first's.
// The default (single window, no opt-out) keeps the shared profile so
// remembered state survives as before.

// webviewPIDAlive is procprobe.Alive, injectable for rigs.
var webviewPIDAlive = procprobe.Alive

// webviewProfilePlan decides this launch's WebView2 user-data folder:
//   - the rig env always wins unchanged (test isolation, drag-live);
//   - a multi-instance launch gets a per-PID folder under the config dir
//     (system temp when the config dir cannot be resolved);
//   - the default launch keeps the shared profile ("" — WebView2 default).
//
// multi reports whether this launch is a multi-instance one, so callers
// can shape window placement (side-by-side copies cascade instead of
// stacking on the remembered rect) and run the profile sweep.
func webviewProfilePlan() (path string, multi bool) {
	if v := os.Getenv("S3B_RIG_WEBVIEW_PROFILE"); v != "" {
		return v, false
	}
	if os.Getenv(api.EnvMultiInstance) == "" {
		return "", false
	}
	if dir, err := profile.DefaultDir(); err == nil {
		return filepath.Join(dir, "webview", strconv.Itoa(os.Getpid())), true
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("s3b-webview-%d", os.Getpid())), true
}

// sweepStaleWebviewProfiles removes the profile folders of instances that
// no longer run (crash leftovers: a killed window's folder stays until
// some later boot sweeps it). A live sibling's folder is hands-off, and
// anything the plan does not recognise — non-numeric names, plain files —
// is left alone: the sweep only takes what it can name.
func sweepStaleWebviewProfiles() {
	// config-dir seats: <config>/webview/<pid>
	if dir, err := profile.DefaultDir(); err == nil {
		root := filepath.Join(dir, "webview")
		if entries, err := os.ReadDir(root); err == nil {
			for _, e := range entries {
				sweepStaleSeat(filepath.Join(root, e.Name()), e.Name())
			}
		}
	}
	// fallback seats (config dir unresolvable): %TEMP%/s3b-webview-<pid>
	if seats, err := filepath.Glob(filepath.Join(os.TempDir(), "s3b-webview-*")); err == nil {
		for _, seat := range seats {
			sweepStaleSeat(seat, strings.TrimPrefix(filepath.Base(seat), "s3b-webview-"))
		}
	}
}

// sweepStaleSeat removes one per-instance profile seat when its PID is
// gone (or is this process — a boot sweeps its own recycled-PID leftover).
func sweepStaleSeat(path, pidName string) {
	pid, err := strconv.Atoi(pidName)
	if err != nil || pid <= 0 {
		return // not a seat we can name — never touched
	}
	if pid != os.Getpid() && webviewPIDAlive(pid) {
		return // a live sibling window's profile — hands off
	}
	_ = os.RemoveAll(path)
}

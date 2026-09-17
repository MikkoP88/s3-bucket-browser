// Package guihealth makes GUI startup failures visible and self-healing.
//
// It exists because of a real incident (2026-09-17): an s3b session was
// cut short abruptly, but its WebView2 browser process tree survived the
// app, kept holding the WebView2 user-data-folder lock, and every later
// launch blocked forever inside WebView2 environment creation — a live
// process with no window, no taskbar icon and no error anywhere. To a
// user that reads as "the app stopped opening"; each retry stacked one
// more invisible process on top.
//
// Two defenses, layered:
//
//   - Preflight sweep (Windows, where the failure mode lives): terminate
//     msedgewebview2.exe trees whose host process is already dead. A
//     healthy webview tree always has a live host ancestor, so a dead
//     parent means the tree is garbage whichever app it came from. PID
//     reuse can only make the sweep skip a zombie (safe), never kill a
//     healthy tree. macOS (WKWebView) and Linux (WebKitGTK) have no
//     equivalent lock, so the sweep is a no-op there.
//   - Startup watchdog (every platform): the window must appear within a
//     generous budget of starting wails.Run; if it does not, fail LOUDLY
//     — event-log entry plus a platform-native message — instead of
//     hanging invisibly forever.
package guihealth

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// StartupBudget is how long the window may take to appear before the
// watchdog declares a failed start. Healthy starts take well under ten
// seconds; the budget only has to clear cold caches and slow disks.
const StartupBudget = 60 * time.Second

var (
	mu    sync.Mutex
	timer *time.Timer
	// windowUp is the race breaker: the timer callback re-checks it before
	// declaring failure, so a window that comes up in the same instant the
	// budget expires wins.
	windowUp atomic.Bool

	// onStartupTimeout runs when the budget expires before the window came
	// up; tests swap it out because the real one exits the process.
	onStartupTimeout = startupTimedOut
)

// ArmStartupWatchdog starts the pre-window watchdog. Call immediately
// before wails.Run; the window appearing (OnStartup) or Run returning
// disarms it again.
func ArmStartupWatchdog() { armStartupWatchdog(StartupBudget) }

func armStartupWatchdog(budget time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	windowUp.Store(false)
	timer = time.AfterFunc(budget, func() {
		if windowUp.Load() {
			return // window made it after all — the timer just lost the race
		}
		onStartupTimeout()
	})
}

// MarkWindowUp declares success: wails calls OnStartup once the window and
// the webview behind it exist, so the loud-failure timer must never fire
// after this point.
func MarkWindowUp() {
	windowUp.Store(true)
	DisarmStartupWatchdog()
}

// DisarmStartupWatchdog cancels the watchdog without declaring success —
// wails.Run returned (with or without an error), so there is no pending
// start to guard anymore.
func DisarmStartupWatchdog() {
	mu.Lock()
	defer mu.Unlock()
	if timer != nil {
		timer.Stop()
		timer = nil
	}
}

// startupTimedOut is the real timeout handler: record why, tell the user
// where they cannot miss it, and exit — a loud failure beats an invisible
// hang, and the preflight sweep of the NEXT start clears the usual cause.
func startupTimedOut() {
	msg := TimeoutMessage(StartupBudget)
	eventlog.Append("error", "app", "", msg)
	notifyUser(msg)
	if windowUp.Load() {
		return // the window came up while the message was being shown
	}
	os.Exit(1)
}

// TimeoutMessage is the remediation text shown when a start times out.
func TimeoutMessage(budget time.Duration) string {
	return fmt.Sprintf("The app window failed to appear within %.0f seconds. "+
		"On Windows the usual cause is leftover WebView2 processes from an earlier "+
		"session holding the user-data folder; the app clears those automatically "+
		"on startup, so starting it again should work. Details are in the event log "+
		"(Ctrl+L in the app, or events.jsonl in the config directory).",
		budget.Seconds())
}

// ---- preflight: orphaned WebView2 sweep ---------------------------------

// procInfo is one row of a process snapshot: pid, parent pid, and the
// executable base name (e.g. "msedgewebview2.exe").
type procInfo struct {
	pid  uint32
	ppid uint32
	name string
}

// The snapshot/terminate hooks default to no-ops (non-Windows platforms
// have nothing to sweep) and are replaced by the Windows implementation
// in sweep_windows.go. Keeping the algorithm in this platform-neutral
// file means `go test` exercises it on every OS.
var (
	listProcs     func() ([]procInfo, error) = func() ([]procInfo, error) { return nil, nil }
	terminateProc func(pid uint32) error     = func(uint32) error { return nil }
)

// sweep terminates every msedgewebview2.exe process tree whose parent
// process no longer exists and returns the root PIDs it killed. A live
// host always sits at the root of a healthy webview tree, so a dead
// parent marks the whole tree as leftover from a finished app session.
func sweep() []uint32 {
	procs, err := listProcs()
	if err != nil {
		return nil // no snapshot, no sweep — the watchdog still guards the start
	}
	alive := make(map[uint32]struct{}, len(procs))
	for _, p := range procs {
		alive[p.pid] = struct{}{}
	}
	var killed []uint32
	for _, p := range procs {
		if !strings.EqualFold(p.name, "msedgewebview2.exe") {
			continue
		}
		if _, ok := alive[p.ppid]; ok {
			continue // host chain still alive — a working webview, not ours to touch
		}
		killTree(procs, p.pid)
		killed = append(killed, p.pid)
	}
	return killed
}

// killTree terminates pid and everything descended from it, deepest
// children first (mirrors taskkill /T).
func killTree(procs []procInfo, pid uint32) {
	for _, c := range procs {
		if c.ppid == pid {
			killTree(procs, c.pid)
		}
	}
	_ = terminateProc(pid) // already-gone is fine; it wanted to die anyway
}

// Preflight prepares a safe GUI start before the webview is created.
// On Windows that means clearing orphaned WebView2 trees that would
// otherwise block webview creation (see the package comment); elsewhere
// it is a no-op, which keeps the call site unconditional. It returns the
// root PIDs of the trees it terminated.
func Preflight() []uint32 {
	killed := sweep()
	if len(killed) == 0 {
		return killed
	}
	pids := make([]string, len(killed))
	for i, p := range killed {
		pids[i] = strconv.FormatUint(uint64(p), 10)
	}
	eventlog.Append("warn", "app", "",
		"startup cleared "+strconv.Itoa(len(killed))+" orphaned WebView2 process tree(s) "+
			"left over by an earlier session (root PIDs: "+strings.Join(pids, ", ")+")")
	return killed
}

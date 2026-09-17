package guihealth

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// withProcs swaps the process-table hooks for a fixed snapshot and records
// terminations; everything is restored on cleanup.
func withProcs(t *testing.T, table []procInfo) *[]uint32 {
	t.Helper()
	killed := []uint32{}
	oldList, oldKill := listProcs, terminateProc
	listProcs = func() ([]procInfo, error) { return table, nil }
	terminateProc = func(pid uint32) error { killed = append(killed, pid); return nil }
	t.Cleanup(func() { listProcs, terminateProc = oldList, oldKill })
	return &killed
}

func TestSweepKillsOrphanedWebviewTrees(t *testing.T) {
	table := []procInfo{
		{pid: 1000, ppid: 900, name: "explorer.exe"},        // live host-chain root
		{pid: 1001, ppid: 1000, name: "s3b.exe"},            // a healthy app instance
		{pid: 1002, ppid: 1001, name: "msedgewebview2.exe"}, // its healthy webview — parent alive
		{pid: 2000, ppid: 1999, name: "msedgewebview2.exe"}, // orphan root: 1999 not in the snapshot
		{pid: 2001, ppid: 2000, name: "msedgewebview2.exe"}, // orphan child
		{pid: 2002, ppid: 2000, name: "msedgewebview2.exe"}, // orphan child
		{pid: 3000, ppid: 2999, name: "notepad.exe"},        // orphan but wrong name — untouched
	}
	killed := withProcs(t, table)

	roots := sweep()
	if !slices.Equal(roots, []uint32{2000}) {
		t.Fatalf("sweep roots = %v, want [2000]", roots)
	}
	if !slices.Contains(*killed, uint32(2000)) || !slices.Contains(*killed, uint32(2001)) || !slices.Contains(*killed, uint32(2002)) {
		t.Fatalf("tree not fully killed: %v", *killed)
	}
	for _, pid := range []uint32{1000, 1001, 1002, 3000} {
		if slices.Contains(*killed, pid) {
			t.Fatalf("sweep killed healthy/wrong-name process %d: %v", pid, *killed)
		}
	}
	// children die before their root (taskkill /T semantics)
	if !diesBefore(*killed, 2001, 2000) || !diesBefore(*killed, 2002, 2000) {
		t.Fatalf("children must be terminated before their root: %v", *killed)
	}
}

func TestSweepSkipsWhenParentPIDGotReused(t *testing.T) {
	// The webview's original host died, but its PID was reused by an
	// unrelated live process — the sweep must skip, never gamble.
	table := []procInfo{
		{pid: 5001, ppid: 1, name: "unrelated.exe"},
		{pid: 5000, ppid: 5001, name: "msedgewebview2.exe"},
	}
	killed := withProcs(t, table)
	if roots := sweep(); len(roots) != 0 {
		t.Fatalf("sweep roots = %v, want none (PID reuse must skip)", roots)
	}
	if len(*killed) != 0 {
		t.Fatalf("PID-reuse case must not kill: %v", *killed)
	}
}

func TestSweepNameMatchIsCaseInsensitive(t *testing.T) {
	table := []procInfo{
		{pid: 7000, ppid: 6999, name: "MSEdgeWebView2.EXE"},
	}
	killed := withProcs(t, table)
	if roots := sweep(); !slices.Equal(roots, []uint32{7000}) {
		t.Fatalf("case-insensitive name match failed: roots=%v killed=%v", roots, *killed)
	}
}

func TestSweepNothingToKill(t *testing.T) {
	killed := withProcs(t, nil)
	if roots := sweep(); len(roots) != 0 || len(*killed) != 0 {
		t.Fatalf("empty table must kill nothing: roots=%v killed=%v", roots, *killed)
	}
}

func diesBefore(order []uint32, before, after uint32) bool {
	for _, pid := range order {
		if pid == before {
			return true
		}
		if pid == after {
			return false
		}
	}
	return false
}

// ---- watchdog ------------------------------------------------------------

func TestWatchdogFiresWhenWindowNeverComesUp(t *testing.T) {
	fired := make(chan struct{})
	old := onStartupTimeout
	onStartupTimeout = func() { close(fired) }
	t.Cleanup(func() { onStartupTimeout = old })

	armStartupWatchdog(20 * time.Millisecond)
	defer DisarmStartupWatchdog()
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not fire before the window came up")
	}
}

func TestWatchdogMarkWindowUpCancels(t *testing.T) {
	fired := make(chan struct{})
	old := onStartupTimeout
	onStartupTimeout = func() { close(fired) }
	t.Cleanup(func() { onStartupTimeout = old })

	armStartupWatchdog(50 * time.Millisecond)
	MarkWindowUp()
	select {
	case <-fired:
		t.Fatal("watchdog fired although the window came up")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatchdogDisarmCancels(t *testing.T) {
	fired := make(chan struct{})
	old := onStartupTimeout
	onStartupTimeout = func() { close(fired) }
	t.Cleanup(func() { onStartupTimeout = old })

	armStartupWatchdog(50 * time.Millisecond)
	DisarmStartupWatchdog()
	select {
	case <-fired:
		t.Fatal("watchdog fired although it was disarmed")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestTimeoutMessageGuides(t *testing.T) {
	msg := TimeoutMessage(time.Minute)
	for _, want := range []string{"60", "WebView2", "again"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("timeout message lacks %q: %q", want, msg)
		}
	}
}

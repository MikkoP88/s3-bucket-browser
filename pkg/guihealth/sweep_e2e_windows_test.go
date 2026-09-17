//go:build windows

package guihealth

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestPreflightKillsRealOrphanNamedWebview reproduces the 2026-09-17
// incident shape for real: a process NAMED msedgewebview2.exe whose parent
// is gone must be terminated by Preflight, while nothing else is touched.
// It copies powershell.exe under the webview's name and launches it via a
// throwaway cmd that exits immediately, orphaning it.
func TestPreflightKillsRealOrphanNamedWebview(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real processes")
	}
	// Preflight logs its kill through the real eventlog; point the config
	// dir at a scratch dir so the test never writes to the user's
	// events.jsonl.
	t.Setenv("S3B_CONFIG", t.TempDir())
	src := filepath.Join(os.Getenv("WINDIR"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no powershell to impersonate with: %v", err)
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "msedgewebview2.exe")
	if data, err := os.ReadFile(src); err != nil {
		t.Fatalf("read powershell: %v", err)
	} else if err := os.WriteFile(fake, data, 0o755); err != nil {
		t.Fatalf("write fake webview: %v", err)
	}

	// `cmd /c start /b` launches the fake and exits at once — the fake's
	// recorded parent PID stops existing, exactly like the incident's
	// zombie webview tree.
	launcher := exec.Command("cmd.exe", "/c", "start", "/b", "", fake,
		"-NoProfile", "-Command", "Start-Sleep -Seconds 120")
	if err := launcher.Run(); err != nil {
		t.Fatalf("launch fake orphan: %v", err)
	}

	// Wait until the snapshot shows an orphaned msedgewebview2.exe.
	deadline := time.Now().Add(15 * time.Second)
	var orphan uint32
	for time.Now().Before(deadline) {
		procs, err := snapshotProcs()
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		alive := make(map[uint32]struct{}, len(procs))
		for _, p := range procs {
			alive[p.pid] = struct{}{}
		}
		for _, p := range procs {
			if p.name == "msedgewebview2.exe" {
				if _, ok := alive[p.ppid]; !ok {
					orphan = p.pid
				} else {
					// someone's real webview with a live host — leave it be
				}
			}
		}
		if orphan != 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if orphan == 0 {
		t.Fatal("fake orphan never showed up with a dead parent")
	}

	killed := Preflight()
	if !slices.Contains(killed, orphan) {
		t.Fatalf("Preflight roots %v do not include the fake orphan %d", killed, orphan)
	}
	// Termination is requested, not reaped — poll for it to stick.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		procs, _ := snapshotProcs()
		gone := true
		for _, p := range procs {
			if p.pid == orphan {
				gone = false
				break
			}
		}
		if gone {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("fake orphan survived the preflight sweep")
}

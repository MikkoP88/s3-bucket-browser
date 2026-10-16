package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/lockfile"
)

// The transfers.log sink is shared by every app window, so every append
// rides the cross-process lock beside the file — and the same best-effort
// law the event log rides: a sibling window holding the lock past the
// budget costs this window its line, never a blocked transfer.
func TestLogTransferDropsLineWhenSinkLockHeld(t *testing.T) {
	a := newTestApp(t)
	dir := os.Getenv("S3B_CONFIG")
	p := filepath.Join(dir, "transfers.log")

	release, err := lockfile.TryLock(p + ".lock")
	if err != nil {
		t.Fatalf("seeding holder: %v", err)
	}
	start := time.Now()
	a.logTransfer(map[string]any{"id": "contended"})
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("contended logTransfer blocked for %v — the budget must bound it", d)
	}
	release()

	// after the holder leaves, lines land whole — and the contended one
	// never half-landed beside them
	a.logTransfer(map[string]any{"id": "settled"})
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading transfers.log after settle: %v", err)
	}
	if strings.Contains(string(b), "contended") {
		t.Fatalf("contended line landed after the budget: %s", b)
	}
	if !strings.Contains(string(b), `"id":"settled"`) {
		t.Fatalf("settled line missing: %s", b)
	}
}

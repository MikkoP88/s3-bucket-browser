package guihealth

import (
	"errors"
	"strings"
	"testing"
)

// swapProbe replaces the platform probe for one test and restores it
// after; the real one may answer anything on the machine running the
// tests, so every case pins its own.
func swapProbe(t *testing.T, probe func() (string, bool)) {
	t.Helper()
	prev := probeWebView2Runtime
	probeWebView2Runtime = probe
	t.Cleanup(func() { probeWebView2Runtime = prev })
}

// swapNotify captures announcements instead of raising real dialogs.
func swapNotify(t *testing.T) *[]string {
	t.Helper()
	prev := notifyUser
	shown := &[]string{}
	notifyUser = func(text string) { *shown = append(*shown, text) }
	t.Cleanup(func() { notifyUser = prev })
	return shown
}

func TestWebview2VersionUsable(t *testing.T) {
	tests := []struct {
		pv   string
		want bool
	}{
		{"", false},             // never populated
		{"0.0.0.0", false},      // EdgeUpdate's mid-install / broken marker
		{"131.0.2903.86", true}, // a real Evergreen version
		{"0.0.0.1", true},       // only the exact marker is unusable
	}
	for _, tt := range tests {
		if got := webview2VersionUsable(tt.pv); got != tt.want {
			t.Errorf("webview2VersionUsable(%q) = %v, want %v", tt.pv, got, tt.want)
		}
	}
}

func TestEnsureWebView2MissingIsLoud(t *testing.T) {
	swapProbe(t, func() (string, bool) { return "", true }) // asked everywhere: absent
	shown := swapNotify(t)
	if err := EnsureWebView2(); !errors.Is(err, ErrWebView2Missing) {
		t.Fatalf("err = %v, want ErrWebView2Missing", err)
	}
	if len(*shown) != 1 {
		t.Fatalf("announcements = %d, want exactly 1", len(*shown))
	}
	for _, want := range []string{"WebView2 Runtime", "winget install Microsoft.EdgeWebView2Runtime", "developer.microsoft.com"} {
		if !strings.Contains((*shown)[0], want) {
			t.Errorf("message missing %q:\n%s", want, (*shown)[0])
		}
	}
}

// A broken pv in the registry is the same verdict as no pv at all.
func TestEnsureWebView2BrokenVersionIsLoud(t *testing.T) {
	swapProbe(t, func() (string, bool) { return "0.0.0.0", true })
	shown := swapNotify(t)
	if err := EnsureWebView2(); !errors.Is(err, ErrWebView2Missing) {
		t.Fatalf("err = %v, want ErrWebView2Missing", err)
	}
	if len(*shown) != 1 {
		t.Fatalf("announcements = %d, want exactly 1", len(*shown))
	}
}

func TestEnsureWebView2PresentPassesSilently(t *testing.T) {
	swapProbe(t, func() (string, bool) { return "131.0.2903.86", true })
	shown := swapNotify(t)
	if err := EnsureWebView2(); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(*shown) != 0 {
		t.Fatalf("announcements = %d, want none", len(*shown))
	}
}

// An unreadable registry must never block a launch: known=false reads as
// present, whatever pv carries.
func TestEnsureWebView2UnreadableProbeFailsOpen(t *testing.T) {
	swapProbe(t, func() (string, bool) { return "", false })
	shown := swapNotify(t)
	if err := EnsureWebView2(); err != nil {
		t.Fatalf("err = %v, want nil (fail open)", err)
	}
	if len(*shown) != 0 {
		t.Fatalf("announcements = %d, want none", len(*shown))
	}
}

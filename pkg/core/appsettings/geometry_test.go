package appsettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStateRoundTrip(t *testing.T) {
	isolate(t)
	ws := WindowState{
		Main:    WindowRect{X: 40, Y: 80, W: 1600, H: 900, Max: true},
		Popouts: map[string]WindowRect{"tasks": {X: 100, Y: 200, W: 490, H: 300}},
	}
	if err := SaveWindowState(ws); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowState()
	if got.Main != ws.Main {
		t.Fatalf("main = %+v, want %+v", got.Main, ws.Main)
	}
	if got.Popouts["tasks"] != ws.Popouts["tasks"] {
		t.Fatalf("popout = %+v, want %+v", got.Popouts["tasks"], ws.Popouts["tasks"])
	}
}

func TestWindowStateMissingIsZero(t *testing.T) {
	isolate(t)
	if got := LoadWindowState(); got.Main != (WindowRect{}) || got.Popouts != nil {
		t.Fatalf("missing file must read as zero state, got %+v", got)
	}
}

// TestWindowStateCorruptIsZero pins the boot contract: a truncated or
// garbage memory is placement defaults, never a failed start.
func TestWindowStateCorruptIsZero(t *testing.T) {
	dir := isolate(t)
	if err := os.WriteFile(filepath.Join(dir, "windows.json"), []byte("{\"main\": {\"X\": 12,"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadWindowState(); got.Main != (WindowRect{}) {
		t.Fatalf("corrupt file must read as zero state, got %+v", got)
	}
}

func TestSane(t *testing.T) {
	screens := []ScreenRect{{X: 0, Y: 0, W: 1920, H: 1080}, {X: 1920, Y: 0, W: 2560, H: 1440}}
	tests := []struct {
		name      string
		in        WindowRect
		want      WindowRect
		wantOK    bool
		noScreens bool
	}{
		{name: "on second monitor kept", in: WindowRect{X: 2000, Y: 100, W: 1200, H: 800},
			want: WindowRect{X: 2000, Y: 100, W: 1200, H: 800}, wantOK: true},
		{name: "undersized clamps, placement kept", in: WindowRect{X: 10, Y: 10, W: 900, H: 500},
			want: WindowRect{X: 10, Y: 10, W: 960, H: 600}, wantOK: true},
		{name: "monitor gone rejected", in: WindowRect{X: 5000, Y: 2000, W: 1200, H: 800}, wantOK: false},
		{name: "zero size rejected", in: WindowRect{X: 0, Y: 0, W: 0, H: 0}, wantOK: false},
		{name: "no screens rejected", in: WindowRect{X: 0, Y: 0, W: 1280, H: 800}, wantOK: false, noScreens: true},
		// A window hanging almost entirely off the left edge keeps only
		// 60 DIPs on-screen: nothing grabbable stays visible.
		{name: "sliver overlap rejected", in: WindowRect{X: -1220, Y: 100, W: 1280, H: 800}, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := screens
			if tt.noScreens {
				sc = nil
			}
			got, ok := tt.in.Sane(960, 600, sc)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("rect = %+v, want %+v", got, tt.want)
			}
		})
	}
}

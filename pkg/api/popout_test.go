package api

import "testing"

// The popout placement contract: the frontend's Center value ("display" /
// "app", or anything else a stale client might send) must reach the shell
// normalized — "app" passes through, everything else becomes the
// "display" default. The shell itself (gui.go) owns the actual placement
// math; this pins the seam.
func TestOpenPopoutCenterNormalized(t *testing.T) {
	var got ShellGeometry
	InstallDesktopShell(&DesktopShell{
		OpenPopout: func(id, title, query string, geo ShellGeometry) bool {
			got = geo
			return true
		},
	})
	t.Cleanup(func() { InstallDesktopShell(nil) })
	a := newTestApp(t)

	for _, tc := range []struct{ in, want string }{
		{"", "display"},
		{"display", "display"},
		{"app", "app"},
		{"bogus", "display"},
	} {
		created, err := a.OpenPopout(PopoutSpec{ID: "transfers", Title: "File transfers", Query: "popout=transfers", Center: tc.in})
		if err != nil {
			t.Fatalf("OpenPopout(center=%q): %v", tc.in, err)
		}
		if !created {
			t.Fatalf("OpenPopout(center=%q): shell reported no creation", tc.in)
		}
		if got.Center != tc.want {
			t.Errorf("center %q reached shell as %q, want %q", tc.in, got.Center, tc.want)
		}
	}

	// W/H ride along untouched; a missing id is still refused.
	if _, err := a.OpenPopout(PopoutSpec{ID: "x", W: 720, H: 560, Center: "app"}); err != nil {
		t.Fatal(err)
	}
	if got.W != 720 || got.H != 560 {
		t.Errorf("geometry passthrough got %dx%d, want 720x560", got.W, got.H)
	}
	if _, err := a.OpenPopout(PopoutSpec{Center: "app"}); err == nil {
		t.Error("OpenPopout without id: want error, got nil")
	}
}

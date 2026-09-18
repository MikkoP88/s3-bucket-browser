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

// The auto-height windows' resize bounds (the Windows file-transfer
// footprint: 490x300 default/min, 740 max height) must reach the shell
// verbatim, and ResizePopout must pass the content fit through — with a
// nil hook it stays a no-op (the call is best-effort from the window).
func TestOpenPopoutBoundsAndResize(t *testing.T) {
	var gotGeo ShellGeometry
	var gotResize [3]any
	InstallDesktopShell(&DesktopShell{
		OpenPopout: func(id, title, query string, geo ShellGeometry) bool {
			gotGeo = geo
			return true
		},
		ResizePopout: func(id string, w, h int) {
			gotResize = [3]any{id, w, h}
		},
	})
	t.Cleanup(func() { InstallDesktopShell(nil) })
	a := newTestApp(t)

	if _, err := a.OpenPopout(PopoutSpec{
		ID: "transfers", Title: "File transfers", Query: "popout=transfers",
		W: 490, H: 300, MinW: 490, MinH: 300, MaxH: 740,
	}); err != nil {
		t.Fatal(err)
	}
	if gotGeo.W != 490 || gotGeo.H != 300 {
		t.Errorf("size passthrough got %dx%d, want 490x300", gotGeo.W, gotGeo.H)
	}
	if gotGeo.MinW != 490 || gotGeo.MinH != 300 || gotGeo.MaxH != 740 {
		t.Errorf("bounds passthrough got min %dx%d max-h %d, want min 490x300 max-h 740",
			gotGeo.MinW, gotGeo.MinH, gotGeo.MaxH)
	}

	a.ResizePopout("transfers", 490, 512)
	if gotResize[0] != "transfers" || gotResize[1] != 490 || gotResize[2] != 512 {
		t.Errorf("ResizePopout passthrough got %v, want transfers 490x512", gotResize)
	}
	InstallDesktopShell(&DesktopShell{}) // nil hook: no-op, no panic
	a.ResizePopout("transfers", 490, 512)
}

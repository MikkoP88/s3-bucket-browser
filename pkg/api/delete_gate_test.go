package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// seedFiles writes n files (f001..fNNN) under dir/victim/ and returns the
// victim tree path. Used to cross the deleteForceThreshold gate honestly.
func seedFiles(t *testing.T, dir string, n int) string {
	t.Helper()
	victim := filepath.Join(dir, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		p := filepath.Join(victim, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return victim
}

// LocalRemove re-counts at act time and refuses above the force threshold:
// nothing is deleted without force, everything with it, and a small
// selection still deletes without force (the preview already confirmed it).
func TestLocalRemoveForceGate(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	over := deleteForceThreshold + 5
	victim := seedFiles(t, t.TempDir(), over)

	// Refused: the tree survives byte for byte.
	res, err := a.LocalRemove([]string{victim}, false)
	if err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("over-threshold remove without force: err = %v", err)
	}
	if res.Deleted != 0 {
		t.Errorf("refused remove deleted %d item(s)", res.Deleted)
	}
	if _, err := os.Stat(filepath.Join(victim, "f000.txt")); err != nil {
		t.Fatalf("victim file vanished after a refused delete: %v", err)
	}

	// Forced: the whole tree goes.
	res, err = a.LocalRemove([]string{victim}, true)
	if err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	if res.Deleted != 1 || len(res.Errors) != 0 {
		t.Fatalf("forced remove = %+v", res)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("victim tree survived a forced remove")
	}

	// Under the threshold: no force needed (the window's plain confirm is it).
	small := seedFiles(t, t.TempDir(), 3)
	if _, err := a.LocalRemove([]string{small}, false); err != nil {
		t.Fatalf("small remove without force: %v", err)
	}
	if _, err := os.Stat(small); !os.IsNotExist(err) {
		t.Error("small tree survived an ungated remove")
	}
}

// A filesystem-root operand is a per-item refusal on BOTH rungs — and the
// counting pass never walks it to decide (pinning the GUI-57 regression:
// a root had to fail fast, not after a full-drive walk that ends in the
// same refusal). Drive roots are refused BY SHAPE on every platform: a
// pasted C:\ meets the same gate on a mac or linux runner, where the OS
// would otherwise only answer "no such file" after the gate stayed quiet.
func TestLocalRemoveRootRefusedFast(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	for _, force := range []bool{false, true} {
		for _, root := range []string{`C:\`, "/"} {
			res, err := a.LocalRemove([]string{root}, force)
			if err != nil {
				t.Fatalf("root operand %q (force=%v) must be a per-item refusal, not a call error: %v", root, force, err)
			}
			if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "root") || res.Deleted != 0 {
				t.Fatalf("root refusal (%q, force=%v) = %+v", root, force, res)
			}
		}
	}
	if _, err := os.Stat("/"); err != nil {
		t.Fatalf("/ became unreadable after the refused delete: %v", err)
	}
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(`C:\`); err != nil {
			t.Fatalf("C:\\ became unreadable after the refused delete: %v", err)
		}
	}
}

// RemoteRemove runs the same act-time gate through a real (local) engine,
// and RemoteDeletePreview flags the same selection with RequiresL2 so the
// Delete Window knows to escalate.
func TestRemoteRemoveForceGate(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	over := deleteForceThreshold + 5
	root := t.TempDir()
	victim := seedFiles(t, root, over)
	src := profile.Source{Name: "lab", Type: profile.TypeLocal, LocalRoot: root}
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	pv, err := a.RemoteDeletePreview("lab", []string{"/victim/"})
	if err != nil {
		t.Fatalf("RemoteDeletePreview: %v", err)
	}
	if pv.Files != int64(over) || !pv.RequiresL2 {
		t.Fatalf("preview = %+v (want %d files, RequiresL2)", pv, over)
	}

	// Refused without force — the tree stays browsable.
	res, err := a.RemoteRemove("lab", []string{"/victim/"}, false)
	if err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("over-threshold remote remove without force: err = %v", err)
	}
	if res == nil || res.Deleted != 0 {
		t.Fatalf("refused remote remove = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(victim, "f000.txt")); err != nil {
		t.Fatalf("victim file vanished after a refused remote delete: %v", err)
	}

	// Forced: the tree goes.
	res, err = a.RemoteRemove("lab", []string{"/victim/"}, true)
	if err != nil {
		t.Fatalf("forced remote remove: %v", err)
	}
	if res.Deleted != 1 || len(res.Errors) != 0 {
		t.Fatalf("forced remote remove = %+v", res)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("victim tree survived a forced remote remove")
	}
}

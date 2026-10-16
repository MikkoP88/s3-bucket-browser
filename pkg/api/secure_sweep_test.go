package api

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInstanceWorkspaceLayout(t *testing.T) {
	a := newTestApp(t)
	_ = a
	got := instanceWorkspace("edit")
	want := filepath.Join(workspaceBase("edit"), strconv.Itoa(os.Getpid()))
	if got != want {
		t.Fatalf("instanceWorkspace(edit) = %s, want %s", got, want)
	}
}

func TestWipeWorkspacesPreservesLiveSiblings(t *testing.T) {
	a := newTestApp(t)
	// 999999 is odd and so can never be a real Windows PID; the injected
	// pidAlive makes the choice deterministic on every OS.
	const liveSibling = 999999
	prev := pidAlive
	pidAlive = func(pid int) bool { return pid == liveSibling }
	defer func() { pidAlive = prev }()

	edit := workspaceBase("edit")
	live := filepath.Join(edit, strconv.Itoa(liveSibling))
	dead := filepath.Join(edit, "4194302")          // even, far beyond live PIDs, probed dead
	legacy := filepath.Join(edit, "staged-doc.txt") // pre-instance era: flat in the base
	own := instanceWorkspace("edit")
	for _, d := range []string{live, dead, own} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(live, "in-flight.txt"),
		filepath.Join(dead, "crash-leftover.txt"),
		legacy,
		filepath.Join(own, "own-boot.txt"),
	} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	a.wipeWorkspaces()

	if _, err := os.Stat(filepath.Join(live, "in-flight.txt")); err != nil {
		t.Fatal("a live sibling window's staging must survive another window's boot wipe")
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("a dead instance's seat must be swept")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy flat staging must be swept")
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatal("this instance's own boot seat must be swept (it is empty at Startup)")
	}
}

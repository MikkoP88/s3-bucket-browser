//go:build windows && !s3b_headless && !server

package gui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/api"
)

func TestWebviewProfilePlanPrecedence(t *testing.T) {
	// No env at all: the default shared profile, and not multi.
	t.Setenv("S3B_RIG_WEBVIEW_PROFILE", "")
	t.Setenv(api.EnvMultiInstance, "")
	if p, multi := webviewProfilePlan(); p != "" || multi {
		t.Fatalf("default launch: plan = (%q, %v), want shared profile", p, multi)
	}

	// Multi-instance: a per-PID folder under the config dir, multi=true.
	t.Setenv(api.EnvMultiInstance, "1")
	p, multi := webviewProfilePlan()
	if !multi {
		t.Fatal("multi-instance launch must report multi")
	}
	if want := strconv.Itoa(os.Getpid()); filepath.Base(p) != want {
		t.Fatalf("multi-instance profile dir = %s, want base %s", p, want)
	}

	// The rig env outranks everything, including a multi-instance launch
	// (the rig asked for this exact folder — it gets it, and no sweep).
	t.Setenv("S3B_RIG_WEBVIEW_PROFILE", filepath.Join(t.TempDir(), "rig-profile"))
	rig, multi := webviewProfilePlan()
	if multi {
		t.Fatal("a rig-pinned launch must never report multi (no cascade, no sweep)")
	}
	if filepath.Base(rig) != "rig-profile" {
		t.Fatalf("rig profile = %s, want the env's own path", rig)
	}
}

func TestSweepStaleWebviewProfiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)

	const liveSibling = 999999 // odd: cannot be a real Windows PID
	prev := webviewPIDAlive
	webviewPIDAlive = func(pid int) bool { return pid == liveSibling }
	defer func() { webviewPIDAlive = prev }()

	root := filepath.Join(dir, "webview")
	live := filepath.Join(root, strconv.Itoa(liveSibling))
	dead := filepath.Join(root, "4194302")
	own := filepath.Join(root, strconv.Itoa(os.Getpid()))
	foreign := filepath.Join(root, "not-a-pid")
	tempLive := filepath.Join(os.TempDir(), "s3b-webview-"+strconv.Itoa(liveSibling))
	tempDead := filepath.Join(os.TempDir(), "s3b-webview-4194301")
	for _, d := range []string{live, dead, own, foreign, tempLive, tempDead} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	sweepStaleWebviewProfiles()

	if _, err := os.Stat(live); err != nil {
		t.Fatal("a live sibling's config-dir profile must survive the sweep")
	}
	if _, err := os.Stat(tempLive); err != nil {
		t.Fatal("a live sibling's fallback temp seat must survive the sweep")
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("a dead instance's profile must be swept")
	}
	if _, err := os.Stat(tempDead); !os.IsNotExist(err) {
		t.Fatal("a dead instance's fallback temp seat must be swept")
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatal("this process's own leftover seat must be swept")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("a name the plan does not recognise must never be touched")
	}
}

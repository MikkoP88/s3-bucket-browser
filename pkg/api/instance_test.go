package api

import (
	"errors"
	"strings"
	"testing"
)

func TestNewInstanceSpawnsWithOptOut(t *testing.T) {
	a := newTestApp(t)

	// Without a desktop shell there is nothing to twin.
	if err := a.NewInstance(); err == nil {
		t.Fatal("NewInstance without a desktop shell must refuse")
	}

	prevShell := shell
	shell = &DesktopShell{NativeWindows: true}
	prevSpawn := spawnInstance
	var gotExe, gotEnv string
	spawnInstance = func(exe, extraEnv string) error {
		gotExe = exe
		gotEnv = extraEnv
		return nil
	}
	defer func() { shell = prevShell; spawnInstance = prevSpawn }()

	if err := a.NewInstance(); err != nil {
		t.Fatalf("NewInstance with a shell: %v", err)
	}
	if gotExe == "" {
		t.Fatal("spawner never ran")
	}
	if gotEnv != EnvMultiInstance+"=1" {
		t.Fatalf("spawned env = %q, want %s=1", gotEnv, EnvMultiInstance)
	}
}

func TestNewInstanceSurfacesSpawnFailure(t *testing.T) {
	a := newTestApp(t)
	prevShell := shell
	shell = &DesktopShell{NativeWindows: true}
	prevSpawn := spawnInstance
	fail := func(exe, extraEnv string) error { return errors.New("probe: spawn refused") }
	spawnInstance = fail
	defer func() { shell = prevShell; spawnInstance = prevSpawn }()

	if err := a.NewInstance(); err == nil || !strings.Contains(err.Error(), "starting a new window") {
		t.Fatalf("a failed spawn must surface as a new-window error, got %v", err)
	}
}

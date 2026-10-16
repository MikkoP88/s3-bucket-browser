package api

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// EnvMultiInstance opts a launch out of the desktop single-instance guard
// (gui): any non-empty value. One name, three doors — the File → New
// window verb, the `s3b --new-instance` flag, and the env var set by hand.
const EnvMultiInstance = "S3B_MULTI_INSTANCE"

// spawnInstance is the process spawner behind NewInstance, injectable for
// rigs. Production: start the current executable with one extra
// environment pair (the multi-instance opt-out), detached from this
// window's stdio.
var spawnInstance = func(exe, extraEnv string) error {
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), extraEnv)
	return cmd.Start()
}

// NewInstance launches one more window of the app beside this one: a full
// second instance with its own view state, its own WebView profile and
// its own dialog gate, sharing nothing mutable in memory — exactly what
// the container lock (one profile file open in one window) makes safe.
// Refused where no desktop shell exists (server embeds, headless builds);
// the terminal face answers --new-instance instead.
func (a *App) NewInstance() error {
	if !a.IsDesktopShell() {
		return errors.New("new windows need the desktop app — this build runs without a desktop shell")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the app executable: %w", err)
	}
	if err := spawnInstance(exe, EnvMultiInstance+"=1"); err != nil {
		return fmt.Errorf("starting a new window: %w", err)
	}
	a.emitLog(LogInfo, "app", "launched a new app window (multi-instance)")
	return nil
}

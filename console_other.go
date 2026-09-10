//go:build !windows && !s3b_headless

package gui

// detachConsole is a no-op outside Windows (no console subsystem popup there).
func detachConsole() {}

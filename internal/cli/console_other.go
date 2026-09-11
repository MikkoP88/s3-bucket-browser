//go:build !windows

package cli

// attachParentConsole is a Windows-only concern (GUI-subsystem binaries).
func attachParentConsole() {}

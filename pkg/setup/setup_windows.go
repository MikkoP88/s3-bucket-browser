//go:build windows

package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const appPathsKey = `Software\Microsoft\Windows\CurrentVersion\App Paths`

type windowsLocator struct{}

func defaultLocator() Locator { return windowsLocator{} }

func (windowsLocator) ExeName() string { return "s3b.exe" }

func (windowsLocator) UserDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("%LOCALAPPDATA% is not set — cannot locate the user install dir")
	}
	return filepath.Join(base, "s3b"), nil
}

func (windowsLocator) SystemDir() (string, error) {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		return "", errors.New("%ProgramFiles% is not set — cannot locate the system install dir")
	}
	return filepath.Join(base, "s3b"), nil
}

// MaySystem asks the registry, not the token: opening HKLM\Software for
// writing succeeds exactly when the process is elevated (an asInvoker
// manifest disables registry virtualization, so there is no silent
// fallback hive).
func (windowsLocator) MaySystem() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software`, registry.SET_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

func (windowsLocator) SystemRemedy() string {
	return "rerun from an elevated terminal (Run as administrator)"
}

// Both Windows seats are dedicated s3b directories (never a shared bin
// dir), so removing them when they end up empty is always safe.
func (windowsLocator) MayRemoveDir(Scope) bool { return true }

func hiveFor(sc Scope) registry.Key {
	if sc == System {
		return registry.LOCAL_MACHINE
	}
	return registry.CURRENT_USER
}

func (windowsLocator) RegisterApp(exe string, sc Scope) error {
	// App Paths lets Win+R, ShellExecute and the Run dialog find s3b
	// without touching PATH.
	k, _, err := registry.CreateKey(hiveFor(sc), appPathsKey+`\`+filepath.Base(exe), registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("", exe); err != nil {
		return err
	}
	return k.SetStringValue("Path", filepath.Dir(exe))
}

func (windowsLocator) UnregisterApp(exe string, sc Scope) (string, error) {
	sub := appPathsKey + `\` + filepath.Base(exe)
	k, err := registry.OpenKey(hiveFor(sc), sub, registry.QUERY_VALUE)
	if err != nil {
		// Nothing registered (or unreadable): nothing to remove, and a
		// key we cannot read is never deleted blind.
		return "", nil
	}
	cur, _, err := k.GetStringValue("")
	k.Close()
	if err != nil || !strings.EqualFold(cur, exe) {
		// Points at another install (the setup wizard's) — leave it and
		// name it so the report can say so.
		return cur, nil
	}
	return "", registry.DeleteKey(hiveFor(sc), sub)
}

func (windowsLocator) RegisteredApp(sc Scope) string {
	k, err := registry.OpenKey(hiveFor(sc), appPathsKey+`\s3b.exe`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	if err != nil {
		return ""
	}
	return v
}

func (windowsLocator) PATHAdvice(dir string) string {
	return fmt.Sprintf("Win+R and ShellExecute already find s3b (App Paths); to call it from any terminal, add %s to PATH", dir)
}

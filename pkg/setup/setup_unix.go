//go:build !windows

package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

type unixLocator struct{}

func defaultLocator() Locator { return unixLocator{} }

func (unixLocator) ExeName() string { return "s3b" }

func (unixLocator) UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home dir: %w", err)
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// The freedesktop-era convention every distribution honors.
func (unixLocator) SystemDir() (string, error) { return "/usr/local/bin", nil }

func (unixLocator) MaySystem() bool { return os.Geteuid() == 0 }

func (unixLocator) SystemRemedy() string {
	return "rerun with sudo (or install for the current user only: s3b setup install --user)"
}

// ~/.local/bin and /usr/local/bin are shared with every other tool on
// the machine — s3b never removes them.
func (unixLocator) MayRemoveDir(Scope) bool { return false }

// No shell integration on unix: the binary in a bin dir is the whole
// registration, and PATH is the only finder.
func (unixLocator) RegisterApp(string, Scope) error             { return nil }
func (unixLocator) UnregisterApp(string, Scope) (string, error) { return "", nil }
func (unixLocator) RegisteredApp(Scope) string                  { return "" }

func (unixLocator) PATHAdvice(dir string) string {
	return fmt.Sprintf("add it to PATH: export PATH=%q:$PATH", dir)
}

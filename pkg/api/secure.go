// secure.go — the GUI face of Secure Storage (docs/security.md): the
// global toggle + status for Settings → Security, the secure-aware temp
// workspaces (external-editor files, cross-source transfer spool,
// clipboard staging), and the startup wipe that removes crash leftovers.
//
// Under secure storage the workspaces live inside the config dir
// (0700 — protected like profiles.json) instead of the shared system
// temp dir; in the default mode they stay in the system temp dir with
// owner-only permissions.
package api

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/procprobe"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// SecureStatus is what the Settings → Security panel shows.
type SecureStatus struct {
	Enabled          bool   `json:"enabled"`
	KeyringAvailable bool   `json:"keyringAvailable"`
	KeyringBackend   string `json:"keyringBackend"`
	EditorDir        string `json:"editorDir"`
	SpoolDir         string `json:"spoolDir"`
}

// keyringBackendName names the OS secret store for display.
func keyringBackendName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows Credential Manager"
	case "darwin":
		return "macOS Keychain"
	case "linux":
		return "Linux SecretService"
	default:
		return runtime.GOOS
	}
}

// workspaceBase returns the parent directory for a named temp workspace
// ("edit", "tmp", "clip"): the config dir (0700) under secure storage,
// else the system temp dir. Falls back to the system temp dir when the
// config dir cannot be resolved.
func workspaceBase(name string) string {
	if profile.SecureModeOnDisk() {
		if dir, err := profile.DefaultDir(); err == nil {
			return filepath.Join(dir, name)
		}
	}
	return filepath.Join(os.TempDir(), "s3b-"+name)
}

// instanceWorkspace is this app instance's private seat inside a named
// workspace: workspaceBase(name)/<pid>. The live workspaces are
// per-instance, because more than one app window can run at once
// (multi-instance, gui) and each window's editor sessions, transfer
// spool and clipboard staging must be invisible to the others — above all
// to the others' startup sweeps. wipeWorkspaces removes only dead
// instances' seats (see sweepInstanceEntries).
func instanceWorkspace(name string) string {
	return filepath.Join(workspaceBase(name), strconv.Itoa(os.Getpid()))
}

// pidAlive is procprobe.Alive, injectable for rigs.
var pidAlive = procprobe.Alive

// sweepInstanceEntries empties a workspace base for this boot: everything
// goes except sibling seats whose PID still runs — another live window's
// in-flight staging, never ours to touch. Legacy entries (files and dirs
// from the pre-instance era, when staging lived flat in the base) carry
// non-numeric names and are swept like any other leftover.
func sweepInstanceEntries(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return // nothing to sweep (never created, or already gone)
	}
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			if pid > 0 && pid != os.Getpid() && pidAlive(pid) {
				continue // a live sibling window's seat — hands off
			}
		}
		_ = os.RemoveAll(filepath.Join(base, e.Name()))
	}
}

// workspaceTempFile pattern for the transfer spool.
const spoolPattern = "s3b-xfer-*"

// GetSecureStorage reports the current mode for the Settings panel.
func (a *App) GetSecureStorage() SecureStatus {
	return SecureStatus{
		Enabled:          profile.SecureModeOnDisk(),
		KeyringAvailable: profile.KeyringAvailable(),
		KeyringBackend:   keyringBackendName(),
		EditorDir:        workspaceBase("edit"),
		SpoolDir:         workspaceBase("tmp"),
	}
}

// SetSecureStorage enables or disables secure storage globally. Enabling
// encrypts the profile store (requires an OS keyring), turns file
// logging off (the log is plaintext by nature; the previous directory
// and filters are kept so re-enabling restores them) and wipes the old
// workspaces. Disabling decrypts the store and deletes the master key.
func (a *App) SetSecureStorage(on bool) (SecureStatus, error) {
	s, err := profile.Load()
	if err != nil {
		return a.GetSecureStorage(), err // e.g. encrypted store without a keyring — loud by design
	}
	if on != s.SecureEnabled() {
		if on {
			if err := s.EnableSecureStorage(); err != nil {
				return a.GetSecureStorage(), err
			}
			loggingOff := true // the promise the info line makes below
			if ls := eventlog.LoadSettings(); ls.Mode != "off" {
				ls.Mode = "off" // Dir/Levels/Scopes kept for a conscious re-enable
				if err := eventlog.SaveSettings(ls); err != nil {
					loggingOff = false
					a.emitLog(LogError, "security", fmt.Sprintf("secure storage enabled, but turning file logging off failed: %v — turn it off in Settings before trusting this mode", err))
				}
			}
			if loggingOff {
				a.emitLog(LogInfo, "security", "secure storage enabled — profile store encrypted, temp workspaces moved into the config dir, file logging off")
			}
		} else {
			if err := s.DisableSecureStorage(); err != nil {
				return a.GetSecureStorage(), err
			}
			a.emitLog(LogInfo, "security", "secure storage disabled — profile store decrypted, master key removed")
		}
		a.wipeWorkspaces()
	}
	return a.GetSecureStorage(), nil
}

// wipeWorkspaces empties every temp workspace location (both the
// secure-mode config-dir areas and the default system-temp ones), so
// leftovers from a crash never survive the next launch. Called from
// Startup — before any editor session or transfer can exist — and right
// after a mode switch. Since the workspaces went per-instance, the wipe
// is a sweep: this instance's own seat and every dead instance's seat
// goes, while a sibling window that is still running keeps its staging
// (its PID is live — sweepInstanceEntries).
func (a *App) wipeWorkspaces() {
	dirs := []string{workspaceBase("edit"), workspaceBase("tmp"), workspaceBase("clip")}
	// always sweep the other mode's locations too (mode may have changed)
	if cfg, err := profile.DefaultDir(); err == nil {
		dirs = append(dirs, filepath.Join(cfg, "edit"), filepath.Join(cfg, "tmp"), filepath.Join(cfg, "clip"))
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), "s3b-edit"), filepath.Join(os.TempDir(), "s3b-tmp"))
	for _, d := range dirs {
		sweepInstanceEntries(d)
	}
	// legacy clipboard staging dirs (pre-instance era, flat in the system
	// temp) are per-copy; sweep any left behind
	if clips, err := filepath.Glob(filepath.Join(os.TempDir(), "s3b-clip-*")); err == nil {
		for _, c := range clips {
			_ = os.RemoveAll(c)
		}
	}
}

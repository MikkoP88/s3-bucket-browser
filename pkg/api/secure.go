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
	"os"
	"path/filepath"
	"runtime"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
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
			if ls := eventlog.LoadSettings(); ls.Mode != "off" {
				ls.Mode = "off" // Dir/Levels/Scopes kept for a conscious re-enable
				_ = eventlog.SaveSettings(ls)
			}
			a.emitLog(LogInfo, "security", "secure storage enabled — profile store encrypted, temp workspaces moved into the config dir, file logging off")
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

// wipeWorkspaces removes every temp workspace location (both the
// secure-mode config-dir areas and the default system-temp ones), so
// leftovers from a crash never survive the next launch. Called from
// Startup — before any editor session or transfer can exist — and right
// after a mode switch.
func (a *App) wipeWorkspaces() {
	dirs := []string{workspaceBase("edit"), workspaceBase("tmp"), workspaceBase("clip")}
	// always sweep the other mode's locations too (mode may have changed)
	if cfg, err := profile.DefaultDir(); err == nil {
		dirs = append(dirs, filepath.Join(cfg, "edit"), filepath.Join(cfg, "tmp"), filepath.Join(cfg, "clip"))
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), "s3b-edit"), filepath.Join(os.TempDir(), "s3b-tmp"))
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
	// clipboard staging dirs are per-copy; sweep any left behind
	if clips, err := filepath.Glob(filepath.Join(os.TempDir(), "s3b-clip-*")); err == nil {
		for _, c := range clips {
			_ = os.RemoveAll(c)
		}
	}
}

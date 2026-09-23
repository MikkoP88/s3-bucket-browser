// license.go: the first-run license acceptance record (license.json in
// the config dir). Every fresh install walks an accept-license phase
// before the app unlocks: the GUI's setup gate and the `s3b license`
// CLI commands read and write this same store, so one install carries
// one acceptance across both faces. The record is keyed by license name
// and version — a future license bump makes LoadLicense stale and the
// gate runs again. The package stays identity-agnostic: callers pass
// the name/version they compiled in (internal/provenance), so this file
// never duplicates the identity.
package appsettings

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// LicenseAcceptance is the persisted acceptance record. The zero value
// means "not accepted" — a missing or torn file must re-ask, never
// unlock. AcceptedAt is RFC 3339 UTC; AcceptedBy is the OS user that
// clicked or typed the acceptance; Face records which side of the app
// recorded it (gui or cli).
type LicenseAcceptance struct {
	Accepted   bool   `json:"accepted"`
	License    string `json:"license,omitempty"`
	Version    string `json:"version,omitempty"`
	AcceptedAt string `json:"acceptedAt,omitempty"`
	AcceptedBy string `json:"acceptedBy,omitempty"`
	Face       string `json:"face,omitempty"`
}

// Covers reports whether the record is a live acceptance of exactly
// this license name and version.
func (l LicenseAcceptance) Covers(name, version string) bool {
	return l.Accepted && l.License == name && l.Version == version
}

func licensePath() (string, error) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "license.json"), nil
}

// LoadLicense reads the acceptance record; the zero value when the file
// is missing or unreadable. Like Load, it reads per call so tests with
// isolated config dirs see changes immediately.
func LoadLicense() LicenseAcceptance {
	p, err := licensePath()
	if err != nil {
		return LicenseAcceptance{}
	}
	var l LicenseAcceptance
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &l) != nil {
		return LicenseAcceptance{}
	}
	return l
}

// SaveLicense persists the record (0600, like appsettings.json).
func SaveLicense(l LicenseAcceptance) error {
	p, err := licensePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// license.go: the license-gate bindings — the setup phase every fresh
// install walks before the app unlocks. The frontend asks once at boot;
// while Required is true it renders a full-screen gate that only
// AcceptLicense clears. The record lives in the config store
// (license.json) keyed by the license name+version compiled into this
// binary, so a license bump re-runs the phase on the next launch.
package api

import (
	"fmt"
	"os"
	"os/user"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/internal/provenance"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
)

// LicenseState is everything the frontend needs to decide and render
// the gate in one round-trip.
type LicenseState struct {
	Required   bool   `json:"required"` // true until the current license version is accepted
	Accepted   bool   `json:"accepted"`
	Product    string `json:"product"`
	Holder     string `json:"holder"`
	Year       string `json:"year"`
	License    string `json:"license"`
	Version    string `json:"version"`
	URL        string `json:"url"`
	Repo       string `json:"repo"`
	AcceptedAt string `json:"acceptedAt,omitempty"`
	Face       string `json:"face,omitempty"`
}

func licenseState() LicenseState {
	rec := appsettings.LoadLicense()
	return LicenseState{
		Required:   !rec.Covers(provenance.LicenseName, provenance.LicenseVersion),
		Accepted:   rec.Accepted,
		Product:    provenance.Product,
		Holder:     provenance.HolderFull,
		Year:       provenance.Year,
		License:    provenance.LicenseName,
		Version:    provenance.LicenseVersion,
		URL:        provenance.LicenseURL,
		Repo:       "https://" + provenance.Repo,
		AcceptedAt: rec.AcceptedAt,
		Face:       rec.Face,
	}
}

// GetLicenseState reports whether the accept-license phase is due, with
// the identity the gate renders.
func (a *App) GetLicenseState() LicenseState { return licenseState() }

// AcceptLicense records acceptance of the current license (face: "gui"
// or "cli") and returns the state after the write.
func (a *App) AcceptLicense(face string) (LicenseState, error) {
	if face != "cli" {
		face = "gui"
	}
	rec := appsettings.LicenseAcceptance{
		Accepted:   true,
		License:    provenance.LicenseName,
		Version:    provenance.LicenseVersion,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
		AcceptedBy: whoami(),
		Face:       face,
	}
	if err := appsettings.SaveLicense(rec); err != nil {
		return licenseState(), err
	}
	a.emitLog(LogInfo, "license", fmt.Sprintf("license accepted: %s %s (%s)", provenance.LicenseName, provenance.LicenseVersion, face))
	return licenseState(), nil
}

// DeclineLicense revokes any recorded acceptance — the next launch (or
// reload) walks the gate again.
func (a *App) DeclineLicense() (LicenseState, error) {
	if err := appsettings.SaveLicense(appsettings.LicenseAcceptance{}); err != nil {
		return licenseState(), err
	}
	a.emitLog(LogWarn, "license", "license acceptance revoked")
	return licenseState(), nil
}

// whoami names the account that accepted — best effort, never a failure.
func whoami() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if v := os.Getenv("USERNAME"); v != "" {
		return v
	}
	return os.Getenv("USER")
}

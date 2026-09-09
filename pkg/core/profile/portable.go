// portable.go implements portable mode (M5, PLAN.md §8): when a marker
// file named "s3b-portable" sits next to the executable, all state lives
// in a "config" folder beside the binary — nothing is written to the
// user profile directory. Designed for USB-stick / running-from-downloads
// usage.
package profile

import (
	"os"
	"path/filepath"
)

// portableMarker is the file whose presence enables portable mode.
const portableMarker = "s3b-portable"

// portableConfigDir returns the config directory when running portably,
// ok=false otherwise.
func portableConfigDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), portableMarker)); err != nil {
		return "", false
	}
	return filepath.Join(filepath.Dir(exe), "config"), true
}

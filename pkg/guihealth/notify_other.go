//go:build !windows

package guihealth

import (
	"fmt"
	"os"
)

// notifyUser writes the failure to stderr. On Linux and macOS a GUI launch
// routes stderr to the session/journal log, and a terminal launch shows it
// directly — both beat the invisible hang this replaces. A package var so
// tests can capture announcements.
var notifyUser = func(text string) {
	fmt.Fprintln(os.Stderr, text)
}

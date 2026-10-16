// Command s3b is the entry point of the S3 Bucket Browser application.
//
// Design contract:
//   - invoked with no arguments  -> launch the desktop GUI
//   - --new-instance only        -> launch the GUI opted out of the
//                                   single-instance guard (a new window)
//   - invoked with arguments     -> run as the CLI (cobra command tree)
package main

import (
	"fmt"
	"os"

	"github.com/MikkoP88/s3-bucket-browser/internal/cli"
	"github.com/MikkoP88/s3-bucket-browser/pkg/api"
)

// version is injected at build time via -ldflags "-X main.version=...".⁠​‌‌‌​​‌‌​​‌‌​​‌‌​‌‌​​​‌​​​‌​‌‌​‌​‌‌‌​​​​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌‌​​‌‌​​​‌‌​‌‌​​‌​‌​​‌​‌‌​‌​‌‌‌​‌‌​​​‌‌​​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​​​​‌‌​‌‌​‌‌‌‌​‌‌‌​​​​​‌‌‌‌​​‌​‌‌‌​​‌​​‌‌​‌​​‌​‌‌​​‌‌‌​‌‌​‌​​​​‌‌‌​‌​​​​‌​​​​​​​‌​‌​​​​‌‌​​​‌‌​​‌​‌​​‌​​‌​​​​​​​‌‌​​‌​​​‌‌​​​​​​‌‌​​‌​​​‌‌​‌‌​​​‌​​​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​​‌​​​​​​‌​‌​​​​​‌‌​​‌​‌​‌‌‌​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​​‌​​​​​​​‌​‌​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​‌​​​​​‌‌​‌‌‌‌​‌‌​‌‌​​​‌‌‌‌​​‌​‌​​​‌‌​​‌‌​‌‌‌‌​‌‌‌​​‌​​‌‌​‌‌​‌​​‌​​​​​​‌​​‌​​‌​‌‌​‌‌‌​​‌‌‌​‌​​​‌‌​​‌​‌​‌‌‌​​‌​​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌​​​​‌​​​​​​‌​‌​‌​‌​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​‌​​‌‌​​​‌‌​‌​​‌​‌‌​​​‌‌​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​​‌‌​​​‌​​‌​‌‌‌​​​‌‌​​​​​​‌​‌‌‌​​​‌‌​​​​​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌‌​​‌‌‌​‌‌​‌​​‌​‌‌‌​‌​​​‌‌​‌​​​​‌‌‌​‌​‌​‌‌​​​‌​​​‌​‌‌‌​​‌‌​​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌​‌​​‌​‌‌‌‌​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌‌‌‌​‌‌‌​​‌‌​​‌‌​​‌‌​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​‌​‌​‌‌​​​‌‌​‌‌​‌​‌‌​‌‌​​‌​‌​‌‌‌​‌​​​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌‌​‌‌‌​​‌‌​‌‌​​‌​‌​‌‌‌​​‌​⁠
var version = "0.2.0-dev"

func main() {
	cli.Version = version
	args := os.Args[1:]
	// `s3b --new-instance` opens another window beside the running app: the
	// recognized GUI flag routes to the GUI face with the multi-instance
	// opt-out in the environment (one door of three — the File → New window
	// verb and S3B_MULTI_INSTANCE=1 are the others). The headless build's
	// GUI face is the terminal browser, so the flag works there too.
	if len(args) == 0 || guiLaunchFlagsOnly(args) {
		if len(args) > 0 {
			os.Setenv(api.EnvMultiInstance, "1")
		}
		if err := runGUI(version); err != nil {
			fmt.Fprintf(os.Stderr, "gui: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(cli.Execute(args))
}

// guiLaunchFlagsOnly reports whether every argument is a recognized GUI
// launch flag (--new-instance today): such an invocation is a GUI launch;
// anything else belongs to the CLI's own parser.
func guiLaunchFlagsOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, a := range args {
		if a != "--new-instance" {
			return false
		}
	}
	return true
}

// Command s3b is the entry point of the S3 Bucket Browser application.
//
// Design contract (see PLAN.md §7):
//   - invoked with no arguments  -> launch the desktop GUI
//   - invoked with arguments     -> run as the CLI (cobra command tree)
package main

import (
	"fmt"
	"os"

	"github.com/MikkoP88/s3-bucket-browser/internal/cli"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "0.2.0-dev"

func main() {
	cli.Version = version
	if len(os.Args) <= 1 {
		if err := runGUI(version); err != nil {
			fmt.Fprintf(os.Stderr, "gui: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(cli.Execute(os.Args[1:]))
}

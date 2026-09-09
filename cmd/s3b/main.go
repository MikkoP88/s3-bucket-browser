// Command s3b is the entry point of the S3 Bucket Browser application.
//
// Design contract (see PLAN.md §7):
//   - invoked with no arguments  -> launch the desktop GUI
//   - invoked with arguments     -> run as the CLI (cobra command tree)
//
// During milestone M0 this placeholder only reports version and plan status;
// M1 introduces pkg/core and the first CLI commands.
package main

import (
	"fmt"
	"os"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const planPointer = "Product plan: https://github.com/MikkoP88/s3-bucket-browser/blob/main/PLAN.md"

func main() {
	fmt.Printf("s3-bucket-browser %s (M0: planning)\n%s\n", version, planPointer)
	os.Exit(0)
}

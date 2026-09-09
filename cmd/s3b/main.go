// Command s3b is the entry point of the S3 Bucket Browser application.
//
// Design contract (see PLAN.md §7):
//   - invoked with no arguments  -> launch the desktop GUI (M2)
//   - invoked with arguments     -> run as the CLI (cobra command tree)
package main

import (
	"fmt"
	"os"

	"github.com/MikkoP88/s3-bucket-browser/internal/cli"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	cli.Version = version
	if len(os.Args) <= 1 {
		// M2 will launch the desktop GUI here.
		fmt.Printf("s3b %s — the desktop GUI ships in milestone M2.\n", version)
		fmt.Println()
		fmt.Println("Use the CLI face meanwhile:")
		fmt.Println("  s3b --help                 command overview")
		fmt.Println("  s3b profile add ...        connect to S3 / MinIO / any S3 provider")
		fmt.Println("  s3b ls                     list your buckets")
		fmt.Println("  s3b doctor                 diagnose connectivity")
		os.Exit(0)
	}
	os.Exit(cli.Execute(os.Args[1:]))
}

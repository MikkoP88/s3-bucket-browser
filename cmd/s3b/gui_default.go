//go:build !s3b_headless

package main

import (
	gui "github.com/MikkoP88/s3-bucket-browser" // module-root package: embeds the frontend
)

// runGUI launches the desktop GUI (default build).
func runGUI(version string) error { return gui.Run(version) }

//go:build s3b_headless

package main

import "fmt"

// runGUI explains that this binary was built CLI-only. Headless Linux
// servers build with -tags s3b_headless to avoid GTK/WebKit dependencies.
func runGUI(version string) error {
	return fmt.Errorf("this build is CLI-only (built with -tags s3b_headless); the GUI is not included")
}

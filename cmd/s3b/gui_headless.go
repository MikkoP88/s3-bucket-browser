//go:build s3b_headless

package main

import (
	"fmt"
	"os"

	"github.com/MikkoP88/s3-bucket-browser/internal/cli"
	"github.com/MikkoP88/s3-bucket-browser/internal/tui"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// runGUI launches the interactive terminal face. Headless Linux servers
// build with -tags s3b_headless to avoid GTK/WebKit dependencies — and
// the no-argument seat still gets a full browser: the TUI, which needs
// nothing beyond a terminal.
func runGUI(version string) error {
	store, err := profile.Load()
	if err != nil {
		return fmt.Errorf("loading sources: %w", err)
	}
	cli.Version = version
	app, err := tui.NewApp()
	if err != nil {
		return err
	}
	browser := tui.NewBrowser(app, store, version)
	app.SetRoot(browser)
	err = app.Run()
	browser.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
	return nil
}

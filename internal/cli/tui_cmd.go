package cli

import (
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/internal/tui"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/spf13/cobra"
)

// tuiCmd launches the interactive terminal face: the same source tree
// and pkg/core engine the GUI rides, drawn over plain ANSI — no
// WebView2, no extra dependencies, in every build that carries the
// CLI. Keyboard and mouse both drive it, and it stays drivable over
// pipes (S3B_TUI_SIZE / S3B_TUI_FRAMES), which is how the battery
// walks it black-box.
func tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI (keyboard + mouse browser)",
		Long: "Launch the interactive terminal face: a source rail beside the current listing,\n" +
			"rename, delete, new folder, upload and download with live progress, properties,\n" +
			"a '/' filter and a ':' command bar — the GUI's grammar in the terminal.\n" +
			"Mouse and keyboard both drive it; works over SSH and in any modern terminal.\n" +
			"Press F1 inside for the key map.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := profile.Load()
			if err != nil {
				return opErr(fmt.Errorf("loading sources: %w", err))
			}
			app, err := tui.NewApp()
			if err != nil {
				return opErr(err)
			}
			browser := tui.NewBrowser(app, store, Version)
			app.SetRoot(browser)
			err = app.Run()
			browser.Close()
			if err != nil {
				return opErr(err)
			}
			return nil
		},
	}
}

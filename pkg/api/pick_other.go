//go:build !windows

package api

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// pickUploadItems on non-Windows: the Wails multi-file dialog (folders are
// still reachable via drag & drop and the folder picker elsewhere).
func pickUploadItems(a *App) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Select files to upload",
		ShowHiddenFiles:      true,
		CanChooseFiles:       true,
		CanChooseDirectories: false,
	})
}

func stageClipboardDir() (string, error) {
	return os.MkdirTemp("", "s3b-clip-")
}

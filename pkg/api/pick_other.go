//go:build !windows

package api

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// pickUploadItems on non-Windows: the Wails multi-file dialog (folders are
// still reachable via drag & drop and the folder picker elsewhere — the
// Wails runtime dialog cannot mix files and folders in one selection).
func pickUploadItems(a *App) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:           "Select files to upload",
		ShowHiddenFiles: true,
	})
}

func stageClipboardDir() (string, error) {
	return os.MkdirTemp("", "s3b-clip-")
}

package api

import (
	"os"
)

// pickerSeams, when set, answer the upload-files and folder pickers in
// place of the native dialogs — same rationale as SetProfileDialogs: there
// is no Wails runtime outside wails.Run, and a runtime call there is fatal.
// tools/gui-live rejects the pickers this way so the frontend falls back to
// its path prompt. Must be set before Startup; the desktop app never sets it.
var pickerSeams struct {
	items  func() ([]string, error)
	folder func(title string) (string, error)
}

// SetPickers scripts the upload-files and folder pickers (nil members
// restore the native dialogs). Only tools/gui-live sets this.
func SetPickers(items func() ([]string, error), folder func(title string) (string, error)) {
	pickerSeams.items = items
	pickerSeams.folder = folder
}

// PickUploadFiles opens the native multi-select FILE dialog — the proven
// v1.0.0 picker, restored after the beta.3 experiment with a raw
// files-and-folders COM dialog: on Win10/11 FOS_PICKFOLDERS greys the file
// items out, leaving directories as the only selectable rows. Folders
// upload through PickFolder, drag & drop and paste instead; every backend
// path walks directories wherever a folder path lands.
func (a *App) PickUploadFiles() ([]string, error) {
	if pickerSeams.items != nil {
		return pickerSeams.items()
	}
	if shell == nil {
		return nil, errNoShell
	}
	return shell.OpenFiles(ShellDialog{
		Title:      "Choose files to upload",
		ShowHidden: true,
	})
}

// StageClipboardDir creates a fresh staging directory used to materialize
// an app selection before it is placed on the OS clipboard (remote/S3 rows
// are not files until downloaded). Under secure storage the staging area
// lives inside the config dir (0700) instead of the shared system temp.
func (a *App) StageClipboardDir() (string, error) {
	base := workspaceBase("clip")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, "s3b-clip-")
}

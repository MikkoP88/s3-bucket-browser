package api

// pickerSeams, when set, answer the upload-items and folder pickers in
// place of the native dialogs — same rationale as SetProfileDialogs: there
// is no Wails runtime outside wails.Run, and a runtime call there is fatal.
// tools/gui-live rejects the pickers this way so the frontend falls back to
// its path prompt. Must be set before Startup; the desktop app never sets it.
var pickerSeams struct {
	items  func() ([]string, error)
	folder func(title string) (string, error)
}

// SetPickers scripts the upload-items and folder pickers (nil members
// restore the native dialogs). Only tools/gui-live sets this.
func SetPickers(items func() ([]string, error), folder func(title string) (string, error)) {
	pickerSeams.items = items
	pickerSeams.folder = folder
}

// PickUploadItems opens ONE native picker that selects files AND folders
// together (multi-select) — the single Upload command. The backend walks
// directories either way, so the dialog returns a mixed path list.
func (a *App) PickUploadItems() ([]string, error) {
	if pickerSeams.items != nil {
		return pickerSeams.items()
	}
	return pickUploadItems(a)
}

// StageClipboardDir creates a fresh staging directory (temp) used to
// materialize an app selection before it is placed on the OS clipboard
// (remote/S3 rows are not files until downloaded).
func (a *App) StageClipboardDir() (string, error) {
	return stageClipboardDir()
}

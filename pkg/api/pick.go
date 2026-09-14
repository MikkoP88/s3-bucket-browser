package api

// PickUploadItems opens ONE native picker that selects files AND folders
// together (multi-select) — the single Upload command. The backend walks
// directories either way, so the dialog returns a mixed path list.
func (a *App) PickUploadItems() ([]string, error) {
	return pickUploadItems(a)
}

// StageClipboardDir creates a fresh staging directory (temp) used to
// materialize an app selection before it is placed on the OS clipboard
// (remote/S3 rows are not files until downloaded).
func (a *App) StageClipboardDir() (string, error) {
	return stageClipboardDir()
}

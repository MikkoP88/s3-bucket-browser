package api

// OS clipboard bridge (Explorer ⇄ app). The Windows implementation speaks
// CF_HDROP natively (drag-drop file list); other platforms report "no files".

// OsClipboardFiles returns the file paths currently on the OS clipboard
// (Ctrl+C in Explorer), or an empty list when it holds no file payload.
func (a *App) OsClipboardFiles() []string {
	return osClipboardFiles()
}

// OsClipboardSetFiles puts local file paths on the OS clipboard so Ctrl+V
// works in Explorer and every other app. Paths must exist.
func (a *App) OsClipboardSetFiles(paths []string) error {
	return osClipboardSetFiles(paths)
}

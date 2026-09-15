package api

import "github.com/wailsapp/wails/v2/pkg/runtime"

// OS clipboard bridge (Explorer ⇄ app). The Windows implementation speaks
// CF_HDROP natively (drag-drop file list); other platforms report "no files".

// clipboardTextSeam, when set, receives copy-as-text writes in place of the
// OS clipboard — same rationale as pickerSeams: there is no Wails runtime
// outside wails.Run, and a runtime call there is fatal. tools/gui-live
// records the text so the live walk can assert what a "Copy path" click put
// on the clipboard.
var clipboardTextSeam func(text string)

// SetClipboardTextSink scripts ClipboardSetText (nil restores the OS
// clipboard). Only tools/gui-live sets this.
func SetClipboardTextSink(fn func(string)) { clipboardTextSeam = fn }

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

// ClipboardSetText puts plain text on the OS clipboard — the "Copy name /
// Copy path / Copy URI" actions (Ctrl+C keeps mirroring the selection as
// real OS file objects for Explorer/Finder interop).
func (a *App) ClipboardSetText(text string) error {
	if clipboardTextSeam != nil {
		clipboardTextSeam(text)
		return nil
	}
	return runtime.ClipboardSetText(a.ctx, text)
}

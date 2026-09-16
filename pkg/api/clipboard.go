package api

import (
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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

// presignScrubDelay is how long a pre-signed URL may stay on the OS
// clipboard under secure storage before it is scrubbed (docs/security.md).
const presignScrubDelay = 60 * time.Second

// scrubPending holds a pre-signed URL whose scrub has not succeeded yet;
// Shutdown retries it once (best-effort — some desktops refuse clipboard
// writes from background timers).
var (
	scrubMu      sync.Mutex
	scrubPending string
)

// isPresignedURL reports whether text carries a query-string request
// signature: AWS SigV4 presigns (X-Amz-Signature, all S3-compatible
// providers) or the legacy v2 Signature parameter.
func isPresignedURL(text string) bool {
	return strings.Contains(text, "X-Amz-Signature=") || strings.Contains(text, "&Signature=")
}

// ClipboardSetText puts plain text on the OS clipboard — the "Copy name /
// Copy path / Copy URI" actions (Ctrl+C keeps mirroring the selection as
// real OS file objects for Explorer/Finder interop). Under secure storage
// a pre-signed URL is a bearer credential, so it is auto-cleared
// presignScrubDelay later.
func (a *App) ClipboardSetText(text string) error {
	if clipboardTextSeam != nil {
		clipboardTextSeam(text)
	} else if err := runtime.ClipboardSetText(a.ctx, text); err != nil {
		return err
	}
	if profile.SecureModeOnDisk() && isPresignedURL(text) {
		a.armPresignScrub(text)
	}
	return nil
}

// armPresignScrub clears the URL from the clipboard after the delay —
// but only if it is still what the clipboard holds (a newer copy wins).
func (a *App) armPresignScrub(url string) {
	scrubMu.Lock()
	scrubPending = url
	scrubMu.Unlock()
	go func() {
		t := time.NewTimer(presignScrubDelay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-a.done(): // app quit first — Shutdown retries once
			return
		}
		a.scrubClipboard(url)
	}()
}

// scrubClipboard overwrites the URL when it is still on the clipboard.
func (a *App) scrubClipboard(url string) {
	if a.ctx == nil {
		return
	}
	if cur, err := runtime.ClipboardGetText(a.ctx); err == nil && cur != url {
		a.clearScrub(url) // user copied something else meanwhile — leave it
		return
	}
	if err := runtime.ClipboardSetText(a.ctx, ""); err != nil {
		return // keep pending; Shutdown retries
	}
	a.clearScrub(url)
}

// clearScrub drops a satisfied scrub marker.
func (a *App) clearScrub(url string) {
	scrubMu.Lock()
	if scrubPending == url {
		scrubPending = ""
	}
	scrubMu.Unlock()
}

// retryPresignScrub makes one final attempt to clear a pre-signed URL the
// delayed scrub could not remove (called from Shutdown).
func (a *App) retryPresignScrub() {
	scrubMu.Lock()
	url := scrubPending
	scrubMu.Unlock()
	if url != "" {
		a.scrubClipboard(url)
	}
}

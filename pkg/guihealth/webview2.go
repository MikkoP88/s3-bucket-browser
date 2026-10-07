// webview2.go: can this machine host the app's webview at all? Windows
// Server SKUs (and stripped-down Windows installs) ship without the Edge
// WebView2 Evergreen runtime that client SKUs carry, so a first launch
// there dies fast inside webview creation — before any window exists,
// with the error printed to a stderr a -H windowsgui build does not have:
// the user sees nothing happen at all. EnsureWebView2 asks the loader's
// own registry question up front and turns absence into the loud,
// actionable failure that path deserves instead of the silent one.
package guihealth

import (
	"errors"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// ErrWebView2Missing is returned — after telling the user loudly — when
// no usable WebView2 runtime is installed. The caller refuses to start
// rather than dying invisibly inside webview creation.
var ErrWebView2Missing = errors.New("webview2: no usable WebView2 runtime installed")

// probeWebView2Runtime reports the installed Evergreen runtime's version
// string and whether the answer is trustworthy. known=false means "could
// not ask" (platform without a probe, or an unreadable registry) and MUST
// read as present — a broken probe may never block a launch. The neutral
// default serves every non-Windows platform and the tests; the Windows
// implementation in webview2_windows.go replaces it.
var probeWebView2Runtime = func() (pv string, known bool) { return "", false }

// webview2VersionUsable interprets a registry pv value: EdgeUpdate writes
// "" or "0.0.0.0" while an install is mid-flight or broken, and the
// WebView2 loader treats those as not-installed — so must the probe.
func webview2VersionUsable(pv string) bool {
	return pv != "" && pv != "0.0.0.0"
}

// WebView2MissingMessage is the remediation text for a machine with no
// usable runtime: what is missing, why (Server SKUs), and both install
// shapes — winget and the standalone download.
func WebView2MissingMessage() string {
	return "The app cannot start because the Microsoft Edge WebView2 Runtime is not " +
		"installed. Windows Server and minimal Windows installs do not include it by default. " +
		"Install the runtime, then start the app again:\n\n" +
		"  winget install Microsoft.EdgeWebView2Runtime\n\n" +
		"or download it from https://developer.microsoft.com/microsoft-edge/webview2/"
}

// EnsureWebView2 checks that a usable Evergreen runtime exists before the
// app tries to create a webview. Absence is announced through the event
// log and the platform-native channel (a message box on Windows) — this
// is a launch that used to fail with NO feedback whatsoever, so silence
// is the bug being fixed — and then returned as an error so the caller
// can stop cleanly instead of dying somewhere inside webview creation.
func EnsureWebView2() error {
	pv, known := probeWebView2Runtime()
	if !known || webview2VersionUsable(pv) {
		return nil
	}
	eventlog.Append("error", "app", "",
		"startup refused: no usable WebView2 runtime installed (registry pv=\""+pv+"\") — "+
			"install the Edge WebView2 Evergreen runtime (winget install Microsoft.EdgeWebView2Runtime)")
	notifyUser(WebView2MissingMessage())
	return ErrWebView2Missing
}

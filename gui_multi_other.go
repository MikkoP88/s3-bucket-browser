//go:build !windows || s3b_headless || server

package gui

// webviewProfilePlan on builds without the Windows desktop webview: no
// per-instance profile. The WebView2 user-data option is Windows-only;
// server and headless builds have no webview at all, and macOS/Linux
// desktops seat their windows through their own windowing systems.
func webviewProfilePlan() (path string, multi bool) { return "", false }

// sweepStaleWebviewProfiles — nothing seated, nothing to sweep.
func sweepStaleWebviewProfiles() {}

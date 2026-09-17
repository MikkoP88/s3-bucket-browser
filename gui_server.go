//go:build server

package gui

// serverBuild is true for Wails server builds (-tags server): the app runs
// as a plain HTTP server with no windows, so there is no window whose
// appearance could mark a healthy start, and no webview to sweep.
const serverBuild = true

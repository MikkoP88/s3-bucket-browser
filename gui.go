//go:build !s3b_headless

// Package gui wires the Wails webview shell around the pkg/api service and
// the embedded frontend (PLAN.md §7). It lives at the module root because
// go:embed can only reference files below the embedding package.
//
// Build tag: plain builds include the GUI everywhere. On headless Linux
// servers build with -tags s3b_headless to get a pure-Go CLI without GTK
// dependencies (see cmd/s3b/gui_headless.go).
package gui

import (
	"embed"
	"io/fs"

	"github.com/MikkoP88/s3-bucket-browser/pkg/api"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var frontendFS embed.FS

// Run starts the desktop GUI and blocks until the window closes.
func Run(version string) error {
	app := api.New(version)
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return err
	}
	return wails.Run(&options.App{
		Title:       "S3 Bucket Browser",
		Width:       1280,
		Height:      800,
		MinWidth:    960,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   app.Startup,
		OnShutdown:  app.Shutdown,
		Bind:        []interface{}{app},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true, // native OS file drops -> wails:file-drop
			DisableWebViewDrop: true, // don't let the webview "open" dropped files
		},
	})
}

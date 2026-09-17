//go:build !s3b_headless

// Package gui wires the Wails v3 webview shell around the pkg/api service and
// the embedded frontend. It lives at the module root because the embed
// directive can only reference files below the embedding package.
//
// Build tag: plain builds include the GUI everywhere. On headless Linux
// servers build with -tags s3b_headless to get a pure-Go CLI without GTK
// dependencies (see cmd/s3b/gui_headless.go).
package gui

import (
	"context"
	"embed"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/api"
	"github.com/MikkoP88/s3-bucket-browser/pkg/guihealth"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend
var frontendFS embed.FS

// popoutPrefix namespaces the native popout window names ("popout:<id>") so
// they can never collide with the main window.
const popoutPrefix = "popout:"

// lifecycle adapts the app's Startup to Wails v3's service lifecycle: Run
// starts every service before it creates the first window, and Startup must
// have run before the frontend can call anything. ServiceStartup and friends
// are excluded from the generated bindings, so this service binds nothing.
type lifecycle struct {
	app *api.App
}

// ServiceName labels the service in diagnostics.
func (l *lifecycle) ServiceName() string { return "s3b-lifecycle" }

// ServiceStartup hands the application context to the api service.
func (l *lifecycle) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	l.app.Startup(ctx)
	return nil
}

// Run starts the desktop GUI and blocks until the app quits.
func Run(version string) error {
	detachConsole()
	// The embedded WebView2 bindings log one unconditional line at startup
	// ("[WebView2] Environment created successfully", go-webview2
	// chromium.go) straight to the std logger; a GUI has no console to
	// earn it — drop the whole default logger. Application events use the
	// separate eventlog package and are unaffected.
	log.SetOutput(io.Discard)
	// Clear anything an earlier session left wedged (Windows: orphaned
	// WebView2 trees holding the user-data folder), then guarantee that a
	// start which never produces a window fails loudly — message box plus
	// event-log entry — instead of hanging invisibly forever. Server builds
	// (-tags server) have no window and no webview at all, so both defenses
	// are desktop-only.
	if !serverBuild {
		guihealth.Preflight()
		guihealth.ArmStartupWatchdog()
		defer guihealth.DisarmStartupWatchdog()
	}
	app := api.New(version)
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return err
	}
	app3 := application.New(application.Options{
		Name:   "S3 Bucket Browser",
		Assets: application.AssetOptions{Handler: http.FileServer(http.FS(assets))},
		Services: []application.Service{
			// *api.App binds every exported method; the lifecycle service
			// runs app.Startup before the first window exists.
			application.NewService(app),
			application.NewService(&lifecycle{app: app}),
		},
		OnShutdown: func() { app.Shutdown(context.Background()) },
	})
	mainWindow := app3.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "main",
		Title:          "S3 Bucket Browser",
		Width:          1280,
		Height:         800,
		MinWidth:       960,
		MinHeight:      600,
		EnableFileDrop: true,
	})
	// The X button asks before losing work (running transfers, unsaved
	// profile) — the frontend confirms through exit:confirm/ConfirmExit.
	// RegisterHook runs synchronously inside the window event dispatch:
	// Cancel() stops the event before the framework's own WindowClosing
	// listener (installed at window creation) destroys the window.
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.ShouldClose() {
			e.Cancel()
		}
	})
	// v3 keeps the process alive while any window exists, so with popouts
	// open an allowed main-window close must still end the app (without
	// popouts Windows posts WM_QUIT itself on the last close). Quit() must
	// not run inside the main-thread event dispatch (it waits on the very
	// loop it would run in), hence the goroutine hop.
	mainWindow.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if e.IsCancelled() {
			return
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			app3.Quit()
		}()
	})
	// Native OS file drops -> wails:file-drop. The webview's own DOM drop
	// stays enabled: the frontend drop-in chain preventDefaults every
	// external drag, so the webview never navigates to the dropped file.
	// Three payloads (x, y, files) — the bridge spreads them into the
	// frontend's handleOSDrop(x, y, paths).
	mainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		// A native drag-out may cross back over the app window: dropping it
		// here must not re-import the staged files as an upload — internal
		// routing arrives separately through the drag:self-drop event.
		if app.DragOutActive() {
			return
		}
		ec := e.Context()
		if ec == nil {
			return
		}
		x, y := 0, 0
		if det := ec.DropTargetDetails(); det != nil {
			x, y = det.X, det.Y
		}
		files := ec.DroppedFiles()
		if len(files) == 0 {
			return
		}
		app3.Event.Emit("wails:file-drop", x, y, files)
	})
	mainWindow.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		guihealth.MarkWindowUp() // window + webview exist — start succeeded
	})
	installShell(app3, app)
	return app3.Run()
}

// toFilters converts shell dialog filters to Wails file filters.
func toFilters(filters []api.ShellFilter) []application.FileFilter {
	if len(filters) == 0 {
		return nil
	}
	out := make([]application.FileFilter, len(filters))
	for i, f := range filters {
		out[i] = application.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern}
	}
	return out
}

// installShell bridges the framework-free pkg/api shell onto the v3
// application managers: events, clipboard, dialogs, and the window list
// (native popout windows).
func installShell(app3 *application.App, a *api.App) {
	api.InstallDesktopShell(&api.DesktopShell{
		// Server builds (-tags server) have no OS windows: popouts must
		// float in-page, which IsDesktopShell tells the frontend.
		NativeWindows: !serverBuild,
		Emit: func(event string, data ...any) {
			app3.Event.Emit(event, data...)
		},
		ClipGetText: func() (string, error) {
			text, ok := app3.Clipboard.Text()
			if !ok {
				return "", errors.New("clipboard: no text available")
			}
			return text, nil
		},
		ClipSetText: func(text string) error {
			if !app3.Clipboard.SetText(text) {
				return errors.New("clipboard: write failed")
			}
			return nil
		},
		OpenFiles: func(d api.ShellDialog) ([]string, error) {
			return app3.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
				CanChooseFiles:  true,
				ShowHiddenFiles: d.ShowHidden,
				Title:           d.Title,
				Filters:         toFilters(d.Filters),
			}).PromptForMultipleSelection()
		},
		OpenFile: func(d api.ShellDialog) (string, error) {
			return app3.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
				CanChooseFiles:  true,
				ShowHiddenFiles: d.ShowHidden,
				Title:           d.Title,
				Filters:         toFilters(d.Filters),
			}).PromptForSingleSelection()
		},
		SaveFile: func(d api.ShellDialog) (string, error) {
			return app3.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
				Title:    d.Title,
				Filename: d.DefaultName,
				Filters:  toFilters(d.Filters),
			}).PromptForSingleSelection()
		},
		OpenDir: func(title string) (string, error) {
			return app3.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
				CanChooseDirectories: true,
				CanChooseFiles:       false,
				Title:                title,
			}).PromptForSingleSelection()
		},
		Quit: app3.Quit,
		OpenPopout: func(id, title, query string, geo api.ShellGeometry) bool {
			if w, ok := app3.Window.GetByName(popoutPrefix + id); ok {
				w.Focus()
				return false
			}
			opts := application.WebviewWindowOptions{
				Name:      popoutPrefix + id,
				Title:     title,
				URL:       "/?" + query,
				MinWidth:  360,
				MinHeight: 220,
			}
			if geo.W > 0 {
				opts.Width = geo.W
			}
			if geo.H > 0 {
				opts.Height = geo.H
			}
			// Every popout opens centered on the app's main window —
			// never the OS cascade — while a remembered size still
			// shapes the window. A popout larger than the main window
			// just spills around the shared center point.
			if main, ok := app3.Window.GetByName("main"); ok {
				w, h := opts.Width, opts.Height
				if w <= 0 {
					w = 640
				}
				if h <= 0 {
					h = 520
				}
				mx, my := main.Position()
				mw, mh := main.Size()
				opts.X, opts.Y = mx+(mw-w)/2, my+(mh-h)/2
				opts.InitialPosition = application.WindowXY
			}
			w := app3.Window.NewWithOptions(opts)
			// Tell the main window when a popout closes so its bookkeeping
			// (per-id singletons, onClose callbacks) stays correct. Runs
			// for user closes and ClosePopout alike.
			w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
				app3.Event.Emit(api.EventPopoutClosed, map[string]string{"id": id})
			})
			return true
		},
		ClosePopout: func(id string) {
			if w, ok := app3.Window.GetByName(popoutPrefix + id); ok {
				w.Close()
			}
		},
		FocusPopout: func(id string) {
			if w, ok := app3.Window.GetByName(popoutPrefix + id); ok {
				w.Focus()
			}
		},
		// Screen point → main window CSS coordinates for the native
		// drag-out: detects a gesture released over the app itself and
		// gives the frontend drop coordinates in the same space
		// wails:file-drop uses. Server builds have no main window — the
		// lookup just fails and "over self" stays false.
		ScreenToClient: func(sx, sy int) (int, int, bool) {
			w, ok := app3.Window.GetByName("main")
			if !ok {
				return 0, 0, false
			}
			h := w.NativeWindow()
			if h == nil {
				return 0, 0, false
			}
			return clientPoint(uintptr(h), sx, sy)
		},
	})
}

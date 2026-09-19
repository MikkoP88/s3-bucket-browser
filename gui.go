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
	"os"
	"sync"
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

// rigBrowserArgs opens the app's WebView2 CDP port when the live drag-out
// rig asks for it (S3B_RIG_CDP_PORT) — nil in every other run.
func rigBrowserArgs() []string {
	if p := os.Getenv("S3B_RIG_CDP_PORT"); p != "" {
		return []string{"--remote-debugging-port=" + p}
	}
	return nil
}

// popoutRect is a native popout's last on-screen rectangle. popoutGeoms
// remembers it per id for the session only: a reopen lands where the user
// left the window (position AND size), and closing the app forgets
// everything — the memory lives in the process, never on disk.
type popoutRect struct{ x, y, w, h int }

var (
	popoutMu    sync.Mutex
	popoutGeoms = map[string]popoutRect{}
)

// rememberPopout snapshots a popout window's rect on its way out; a
// closing window still answers Position/Size.
func rememberPopout(id string, w *application.WebviewWindow) {
	x, y := w.Position()
	ww, hh := w.Size()
	popoutMu.Lock()
	popoutGeoms[id] = popoutRect{x, y, ww, hh}
	popoutMu.Unlock()
}

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
		// Live-test rig hooks (scripts/drag-live.mjs): Wails' Go WebView2
		// loader ignores the WEBVIEW2_* env vars, so CDP and profile
		// isolation must ride the real options — set only when the rig
		// exports these vars, never in normal use.
		Windows: application.WindowsOptions{
			AdditionalBrowserArgs: rigBrowserArgs(),
			WebviewUserDataPath:   os.Getenv("S3B_RIG_WEBVIEW_PROFILE"),
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
			// Per-window resize bounds when the caller passes them (the
			// auto-height windows pin the Windows file-transfer footprint).
			if geo.MinW > 0 {
				opts.MinWidth = geo.MinW
			}
			if geo.MinH > 0 {
				opts.MinHeight = geo.MinH
			}
			if geo.MaxH > 0 {
				opts.MaxHeight = geo.MaxH
			}
			// The auto-height windows (MinH > 0 marks them) are
			// app-driven: the content fit owns the height
			// (ResizePopout) and the width is the fixed profile
			// footprint — the user never resizes them, so the OS border
			// is disabled outright (programmatic SetSize still works).
			if geo.MinH > 0 {
				opts.DisableResize = true
			}
			if geo.W > 0 {
				opts.Width = geo.W
			}
			if geo.H > 0 {
				opts.Height = geo.H
			}
			// A rect remembered this session outranks everything: the
			// window reopens exactly where the user left it, at the size
			// they left it (session memory — see popoutGeoms) — except an
			// auto-height window (MinH > 0 marks it), which reopens at its
			// profile footprint: the height tracks the content again and
			// the width is fixed, so no remembered size may bleed in —
			// only the placement is honored.
			autoH := geo.MinH > 0
			popoutMu.Lock()
			last, hadLast := popoutGeoms[id]
			popoutMu.Unlock()
			if hadLast {
				opts.X, opts.Y = last.x, last.y
				opts.InitialPosition = application.WindowXY
				if !autoH {
					opts.Width = last.w
					opts.Height = last.h
				}
			}
			// Placement — never the OS cascade. The default ("display")
			// centers the popout on the display carrying the app's main
			// window (multi-monitor aware: the display's work area keeps
			// the taskbar/dock out of the math, and the popout follows
			// the app onto whatever display it lives on). "app" keeps
			// the original behavior — centered on the main window's own
			// rect. A screen lookup that fails falls back to the
			// app-window rect; without a main window at all the OS
			// places the window. A remembered size still shapes the
			// window either way, and a popout larger than its anchor
			// just spills around the shared center point.
			if main, ok := app3.Window.GetByName("main"); ok && !hadLast {
				w, h := opts.Width, opts.Height
				if w <= 0 {
					w = 640
				}
				if h <= 0 {
					h = 520
				}
				placed := false
				if geo.Center != "app" {
					if scr, err := main.GetScreen(); err == nil && scr != nil {
						wa := scr.WorkArea
						opts.X = wa.X + (wa.Width-w)/2
						opts.Y = wa.Y + (wa.Height-h)/2
						placed = true
					}
				}
				if !placed {
					mx, my := main.Position()
					mw, mh := main.Size()
					opts.X, opts.Y = mx+(mw-w)/2, my+(mh-h)/2
				}
				opts.InitialPosition = application.WindowXY
			}
			w := app3.Window.NewWithOptions(opts)
			// Tell the main window when a popout closes so its bookkeeping
			// (per-id singletons, onClose callbacks) stays correct. Runs
			// for user closes and ClosePopout alike — and snapshots the
			// rect so the next open lands where this one was left.
			w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
				rememberPopout(id, w)
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
		// The auto-height windows' content fit: the in-window controller
		// measures its content and drives this as jobs come and go.
		// w <= 0 means "keep the current width" — the auto-height windows
		// never touch their width, and re-reporting it from the webview
		// (window.outerWidth) would drift it through the DIP<->physical
		// rounding on every fit.
		ResizePopout: func(id string, w, h int) {
			if win, ok := app3.Window.GetByName(popoutPrefix + id); ok {
				if w <= 0 {
					w, _ = win.Size()
				}
				win.SetSize(w, h)
			}
		},
		// The frontend's verification hook: a window id it believes open
		// must answer true, or it falls back to the DOM popout and heals
		// its flags (a window can die without the closing event).
		PopoutOpen: func(id string) bool {
			w, ok := app3.Window.GetByName(popoutPrefix + id)
			return ok && w != nil && w.IsVisible()
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
		// The native drag-out's OLE modal loop must run on the thread that
		// owns the app's windows and their message pump — the main thread.
		// InvokeSyncWithError blocks the calling binding goroutine while
		// the main thread runs the drag; DoDragDrop pumps messages itself,
		// so the UI stays alive inside the gesture.
		InvokeMain: application.InvokeSyncWithError,
	})
}

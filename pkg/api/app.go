// Package api is the GUI service layer (PLAN.md §7): the single place where
// the Wails frontend meets the pkg/core engine. Everything the webview can
// call is a method on App; core packages stay free of Wails imports.
package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Wails event names emitted by the service layer.
const (
	EventTransferUpdate = "transfer:update" // payload: JobInfo snapshot
	EventS3Changed      = "s3:changed"      // payload: {bucket, prefix} — refresh views
	EventLogLine        = "log:line"        // payload: LogLine — in-app log drawer
)

// quickOpTimeout bounds listing/stat/presign calls so a dead endpoint can
// never freeze the UI (long transfers use cancellable job contexts instead).
const quickOpTimeout = 30 * time.Second

// App is the Wails-bound service. A single instance lives for the whole
// application lifetime; all frontend calls arrive on its exported methods.
type App struct {
	version string

	ctx context.Context

	mu      sync.Mutex
	clients map[string]*s3client.Client // cache: source name -> client

	engMu   sync.Mutex             // guards engines + srcOps
	engines map[string]remotefs.FS // cache: source ID -> live engine (M9)
	srcOps  map[string]*sync.Mutex // per-source engine-op locks (FTP: one data connection)

	pfMu    sync.Mutex
	pf      *openProfileFile // open encrypted Profile file session (nil = none)
	session []profile.Source // session-only sources (strict model): visible, never persisted

	jobs *jobManager

	editorsMu sync.Mutex
	editors   map[string]*editSession // open-in-editor sessions (editor.go)

	searchMu sync.Mutex
	searches map[string]context.CancelFunc // running deep searches

	streamMu sync.Mutex
	streams  map[string]context.CancelFunc // running listing streams
}

// New creates the service. version is shown in the About dialog / status bar.
func New(version string) *App {
	return &App{
		version:  version,
		clients:  map[string]*s3client.Client{},
		engines:  map[string]remotefs.FS{},
		srcOps:   map[string]*sync.Mutex{},
		jobs:     newJobManager(),
		editors:  map[string]*editSession{},
		searches: map[string]context.CancelFunc{},
		streams:  map[string]context.CancelFunc{},
	}
}

// Startup captures the Wails runtime context.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.jobs.setContext(ctx)
}

// Shutdown cancels any transfers, searches or listing streams still
// running when the window closes.
func (a *App) Shutdown(ctx context.Context) {
	a.jobs.cancelAll()
	a.searchMu.Lock()
	for _, cancel := range a.searches {
		cancel()
	}
	a.searchMu.Unlock()
	a.streamMu.Lock()
	for _, cancel := range a.streams {
		cancel()
	}
	a.streamMu.Unlock()
	a.closeEngines() // live SFTP/FTP connections
}

// GetVersion returns the application version string.
func (a *App) GetVersion() string {
	return a.version
}

// ExitApp quits the application (menu bar / toolbar "Exit").
func (a *App) ExitApp() {
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

// quickCtx returns a context for fast operations (list/stat/presign/test).
func (a *App) quickCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, quickOpTimeout)
}

// client resolves (and caches) an S3 client. An empty name selects the
// default s3 source. Sources come from the workspace only — the open
// Profile file, else the session registry (strict sources model: the GUI
// never resolves from the CLI's profile store). The cache is dropped
// whenever sources change. Locking: pfMu is only ever taken OUTSIDE a.mu,
// so the source snapshot is resolved before locking.
func (a *App) client(name string) (*s3client.Client, error) {
	if a.ctx == nil {
		return nil, errNoContext
	}
	cSrc, haveSrc := a.containerS3Source(name)
	if !haveSrc {
		cSrc, haveSrc = a.sessionS3Source(name)
	}
	if !haveSrc {
		if name == "" {
			return nil, errors.New("no S3 data source configured — add one or open a profile file")
		}
		return nil, fmt.Errorf("%w: S3 data source %q", profile.ErrNotFound, name)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.clients[name]; ok && name != "" {
		return c, nil
	}
	// Timeout 0: no whole-request deadline — large uploads/downloads are
	// bounded by the per-job cancellation instead (quick ops use quickCtx).
	c, err := s3client.New(a.ctx, *cSrc.S3, s3client.Options{Timeout: 0})
	if err != nil {
		return nil, err
	}
	if name != "" {
		a.clients[name] = c
	}
	a.clients[""] = c // remember last default resolution
	return c, nil
}

// invalidateClients drops cached clients after a profile mutation. Remote
// engines ride along: a source edit may change host or credentials.
func (a *App) invalidateClients() {
	a.mu.Lock()
	a.clients = map[string]*s3client.Client{}
	a.mu.Unlock()
	a.closeEngines()
}

// emit sends a Wails event (no-op before Startup).
func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		emitEvent(a.ctx, event, data...)
	}
}

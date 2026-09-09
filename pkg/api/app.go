// Package api is the GUI service layer (PLAN.md §7): the single place where
// the Wails frontend meets the pkg/core engine. Everything the webview can
// call is a method on App; core packages stay free of Wails imports.
package api

import (
	"context"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// Wails event names emitted by the service layer.
const (
	EventTransferUpdate = "transfer:update" // payload: JobInfo snapshot
	EventS3Changed      = "s3:changed"      // payload: {bucket, prefix} — refresh views
)

// quickOpTimeout bounds listing/stat/presign calls so a dead endpoint can
// never freeze the UI (long transfers use cancellable job contexts instead).
const quickOpTimeout = 30 * time.Second

// App is the Wails-bound service. A single instance lives for the whole
// application lifetime; all frontend calls arrive on its exported methods.
type App struct {
	version string

	ctx   context.Context
	store *profile.Store

	mu      sync.Mutex
	clients map[string]*s3client.Client // cache: profile name -> client

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
}

// GetVersion returns the application version string.
func (a *App) GetVersion() string {
	return a.version
}

// quickCtx returns a context for fast operations (list/stat/presign/test).
func (a *App) quickCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, quickOpTimeout)
}

// loadStore (re)reads the profile store from disk so changes made by the
// CLI or another instance become visible without a restart.
func (a *App) loadStore() (*profile.Store, error) {
	s, err := profile.Load()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.store = s
	a.mu.Unlock()
	return s, nil
}

// client resolves (and caches) an S3 client. An empty name selects the
// default profile. The cache is dropped whenever profiles change.
func (a *App) client(name string) (*s3client.Client, error) {
	if a.ctx == nil {
		return nil, errNoContext
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.clients[name]; ok && name != "" {
		return c, nil
	}
	s := a.store
	if s == nil {
		var err error
		if s, err = profile.Load(); err != nil {
			return nil, err
		}
		a.store = s
	}
	p, err := a.profileByName(s, name)
	if err != nil {
		return nil, err
	}
	// Timeout 0: no whole-request deadline — large uploads/downloads are
	// bounded by the per-job cancellation instead (quick ops use quickCtx).
	c, err := s3client.New(a.ctx, p, s3client.Options{Timeout: 0})
	if err != nil {
		return nil, err
	}
	if name != "" {
		a.clients[name] = c
	}
	a.clients[""] = c // remember last default resolution
	return c, nil
}

// profileByName returns the named profile, or the default when name == "".
func (a *App) profileByName(s *profile.Store, name string) (profile.Profile, error) {
	if name == "" {
		return s.DefaultProfile()
	}
	return s.Get(name)
}

// invalidateClients drops cached clients after a profile mutation.
func (a *App) invalidateClients() {
	a.mu.Lock()
	a.clients = map[string]*s3client.Client{}
	a.mu.Unlock()
}

// emit sends a Wails event (no-op before Startup).
func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		emitEvent(a.ctx, event, data...)
	}
}

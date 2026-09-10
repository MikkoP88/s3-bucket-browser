// remote.go routes browsing of non-S3 data sources through the remotefs
// engines (M9): one live FS per source, cached by source ID and dropped
// whenever sources change — the same lifecycle as the S3 client cache.
package api

import (
	"context"
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
)

// engine resolves (and caches) the remotefs engine for a non-S3 source.
// S3 sources have no remotefs engine — they keep their dedicated
// streaming pipeline.
func (a *App) engine(idOrName string) (remotefs.FS, error) {
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return nil, err
	}
	if src.Type == profile.TypeS3 {
		return nil, fmt.Errorf("source %q is S3 — it is browsed through the S3 pipeline", src.Name)
	}
	if a.ctx == nil {
		return nil, errNoContext
	}
	// Snapshot the container decision OUTSIDE engMu (same discipline as
	// client(): pfMu is only ever taken outside the cache lock).
	a.engMu.Lock()
	defer a.engMu.Unlock()
	if fs, ok := a.engines[src.ID]; ok {
		return fs, nil
	}
	fs, err := remotefs.Dial(a.ctx, src)
	if err != nil {
		return nil, err
	}
	a.engines[src.ID] = fs
	return fs, nil
}

// closeEngines drops the engine cache (source edits, container switches,
// shutdown); the next browse reconnects.
func (a *App) closeEngines() {
	a.engMu.Lock()
	for id, fs := range a.engines {
		_ = fs.Close()
		delete(a.engines, id)
	}
	a.engMu.Unlock()
}

// RemoteList returns one folder view of a non-S3 source: folders first,
// then files — the same row shape as the S3 grid, so the frontend renders
// it unchanged. dir is anchored at the source root ("/" = root).
func (a *App) RemoteList(idOrName, dir string) ([]listing.Entry, error) {
	fs, err := a.engine(idOrName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	a.remoteOpMu.Lock()
	defer a.remoteOpMu.Unlock()
	entries, err := fs.List(ctx, dir)
	if err != nil {
		a.emitLog(LogError, "list", fmt.Sprintf("listing %s:%s failed: %v", idOrName, dir, err))
	}
	return entries, err
}

// testRemoteSource dials the source's engine, lists the root, and closes
// the probe — a real end-to-end check, not just a TCP ping.
func (a *App) testRemoteSource(src profile.Source) TestResult {
	if a.ctx == nil {
		return TestResult{OK: false, Message: errNoContext.Error()}
	}
	fs, err := remotefs.Dial(a.ctx, src)
	if err != nil {
		return TestResult{OK: false, Message: err.Error()}
	}
	defer fs.Close()
	ctx, cancel := context.WithTimeout(a.ctx, quickOpTimeout)
	defer cancel()
	entries, err := fs.List(ctx, "/")
	if err != nil {
		return TestResult{OK: false, Message: fmt.Sprintf("connected, but listing the root failed: %v", err)}
	}
	return TestResult{
		OK:      true,
		Message: fmt.Sprintf("connected — %d entries at the source root", len(entries)),
	}
}

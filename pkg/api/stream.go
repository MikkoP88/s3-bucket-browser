// stream.go: memory-bounded streaming directory listing for the GUI
// (M5 performance pass). Pages of entries flow to the
// frontend as events; the Go side never accumulates the folder, so a
// million-object prefix costs one paginator page of memory.
package api

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// EventListPage streams listing pages; the final page carries Done=true.
const EventListPage = "list:page"

const listPageSize = 1000

// listWatchdog bounds how long a listing stream may go without producing
// a page. A slow-but-alive endpoint resets it with every page (a million
// object walk can run for minutes, page after page); a dead one — a
// blackholed connection that accepts and never responds — is cut off and
// REPORTED as a timeout instead of leaving the view's loading state up
// forever. The budget is the Settings → Network listing timeout (default
// 30s), captured once per stream; tests shorten it by writing the tuning
// file into an isolated S3B_CONFIG dir.

// ListPage is one streamed chunk of a directory view.
type ListPage struct {
	Token   string          `json:"token"`
	Bucket  string          `json:"bucket"`
	Prefix  string          `json:"prefix"`
	Entries []listing.Entry `json:"entries"`
	Total   int             `json:"total"` // running count
	Done    bool            `json:"done"`
	Error   string          `json:"error,omitempty"`
}

// ListObjectsStream starts a streaming directory listing of bucket/prefix
// on the S3 source the main view is browsing and returns a token
// immediately. Entries arrive via EventListPage; the stream always
// terminates with a Done=true page.
// Cancel with CancelList(token) — e.g. when navigation moved on.
func (a *App) ListObjectsStream(bucket, prefix string) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	return a.streamObjects(c, bucket, prefix, "")
}

// ListObjectsWindow is ListObjectsStream starting strictly past startAfter
// in key order — the windowed-fetch half of the big-folder law: a folder
// with more objects than the frontend materializes stays reachable at any
// depth, one bounded stream at a time.
func (a *App) ListObjectsWindow(bucket, prefix, startAfter string) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	return a.streamObjects(c, bucket, prefix, startAfter)
}

// ListSourceObjectsWindow is ListObjectsWindow for one named S3 source
// (the dual pane's side view).
func (a *App) ListSourceObjectsWindow(idOrName, bucket, prefix, startAfter string) (string, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return "", err
	}
	return a.streamObjects(c, bucket, prefix, startAfter)
}

// ListSourceObjectsStream is ListObjectsStream for one named S3 source
// (the dual-pane side view bound to an S3 source).
func (a *App) ListSourceObjectsStream(idOrName, bucket, prefix string) (string, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return "", err
	}
	return a.streamObjects(c, bucket, prefix, "")
}

// ListSourceDirsStream is the sidebar tree's listing: one source-pinned
// prefix walked for its FOLDERS only — common prefixes and markers,
// never the objects (listing.WalkDirs). A folder with a million files
// costs the wire its scan but the event stream a handful of dir rows,
// so the tree's lazy expansion stays cheap at any depth.
func (a *App) ListSourceDirsStream(idOrName, bucket, prefix string) (string, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return "", err
	}
	return a.streamListing(bucket, prefix, fmt.Sprintf("s3://%s/%s (folders)", bucket, prefix),
		func(ctx context.Context, entry func(listing.Entry)) error {
			return listing.WalkDirs(ctx, c.S3, bucket, prefix, func(e listing.Entry) error {
				entry(e)
				return nil
			})
		})
}

// streamObjects runs the object view's listing stream over one resolved
// client: the full directory walk (folders AND objects).
func (a *App) streamObjects(c *s3client.Client, bucket, prefix, startAfter string) (string, error) {
	return a.streamListing(bucket, prefix, fmt.Sprintf("s3://%s/%s", bucket, prefix),
		func(ctx context.Context, entry func(listing.Entry)) error {
			return listing.WalkDir(ctx, c.S3, bucket, prefix, listing.Options{StartAfter: startAfter}, func(e listing.Entry) error {
				entry(e)
				return nil
			})
		})
}

// streamListing is the one streaming-listing law: token, watchdog (reset
// by every entry and every page — a slow-but-alive paginator is
// legitimate, a silent endpoint is cut at the listing-timeout budget and
// reported), transient task row, batched pages, and the always-final
// Done page. The walk function chooses WHAT streams — the object view's
// full directory walk or the tree's folders-only walk.
func (a *App) streamListing(bucket, prefix, label string, walk func(context.Context, func(listing.Entry)) error) (string, error) {
	if a.ctx == nil {
		return "", errNoContext
	}
	prefix = dirPrefix(prefix)
	token := fmt.Sprintf("l%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(a.ctx)
	a.streamMu.Lock()
	a.streams[token] = cancel
	a.streamMu.Unlock()
	// Watchdog: every emitted page resets it. A listing that produces
	// nothing for listWatchdog is a dead endpoint, not a big folder —
	// cut it off (the timeout is reported on the final page).
	var timedOut atomic.Bool
	listWatchdog := a.quickBudget() // Settings → Network listing timeout
	wd := time.AfterFunc(listWatchdog, func() { timedOut.Store(true); cancel() })
	// Transient task: visible while it runs (a stuck listing is killable
	// from the Running tasks window), gone when it ends — navigation
	// would otherwise pile done rows up forever.
	task := a.tasks.addWithID(token, "list", label)

	go func() {
		// The watchdog lives as long as the listing goroutine, not the
		// spawning call (which returns the token immediately — a Stop()
		// deferred there would disarm it before the first page).
		defer wd.Stop()
		defer func() {
			cancel()
			a.streamMu.Lock()
			delete(a.streams, token)
			a.streamMu.Unlock()
		}()
		total := 0
		batch := make([]listing.Entry, 0, listPageSize)
		emit := func(done bool, errMsg string) {
			wd.Reset(listWatchdog)
			a.emit(EventListPage, ListPage{
				Token: token, Bucket: bucket, Prefix: prefix,
				Entries: batch, Total: total, Done: done, Error: errMsg,
			})
			batch = make([]listing.Entry, 0, listPageSize)
		}
		// The worker panic net (guard.go): a panic mid-walk still settles
		// the token — final page carrying the error, task row failed —
		// instead of a spinner that never ends. Registered after the
		// cleanup defers so it runs first on unwind (LIFO).
		defer a.guardWorker("list", func(err error) {
			task.finish(err, true)
			emit(true, "internal error: "+err.Error())
		})
		err := walk(ctx, func(e listing.Entry) {
			// Any progress resets the watchdog — a paginating walk whose
			// pages trickle in slowly is alive; emit-level resets alone
			// would starve small folders (a batch needs 1000 entries).
			wd.Reset(listWatchdog)
			batch = append(batch, e)
			total++
			if len(batch) >= listPageSize {
				emit(false, "")
				task.progress(total) // live entry count on the transient row
			}
		})
		msg := ""
		var ferr error
		switch {
		case err != nil && ctx.Err() == nil:
			msg = err.Error()
			ferr = err
		case timedOut.Load():
			// The watchdog fired: the source went silent mid-listing.
			// Without this the final page would carry no error and the
			// view would present a partial listing as complete.
			msg = fmt.Sprintf("listing timed out — no data from the source for %s", listWatchdog)
			ferr = context.DeadlineExceeded
		}
		task.progress(total)
		task.finish(ferr, true)
		emit(true, msg)
	}()
	return token, nil
}

// CancelList aborts a running listing stream (unknown tokens are a no-op).
func (a *App) CancelList(token string) {
	a.streamMu.Lock()
	if cancel, ok := a.streams[token]; ok {
		cancel()
		delete(a.streams, token)
	}
	a.streamMu.Unlock()
}

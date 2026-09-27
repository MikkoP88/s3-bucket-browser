// engineheal.go: self-healing for cached remotefs engines. A remote
// connection that died under the app (network blip, reset, idle teardown
// by the server) must not strand every later operation on that source
// until the user re-saves the source or restarts — the engine cache holds
// one wrapper per source whose idempotent, single-request operations
// redial once and retry themselves on a fresh connection.
package api

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
)

// connDead reports whether err is the death of the underlying connection
// (reset, closed transport, pooled socket gone) as opposed to a semantic
// failure (missing path, refused name) or the caller's own patience limit
// (context canceled / deadline) — those never trigger a redial.
func connDead(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	// The ssh/sftp stack (and some ftp paths) raise plain sentinel strings
	// that carry no type to errors.Is against.
	s := strings.ToLower(err.Error())
	for _, m := range []string{
		"connection lost", "connection reset", "broken pipe",
		"connection closed", "use of closed network connection",
		"connection refused", "no route to host", "i/o timeout",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// healingFS wraps one cached engine. The cached identity is stable for the
// app lifetime; the inner connection is replaced in place when it dies.
//
// Retried operations are the idempotent, single-request ones (List, Stat,
// MkdirAll, Remove, Rename): a retry either re-issues a pure read, re-runs
// an idempotent mutation, or — for Rename, whose first attempt may have
// landed before the response was lost — fails harmlessly on the now-missing
// source path. Open is healed at the handshake only: an Open whose open
// call dies on the wire has flowed zero bytes and holds no handle, so it is
// as idempotent as Stat — without this, the first operation of a user's
// transfer retry after a wire death speaks to the stale engine and fails
// with "connection lost" until an unrelated listing happens to heal the
// source. What is never retried is the stream beyond that handshake and
// Create (whose reader the engine consumes mid-call): a partially consumed
// reader or writer restarted transparently would duplicate or truncate
// bytes — those failures surface to the caller, which owns the
// transfer-level restart.
//
// Mutating callers already serialize per source via lockSrcs; the mutex
// here guards the inner swap against the one true overlap — a stream
// returned by Open still reading while another operation heals. Such a
// swap closes the old connection, ending the stream exactly as a source
// edit would (transfers ride their own restart machinery).
type healingFS struct {
	a   *App
	src profile.Source
	// dial is remotefs.Dial in production; a seam for the heal tests.
	dial func(ctx context.Context, src profile.Source) (remotefs.FS, error)

	mu     sync.Mutex
	closed bool
	inner  remotefs.FS
}

var _ remotefs.FS = (*healingFS)(nil)

// newHealingFS wraps a freshly dialed engine for the cache.
func newHealingFS(a *App, src profile.Source, inner remotefs.FS) *healingFS {
	return &healingFS{a: a, src: src, dial: remotefs.Dial, inner: inner}
}

// redial replaces a dead inner engine with a fresh connection; ok=false
// when the wire is still down, the app context is gone, or the wrapper
// was closed under us (source edit — do not resurrect a dropped engine).
func (h *healingFS) redial() (remotefs.FS, bool) {
	if h.a.ctx == nil || h.a.ctx.Err() != nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	fs, err := h.dial(h.a.ctx, h.src)
	if err != nil {
		return nil, false
	}
	_ = h.inner.Close()
	h.inner = fs
	return fs, true
}

func (h *healingFS) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	es, err := h.inner.List(ctx, dir)
	if err == nil || !connDead(err) || ctx.Err() != nil {
		return es, err
	}
	if fs, ok := h.redial(); ok {
		return fs.List(ctx, dir)
	}
	return es, err
}

func (h *healingFS) Stat(ctx context.Context, p string) (listing.Entry, error) {
	e, err := h.inner.Stat(ctx, p)
	if err == nil || !connDead(err) || ctx.Err() != nil {
		return e, err
	}
	if fs, ok := h.redial(); ok {
		return fs.Stat(ctx, p)
	}
	return e, err
}

func (h *healingFS) MkdirAll(ctx context.Context, dir string) error {
	err := h.inner.MkdirAll(ctx, dir)
	if err == nil || !connDead(err) || ctx.Err() != nil {
		return err
	}
	if fs, ok := h.redial(); ok {
		return fs.MkdirAll(ctx, dir)
	}
	return err
}

func (h *healingFS) Remove(ctx context.Context, p string) error {
	err := h.inner.Remove(ctx, p)
	if err == nil || !connDead(err) || ctx.Err() != nil {
		return err
	}
	if fs, ok := h.redial(); ok {
		return fs.Remove(ctx, p)
	}
	return err
}

func (h *healingFS) Rename(ctx context.Context, oldp, newp string) error {
	err := h.inner.Rename(ctx, oldp, newp)
	if err == nil || !connDead(err) || ctx.Err() != nil {
		return err
	}
	if fs, ok := h.redial(); ok {
		return fs.Rename(ctx, oldp, newp)
	}
	return err
}

// The Open handshake heals like Stat; the stream it returns never restarts
// and Create never retries (see the type comment).

func (h *healingFS) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	rc, sz, err := h.inner.Open(ctx, p)
	if err == nil || !connDead(err) || ctx.Err() != nil {
		return rc, sz, err
	}
	if fs, ok := h.redial(); ok {
		return fs.Open(ctx, p)
	}
	return rc, sz, err
}

func (h *healingFS) Create(ctx context.Context, p string, r io.Reader) error {
	return h.inner.Create(ctx, p, r)
}

func (h *healingFS) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return h.inner.Close()
}

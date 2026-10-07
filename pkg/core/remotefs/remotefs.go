// Package remotefs is the M9 engine abstraction: one directory-oriented
// filesystem interface over remote-filesystem data sources (sftp/scp,
// ftp/ftps) and local directory roots, so browsing, transfers and CLI
// commands treat every source type uniformly.
//
// Paths are slash-separated and anchored at the source root: "/" is the
// root the user configured (LocalRoot for local sources, Root for remote
// ones) and no path may escape above it. Directory entries carry a
// trailing slash in Key, matching the S3 folder-marker convention, and
// every listing uses listing.Entry — the same row shape the GUI grid and
// CLI tables render for S3.
package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// FS is one browsable filesystem. All methods take paths as described in
// the package comment; implementations are not safe for concurrent use
// (wrap with a mutex at the call site if needed).
type FS interface {
	// List returns one directory view: folders first, then files,
	// both sorted by name (case-insensitive).
	List(ctx context.Context, dir string) ([]listing.Entry, error)
	// Stat returns metadata for one file or directory.
	Stat(ctx context.Context, p string) (listing.Entry, error)
	// Open returns a reader over one file and its size.
	Open(ctx context.Context, p string) (io.ReadCloser, int64, error)
	// Create writes one file from r, truncating any existing content.
	Create(ctx context.Context, p string, r io.Reader) error
	// MkdirAll creates dir and any missing parents.
	MkdirAll(ctx context.Context, dir string) error
	// Remove deletes one file or a whole directory tree.
	Remove(ctx context.Context, p string) error
	// Rename moves or renames within this filesystem.
	Rename(ctx context.Context, oldp, newp string) error
	// Close releases the underlying connection (no-op for stateless
	// engines).
	Close() error
}

// ErrCmdDeadline is the verdict an engine returns when it tore its own
// connection down at a per-command deadline because the peer stopped
// answering — no FIN, no RST, just silence. The connection is dead by
// the engine's hand, so anything that heals connections (FTP's
// withRetry, the api layer's engine cache) must treat it exactly like a
// reset or an EOF and redial.
var ErrCmdDeadline = errors.New("remotefs: command deadline exceeded")

// forceBreaker is the surface the deadline machinery needs from an
// engine's connection: something Quit can tear down hard. *ftp.ServerConn
// carries Quit itself; the SSH engines adapt their transport.
type forceBreaker interface{ Quit() error }

// bound runs one engine command round-trip and force-breaks a wedged
// channel. On the deadline (d > 0) or the caller's cancellation the
// break is Quit: for FTP that is textproto's command write plus a socket
// Close, for SSH the transport Close — either way any concurrent reply
// read unblocks immediately with a connection-class error, so the
// engine's redial policy takes over instead of the call hanging for the
// life of the process. The runner goroutine holds only what fn captured
// (the connection is passed in, never read back off the engine), so a
// timed-out attempt still unwinding can never reach past its own
// connection into a redialed successor. d <= 0 means context-only
// bounding — the right shape for transfers and tree walks whose
// legitimate duration is unknowable.
func bound[T any](ctx context.Context, c forceBreaker, d time.Duration, fn func() (T, error)) (T, error) {
	type res struct {
		v   T
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := fn()
		ch <- res{v: v, err: err}
	}()
	var timeout <-chan time.Time
	if d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		_ = c.Quit() // force-close: the blocked read unblocks NOW
		var zero T
		return zero, fmt.Errorf("remotefs command: %w", ctx.Err())
	case <-timeout:
		_ = c.Quit()
		var zero T
		return zero, ErrCmdDeadline
	}
}

// boundErr is bound for the commands that return nothing but their
// error (the mutation half of the engine verb set — the T in bound
// cannot be inferred from a func() error).
func boundErr(ctx context.Context, c forceBreaker, d time.Duration, fn func() error) error {
	_, err := bound(ctx, c, d, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}

// Dial connects to a data source and returns its filesystem. S3 sources
// are not handled here — they keep their dedicated s3client pipeline.
func Dial(ctx context.Context, src profile.Source) (FS, error) {
	switch src.Type {
	case profile.TypeLocal:
		return NewLocal(src.LocalRoot)
	case profile.TypeSFTP, profile.TypeSCP:
		return DialSFTP(ctx, src)
	case profile.TypeFTP, profile.TypeFTPS:
		return DialFTP(ctx, src)
	case profile.TypeWebDAV, profile.TypeWebDAVS:
		return DialWebDAV(ctx, src)
	default:
		return nil, fmt.Errorf("source %q: no filesystem engine for type %q", src.Name, src.Type)
	}
}

// CleanPath normalizes a user-supplied path into anchored form: "/"-based,
// no "..", no trailing slash except the root itself. It is the single
// choke point every engine uses before touching the real filesystem, so
// escaping above the source root is impossible by construction.
func CleanPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || p == "/" {
		return "/"
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if cleaned == "" || cleaned == "." {
		return "/"
	}
	return cleaned
}

// Key builds the listing.Entry key for dir + name: the anchored path,
// with a trailing slash on directories (S3 folder convention). Children
// of the root are "/name", nested ones "/dir/name".
func Key(dir, name string, isDir bool) string {
	k := strings.TrimSuffix(CleanPath(dir), "/") + "/" + name
	if isDir {
		k += "/"
	}
	return k
}

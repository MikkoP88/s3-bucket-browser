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
	"fmt"
	"io"
	"path"
	"strings"

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

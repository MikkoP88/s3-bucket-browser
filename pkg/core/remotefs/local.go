// local.go is the remotefs engine over a local directory root — both the
// TypeLocal data source and the test bed for the FS contract (every other
// engine is validated against the same behaviors).
package remotefs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
)

// Local is an FS rooted at a directory on the local machine. Paths are
// anchored at the root; escaping above it is impossible by construction
// (CleanPath collapses leading ".." segments before the join).
type Local struct {
	root string // absolute, cleaned
}

// NewLocal returns an FS over root (which must exist).
func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local root %s is not a directory", abs)
	}
	return &Local{root: abs}, nil
}

// join maps an anchored slash path onto the real filesystem.
func (l *Local) join(p string) string {
	p = CleanPath(p)
	if p == "/" {
		return l.root
	}
	return filepath.Join(l.root, filepath.FromSlash(p))
}

func entryFromInfo(dir, name string, info fs.FileInfo) listing.Entry {
	e := listing.Entry{
		Key:   Key(dir, name, info.IsDir()),
		Name:  name,
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}
	if t := info.ModTime(); !t.IsZero() {
		e.LastModified = &t
	}
	return e
}

func (l *Local) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	full := l.join(dir)
	dirInfos, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	parent := CleanPath(dir)
	entries := make([]listing.Entry, 0, len(dirInfos))
	for _, d := range dirInfos {
		info, err := d.Info()
		if err != nil {
			continue // raced with a delete; not worth failing the listing
		}
		entries = append(entries, entryFromInfo(parent, d.Name(), info))
	}
	listing.SortEntries(entries)
	return entries, nil
}

func (l *Local) Stat(ctx context.Context, p string) (listing.Entry, error) {
	cleaned := CleanPath(p)
	info, err := os.Stat(l.join(cleaned))
	if err != nil {
		return listing.Entry{}, err
	}
	name := path.Base(strings.TrimSuffix(cleaned, "/"))
	return entryFromInfo(path.Dir(cleaned), name, info), nil
}

func (l *Local) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	full := l.join(p)
	f, err := os.Open(full)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if info.IsDir() {
		f.Close()
		return nil, 0, fmt.Errorf("%s is a directory", CleanPath(p))
	}
	return f, info.Size(), nil
}

func (l *Local) Create(ctx context.Context, p string, r io.Reader) error {
	full := l.join(p)
	f, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (l *Local) MkdirAll(ctx context.Context, dir string) error {
	return os.MkdirAll(l.join(dir), 0o755)
}

func (l *Local) Remove(ctx context.Context, p string) error {
	cleaned := CleanPath(p)
	if cleaned == "/" {
		return fmt.Errorf("refusing to remove the source root")
	}
	full := l.join(cleaned)
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(full)
	}
	return os.Remove(full)
}

func (l *Local) Rename(ctx context.Context, oldp, newp string) error {
	if CleanPath(newp) == "/" {
		return fmt.Errorf("invalid target")
	}
	return os.Rename(l.join(oldp), l.join(newp))
}

func (l *Local) Close() error { return nil }

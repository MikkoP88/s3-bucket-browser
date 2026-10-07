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
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// localReadDir and localStat are the point primitives this engine rides;
// vars so the wedge tests can park one (the openInEditor discipline). Each
// caller captures its seam into a local BEFORE the step goroutine spawns:
// an abandoned step can outlive its caller until the OS itself gives up,
// and the only reads of these vars live on the spawning goroutine — the
// retire-grace discipline LocalOpBudget already follows.
var (
	localReadDir = os.ReadDir
	localStat    = os.Stat
)

// Local is an FS rooted at a directory on the local machine. Paths are
// anchored at the root; escaping above it is impossible by construction
// (CleanPath collapses leading ".." segments before the join).
type Local struct {
	root string // absolute, cleaned
}

// NewLocal returns an FS over root (which must exist). The root's stat is
// a point step under the local budget and the caller's patience: in the
// GUI this dial runs under the engine-cache lock, and a wedged root (dead
// UNC path) must never freeze every source's engine resolution — the same
// lock discipline the SFTP handshake round bought its bound for.
func NewLocal(ctx context.Context, root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	stat := localStat // captured before the step spawns (seam discipline)
	info, err := LocalStep(ctx, func() (fs.FileInfo, error) { return stat(abs) })
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
		Mode:  info.Mode().String(), // "drwxr-xr-x" (Windows synthesizes from attributes)
	}
	if t := info.ModTime(); !t.IsZero() {
		e.LastModified = &t
	}
	if t, ok := CreationTimeOf(info); ok {
		e.Created = &t
	}
	return e
}

func (l *Local) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	full := l.join(dir)
	parent := CleanPath(dir)
	// One bounded step covers the whole directory view — the read AND the
	// per-entry info calls — so a wedge anywhere in it is one verdict.
	readDir := localReadDir // captured before the step spawns (seam discipline)
	entries, err := LocalStep(ctx, func() ([]listing.Entry, error) {
		dirInfos, err := readDir(full)
		if err != nil {
			return nil, err
		}
		entries := make([]listing.Entry, 0, len(dirInfos))
		for _, d := range dirInfos {
			info, err := d.Info()
			if err != nil {
				continue // raced with a delete; not worth failing the listing
			}
			entries = append(entries, entryFromInfo(parent, d.Name(), info))
		}
		return entries, nil
	})
	if err != nil {
		return nil, err
	}
	listing.SortEntries(entries)
	return entries, nil
}

func (l *Local) Stat(ctx context.Context, p string) (listing.Entry, error) {
	cleaned := CleanPath(p)
	stat := localStat // captured before the step spawns (seam discipline)
	info, err := LocalStep(ctx, func() (fs.FileInfo, error) { return stat(l.join(cleaned)) })
	if err != nil {
		return listing.Entry{}, err
	}
	name := path.Base(strings.TrimSuffix(cleaned, "/"))
	return entryFromInfo(path.Dir(cleaned), name, info), nil
}

func (l *Local) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	full := l.join(p)
	// The open handshake is a point step (bounded, and an abandoned open
	// releases its handle); the stream it returns rides the caller alone —
	// a read's legitimate duration is not the engine's to guess, the same
	// exemption every wire engine gives its streams.
	f, err := LocalStep(ctx, func() (*os.File, error) { return os.Open(full) })
	if err != nil {
		return nil, 0, err
	}
	info, err := LocalStep(ctx, func() (fs.FileInfo, error) { return f.Stat() })
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

// Create writes a file from a stream — staged and committed, never
// truncated in place: a transfer that fails mid-stream (network drop,
// cancel) must leave the previous local bytes untouched, the same
// critical-data contract the download path established. The copy is a
// stream and rides the caller's context alone (no engine cap — the same
// exemption class as every other engine's Create).
func (l *Local) Create(ctx context.Context, p string, r io.Reader) error {
	return transfer.StageAndCommit(l.join(p), func(f *os.File) error {
		_, err := io.Copy(f, r)
		return err
	})
}

func (l *Local) MkdirAll(ctx context.Context, dir string) error {
	full := l.join(dir)
	_, err := LocalStep(ctx, func() (struct{}, error) { return struct{}{}, os.MkdirAll(full, 0o755) })
	return err
}

func (l *Local) Remove(ctx context.Context, p string) error {
	cleaned := CleanPath(p)
	if cleaned == "/" {
		return fmt.Errorf("refusing to remove the source root")
	}
	full := l.join(cleaned)
	stat := localStat // captured before the step spawns (seam discipline)
	info, err := LocalStep(ctx, func() (fs.FileInfo, error) { return stat(full) })
	if err != nil {
		return err
	}
	if info.IsDir() {
		// A tree's delete time is real work, not a dead peer — capping it
		// would report a failure while the delete kept running. The gate in
		// front of it (the delete preview) is per-step bounded, so a wedged
		// root never gets this far.
		return os.RemoveAll(full)
	}
	_, err = LocalStep(ctx, func() (struct{}, error) { return struct{}{}, os.Remove(full) })
	return err
}

func (l *Local) Rename(ctx context.Context, oldp, newp string) error {
	oldFull, newFull := l.join(oldp), l.join(newp)
	if CleanPath(newp) == "/" {
		return fmt.Errorf("invalid target")
	}
	_, err := LocalStep(ctx, func() (struct{}, error) { return struct{}{}, os.Rename(oldFull, newFull) })
	return err
}

func (l *Local) Close() error { return nil }

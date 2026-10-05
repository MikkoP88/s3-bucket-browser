// usage.go: real recursive content sizes for the content viewer's bottom
// bar (WinSCP-style) and for folder Size cells: the whole listing or the
// selection, always including everything inside folders, on every source
// type the viewer hosts. S3 walks are version-aware — old versions and
// delete markers are counted and summed separately, so a versioned bucket
// shows what its history really costs on top of the live content. The S3
// pair comes in two flavors — the main view's own source, and a
// source-pinned variant for panes and search hits browsing elsewhere.
package api

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
)

// UsageStat is the real recursive footprint of one bucket, prefix or path.
// Files/Dirs/CurrentBytes describe the live content (what a plain listing
// shows); VersionBytes/VersionCount/MarkerCount exist only on S3 and carry
// the noncurrent history. Partial means the walk failed partway — every
// number is then a floor, not the truth.
type UsageStat struct {
	Key          string `json:"key"` // bucket, object key or remote path
	Files        int64  `json:"files"`
	Dirs         int64  `json:"dirs"`
	CurrentBytes int64  `json:"currentBytes"`
	VersionBytes int64  `json:"versionBytes"` // noncurrent versions only
	VersionCount int64  `json:"versionCount"` // versions incl. current
	MarkerCount  int64  `json:"markerCount"`  // delete markers
	Versioned    bool   `json:"versioned"`
	Partial      bool   `json:"partial"`
	Error        string `json:"error,omitempty"`
}

// dirSetCap bounds the implicit-directory set of an S3 walk: beyond it the
// folder count stays a floor (the set stops growing) instead of costing
// unbounded memory on pathologically deep hierarchies.
const dirSetCap = 50_000

// S3Usage reports the recursive usage of one folder's children (or the
// whole prefix when children is empty) in the bucket the main view is
// browsing — SourceS3Usage pinned to that source.
func (a *App) S3Usage(bucket, prefix string, children []string) ([]UsageStat, error) {
	return a.SourceS3Usage("", bucket, prefix, children)
}

// SourceS3Usage is S3Usage on a named S3 source ("" = the source the main
// view is browsing), so a secondary pane or a search hit can walk a bucket
// the main view is not browsing. Folder children (trailing "/") aggregate
// everything beneath them; file children report the key's whole version
// timeline. The walk is ListObjectVersions-based, so versioned, suspended
// and never-versioned buckets all take the same one-pass path — on the
// last, every object is simply its own single current version.
func (a *App) SourceS3Usage(idOrName, bucket, prefix string, children []string) ([]UsageStat, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	versioned := s3Versioned(ctx, c, bucket)
	if len(children) == 0 {
		root := dirPrefix(prefix)
		u := s3UsageOne(ctx, c, bucket, root, versioned)
		return []UsageStat{u}, nil
	}
	out := make([]UsageStat, 0, len(children))
	for _, k := range children {
		// file children walk their exact key (no trailing slash); folder
		// children walk the prefix in folder form
		root := k
		if strings.HasSuffix(k, "/") {
			root = dirPrefix(k)
		}
		out = append(out, s3UsageOne(ctx, c, bucket, root, versioned))
	}
	return out, nil
}

// BucketUsage reports the recursive usage of the named buckets on the
// source the main view is browsing (the buckets view's bar) —
// SourceBucketUsage pinned to that source.
func (a *App) BucketUsage(buckets []string) ([]UsageStat, error) {
	return a.SourceBucketUsage("", buckets)
}

// SourceBucketUsage is BucketUsage on a named S3 source ("" = the source
// the main view is browsing): one whole-bucket walk per name, versions
// included.
func (a *App) SourceBucketUsage(idOrName string, buckets []string) ([]UsageStat, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	out := make([]UsageStat, 0, len(buckets))
	for _, b := range buckets {
		u := s3UsageOne(ctx, c, b, "", s3Versioned(ctx, c, b))
		u.Key = b
		out = append(out, u)
	}
	return out, nil
}

// RemoteUsage reports the recursive usage of one directory's children (or
// the whole directory when children is empty) on a non-S3 source — the
// local, sftp/scp, ftp(s) and webdav engines. The per-source operation
// lock is held across the walk: engines are single-op (the FTP engine
// allows exactly one data connection).
func (a *App) RemoteUsage(idOrName, path string, children []string) ([]UsageStat, error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()

	roots := children
	if len(roots) == 0 {
		roots = []string{remotefs.CleanPath(path)}
	}
	out := make([]UsageStat, 0, len(roots))
	for _, root := range roots {
		u := UsageStat{Key: root}
		// a file child is stated, not walked — engine listings mark
		// directories with a trailing slash, so anything else is a file
		if !strings.HasSuffix(root, "/") {
			if e, serr := fs.Stat(ctx, root); serr == nil && !e.IsDir {
				u.Files = 1
				u.CurrentBytes = e.Size
				out = append(out, u)
				continue
			}
		}
		dir := root
		if !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		werr := remotefs.Walk(ctx, fs, dir, func(e listing.Entry) error {
			if e.IsDir {
				u.Dirs++
			} else {
				u.Files++
				u.CurrentBytes += e.Size
			}
			return nil
		})
		if werr != nil {
			u.Partial = true
			u.Error = werr.Error()
		}
		out = append(out, u)
	}
	return out, nil
}

// LocalUsage reports the recursive usage of arbitrary workstation paths —
// the local view's rows carry absolute filesystem paths, not a source, so
// this walks the OS directly (the local engine's virtual root never
// enters). Files are stated, not walked; directories are walked
// best-effort: a failed descent marks Partial and skips that subtree, and
// the root's own directory entry is never counted as content — a folder
// row's size is what is inside it, exactly like every other walk here.
func (a *App) LocalUsage(paths []string) ([]UsageStat, error) {
	out := make([]UsageStat, 0, len(paths))
	for _, p := range paths {
		out = append(out, localUsageOne(p))
	}
	return out, nil
}

// localUsageOne walks one absolute path. A vanished path reports Partial
// with zero totals rather than an error, so one stale row never blanks
// its siblings.
func localUsageOne(p string) UsageStat {
	u := UsageStat{Key: p}
	st, err := os.Stat(p)
	if err != nil {
		u.Partial = true
		u.Error = err.Error()
		return u
	}
	if !st.IsDir() {
		u.Files = 1
		u.CurrentBytes = st.Size()
		return u
	}
	werr := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			u.Partial = true
			if u.Error == "" {
				u.Error = err.Error()
			}
			// a failed directory descent skips that subtree; a failed
			// file entry is simply not counted — either way the walk goes on
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == p {
			return nil // the root is the folder itself, not content
		}
		if d.IsDir() {
			u.Dirs++
			return nil
		}
		info, serr := d.Info()
		if serr != nil {
			u.Partial = true
			return nil
		}
		u.Files++
		u.CurrentBytes += info.Size()
		return nil
	})
	if werr != nil {
		u.Partial = true
		if u.Error == "" {
			u.Error = werr.Error()
		}
	}
	return u
}

// s3Versioned reports whether the bucket has versioning configured
// (Enabled or Suspended — a suspended bucket still carries its old
// versions). Best-effort: an unreadable status reads as unversioned, and
// the bar drives off the numbers, not this flag.
func s3Versioned(ctx context.Context, c *s3client.Client, bucket string) bool {
	st, err := versioning.Status(ctx, c.S3, bucket)
	if err != nil {
		return false
	}
	return st == "Enabled" || st == "Suspended"
}

// s3UsageOne aggregates one walk root — a folder prefix (trailing "/") or
// an exact file key — over the version timeline beneath it.
func s3UsageOne(ctx context.Context, c *s3client.Client, bucket, root string, versioned bool) UsageStat {
	u := UsageStat{Key: root, Versioned: versioned}
	isFile := root != "" && !strings.HasSuffix(root, "/")
	dirs := map[string]bool{}

	werr := versioning.WalkVersions(ctx, c.S3, bucket, root, func(v versioning.Version) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isFile {
			if v.Key != root {
				return nil // prefix match, not the exact file
			}
		} else if v.Key == root {
			return nil // the folder's own marker: the folder, not content
		}
		if v.IsDeleteMarker {
			u.MarkerCount++
			return nil
		}
		u.VersionCount++
		if !v.IsLatest {
			u.VersionBytes += v.Size
			return nil
		}
		// The live content — what a plain listing shows. A latest version
		// ending in "/" is a folder marker: its existence is what Dirs
		// counts, never a file.
		if !strings.HasSuffix(v.Key, "/") {
			u.Files++
			u.CurrentBytes += v.Size
		}
		if isFile {
			return nil // a file child has no interior folders
		}
		acc, rest := "", strings.TrimPrefix(v.Key, root)
		for {
			i := strings.Index(rest, "/")
			if i < 0 {
				break
			}
			acc += rest[:i+1]
			if len(dirs) < dirSetCap {
				dirs[acc] = true
			}
			rest = rest[i+1:]
		}
		return nil
	})
	u.Dirs = int64(len(dirs))
	if werr != nil {
		u.Partial = true
		u.Error = werr.Error()
	}
	return u
}

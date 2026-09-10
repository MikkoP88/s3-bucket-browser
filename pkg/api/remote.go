// remote.go routes browsing and file operations on non-S3 data sources
// through the remotefs engines (M9): one live FS per source, cached by
// source ID and dropped whenever sources change — the same lifecycle as
// the S3 client cache.
package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

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

// srcLock returns (creating on demand) the operation lock of one source.
// remotefs engines are not safe for concurrent use — the FTP engine
// allows exactly one data connection — so every engine call touching a
// source serializes on its lock. Locks are never deleted, so pointers are
// stable for the app lifetime.
func (a *App) srcLock(id string) *sync.Mutex {
	a.engMu.Lock()
	defer a.engMu.Unlock()
	if a.srcOps == nil {
		a.srcOps = map[string]*sync.Mutex{}
	}
	mu, ok := a.srcOps[id]
	if !ok {
		mu = &sync.Mutex{}
		a.srcOps[id] = mu
	}
	return mu
}

// lockSrcs locks the per-source operation locks of every listed id and
// returns the unlock function. Ids are deduplicated and locked in sorted
// order, so operations spanning several sources (cross-source transfers
// hold both sides) can never deadlock against each other.
func (a *App) lockSrcs(ids ...string) func() {
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			seen[id] = true
		}
	}
	ordered := make([]string, 0, len(seen))
	for id := range seen {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	locks := make([]*sync.Mutex, len(ordered))
	for i, id := range ordered {
		locks[i] = a.srcLock(id)
		locks[i].Lock()
	}
	return func() {
		for _, mu := range locks {
			mu.Unlock()
		}
	}
}

// remoteSource resolves idOrName to a non-S3 source (the common guard of
// every Remote* operation).
func (a *App) remoteSource(idOrName string) (profile.Source, remotefs.FS, error) {
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return profile.Source{}, nil, err
	}
	if src.Type == profile.TypeS3 {
		return profile.Source{}, nil, fmt.Errorf("source %q is S3 — it is browsed through the S3 pipeline", src.Name)
	}
	fs, err := a.engine(idOrName)
	if err != nil {
		return profile.Source{}, nil, err
	}
	return src, fs, nil
}

// RemoteList returns one folder view of a non-S3 source: folders first,
// then files — the same row shape as the S3 grid, so the frontend renders
// it unchanged. dir is anchored at the source root ("/" = root).
func (a *App) RemoteList(idOrName, dir string) ([]listing.Entry, error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	entries, err := fs.List(ctx, dir)
	if err != nil {
		a.emitLog(LogError, "list", fmt.Sprintf("listing %s:%s failed: %v", idOrName, dir, err))
	}
	return entries, err
}

// RemoteStat returns metadata for one file or directory of a non-S3 source.
func (a *App) RemoteStat(idOrName, path string) (listing.Entry, error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return listing.Entry{}, err
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	return fs.Stat(ctx, path)
}

// RemoteMkdir creates a directory (and missing parents) on a non-S3 source.
func (a *App) RemoteMkdir(idOrName, dir string) error {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return err
	}
	if remotefs.CleanPath(dir) == "/" {
		return fmt.Errorf("invalid folder name")
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	err = fs.MkdirAll(ctx, dir)
	if err != nil {
		a.emitLog(LogError, "mkdir", fmt.Sprintf("mkdir %s:%s failed: %v", idOrName, dir, err))
	} else {
		a.emitLog(LogInfo, "mkdir", fmt.Sprintf("created %s:%s", idOrName, dir))
	}
	return err
}

// RemoteRename renames one file or directory within its source. newName is
// a bare name (Explorer semantics, like the S3 RenameObject command); the
// entry keeps its parent directory and its kind (folder keys keep the
// trailing slash).
func (a *App) RemoteRename(idOrName, path, newName string) error {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return err
	}
	cleaned := remotefs.CleanPath(path)
	if cleaned == "/" {
		return fmt.Errorf("cannot rename the source root")
	}
	newName = strings.TrimSpace(strings.Trim(newName, "/"))
	if newName == "" || strings.Contains(newName, "/") {
		return fmt.Errorf("invalid name %q", newName)
	}
	trimmed := strings.TrimSuffix(cleaned, "/")
	i := strings.LastIndex(trimmed, "/")
	target := trimmed[:i+1] + newName
	if strings.HasSuffix(cleaned, "/") && cleaned != "/" {
		target += "/"
	}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	if err := fs.Rename(ctx, cleaned, target); err != nil {
		a.emitLog(LogError, "rename", fmt.Sprintf("rename %s:%s failed: %v", idOrName, cleaned, err))
		return err
	}
	a.emitLog(LogInfo, "rename", fmt.Sprintf("renamed %s:%s to %s", idOrName, cleaned, target))
	return nil
}

// RemoteDeletePreview is the count-then-act half of remote deletes
// (PLAN.md §9): it walks every listed path (file or directory tree) and
// reports what a delete would remove. Paths that do not exist are counted
// as errors — the caller decides how loud to be.
type RemoteDeletePreview struct {
	Files   int64    `json:"files"`
	Folders int64    `json:"folders"`
	Bytes   int64    `json:"bytes"`
	Errors  []string `json:"errors,omitempty"`
}

func (a *App) RemoteDeletePreview(idOrName string, paths []string) (*RemoteDeletePreview, error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return nil, err
	}
	out := &RemoteDeletePreview{}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	for _, p := range paths {
		cleaned := remotefs.CleanPath(p)
		if cleaned == "/" {
			out.Errors = append(out.Errors, "refusing to delete the source root")
			continue
		}
		st, err := fs.Stat(ctx, cleaned)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", cleaned, err))
			continue
		}
		if !st.IsDir {
			out.Files++
			out.Bytes += st.Size
			continue
		}
		out.Folders++
		err = remotefs.Walk(ctx, fs, cleaned, func(e listing.Entry) error {
			if e.IsDir {
				out.Folders++
			} else {
				out.Files++
				out.Bytes += e.Size
			}
			return nil
		})
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", cleaned, err))
		}
	}
	return out, nil
}

// RemoteDeleteResult reports the outcome of a remote batch delete.
type RemoteDeleteResult struct {
	Deleted int      `json:"deleted"`
	Errors  []string `json:"errors,omitempty"`
}

// RemoteRemove deletes the listed paths (files or whole directory trees)
// from a non-S3 source. The safety ladder is enforced by the caller: the
// GUI previews with RemoteDeletePreview and asks for a typed confirm
// first; S3's L2/L3 layers have no remote equivalent (no trash, no
// versions — deletion is permanent, like `rm -rf`).
func (a *App) RemoteRemove(idOrName string, paths []string) (*RemoteDeleteResult, error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return nil, err
	}
	out := &RemoteDeleteResult{}
	ctx, cancel := a.quickCtx()
	defer cancel()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	for _, p := range paths {
		cleaned := remotefs.CleanPath(p)
		if cleaned == "/" {
			out.Errors = append(out.Errors, "refusing to delete the source root")
			continue
		}
		if err := fs.Remove(ctx, cleaned); err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", cleaned, err))
			a.emitLog(LogError, "delete", fmt.Sprintf("delete %s:%s failed: %v", idOrName, cleaned, err))
			continue
		}
		out.Deleted++
	}
	if out.Deleted > 0 {
		a.emitLog(LogInfo, "delete", fmt.Sprintf("deleted %d item(s) from %s", out.Deleted, idOrName))
	}
	return out, nil
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

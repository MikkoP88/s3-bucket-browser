// remote.go routes browsing and file operations on non-S3 data sources
// through the remotefs engines (M9): one live FS per source, cached by
// source ID and dropped whenever sources change — the same lifecycle as
// the S3 client cache.
package api

import (
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
		a.emitLogSrc(LogError, "list", idOrName, fmt.Sprintf("listing %s failed: %v", dir, err))
	}
	return entries, err
}

// RemoteListDraft lists a directory of an UNSAVED source (the editor's
// "browse start directory" button): dials with the form values, lists,
// disconnects. Masked secrets inherit from a same-named stored source so
// editing an existing connection without re-typing the password works.
func (a *App) RemoteListDraft(in profile.Source, dir string) ([]listing.Entry, error) {
	if in.Type == profile.TypeS3 {
		return nil, fmt.Errorf("S3 sources have no start directory")
	}
	if existing, err := a.sourceByIDOrName(in.Name); err == nil && existing.Type == in.Type {
		inheritSourceSecrets(&in, existing)
	}
	if a.ctx == nil {
		return nil, errNoContext
	}
	fs, err := remotefs.Dial(a.ctx, in)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	ctx, cancel := a.quickCtx()
	defer cancel()
	return fs.List(ctx, dir)
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

// RemoteMkdir creates a directory (and missing parents) on a non-S3
// source. Tracked as a task (Running tasks window) like its S3 twin.
func (a *App) RemoteMkdir(idOrName, dir string) (err error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return err
	}
	if remotefs.CleanPath(dir) == "/" {
		return fmt.Errorf("invalid folder name")
	}
	task := a.tasks.add("mkdir", fmt.Sprintf("%s:%s", idOrName, dir))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	err = fs.MkdirAll(ctx, dir)
	if err != nil {
		a.emitLogSrc(LogError, "mkdir", idOrName, fmt.Sprintf("mkdir %s failed: %v", dir, err))
	} else {
		a.emitLogSrc(LogInfo, "mkdir", idOrName, fmt.Sprintf("created %s", dir))
	}
	return err
}

// RemoteCreateFile creates a new empty file on a non-S3 source and
// returns its full path. Tracked as a task like its S3 twin. (There is
// no remote editor flow — the file is simply created, WinSCP's "leave it
// empty" outcome.)
func (a *App) RemoteCreateFile(idOrName, dir, name, ext string) (created string, err error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return "", err
	}
	composed := composeFileName(name, ext)
	if composed == "" || strings.Contains(composed, "/") {
		return "", fmt.Errorf("invalid file name")
	}
	full := remotefs.CleanPath(dir + "/" + composed)
	task := a.tasks.add("mkfile", fmt.Sprintf("%s:%s", idOrName, full))
	ctx := task.ctx
	defer func() { task.finish(err, false) }()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	err = fs.Create(ctx, full, strings.NewReader(""))
	if err != nil {
		a.emitLogSrc(LogError, "mkfile", idOrName, fmt.Sprintf("create %s failed: %v", full, err))
	} else {
		a.emitLogSrc(LogInfo, "mkfile", idOrName, fmt.Sprintf("created %s", full))
	}
	return full, err
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
		a.emitLogSrc(LogError, "rename", idOrName, fmt.Sprintf("rename %s failed: %v", cleaned, err))
		return err
	}
	a.emitLogSrc(LogInfo, "rename", idOrName, fmt.Sprintf("renamed %s to %s", cleaned, target))
	return nil
}

// RemoteDeletePreview is the count-then-act half of remote deletes
// (safety ladder): it walks every listed path (file or directory tree) and
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
// versions — deletion is permanent, like `rm -rf`). Tracked as a task:
// tree deletes over SFTP/FTP are serial and can run long — the row shows
// per-path progress and a working Cancel.
func (a *App) RemoteRemove(idOrName string, paths []string) (out *RemoteDeleteResult, err error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return nil, err
	}
	out = &RemoteDeleteResult{}
	task := a.tasks.add("delete", fmt.Sprintf("%s — %d selected item(s)", idOrName, len(paths)))
	ctx := task.ctx
	task.setTotal(len(paths), "")
	defer func() { task.finish(err, false) }()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	done := 0
	for _, p := range paths {
		if ctx.Err() != nil {
			return out, ctx.Err() // killed from Running tasks: stop between paths
		}
		cleaned := remotefs.CleanPath(p)
		task.setCurrent(cleaned)
		if cleaned == "/" {
			out.Errors = append(out.Errors, "refusing to delete the source root")
			continue
		}
		if err := fs.Remove(ctx, cleaned); err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", cleaned, err))
			a.emitLogSrc(LogError, "delete", idOrName, fmt.Sprintf("delete %s failed: %v", cleaned, err))
			continue
		}
		out.Deleted++
		done++
		task.progress(done)
	}
	if out.Deleted > 0 {
		a.emitLogSrc(LogInfo, "delete", idOrName, fmt.Sprintf("deleted %d item(s)", out.Deleted))
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
	ctx, cancel := a.quickCtx() // the Settings → Network listing timeout bounds the probe
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

package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// LocalEntry is one row of the local (dual-pane) grid.
type LocalEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix millis
	Created int64  `json:"created"` // unix millis, 0 where the platform has no birth time
	Mode    string `json:"mode"`    // unix-style mode string, e.g. "drwxr-xr-x"
}

// The dual-pane side's point primitives; vars so the wedge tests can park
// one (the openInEditor discipline). Every local syscall this file makes
// rides one of them through remotefs.LocalStep — the local wire's share
// of the silent-death cure: a root whose backing vanished (dead UNC path,
// disconnected mapped drive) parks the syscall until the OS gives up, and
// no caller context can reach inside a syscall to stop it, so the wait is
// bounded by abandonment instead (see remotefs/localbudget.go). Each
// caller captures its seam into a local BEFORE the step goroutine spawns:
// an abandoned step can outlive its caller until the OS itself gives up,
// and the only reads of these vars live on the spawning goroutine — the
// retire-grace discipline remotefs.LocalOpBudget already follows.
var (
	localStat    = os.Stat
	localReadDir = os.ReadDir
)

// localEntryInfo is one row of a bounded local walk: everything the walk
// callers need, materialized INSIDE the step so no per-entry syscall can
// park the walker after the budget was paid.
type localEntryInfo struct {
	rel   string // slash-separated, relative to the walk root
	isDir bool
	size  int64
	mtime int64 // unix millis
}

// walkLocalTree walks root's contents depth-first (lexical order within
// each directory, WalkDir's order), calling fn for every entry below
// root, never for root itself. Each directory is one bounded step — the
// read plus the per-entry info calls — so a vanished backing costs one
// budget instead of the life of the process; an unreadable directory is
// skipped with its whole subtree (the first such error is returned, not
// swallowed — callers that don't care ignore it, usage's Partial
// marking does), WalkDir's contract with this code's count-and-compare
// callers.
func walkLocalTree(ctx context.Context, root string, fn func(localEntryInfo)) error {
	return walkLocal(ctx, root, fn, false)
}

// walkLocalStrict is walkLocalTree with WalkDir's other contract: the
// first unreadable directory aborts the walk with its error — for
// callers whose output must be complete or nothing (upload and transfer
// expansion: a silently skipped file is a silently missing upload).
func walkLocalStrict(ctx context.Context, root string, fn func(localEntryInfo)) error {
	return walkLocal(ctx, root, fn, true)
}

func walkLocal(ctx context.Context, root string, fn func(localEntryInfo), abort bool) error {
	type dirRef struct {
		abs string
		rel string // "" for the root itself
	}
	readDir := localReadDir // captured before any step spawns (seam discipline)
	stack := []dirRef{{abs: root}}
	var firstErr error
	for len(stack) > 0 {
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		infos, err := remotefs.LocalStep(ctx, func() ([]localEntryInfo, error) {
			dirEntries, err := readDir(d.abs)
			if err != nil {
				return nil, err
			}
			infos := make([]localEntryInfo, 0, len(dirEntries))
			for _, e := range dirEntries {
				info, err := e.Info()
				if err != nil {
					continue // raced with a delete; not worth failing the walk
				}
				rel := e.Name()
				if d.rel != "" {
					rel = d.rel + "/" + e.Name()
				}
				infos = append(infos, localEntryInfo{
					rel:   rel,
					isDir: e.IsDir(),
					size:  info.Size(),
					mtime: info.ModTime().UnixMilli(),
				})
			}
			return infos, nil
		})
		if err != nil {
			if abort {
				return err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue // unreadable directory: skip it and its subtree
		}
		// push subdirectories in reverse so they pop in lexical order
		for i := len(infos) - 1; i >= 0; i-- {
			if infos[i].isDir {
				stack = append(stack, dirRef{
					abs: filepath.Join(d.abs, filepath.Base(infos[i].rel)),
					rel: infos[i].rel,
				})
			}
		}
		for _, e := range infos {
			fn(e)
		}
	}
	return firstErr
}

// LocalRoots lists the roots of the local filesystem: drive letters on
// Windows, "/" elsewhere. The local pane starts at the home directory.
// The drive probe is one bounded step — a stalled removable drive must
// not park the pane's roots view either.
func (a *App) LocalRoots() []string {
	if runtime.GOOS == "windows" {
		stat := localStat // captured before the step spawns (seam discipline)
		// The slice is BUILT inside the step and returned as the verdict: an
		// abandoned step owns only its own locals, so the caller's return can
		// never race a still-running probe appending to shared state.
		drives, _ := remotefs.LocalStep(a.ctx, func() ([]string, error) {
			var drives []string
			for c := 'A'; c <= 'Z'; c++ {
				p := string(c) + `:\`
				if st, err := stat(p); err == nil && st.IsDir() {
					drives = append(drives, p)
				}
			}
			return drives, nil
		})
		return drives
	}
	return []string{"/"}
}

// LocalHome returns the user's home directory (local pane start).
func (a *App) LocalHome() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Clean(h), nil
}

// ListLocal lists one local directory: folders first, then files, by name.
// dir "" lists the home directory. The read and the per-entry info calls
// are one bounded step — the pane navigates by verdict, never by park.
func (a *App) ListLocal(dir string) ([]LocalEntry, error) {
	if dir == "" || dir == "~" {
		home, err := a.LocalHome()
		if err != nil {
			return nil, err
		}
		dir = home
	}
	dir = filepath.Clean(dir)
	readDir := localReadDir // captured before the step spawns (seam discipline)
	out, err := remotefs.LocalStep(a.ctx, func() ([]LocalEntry, error) {
		ents, err := readDir(dir)
		if err != nil {
			return nil, err
		}
		out := make([]LocalEntry, 0, len(ents))
		for _, e := range ents {
			full := filepath.Join(dir, e.Name())
			info, err := e.Info()
			if err != nil {
				continue // racing delete: skip
			}
			le := LocalEntry{
				Name:    e.Name(),
				Path:    full,
				IsDir:   e.IsDir(),
				Size:    info.Size(),
				ModTime: info.ModTime().UnixMilli(),
				Mode:    info.Mode().String(),
			}
			if t, ok := remotefs.CreationTimeOf(info); ok {
				le.Created = t.UnixMilli()
			}
			out = append(out, le)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// LocalParent returns the parent directory of dir (or "" at a filesystem
// root — the frontend then shows the roots list).
func (a *App) LocalParent(dir string) string {
	p := filepath.Dir(filepath.Clean(dir))
	if p == filepath.Clean(dir) {
		return "" // root reached
	}
	return p
}

// OpenLocal opens a file or folder with the OS default application
// (reveal in Explorer when a folder).
func (a *App) OpenLocal(path string) error {
	stat := localStat // captured before the step spawns (seam discipline)
	st, err := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return stat(path) })
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if st.IsDir() {
			cmd = exec.Command("explorer", path)
		} else {
			cmd = exec.Command("cmd", "/c", "start", "", path)
		}
	case "darwin":
		if st.IsDir() {
			cmd = exec.Command("open", path)
		} else {
			cmd = exec.Command("open", "-R", path)
		}
	default: // linux/bsd
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenLocalWith opens the OS application chooser for a file ("Open
// with…") so the user picks which app handles it. Native choosers exist
// on Windows (shell32's OpenAs dialog) and macOS (AppleScript's choose
// application); Linux has no standard chooser, so the default app opens
// there instead.
func (a *App) OpenLocalWith(path string) error {
	stat := localStat // captured before the step spawns (seam discipline)
	st, err := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return stat(path) })
	if err != nil {
		return err
	}
	if st.IsDir() {
		return a.OpenLocal(path) // folders have no "open with" semantics
	}
	switch runtime.GOOS {
	case "windows":
		// "How do you want to open this file?" — app list + More apps;
		// rundll32 owns the dialog and launches the picked app itself.
		return exec.Command("rundll32", "shell32.dll,OpenAs_RunDLL", path).Start()
	case "darwin":
		return openWithChoose(path)
	default:
		return a.OpenLocal(path)
	}
}

// openWithChoose asks for an application via AppleScript and opens the
// file with it. Blocking is fine: bindings run on their own goroutine, and
// a cancel simply returns the osascript error.
func openWithChoose(path string) error {
	out, err := exec.Command("osascript", "-e",
		`choose application with prompt "Choose the application to edit with" as string`).Output()
	if err != nil {
		return err // user canceled — nothing opened
	}
	app := strings.TrimSpace(string(out))
	if app == "" {
		return errors.New("no application chosen")
	}
	return exec.Command("open", "-a", app, path).Start()
}

// OpenTerminal opens a new terminal window at dir (local-pane context
// menu). The GUI process has no console of its own (Windows: detached at
// startup), so a console child gets a fresh window of its own.
func (a *App) OpenTerminal(dir string) error {
	stat := localStat // captured before the step spawns (seam discipline)
	st, err := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return stat(dir) })
	if err != nil {
		return err
	}
	if !st.IsDir() {
		dir = filepath.Dir(dir)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// /k keeps the window open; the quotes survive Go's argument
		// escaping and handle paths with spaces.
		cmd = exec.Command("cmd", "/k", `cd /d "`+dir+`"`)
	case "darwin":
		cmd = exec.Command("open", "-a", "Terminal", dir)
	default:
		cmd = linuxTerminal(dir)
		if cmd == nil {
			return fmt.Errorf("no terminal emulator found (tried x-terminal-emulator, gnome-terminal, konsole, xfce4-terminal)")
		}
	}
	return cmd.Start()
}

// linuxTerminal picks the first available common terminal emulator.
func linuxTerminal(dir string) *exec.Cmd {
	for _, spec := range [][2]string{
		{"x-terminal-emulator", "--working-directory"},
		{"gnome-terminal", "--working-directory"},
		{"konsole", "--workdir"},
		{"xfce4-terminal", "--working-directory"},
	} {
		if _, err := exec.LookPath(spec[0]); err == nil {
			return exec.Command(spec[0], spec[1], dir)
		}
	}
	return nil
}

// ---------------- local delete (Delete Window, count-then-act) ----------------

// LocalDeletePreview expands a local selection (files and whole directory
// trees) and reports what a delete would remove — the local half of the
// Delete Window's count-then-act contract. Roots are refused outright.
// Every step is bounded (one budget per directory), so a vanished backing
// fails the preview in bounded time instead of parking the window.
func (a *App) LocalDeletePreview(paths []string) (DeletePreview, error) {
	var p DeletePreview
	stat := localStat // captured before any step spawns (seam discipline)
	for _, root := range paths {
		if isFsRoot(root) {
			return DeletePreview{}, errors.New("refusing to delete a filesystem root")
		}
		st, err := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return stat(root) })
		if err != nil {
			return DeletePreview{}, err
		}
		if !st.IsDir() {
			p.Objects++
			p.Count++
			p.Bytes += st.Size()
			continue
		}
		p.Folders++
		_ = walkLocalTree(a.ctx, root, func(e localEntryInfo) {
			if e.isDir {
				p.Folders++
			} else {
				p.Objects++
				p.Count++
				p.Bytes += e.size
			}
		})
	}
	p.RequiresL1 = p.Count > 0
	p.RequiresL2 = p.Count > deleteForceThreshold
	return p, nil
}

// LocalRemove deletes the listed local paths (files or whole directory
// trees). Deletion is permanent — the OS trash is not involved; the GUI
// previews with LocalDeletePreview and confirms first (typed confirmation
// above the L2 threshold, same ladder as S3). Like the S3 delete path, the
// count is re-taken HERE at act time and gated server-side: more than
// deleteForceThreshold files refuses without force, so a stale preview (or
// any caller that skipped it) can never silently wipe a large tree. The
// re-count is per-step bounded; the act itself rides the OS's own budget —
// a tree's delete time is real work, not a dead peer, and its preview (the
// gate in front of it) already failed in bounded time on a vanished
// backing.
func (a *App) LocalRemove(paths []string, force bool) (transfer.DeleteResult, error) {
	var out transfer.DeleteResult
	stat := localStat // captured before any step spawns (seam discipline)
	// Server-side re-count (the preview may be stale by the time the user
	// confirms), mirroring RemoteRemove: skipped entirely once force is
	// presented, and refused roots are never walked — a root operand must
	// fail fast, not after a full-drive walk that only ends in refusal.
	if !force {
		count := 0
		for _, root := range paths {
			if isFsRoot(root) {
				continue // the act loop below reports the root refusal
			}
			st, err := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return stat(root) })
			if err != nil {
				continue // the act loop below reports missing paths honestly
			}
			if !st.IsDir() {
				count++
				continue
			}
			_ = walkLocalTree(a.ctx, root, func(e localEntryInfo) {
				if !e.isDir {
					count++
				}
			})
		}
		if count > deleteForceThreshold {
			return out, fmt.Errorf(
				"%d file(s) selected — typed confirmation (force) required", count)
		}
	}
	for _, root := range paths {
		if isFsRoot(root) {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: refusing to delete a filesystem root", root))
			continue
		}
		st, serr := remotefs.LocalStep(a.ctx, func() (os.FileInfo, error) { return stat(root) })
		var err error
		switch {
		case serr == nil && st.IsDir():
			err = os.RemoveAll(root) // real work, uncapped by design (comment above)
		case errors.Is(serr, remotefs.ErrLocalDeadline):
			// a wedged root reports the wedge — falling through to a
			// remove of the same path would just park inside it
			err = serr
		default:
			// a file, or a stat that missed — os.Remove reports the same
			// truth the stat did, bounded like every point act
			_, err = remotefs.LocalStep(a.ctx, func() (struct{}, error) {
				return struct{}{}, os.Remove(root)
			})
		}
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", root, err))
			a.emitLogSrc(LogError, "delete", "local", fmt.Sprintf("deleting %s failed: %v", root, err))
			continue
		}
		out.Deleted++
	}
	if len(out.Errors) == 0 {
		a.emitLogSrc(LogWarn, "delete", "local", fmt.Sprintf("deleted %d item(s)", out.Deleted))
	}
	return out, nil
}

// isFsRoot reports whether p is a filesystem root (drive root or "/"),
// which local deletes must never touch. Drive roots are recognized BY
// SHAPE on every platform: filepath.VolumeName knows volumes only on
// Windows, but a pasted C:\ must meet the same fast policy refusal on a
// mac or linux workstation as it does at home — the gate decides before
// the OS is ever asked.
func isFsRoot(p string) bool {
	if p == "" || p == "/" || p == "\\" {
		return true
	}
	if len(p) >= 2 && isDriveLetter(p[0]) && p[1] == ':' &&
		(len(p) == 2 || (len(p) == 3 && (p[2] == '\\' || p[2] == '/'))) {
		return true // a drive root (or a bare drive's current dir) — refused everywhere
	}
	vol := filepath.VolumeName(filepath.Clean(p))
	rest := strings.TrimPrefix(filepath.Clean(p), vol)
	return vol != "" && (rest == "" || rest == string(filepath.Separator))
}

// ---------------- directory compare (WinSCP-style keep in sync) ----------------

// CompareStatus classifies one row of a pane-to-pane comparison. The names
// are historical (the original compare was local-dir vs S3-prefix); the
// generalized driver in compare.go uses them for the left/right sides and
// the summary dialog labels the sides for the user.
const (
	CmpOnlyLocal   = "only-local" // only on the left (x) side
	CmpOnlyRemote  = "only-remote"
	CmpSame        = "same"
	CmpNewerLocal  = "newer-local"
	CmpNewerRemote = "newer-remote"
	CmpSizeDiff    = "size-diff"
)

// CompareRow is one compared path (relative to the compared dir/prefix);
// Local* fields are the left side, Remote* the right side.
type CompareRow struct {
	Key         string `json:"key"` // slash-separated relative path
	Status      string `json:"status"`
	LocalSize   int64  `json:"localSize,omitempty"`
	RemoteSize  int64  `json:"remoteSize,omitempty"`
	LocalMtime  int64  `json:"localMtime,omitempty"`  // unix millis
	RemoteMtime int64  `json:"remoteMtime,omitempty"` // unix millis
}

// localFile describes one file found during a local walk.
type localFile struct {
	size  int64
	mtime int64
}

// walkLocalFiles collects files under dir recursively (relative keys),
// one bounded step per directory — the compare walk navigates by verdict
// too.
func walkLocalFiles(ctx context.Context, dir string) (map[string]localFile, error) {
	out := map[string]localFile{}
	_ = walkLocalTree(ctx, dir, func(e localEntryInfo) {
		if !e.isDir {
			out[e.rel] = localFile{size: e.size, mtime: e.mtime}
		}
	})
	return out, nil
}

// CompareDir compares a local directory against a bucket prefix — the
// original dual-pane compare, now a thin wrapper over the generalized
// CompareAny driver (any local/remote/S3 pair).
func (a *App) CompareDir(localDir, bucket, prefix string) ([]CompareRow, error) {
	return a.CompareAny(
		CompareRef{Kind: "local", Dir: localDir},
		CompareRef{Kind: "s3", Bucket: bucket, Prefix: prefix},
	)
}

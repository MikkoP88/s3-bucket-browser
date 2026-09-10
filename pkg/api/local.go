package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// LocalEntry is one row of the local (dual-pane) grid.
type LocalEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix millis
}

// LocalRoots lists the roots of the local filesystem: drive letters on
// Windows, "/" elsewhere. The local pane starts at the home directory.
func (a *App) LocalRoots() []string {
	if runtime.GOOS == "windows" {
		var drives []string
		for c := 'A'; c <= 'Z'; c++ {
			p := string(c) + `:\`
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				drives = append(drives, p)
			}
		}
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
// dir "" lists the home directory.
func (a *App) ListLocal(dir string) ([]LocalEntry, error) {
	if dir == "" || dir == "~" {
		home, err := a.LocalHome()
		if err != nil {
			return nil, err
		}
		dir = home
	}
	dir = filepath.Clean(dir)
	ents, err := os.ReadDir(dir)
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
		out = append(out, LocalEntry{
			Name:    e.Name(),
			Path:    full,
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
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
	st, err := os.Stat(path)
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

// OpenTerminal opens a new terminal window at dir (local-pane context
// menu). The GUI process has no console of its own (Windows: detached at
// startup), so a console child gets a fresh window of its own.
func (a *App) OpenTerminal(dir string) error {
	st, err := os.Stat(dir)
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

// walkLocalFiles collects files under dir recursively (relative keys).
func walkLocalFiles(dir string) (map[string]localFile, error) {
	out := map[string]localFile{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = localFile{size: info.Size(), mtime: info.ModTime().UnixMilli()}
		return nil
	})
	return out, err
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

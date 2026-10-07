package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// EventEditorSaved fires when an edited file was uploaded back (payload:
// {bucket, key}).
const EventEditorSaved = "editor:saved"

// EventEditorPushFailed fires when a watched edit could not be uploaded
// back — once per failing streak, not per attempt (payload:
// {bucket, key, error}). The session stays dirty and retries with backoff
// until a push lands.
const EventEditorPushFailed = "editor:push-failed"

// editSession tracks one file opened in an external editor.
type editSession struct {
	Bucket   string
	Key      string
	Local    string
	mu       sync.Mutex
	origSize int64
	origMod  int64
	lastSize int64
	lastMod  int64
	dirty    bool       // changed since last upload
	fails    int        // consecutive push failures (drives the backoff)
	nextTry  time.Time  // earliest retry after a failure
	done     bool
}

// EditInfo is the status view of an edit session.
type EditInfo struct {
	Bucket     string `json:"bucket"`
	Key        string `json:"key"`
	Local      string `json:"local"`
	Dirty      bool   `json:"dirty"`
	PushFailed bool   `json:"pushFailed"` // last push failed; retrying with backoff
}

// editDir is the temp workspace for edited objects: the config dir
// (0700, secure.go) under secure storage, else the system temp dir.
func editDir(bucket string) string {
	return filepath.Join(workspaceBase("edit"), bucket)
}

// watcherPoll is the file-watch interval; a change is uploaded after it
// stays stable for two consecutive polls (editor save jitters). A var so
// tests can shorten the cadence.
var watcherPoll = 1200 * time.Millisecond

// editPushBackoff spaces out retries of a failing push: one poll's grace
// at first, doubling per consecutive failure and capped at sixteen — a
// dead endpoint is retried forever, but gently (~19s at the cap with the
// shipped cadence).
func editPushBackoff(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	if fails > 4 {
		fails = 4
	}
	return watcherPoll * time.Duration(1<<fails)
}

// openInEditor launches the OS editor for the staged file (or, chooseApp,
// the OS "Open with…" picker). A var so tests can pin the pull's task
// lifecycle without spawning real editors on the test machine.
var openInEditor = func(a *App, path string, chooseApp bool) error {
	if chooseApp {
		return a.OpenLocalWith(path)
	}
	return a.OpenLocal(path)
}

// EditObject downloads bucket/key into a temp workspace, opens it with
// the OS default editor (or, chooseApp=true, the OS "Open with…" picker
// so the user selects the editing application per file) and keeps
// watching: every saved change is uploaded back automatically
// (WinSCP-style "keep remote up to date").
func (a *App) EditObject(bucket, key string, chooseApp bool) (EditInfo, error) {
	c, err := a.client("")
	if err != nil {
		return EditInfo{}, err
	}
	dir := editDir(bucket)
	if err := os.MkdirAll(dir, 0o700); err != nil { // owner-only even on shared /tmp
		return EditInfo{}, err
	}
	local := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(key, "/")))
	if st, err := os.Stat(local); err == nil && st.IsDir() {
		return EditInfo{}, fmt.Errorf("%s is a folder", key)
	}

	// engine tuning (Settings → Transfers) applies to editor pulls too
	partSize, conc := a.partTunables()
	// The pull rides the task registry (kind "edit"): a transient row —
	// visible with a Cancel in the Running tasks window while it runs,
	// gone when it lands, the status-bar editor indicator being the
	// session's lasting surface. Before this the pull was an invisible,
	// uncancellable bound call: a multi-gigabyte object just spun, and
	// the exit gate's task check now covers a quit mid-pull too.
	task := a.tasks.add("edit", fmt.Sprintf("s3://%s/%s", bucket, key))
	task.setTotal(1, "")
	// Idempotent backstop: no-op when the explicit finishes below already
	// ran — and it still settles the row if a panic unwinds through here.
	defer task.finish(nil, true)
	if err := transfer.DownloadFile(task.ctx, c.S3, bucket, key, local,
		transfer.DownloadOptions{PartSize: partSize, Concurrency: conc}); err != nil {
		task.finish(err, true)
		return EditInfo{}, err
	}
	task.progress(1)
	_ = os.Chmod(local, 0o600) // object contents: owner-only in every mode
	st, err := os.Stat(local)
	if err != nil {
		return EditInfo{}, err
	}

	s := &editSession{
		Bucket: bucket, Key: key, Local: local,
		origSize: st.Size(), origMod: st.ModTime().UnixMilli(),
		lastSize: st.Size(), lastMod: st.ModTime().UnixMilli(),
	}
	a.editorsMu.Lock()
	a.editors[s.Bucket+"\x00"+s.Key] = s
	a.editorsMu.Unlock()

	if err := openInEditor(a, local, chooseApp); err != nil {
		return s.info(), fmt.Errorf("downloaded but could not open editor: %w", err)
	}
	go a.watchEditor(s)
	return s.info(), nil
}

// watchEditor polls the file and uploads stable changes back.
func (a *App) watchEditor(s *editSession) {
	defer a.guardWorker("app", nil) // the worker panic net (guard.go)
	t := time.NewTicker(watcherPoll)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-a.done():
			return
		}
		// StopEdit ended the session: no more auto-uploads. Without this
		// check the goroutine outlives the session and would keep pushing
		// edits the user explicitly discarded (StopEdit upload=false) to
		// the object.
		s.mu.Lock()
		done := s.done
		s.mu.Unlock()
		if done {
			return
		}
		st, err := os.Stat(s.Local)
		if err != nil {
			return // deleted: session over
		}
		size, mod := st.Size(), st.ModTime().UnixMilli()
		s.mu.Lock()
		changed := size != s.lastSize || mod != s.lastMod
		wasDirty := s.dirty
		if changed {
			s.lastSize, s.lastMod, s.dirty = size, mod, true
		}
		stableUpload := s.dirty && !changed && wasDirty && (size != s.origSize || mod != s.origMod)
		s.mu.Unlock()

		if stableUpload {
			now := time.Now()
			s.mu.Lock()
			ready := s.fails == 0 || !now.Before(s.nextTry)
			s.mu.Unlock()
			if !ready {
				continue // a failing push waits out its backoff
			}
			if err := a.uploadEdit(s); err == nil {
				s.mu.Lock()
				s.dirty = false
				s.fails = 0
				s.nextTry = time.Time{}
				s.mu.Unlock()
				a.emit(EventEditorSaved, map[string]string{"bucket": s.Bucket, "key": s.Key})
				a.emit(EventS3Changed, map[string]string{"bucket": s.Bucket})
			} else {
				// A failing push gets a voice and a gentler cadence: every
				// attempt logs, the first of a streak toasts (the session
				// stays visibly dirty — indicator and dialog carry the
				// failure — until a push lands), and the retries back off
				// instead of hammering a dead endpoint every poll.
				s.mu.Lock()
				first := s.fails == 0
				s.fails++
				wait := editPushBackoff(s.fails)
				s.nextTry = now.Add(wait)
				s.mu.Unlock()
				a.emitLog(LogError, "edit", fmt.Sprintf(
					"upload of %s/%s failed (retry in %s; edits stay pending): %v",
					s.Bucket, s.Key, wait, err))
				if first {
					a.emit(EventEditorPushFailed, map[string]string{
						"bucket": s.Bucket, "key": s.Key, "error": err.Error(),
					})
				}
			}
		}
	}
}

// uploadEdit pushes the current file content back to the object.
func (a *App) uploadEdit(s *editSession) error {
	c, err := a.client("")
	if err != nil {
		return err
	}
	// Shutdown stops an in-flight push through the app context; the exit
	// gate already refuses a quit while a session is dirty, so a push
	// canceled here is an explicitly confirmed discard.
	ctx := context.Background()
	if a.ctx != nil {
		ctx = a.ctx
	}
	// engine tuning (Settings → Transfers) applies to editor pushes too
	partSize, conc := a.partTunables()
	return transfer.UploadFile(ctx, c.S3, s.Local, s.Bucket, s.Key,
		transfer.UploadOptions{PartSize: partSize, Concurrency: conc})
}

// done signals app shutdown (nil ctx before Startup = never).
func (a *App) done() <-chan struct{} {
	if a.ctx == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return a.ctx.Done()
}

// info snapshots a session (locks appropriately).
func (s *editSession) info() EditInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return EditInfo{
		Bucket: s.Bucket, Key: s.Key, Local: s.Local,
		Dirty: s.dirty, PushFailed: s.fails > 0,
	}
}

// EditingFiles lists open edit sessions (status bar indicator).
func (a *App) EditingFiles() []EditInfo {
	a.editorsMu.Lock()
	defer a.editorsMu.Unlock()
	out := make([]EditInfo, 0, len(a.editors))
	for _, s := range a.editors {
		out = append(out, s.info())
	}
	return out
}

// StopEdit ends a session; upload=true pushes pending changes first.
func (a *App) StopEdit(bucket, key string, upload bool) error {
	a.editorsMu.Lock()
	s := a.editors[bucket+"\x00"+key]
	delete(a.editors, bucket+"\x00"+key)
	a.editorsMu.Unlock()
	if s == nil {
		return fmt.Errorf("not being edited: %s", key)
	}
	s.mu.Lock()
	s.done = true
	// The watcher only notices changes on its poll (1.2s cadence); an
	// explicit save right after an edit must not depend on that timing.
	// Re-check the staged file against the last state the app knew, so
	// "save and close" pushes what is on disk while an untouched file
	// still costs no upload (and no extra object version).
	if upload {
		if st, err := os.Stat(s.Local); err == nil {
			if st.Size() != s.lastSize || st.ModTime().UnixMilli() != s.lastMod {
				s.dirty = true
			}
		}
	}
	dirty := s.dirty
	s.mu.Unlock()
	if upload && dirty {
		if err := a.uploadEdit(s); err != nil {
			return err
		}
		a.emit(EventS3Changed, map[string]string{"bucket": bucket})
	}
	return nil
}

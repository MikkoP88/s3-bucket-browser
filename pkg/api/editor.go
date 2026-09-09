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
	dirty    bool // changed since last upload
	done     bool
}

// EditInfo is the status view of an edit session.
type EditInfo struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Local  string `json:"local"`
	Dirty  bool   `json:"dirty"`
}

// editDir is the temp workspace for edited objects.
func editDir(bucket string) string {
	return filepath.Join(os.TempDir(), "s3b-edit", bucket)
}

// watcherPoll is the file-watch interval; a change is uploaded after it
// stays stable for two consecutive polls (editor save jitters).
const watcherPoll = 1200 * time.Millisecond

// EditObject downloads bucket/key into a temp workspace, opens it with the
// OS default editor and keeps watching: every saved change is uploaded back
// automatically (WinSCP-style "keep remote up to date").
func (a *App) EditObject(bucket, key string) (EditInfo, error) {
	c, err := a.client("")
	if err != nil {
		return EditInfo{}, err
	}
	dir := editDir(bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return EditInfo{}, err
	}
	local := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(key, "/")))
	if st, err := os.Stat(local); err == nil && st.IsDir() {
		return EditInfo{}, fmt.Errorf("%s is a folder", key)
	}

	if err := transfer.DownloadFile(context.Background(), c.S3, bucket, key, local,
		transfer.DownloadOptions{}); err != nil {
		return EditInfo{}, err
	}
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

	if err := a.OpenLocal(local); err != nil {
		return s.info(), fmt.Errorf("downloaded but could not open editor: %w", err)
	}
	go a.watchEditor(s)
	return s.info(), nil
}

// watchEditor polls the file and uploads stable changes back.
func (a *App) watchEditor(s *editSession) {
	t := time.NewTicker(watcherPoll)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-a.done():
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
			if err := a.uploadEdit(s); err == nil {
				s.mu.Lock()
				s.dirty = false
				s.mu.Unlock()
				a.emit(EventEditorSaved, map[string]string{"bucket": s.Bucket, "key": s.Key})
				a.emit(EventS3Changed, map[string]string{"bucket": s.Bucket})
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
	return transfer.UploadFile(context.Background(), c.S3, s.Local, s.Bucket, s.Key,
		transfer.UploadOptions{})
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
	return EditInfo{Bucket: s.Bucket, Key: s.Key, Local: s.Local, Dirty: s.dirty}
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

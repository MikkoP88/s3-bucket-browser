package api

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Native drag-out (Electron-style). The browser path (dragout.go) attaches
// DownloadURL / text-uri-list data to the DOM drag — a Chromium
// browser-process feature WebView2 does not implement, so drags leaving the
// desktop app never carry files and Explorer refuses the drop. The desktop
// path instead stages the selection through the transfer engine (exactly
// like the OS clipboard mirror, osCopyRemote) and floats a native OLE drag
// whose IDataObject offers CF_HDROP delay-rendered: the cursor moves at
// once and a drop resolves only after the bytes exist. DoDragDrop needs a
// windowed STA process, so the browser/server builds keep the loopback-URL
// path and dragOutRun reports the gesture unsupported there.

// The OS drag envelope mirrors the OS clipboard mirror: a stray gesture
// must not stage gigabytes nobody asked to drop.
const (
	maxDragOutItems = 500
	maxDragOutBytes = 256 << 20
	// dragStageTimeout bounds the staging download a drop may wait on
	// (GetData blocks inside the delay-render window) and the abandoned-
	// drag cleanup.
	dragStageTimeout = 2 * time.Minute
)

// dragOutGesture is set while a native drag runs — the GUI suppresses its
// own file-drop handling for it (a native drag crossing back over the app
// window must not re-import the staged files as an upload).
var dragOutGesture atomic.Bool

// errDragOutBusy guards the OLE modal loop: one mouse, one drag.
var errDragOutBusy = errors.New("a drag-out is already in progress")

// DragOutActive reports whether a native drag-out gesture is running.
func (a *App) DragOutActive() bool { return dragOutGesture.Load() }

// DragOutFiles floats items (files only) as a native OS drag. Staging
// starts immediately; the drag offers the staged paths delay-rendered, so
// the gesture begins before the download finishes and blocks a drop until
// it has. The call resolves when the gesture ends (drop, cancel, escape).
func (a *App) DragOutFiles(items []DragItem) error {
	if len(items) == 0 {
		return errors.New("no items to drag")
	}
	if len(items) > maxDragOutItems {
		return fmt.Errorf("drag-out is limited to %d files", maxDragOutItems)
	}
	var total int64
	for _, it := range items {
		if it.Key == "" {
			return errors.New("drag item missing key")
		}
		if it.Size > 0 {
			total += it.Size
		}
	}
	if total > maxDragOutBytes {
		return fmt.Errorf("drag-out is limited to %d MB", maxDragOutBytes>>20)
	}
	return a.dragOutRun(items)
}

// stagedPaths maps drag items to their staged locations. The transfer
// planner flattens each top-level file to dest-dir/leaf-of-key — the same
// layout the OS clipboard mirror relies on (osCopyRemote).
func stagedPaths(dir string, items []DragItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = filepath.Join(dir, path.Base(strings.TrimSuffix(it.Key, "/")))
	}
	return out
}

// dragStage is the staging lifecycle of one native drag: the transfer job
// downloads the selection while the OLE loop runs, and the data object's
// GetData waits on done so a drop resolves only after the transfer.
type dragStage struct {
	a    *App
	mu   sync.Mutex
	job  string // transfer job id (empty until registered)
	done chan struct{}

	// Written before close(done), read only after it.
	paths  []string
	failed error
}

// startDragStage kicks off the staging download: a fresh clip dir, the
// transfer engine, overwrite policy — byte-for-byte the OS clipboard
// mirror's recipe.
func (a *App) startDragStage(items []DragItem) *dragStage {
	st := &dragStage{a: a, done: make(chan struct{})}
	go func() {
		defer close(st.done)
		dir, err := a.StageClipboardDir()
		if err != nil {
			st.failed = err
			return
		}
		xis := make([]XferItem, len(items))
		for i, it := range items {
			xis[i] = XferItem{Source: it.Source, Bucket: it.Bucket, Key: it.Key, Size: it.Size}
		}
		id, err := a.TransferCross(xis, nil, XferDest{Kind: "local", Dir: dir}, PolicyOverwrite, 0, false, nil, true)
		if err != nil {
			st.failed = err
			return
		}
		st.mu.Lock()
		st.job = id
		st.mu.Unlock()
		deadline := time.Now().Add(dragStageTimeout)
		for {
			var status string
			found := false
			for _, ji := range a.ActiveTransfers() {
				if ji.ID == id {
					found, status = true, ji.Status
					break
				}
			}
			switch {
			case !found: // registered before TransferCross returns — defensive
				time.Sleep(50 * time.Millisecond)
			case status != JobRunning:
				if status != JobDone {
					st.failed = fmt.Errorf("staging job %s", status)
				}
				st.paths = stagedPaths(dir, items)
				return
			case time.Now().After(deadline):
				st.failed = errors.New("staging timed out")
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return st
}

// wait blocks until the staging download settles (dragStageTimeout grace
// over the job's own deadline) and reports whether the files exist.
func (st *dragStage) wait() error {
	select {
	case <-st.done:
		return st.failed
	case <-time.After(dragStageTimeout + 5*time.Second):
		return errors.New("staging timed out")
	}
}

// cancel aborts the staging transfer of an abandoned drag (no-op when it
// already finished, failed, or never started).
func (st *dragStage) cancel() {
	st.mu.Lock()
	id := st.job
	st.mu.Unlock()
	if id != "" {
		st.a.CancelTransfer(id)
	}
}

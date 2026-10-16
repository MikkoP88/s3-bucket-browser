package api

import (
	"errors"
	"sync/atomic"
)

// ErrDialogOpen is the one-pickers-at-a-time refusal: a native OS file
// dialog is already up in this app instance.
var ErrDialogOpen = errors.New("a file dialog is already open in this window — close it before opening another")

// dialogBusy gates every native OS file dialog this app instance opens
// (file pickers, folder pickers, profile open/save). One at a time is the
// law the native dialogs themselves assume — a second picker stacked over
// a first grabs the foreground and lets a user's clicks land in the wrong
// dialog, picking the wrong files into the wrong verb. The gate is
// process-wide, not per-App: a window is a process, and every picker
// funnels through the shell.
//
// The scripted seams (pickerSeams, profileDialogs) sit BEFORE the gate on
// purpose: rigs and the server build have no native dialog to protect,
// and their seams must stay freely callable.
var dialogBusy atomic.Bool

// withDialogGate runs fn under the one-dialog law. A second caller while
// one dialog is up is refused fast with ErrDialogOpen; the flag drops the
// moment fn returns, error or not.
func withDialogGate[T any](fn func() (T, error)) (T, error) {
	if !dialogBusy.CompareAndSwap(false, true) {
		var zero T
		return zero, ErrDialogOpen
	}
	defer dialogBusy.Store(false)
	return fn()
}

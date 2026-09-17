//go:build !windows

package api

import "errors"

// dragOutRun is the non-Windows drag-out: the OLE gesture is Windows-only
// (the desktop builds ship there), so the browser/server loopback-URL path
// (dragout.go) remains the only OS drag this process offers.
func (a *App) dragOutRun(items []DragItem) error {
	return errors.New("native drag-out is Windows-only")
}

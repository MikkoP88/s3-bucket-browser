// readonly.go: the read-only source law. A source saved with ReadOnly is
// mounted for reading only — every write verb on the App refuses before
// it dials, whatever seat the write comes from: the main view (the
// client("") resolution), a named source (the Source* verbs and the pane
// bindings), a transfer destination, an editor push, a version restore
// or an admin-panel save. The gate answers without touching the network,
// with a typed error the frontend can name. The GUI's own affordances
// (toolbars, menus, dialog buttons) grey out from the same flag via
// ListSources — Public() passes bools through — so an honest UI and the
// wire-level gate always agree.
package api

import (
	"errors"
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// ErrReadOnlySource is returned by every write verb aimed at a source
// mounted read-only — before any engine or wire work begins.
var ErrReadOnlySource = errors.New("data source is read-only")

// gateSource resolves the source a verb would write through, using the
// exact resolution client()/remoteSource() speak: "" names the current
// view source, falling back to the single-S3-source law (one S3 source
// in the workspace resolves even unnamed). ok is false when nothing
// resolves — the verb will fail at its own dial with the sharper error,
// so the gate stays quiet.
func (a *App) gateSource(idOrName string) (profile.Source, bool) {
	if idOrName == "" {
		idOrName = a.currentViewSource()
	}
	if idOrName != "" {
		if src, ok := a.containerS3Source(idOrName); ok {
			return src, true
		}
		if src, ok := a.sessionS3Source(idOrName); ok {
			return src, true
		}
		if src, err := a.sourceByIDOrName(idOrName); err == nil {
			return src, true
		}
		return profile.Source{}, false
	}
	// The client("") fallback: with no view source named, exactly one S3
	// source in the open container or the session resolves implicitly —
	// the gate must resolve the same source the dial would, or a lone
	// read-only source would be missed.
	if src, ok := a.containerS3Source(""); ok {
		return src, true
	}
	if src, ok := a.sessionS3Source(""); ok {
		return src, true
	}
	return profile.Source{}, false
}

// requireWritable refuses when the source the verb would write through is
// mounted read-only. Call it at the very top of every write verb, before
// any lock is taken or any dial begins (containerS3Source takes pfMu,
// which the app only ever acquires outside a.mu).
func (a *App) requireWritable(idOrName string) error {
	if src, ok := a.gateSource(idOrName); ok && src.ReadOnly {
		return fmt.Errorf("%w: %q — turn read-only off on the data source to write", ErrReadOnlySource, src.Name)
	}
	return nil
}

// requireWritableName is requireWritable for callers that already hold
// the source's canonical name (transfer sides resolved by id-or-name).
func (a *App) requireWritableName(name string) error {
	if name == "" {
		return a.requireWritable("")
	}
	if src, ok := a.gateSource(name); ok && src.ReadOnly {
		return fmt.Errorf("%w: %q — turn read-only off on the data source to write", ErrReadOnlySource, src.Name)
	}
	return nil
}

package api

import (
	"errors"
	"sync/atomic"
)

// errNoContext guards method calls that arrive before the GUI started the
// service (Startup captures the application context).
var errNoContext = errors.New("application not started")

// testNoEvents mutes the event bus in tests: background job goroutines read
// it after their test has returned, while the next test's newTestApp writes
// it (caught by -race in CI). Atomic for exactly that cross-test access.
var testNoEvents atomic.Bool

// eventSink, when set, receives every GUI event in place of the desktop
// shell. The live GUI harness (tools/gui-live) bridges events to a real
// browser this way; the desktop app never sets it. Must be set before
// Startup.
var eventSink func(event string, data ...any)

// SetEventSink redirects all GUI events to fn (nil restores the desktop
// shell). Only tools/gui-live sets this.
func SetEventSink(fn func(event string, data ...any)) { eventSink = fn }

// emitEvent forwards to the desktop shell's event bus (gui.go installs it);
// with neither sink nor shell (pure unit tests) events drop silently.
func emitEvent(event string, data ...any) {
	if testNoEvents.Load() {
		return
	}
	if eventSink != nil {
		eventSink(event, data...)
		return
	}
	if shell != nil {
		shell.Emit(event, data...)
	}
}

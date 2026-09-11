package api

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errNoContext guards method calls that arrive before wails.Run fired Startup.
var errNoContext = errors.New("application not started")

// testNoEvents mutes the Wails event bus in tests: EventsEmit log.Fatals
// when handed a non-Wails context, and every test app uses Background
// (there is no way to fake the runtime context — see log_test.go).
// newTestApp sets this once for the whole package's tests. Atomic because
// background job goroutines read it after their test has returned, while
// the next test's newTestApp writes it (caught by -race in CI).
var testNoEvents atomic.Bool

// eventSink, when set, receives every GUI event in place of the Wails bus.
// The live GUI harness (tools/gui-live) bridges events to a real browser
// this way: Wails' runtime event interface lives in a wails-internal
// package (its Notify method signature names an internal type), so a
// runtime context cannot be constructed outside wails.Run. Must be set
// before Startup; the desktop app never sets it.
var eventSink func(event string, data ...any)

// SetEventSink redirects all GUI events to fn (nil restores the Wails bus).
// Only tools/gui-live sets this.
func SetEventSink(fn func(event string, data ...any)) { eventSink = fn }

// emitEvent forwards to the Wails event bus.
func emitEvent(ctx context.Context, event string, data ...any) {
	if testNoEvents.Load() {
		return
	}
	if eventSink != nil {
		eventSink(event, data...)
		return
	}
	runtime.EventsEmit(ctx, event, data...)
}

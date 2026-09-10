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

// emitEvent forwards to the Wails event bus.
func emitEvent(ctx context.Context, event string, data ...any) {
	if testNoEvents.Load() {
		return
	}
	runtime.EventsEmit(ctx, event, data...)
}

package api

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errNoContext guards method calls that arrive before wails.Run fired Startup.
var errNoContext = errors.New("application not started")

// testNoEvents mutes the Wails event bus in tests: EventsEmit log.Fatals
// when handed a non-Wails context, and every test app uses Background
// (there is no way to fake the runtime context — see log_test.go).
// newTestApp sets this once for the whole package's tests.
var testNoEvents bool

// emitEvent forwards to the Wails event bus.
func emitEvent(ctx context.Context, event string, data ...any) {
	if testNoEvents {
		return
	}
	runtime.EventsEmit(ctx, event, data...)
}

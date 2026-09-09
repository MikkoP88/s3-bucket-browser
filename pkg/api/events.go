package api

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errNoContext guards method calls that arrive before wails.Run fired Startup.
var errNoContext = errors.New("application not started")

// emitEvent forwards to the Wails event bus.
func emitEvent(ctx context.Context, event string, data ...any) {
	runtime.EventsEmit(ctx, event, data...)
}

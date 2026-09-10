package api

import (
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// LogLine is the EventLogLine payload consumed by the GUI log drawer.
type LogLine struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // "info" | "warn" | "error"
	Scope   string    `json:"scope"` // "transfer", "doctor", "upload", ...
	Message string    `json:"message"`
}

// Log levels.
const (
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)

// emitLog pushes one structured log line to the frontend log drawer
// (no-op before Startup, same guard as emit) and persists it to the
// shared event log so `s3b log` can tail GUI activity (M10.5).
func (a *App) emitLog(level, scope, msg string) {
	a.emit(EventLogLine, LogLine{
		Time:    time.Now().UTC(),
		Level:   level,
		Scope:   scope,
		Message: msg,
	})
	eventlog.Append(level, scope, msg)
}

// jobStatusLevel maps a transfer job's final status to a log level.
func jobStatusLevel(status string) string {
	switch status {
	case JobDone:
		return LogInfo
	case JobCanceled:
		return LogWarn
	default: // JobError
		return LogError
	}
}

package api

import (
	"errors"
	"fmt"
	"strings"
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

// Log file modes (Settings dialog, persisted via eventlog.SaveSettings).
const (
	LogModeDefault = "default" // events.jsonl beside profiles.json
	LogModeOff     = "off"     // no file logging
	LogModeCustom  = "custom"  // the user-picked folder
)

// LogSettings is the save-logs-to-file preference shown in Settings.
type LogSettings struct {
	Mode string `json:"mode"` // "default" | "off" | "custom"
	Dir  string `json:"dir"`  // the picked folder (mode == "custom")
}

// GetLogSettings returns the current log-file preference.
func (a *App) GetLogSettings() LogSettings {
	s := eventlog.LoadSettings()
	if s.Mode == "" {
		s.Mode = LogModeDefault
	}
	return LogSettings{Mode: s.Mode, Dir: s.Dir}
}

// SetLogSettings persists the log-file preference; "custom" requires a
// non-empty dir (the dialog browses for it first).
func (a *App) SetLogSettings(mode, dir string) (LogSettings, error) {
	if mode == "" {
		mode = LogModeDefault
	}
	switch mode {
	case LogModeDefault, LogModeOff:
		dir = ""
	case LogModeCustom:
		if strings.TrimSpace(dir) == "" {
			return a.GetLogSettings(), errors.New("choose a folder for the log file first")
		}
	default:
		return a.GetLogSettings(), fmt.Errorf("unknown log mode %q", mode)
	}
	if err := eventlog.SaveSettings(eventlog.Settings{Mode: mode, Dir: dir}); err != nil {
		return a.GetLogSettings(), err
	}
	return a.GetLogSettings(), nil
}

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

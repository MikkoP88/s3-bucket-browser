package api

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// LogLine is the EventLogLine payload consumed by the GUI log drawer.
type LogLine struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`            // "info" | "warn" | "error"
	Scope   string    `json:"scope"`            // "transfer", "doctor", "upload", ...
	Source  string    `json:"source,omitempty"` // bucket / source name the line is about (filter)
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
// Levels/Scopes filter what is WRITTEN to the file (empty = everything);
// they never affect the in-app log drawer, which filters client-side on
// its own controls. AllScopes lists every scope the app emits (selector
// options; read-only metadata, ignored by SetLogSettings).
type LogSettings struct {
	Mode      string   `json:"mode"` // "default" | "off" | "custom"
	Dir       string   `json:"dir"`  // the picked folder (mode == "custom")
	Levels    []string `json:"levels"`
	Scopes    []string `json:"scopes"`
	AllScopes []string `json:"allScopes"`
}

// LogScopes lists every scope the app logs under — the Settings dialog's
// file-log scope selector options. Keep in sync with emitLog/emitLogSrc
// call sites.
var LogScopes = []string{
	"admin", "app", "copy", "delete", "doctor", "download", "import",
	"list", "mkdir", "profile", "rename", "settings", "share",
	"sources", "transfer", "upload", "versions",
}

// GetLogSettings returns the current log-file preference.
func (a *App) GetLogSettings() LogSettings {
	s := eventlog.LoadSettings()
	if s.Mode == "" {
		s.Mode = LogModeDefault
	}
	return LogSettings{Mode: s.Mode, Dir: s.Dir, Levels: s.Levels, Scopes: s.Scopes, AllScopes: LogScopes}
}

// SetLogSettings persists the log-file preference; "custom" requires a
// non-empty dir (the dialog browses for it first).
func (a *App) SetLogSettings(mode, dir string, levels, scopes []string) (LogSettings, error) {
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
	levels = cleanLogFilter(levels, []string{LogInfo, LogWarn, LogError})
	scopes = cleanLogFilter(scopes, LogScopes)
	if err := eventlog.SaveSettings(eventlog.Settings{Mode: mode, Dir: dir, Levels: levels, Scopes: scopes}); err != nil {
		return a.GetLogSettings(), err
	}
	a.emitLog(LogInfo, "settings", fmt.Sprintf("log file mode set to %s%s%s", mode, dirNote(dir), filterNote(levels, scopes)))
	return a.GetLogSettings(), nil
}

// cleanLogFilter keeps known values, trimmed, de-duplicated, first come.
func cleanLogFilter(in, known []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] || !slices.Contains(known, v) {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func filterNote(levels, scopes []string) string {
	if len(levels) == 0 && len(scopes) == 0 {
		return ""
	}
	lv, sc := levels, scopes
	if len(lv) == 0 {
		lv = []string{"all"}
	}
	if len(sc) == 0 {
		sc = []string{"all"}
	}
	return fmt.Sprintf(" (levels: %s; scopes: %s)", strings.Join(lv, ", "), strings.Join(sc, ", "))
}

func dirNote(dir string) string {
	if dir == "" {
		return ""
	}
	return " (" + dir + ")"
}

// emitLog pushes one structured log line to the frontend log drawer
// (no-op before Startup, same guard as emit) and persists it to the
// shared event log so `s3b log` can tail GUI activity (M10.5).
func (a *App) emitLog(level, scope, msg string) {
	a.emitLogSrc(level, scope, "", msg)
}

// emitLogSrc is emitLog with the source (bucket or data-source name) the
// line is about — the log drawer's per-source filter rides on it.
func (a *App) emitLogSrc(level, scope, source, msg string) {
	a.emit(EventLogLine, LogLine{
		Time:    time.Now().UTC(),
		Level:   level,
		Scope:   scope,
		Source:  source,
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

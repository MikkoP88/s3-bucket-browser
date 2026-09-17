// Package eventlog persists the app activity log as JSON lines under the
// config dir (events.jsonl): the same lines the GUI log drawer shows, kept
// across sessions so `s3b log` can tail them (CLI parity).
// The file is capped; when it outgrows the cap the oldest half of the lines
// is dropped (a log, not an audit trail — safety-ladder gates stay in S3).
package eventlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// Line is one log line (same shape the GUI drawer renders).
type Line struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`            // "info" | "warn" | "error"
	Scope   string    `json:"scope"`            // "transfer", "doctor", "upload", ...
	Source  string    `json:"source,omitempty"` // bucket / source name the line is about
	Message string    `json:"message"`
}

// maxBytes caps events.jsonl; on overflow the oldest lines are dropped.
const maxBytes = 1 << 20 // 1 MiB

var mu sync.Mutex

// Path returns the events.jsonl location beside profiles.json.
func Path() (string, error) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "events.jsonl"), nil
}

// Settings is the persisted save-logs-to-file preference (logsettings.json
// in the config dir, written by the GUI Settings dialog). Mode:
// "" / "default" = events.jsonl beside profiles.json (what `s3b log`
// tails), "off" = no file logging, "custom" = the user-picked Dir.
// Levels/Scopes/Sources filter what is WRITTEN to the file (empty =
// everything). They are deliberately independent of the in-app log
// drawer, which filters client-side on its own controls — file logging
// must never change what the user sees on screen.
type Settings struct {
	Mode    string   `json:"logFileMode"`
	Dir     string   `json:"logFileDir"`
	Levels  []string `json:"logFileLevels,omitempty"`
	Scopes  []string `json:"logFileScopes,omitempty"`
	Sources []string `json:"logFileSources,omitempty"`
}

func settingsPath() (string, error) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logsettings.json"), nil
}

// LoadSettings reads the preference; the zero value (default mode) is
// returned when the file is missing or unreadable — logging must never
// be taken down by a torn settings file.
func LoadSettings() Settings {
	p, err := settingsPath()
	if err != nil {
		return Settings{}
	}
	var s Settings
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &s) != nil {
		return Settings{}
	}
	return s
}

// SaveSettings persists the preference (0600, like profiles.json).
func SaveSettings(s Settings) error {
	p, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// sinkPath resolves where events.jsonl is written; ok=false means file
// logging is off. Read per call (no cache) so tests with isolated config
// dirs and live GUI changes take effect immediately.
func sinkPath() (string, bool) {
	return sinkPathFrom(LoadSettings())
}

func sinkPathFrom(s Settings) (string, bool) {
	if s.Mode == "off" {
		return "", false
	}
	if s.Mode == "custom" && s.Dir != "" {
		return filepath.Join(s.Dir, "events.jsonl"), true
	}
	p, err := Path()
	if err != nil {
		return "", false
	}
	return p, true
}

// seenSourcesPath is the registry of every source (bucket / data-source
// name) that has ever appeared on a log line — the Settings dialog's
// file-log source selector options, so it can offer the same grow-with-
// the-stream vocabulary the in-app drawer builds client-side.
func seenSourcesPath() (string, error) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logsources.json"), nil
}

// SeenSources lists the registered sources, sorted. Best effort: a
// missing or torn registry is an empty vocabulary, never an error.
func SeenSources() []string {
	p, err := seenSourcesPath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var srcs []string
	if json.Unmarshal(b, &srcs) != nil {
		return nil
	}
	slices.Sort(srcs)
	return srcs
}

// registerSource adds src to the registry when it is new. Best effort:
// an unwritable config dir only costs the option list a late entry.
// Called with mu held (Append serializes writers).
func registerSource(src string) {
	if src == "" {
		return
	}
	srcs := SeenSources()
	if slices.Contains(srcs, src) {
		return
	}
	srcs = append(srcs, src)
	slices.Sort(srcs)
	p, err := seenSourcesPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	b, err := json.Marshal(srcs)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o600)
}

// Append writes one line, rotating first when the file outgrew the cap.
// Best effort: logging must never take the app down, errors are dropped.
// Lines the saved level/scope/source filters reject never reach the file.
func Append(level, scope, source, msg string) {
	s := LoadSettings()
	if s.Mode == "off" {
		return // off (and Secure Storage) means nothing is written at all
	}
	mu.Lock()
	defer mu.Unlock()
	// the vocabulary registers before the filters: a source whose lines
	// are being filtered out must still become selectable
	registerSource(source)
	if !fileFilterMatches(s, level, scope, source) {
		return
	}
	p, ok := sinkPathFrom(s)
	if !ok {
		return
	}
	if err := rotateIfBig(p); err != nil {
		return // unwritable config dir — the GUI drawer still works
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(Line{Time: time.Now().UTC(), Level: level, Scope: scope, Source: source, Message: msg})
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
}

// fileFilterMatches applies the saved level/scope/source filters (exact
// match; empty list = dimension unrestricted; an unsourced line fails a
// source filter, same rule the drawer's source selector applies).
// File-only by design: the in-app drawer never consults these.
func fileFilterMatches(s Settings, level, scope, source string) bool {
	if len(s.Levels) > 0 && !slices.Contains(s.Levels, level) {
		return false
	}
	if len(s.Scopes) > 0 && !slices.Contains(s.Scopes, scope) {
		return false
	}
	if len(s.Sources) > 0 && !slices.Contains(s.Sources, source) {
		return false
	}
	return true
}

// rotateIfBig rewrites the file with its newest half when it exceeds the
// cap. Missing file = nothing to do.
func rotateIfBig(p string) error {
	st, err := os.Stat(p)
	if os.IsNotExist(err) {
		return nil // first append — O_CREATE below handles it
	}
	if err != nil {
		return err
	}
	if st.Size() <= maxBytes {
		return nil
	}
	lines, err := readLines(p, 0, "", "")
	if err != nil {
		return err
	}
	keep := lines[len(lines)/2:]
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range keep {
		b, err := json.Marshal(l)
		if err != nil {
			continue
		}
		w.Write(append(b, '\n'))
	}
	return w.Flush()
}

// readLines parses the file, newest last. n > 0 keeps only the last n
// lines; level/scope filter when non-empty (level match is exact, scope
// match is prefix so "transfer" also matches "transfers").
func readLines(p string, n int, level, scope string) ([]Line, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Line
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var l Line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // foreign or torn line — skip, never fail the tail
		}
		if level != "" && l.Level != level {
			continue
		}
		if scope != "" && !strings.HasPrefix(l.Scope, scope) {
			continue
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// Tail returns the last n lines (all when n <= 0), filtered by level and
// scope, from the configured log location. A missing file (or logging
// turned off) is an empty log, not an error.
func Tail(n int, level, scope string) ([]Line, error) {
	p, ok := sinkPath()
	if !ok {
		return nil, nil
	}
	mu.Lock()
	defer mu.Unlock()
	lines, err := readLines(p, n, level, scope)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return lines, err
}

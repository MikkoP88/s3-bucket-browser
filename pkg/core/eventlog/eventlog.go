// Package eventlog persists the app activity log as JSON lines under the
// config dir (events.jsonl): the same lines the GUI log drawer shows, kept
// across sessions so `s3b log` can tail them (plan-v2 M10.5 CLI parity).
// The file is capped; when it outgrows the cap the oldest half of the lines
// is dropped (a log, not an audit trail — PLAN.md §9 gates stay in S3).
package eventlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// Line is one log line (same shape the GUI drawer renders).
type Line struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // "info" | "warn" | "error"
	Scope   string    `json:"scope"` // "transfer", "doctor", "upload", ...
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

// Append writes one line, rotating first when the file outgrew the cap.
// Best effort: logging must never take the app down, errors are dropped.
func Append(level, scope, msg string) {
	p, err := Path()
	if err != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if err := rotateIfBig(p); err != nil {
		return // unwritable config dir — the GUI drawer still works
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(Line{Time: time.Now().UTC(), Level: level, Scope: scope, Message: msg})
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
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
// scope. A missing file is an empty log, not an error.
func Tail(n int, level, scope string) ([]Line, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	lines, err := readLines(p, n, level, scope)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return lines, err
}

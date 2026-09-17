package eventlog

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func eventEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	return dir
}

func TestAppendTailFilters(t *testing.T) {
	eventEnv(t)
	Append("info", "transfer", "", "copied a.txt")
	Append("error", "delete", "", "rm failed: boom")
	Append("warn", "transfer", "", "throttled")

	all, err := Tail(0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 lines, got %d", len(all))
	}
	if all[0].Level != "info" || all[0].Scope != "transfer" || all[0].Message != "copied a.txt" {
		t.Fatalf("first line mangled: %+v", all[0])
	}

	errs, _ := Tail(0, "error", "")
	if len(errs) != 1 || errs[0].Message != "rm failed: boom" {
		t.Fatalf("level filter: %+v", errs)
	}
	scoped, _ := Tail(0, "", "transfer")
	if len(scoped) != 2 {
		t.Fatalf("scope filter: %+v", scoped)
	}
	last, _ := Tail(1, "", "")
	if len(last) != 1 || last[0].Message != "throttled" {
		t.Fatalf("tail(1): %+v", last)
	}
}

func TestTailMissingFileIsEmpty(t *testing.T) {
	eventEnv(t)
	lines, err := Tail(0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("want empty log, got %d lines", len(lines))
	}
}

func TestSourceFilterAndRegistry(t *testing.T) {
	eventEnv(t)

	// a source filter keeps only the named sources' lines — unsourced
	// lines are rejected while the filter is set, like the drawer
	if err := SaveSettings(Settings{Sources: []string{"team-files"}}); err != nil {
		t.Fatal(err)
	}
	Append("info", "delete", "team-files", "deleted 3 object(s)")
	Append("info", "delete", "other-bucket", "deleted 1 object(s)")
	Append("warn", "settings", "", "mode changed")

	lines, _ := Tail(0, "", "")
	if len(lines) != 1 || lines[0].Source != "team-files" || lines[0].Message != "deleted 3 object(s)" {
		t.Fatalf("source filter not applied: %+v", lines)
	}

	// the rejected sources still joined the vocabulary — the selector can
	// offer them even while they are filtered out
	seen := SeenSources()
	if !slices.Contains(seen, "team-files") || !slices.Contains(seen, "other-bucket") {
		t.Fatalf("seen registry incomplete: %v", seen)
	}
	if slices.Contains(seen, "") {
		t.Fatalf("unsourced lines must not register: %v", seen)
	}

	// empty filter = everything again, sources included
	if err := SaveSettings(Settings{}); err != nil {
		t.Fatal(err)
	}
	Append("warn", "doctor", "team-files", "check passed")
	lines, _ = Tail(0, "", "")
	if len(lines) != 2 || lines[1].Source != "team-files" {
		t.Fatalf("unfiltered lines wrong: %+v", lines)
	}

	// off mode writes nothing anywhere — registry included
	if err := SaveSettings(Settings{Mode: "off"}); err != nil {
		t.Fatal(err)
	}
	before := len(SeenSources())
	Append("info", "delete", "fresh-bucket", "invisible")
	if len(SeenSources()) != before || slices.Contains(SeenSources(), "fresh-bucket") {
		t.Fatal("off mode still wrote to the seen registry")
	}
}

func TestSettingsOffAndCustom(t *testing.T) {
	cfg := eventEnv(t)

	// off: nothing is written anywhere
	if err := SaveSettings(Settings{Mode: "off"}); err != nil {
		t.Fatal(err)
	}
	Append("info", "test", "", "must not land")
	if _, err := os.Stat(filepath.Join(cfg, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("off mode still wrote the default log")
	}
	if lines, _ := Tail(0, "", ""); len(lines) != 0 {
		t.Fatal("off mode tail is not empty")
	}

	// custom: events.jsonl lands in the picked folder instead
	custom := t.TempDir()
	if err := SaveSettings(Settings{Mode: "custom", Dir: custom}); err != nil {
		t.Fatal(err)
	}
	Append("warn", "test", "", "custom sink")
	lines, err := Tail(0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Message != "custom sink" {
		t.Fatalf("custom sink not used: %+v", lines)
	}
	if _, err := os.Stat(filepath.Join(cfg, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("custom mode still wrote the default log")
	}

	// custom without a dir falls back to the default location
	if err := SaveSettings(Settings{Mode: "custom"}); err != nil {
		t.Fatal(err)
	}
	Append("info", "test", "", "fallback")
	if _, err := os.Stat(filepath.Join(cfg, "events.jsonl")); err != nil {
		t.Fatal("custom-without-dir did not fall back to the default log")
	}

	// a torn settings file reads as the default mode
	if err := os.WriteFile(filepath.Join(cfg, "logsettings.json"), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(); s.Mode != "" || s.Dir != "" {
		t.Fatalf("torn settings not ignored: %+v", s)
	}
}

func TestRotationKeepsNewestHalf(t *testing.T) {
	eventEnv(t)
	// Blow past the cap with fat messages, then verify the log shrank and
	// the newest lines survived.
	filler := strings.Repeat("x", 512)
	n := (maxBytes/len(filler))*2 + 10
	for i := 0; i < n; i++ {
		Append("info", "test", "", filler)
	}
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > maxBytes {
		t.Fatalf("rotation did not cap the file: %d > %d", st.Size(), maxBytes)
	}
	lines, err := Tail(0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("rotation dropped everything")
	}
	if lines[len(lines)-1].Message != filler {
		t.Fatal("newest line did not survive rotation")
	}
	// The append-after-rotate path still works.
	Append("warn", "test", "", "after-rotate")
	lines, _ = Tail(0, "", "")
	if lines[len(lines)-1].Message != "after-rotate" {
		t.Fatal("append after rotation failed")
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Fatal(err)
	}
}

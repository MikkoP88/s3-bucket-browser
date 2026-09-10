package eventlog

import (
	"os"
	"path/filepath"
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
	Append("info", "transfer", "copied a.txt")
	Append("error", "delete", "rm failed: boom")
	Append("warn", "transfer", "throttled")

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

func TestRotationKeepsNewestHalf(t *testing.T) {
	eventEnv(t)
	// Blow past the cap with fat messages, then verify the log shrank and
	// the newest lines survived.
	filler := strings.Repeat("x", 512)
	n := (maxBytes/len(filler))*2 + 10
	for i := 0; i < n; i++ {
		Append("info", "test", filler)
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
	Append("warn", "test", "after-rotate")
	lines, _ = Tail(0, "", "")
	if lines[len(lines)-1].Message != "after-rotate" {
		t.Fatal("append after rotation failed")
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Fatal(err)
	}
}

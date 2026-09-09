package search

import (
	"testing"
	"time"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, key string
		want         bool
	}{
		{"report", "docs/report.pdf", true},
		{"REPORT", "docs/report.pdf", true},   // case-insensitive
		{"nomatch", "docs/report.pdf", false}, // substring miss
		{"*.pdf", "docs/report.pdf", true},    // glob spans '/'
		{"docs/*", "docs/report.pdf", true},
		{"docs/?.pdf", "docs/a.pdf", true},
		{"docs/?.pdf", "docs/ab.pdf", false},
		{"backup*", "logs/2026/backup-old.tar", true}, // '*' crosses '/'
		{"*", "", false},                              // empty key never matches
	}
	for _, c := range cases {
		if got := matchPattern(c.pattern, c.key); got != c.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", c.pattern, c.key, got, c.want)
		}
	}
}

func TestMatchFilters(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	old := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)
	mod := func(tp *time.Time) *time.Time { return tp }

	base := Filter{}
	if !Match(base, "a/b.txt", 100, nil, "", now) {
		t.Error("empty filter must match everything")
	}

	if !Match(Filter{LargerThan: 99}, "a", 100, nil, "", now) ||
		Match(Filter{LargerThan: 100}, "a", 100, nil, "", now) {
		t.Error("LargerThan is strict >")
	}
	if !Match(Filter{SmallerThan: 101}, "a", 100, nil, "", now) ||
		Match(Filter{SmallerThan: 100}, "a", 100, nil, "", now) {
		t.Error("SmallerThan is strict <")
	}
	if !Match(Filter{OlderThan: 24 * time.Hour}, "a", 0, mod(&old), "", now) {
		t.Error("48h-old object must match OlderThan=24h")
	}
	if Match(Filter{OlderThan: 72 * time.Hour}, "a", 0, mod(&old), "", now) {
		t.Error("48h-old object must not match OlderThan=72h")
	}
	if Match(Filter{OlderThan: time.Hour}, "a", 0, nil, "", now) {
		t.Error("unknown mtime never matches age filters")
	}
	if !Match(Filter{NewerThan: 2 * time.Hour}, "a", 0, mod(&fresh), "", now) {
		t.Error("1h-old object must match NewerThan=2h")
	}
	if !Match(Filter{Class: "glacier"}, "a", 0, nil, "GLACIER", now) {
		t.Error("class compare must be case-insensitive")
	}
	if Match(Filter{Class: "standard"}, "a", 0, nil, "GLACIER", now) {
		t.Error("class mismatch must not match")
	}
}

func TestParseSize(t *testing.T) {
	ok := map[string]int64{
		"500":   500,
		"10KB":  10 << 10,
		"1.5MB": int64(1.5 * float64(1<<20)),
		"2GB":   2 << 30,
		"1TB":   1 << 40,
		"3b":    3,
	}
	for in, want := range ok {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-5KB", "1.2.3MB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) must fail", in)
		}
	}
}

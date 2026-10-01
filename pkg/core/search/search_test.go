package search

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
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

func TestMatchExtPath(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)

	if !matchExt("pdf", "docs/report.pdf") || !matchExt(".PDF, jpg", "photos/a.JPG") {
		t.Error("matchExt: case-insensitive, dot optional, list-tolerant")
	}
	if matchExt("pdf", "docs/report.pdf.bak") {
		t.Error("matchExt: the extension must terminate the name")
	}
	if matchExt("md", "readme.md/") {
		t.Error("matchExt: a directory key is not an extension match")
	}

	if !matchPath("docs", "docs/notes.md") || !matchPath("Docs", "a/DOCS/x.txt") {
		t.Error("matchPath: case-insensitive substring on the directory part")
	}
	if matchPath("docs", "docs.md") || matchPath("notes", "docs/notes.md") {
		t.Error("matchPath: the base name must not match")
	}

	if !Match(Filter{Ext: "md", Path: "docs"}, "docs/a.md", false, 0, nil, "", now) ||
		Match(Filter{Ext: "md", Path: "docs"}, "docs/a.txt", false, 0, nil, "", now) ||
		Match(Filter{Ext: "md", Path: "docs"}, "other/a.md", false, 0, nil, "", now) {
		t.Error("Match: ext and path combine as AND predicates")
	}
}

func TestMatchFilters(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	old := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)
	mod := func(tp *time.Time) *time.Time { return tp }

	base := Filter{}
	if !Match(base, "a/b.txt", false, 100, nil, "", now) {
		t.Error("empty filter must match everything")
	}

	if !Match(Filter{LargerThan: 99}, "a", false, 100, nil, "", now) ||
		Match(Filter{LargerThan: 100}, "a", false, 100, nil, "", now) {
		t.Error("LargerThan is strict >")
	}
	if !Match(Filter{SmallerThan: 101}, "a", false, 100, nil, "", now) ||
		Match(Filter{SmallerThan: 100}, "a", false, 100, nil, "", now) {
		t.Error("SmallerThan is strict <")
	}
	if !Match(Filter{OlderThan: 24 * time.Hour}, "a", false, 0, mod(&old), "", now) {
		t.Error("48h-old object must match OlderThan=24h")
	}
	if Match(Filter{OlderThan: 72 * time.Hour}, "a", false, 0, mod(&old), "", now) {
		t.Error("48h-old object must not match OlderThan=72h")
	}
	if Match(Filter{OlderThan: time.Hour}, "a", false, 0, nil, "", now) {
		t.Error("unknown mtime never matches age filters")
	}
	if !Match(Filter{NewerThan: 2 * time.Hour}, "a", false, 0, mod(&fresh), "", now) {
		t.Error("1h-old object must match NewerThan=2h")
	}
	if !Match(Filter{Class: "glacier"}, "a", false, 0, nil, "GLACIER", now) {
		t.Error("class compare must be case-insensitive")
	}
	if Match(Filter{Class: "standard"}, "a", false, 0, nil, "GLACIER", now) {
		t.Error("class mismatch must not match")
	}
}

func TestMatchKind(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	if !Match(Filter{}, "a/b.txt", false, 1, nil, "", now) ||
		!Match(Filter{}, "a/", true, 0, nil, "", now) {
		t.Error("Kind \"\" matches files and folders alike")
	}
	if Match(Filter{Kind: "file"}, "a/", true, 0, nil, "", now) {
		t.Error("Kind=file must not match a folder")
	}
	if !Match(Filter{Kind: "file"}, "a/b.txt", false, 1, nil, "", now) {
		t.Error("Kind=file must match a file")
	}
	if Match(Filter{Kind: "dir"}, "a/b.txt", false, 1, nil, "", now) {
		t.Error("Kind=dir must not match a file")
	}
	if !Match(Filter{Kind: "DIR"}, "a/", true, 0, nil, "", now) {
		t.Error("Kind compare must be case-insensitive")
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

// stubFS is an in-memory remote tree: dir path -> entries (the shape
// RunRemote walks; keys are anchored full paths, folders end with '/').
type stubFS map[string][]listing.Entry

func (s stubFS) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	return s[dir], nil
}
func (s stubFS) Stat(ctx context.Context, p string) (listing.Entry, error) {
	return listing.Entry{}, nil
}
func (s stubFS) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	return nil, 0, nil
}
func (s stubFS) Create(ctx context.Context, p string, r io.Reader) error { return nil }
func (s stubFS) MkdirAll(ctx context.Context, dir string) error          { return nil }
func (s stubFS) Remove(ctx context.Context, p string) error              { return nil }
func (s stubFS) Rename(ctx context.Context, a, b string) error           { return nil }
func (s stubFS) Close() error                                            { return nil }

func TestRunRemote(t *testing.T) {
	fs := stubFS{
		"": {
			{Key: "docs/", Name: "docs", IsDir: true},
			{Key: "readme.md", Name: "readme.md", Size: 1234},
		},
		"docs/": {
			{Key: "docs/notes.md", Name: "notes.md", Size: 900},
		},
	}

	// no filter: every entry below root, depth-first
	var keys []string
	st, err := RunRemote(context.Background(), fs, "", Filter{}, func(r Result) error {
		keys = append(keys, r.Key)
		return nil
	})
	if err != nil || st.Scanned != 3 || st.Matched != 3 {
		t.Fatalf("walk all: stats %+v err %v", st, err)
	}
	if len(keys) != 3 || keys[0] != "docs/" || keys[1] != "docs/notes.md" || keys[2] != "readme.md" {
		t.Errorf("walk order/keys = %v", keys)
	}

	// Kind=dir finds only folders
	keys = nil
	st, err = RunRemote(context.Background(), fs, "", Filter{Kind: "dir"}, func(r Result) error {
		keys = append(keys, r.Key)
		return nil
	})
	if err != nil || st.Matched != 1 || len(keys) != 1 || keys[0] != "docs/" {
		t.Errorf("Kind=dir: keys %v stats %+v err %v", keys, st, err)
	}

	// size + name glob combine
	st, err = RunRemote(context.Background(), fs, "", Filter{Pattern: "*.md", LargerThan: 1000}, func(r Result) error {
		if r.Key != "readme.md" {
			t.Errorf("unexpected match %q", r.Key)
		}
		return nil
	})
	if err != nil || st.Matched != 1 {
		t.Errorf("combined filter: stats %+v err %v", st, err)
	}

	// a class filter never matches remote entries (no storage class)
	st, err = RunRemote(context.Background(), fs, "", Filter{Class: "STANDARD"}, func(r Result) error {
		t.Errorf("class filter must not match remote entries")
		return nil
	})
	if err != nil || st.Matched != 0 || st.Scanned != 3 {
		t.Errorf("class filter: stats %+v err %v", st, err)
	}

	// limit stops early without an error
	st, err = RunRemote(context.Background(), fs, "", Filter{Limit: 2}, func(r Result) error {
		return nil
	})
	if err != nil || st.Matched != 2 {
		t.Errorf("limit: stats %+v err %v", st, err)
	}

	// ErrStop from the callback ends the walk silently
	st, err = RunRemote(context.Background(), fs, "", Filter{}, func(r Result) error {
		return ErrStop
	})
	if err != nil || st.Matched != 1 {
		t.Errorf("ErrStop: stats %+v err %v", st, err)
	}
}

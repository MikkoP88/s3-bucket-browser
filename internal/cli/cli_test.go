package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Cobra's own invocation failures (unknown command, wrong arg count) are
// user typos: they must classify as usage errors (exit 2, "usage error:")
// and never as app failures (exit 3, "unexpected:").
func TestExecuteUsageClassification(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"definitely-not-a-cmd"}},
		{"missing required arg", []string{"rm"}},
		{"unknown flag", []string{"ls", "--definitely-not-a-flag"}},
		{"flag needs an argument", []string{"ls", "--profile"}},
		{"invalid flag value", []string{"--timeout", "abc"}},
	}
	for _, c := range cases {
		if code := Execute(c.args); code != exitUsage {
			t.Errorf("%s: Execute(%v) = exit %d, want exitUsage (%d)", c.name, c.args, code, exitUsage)
		}
	}
}

// The label matters as much as the code: an invocation error must read
// "usage error:" and point at --help — never "unexpected:".
func TestExecuteUsageErrorLabel(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := Execute([]string{"definitely-not-a-cmd"})
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if s := string(out); !strings.Contains(s, "usage error:") || !strings.Contains(s, "--help") {
		t.Errorf("stderr = %q, want usage error label with --help hint", s)
	}
}

func TestParseS3URI(t *testing.T) {
	cases := []struct {
		in             string
		bucket, key    string
		isPrefix, hasK bool
		wantErr        bool
	}{
		{"s3://photos", "photos", "", false, false, false},
		{"s3://photos/", "photos", "", false, false, false},
		{"s3://photos/2026/a.jpg", "photos", "2026/a.jpg", false, true, false},
		{"s3://photos/2026/", "photos", "2026/", true, true, false},
		{"s3://photos//double//slash", "photos", "double//slash", false, true, false},
		{"http://x", "", "", false, false, true},
		{"s3://", "", "", false, false, true},
	}
	for _, c := range cases {
		u, err := parseS3URI(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseS3URI(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseS3URI(%q) unexpected error: %v", c.in, err)
			continue
		}
		if u.Bucket != c.bucket || u.Key != c.key || u.IsPrefix != c.isPrefix || u.HasPrefix != c.hasK {
			t.Errorf("parseS3URI(%q) = %+v", c.in, u)
		}
	}
}

func TestDirPrefix(t *testing.T) {
	cases := map[string]string{
		"s3://b":           "",
		"s3://b/photos":    "photos/",
		"s3://b/photos/":   "photos/",
		"s3://b/a/b/c.jpg": "a/b/c.jpg/",
	}
	for uri, want := range cases {
		u, err := parseS3URI(uri)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		got := ""
		if u.HasPrefix {
			got = dirPrefix(u)
		}
		if got != want {
			t.Errorf("dirPrefix(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestJoinKeyNoSlash(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"photos", "a.jpg"}, "photos/a.jpg"},
		{[]string{"photos/", "sub", "b.txt"}, "photos/sub/b.txt"},
		{[]string{"", "root.txt"}, "root.txt"},
		{[]string{`win\path`, "f"}, "win/path/f"},
	}
	for _, c := range cases {
		if got := joinKeyNoSlash(c.parts...); got != c.want {
			t.Errorf("joinKeyNoSlash(%v) = %q, want %q", c.parts, got, c.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		512:        "512 B",
		1024:       "1.0 KB",
		1048576:    "1.0 MB",
		4718592000: "4.4 GB",
	}
	for n, want := range cases {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestParseIntDuration(t *testing.T) {
	cases := map[string]bool{
		"90s": true,
		"12h": true,
		"7d":  true,
		"abc": false,
	}
	for in, ok := range cases {
		d, err := parseIntDuration(in)
		if ok && err != nil {
			t.Errorf("parseIntDuration(%q) error: %v", in, err)
		}
		if !ok && err == nil {
			t.Errorf("parseIntDuration(%q) expected error, got %v", in, d)
		}
	}
	if d, _ := parseIntDuration("7d"); d.Hours() != 168 {
		t.Errorf("7d = %v hours, want 168", d.Hours())
	}
}

func TestFirst(t *testing.T) {
	if first("", "x", "y") != "x" || first() != "" || first("a", "b") != "a" {
		t.Error("first() fallback chain broken")
	}
}

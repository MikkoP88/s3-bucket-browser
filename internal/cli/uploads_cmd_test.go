// uploads_cmd_test.go pins the operational face of the incomplete-upload
// lifecycle against a fake S3: the listing answers as JSON an array (never
// null — even an empty bucket), the age filter narrows to the stale uploads
// only, --abort refuses to act without --force, a forced abort speaks the
// exact bucket|key|uploadId wire for exactly the matched set, and an unknown
// --id is an honest operation failure that touches nothing.
package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// uploadsLab is a fake S3 speaking ListMultipartUploads for two buckets —
// "lab" carries one fresh (30m) and one stale (3h) upload, "empty" carries
// none — and AbortMultipartUpload, recording every abort request.
type uploadsLab struct {
	mu     sync.Mutex
	aborts []string // "path|uploadId" per abort request, in order
}

func newUploadsLab(t *testing.T) *uploadsLab {
	t.Helper()
	lab := &uploadsLab{}
	fresh := time.Now().Add(-30 * time.Minute)
	old := time.Now().Add(-3 * time.Hour)
	upload := func(key, id string, at time.Time) string {
		return "<Upload><Key>" + key + "</Key><UploadId>" + id + "</UploadId>" +
			"<Initiated>" + at.UTC().Format("2006-01-02T15:04:05.000Z") + "</Initiated></Upload>"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket := strings.Trim(r.URL.Path, "/")
		q := r.URL.Query()
		lab.mu.Lock()
		defer lab.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && q.Has("uploads"):
			var b strings.Builder
			b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
			b.WriteString("<ListMultipartUploadsResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
			b.WriteString("<Bucket>" + bucket + "</Bucket>")
			if bucket == "lab" {
				b.WriteString(upload("docs/report.bin", "id-fresh", fresh))
				b.WriteString(upload("logs/big.log", "id-old", old))
			}
			b.WriteString("<IsTruncated>false</IsTruncated></ListMultipartUploadsResult>")
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, b.String())
		case r.Method == http.MethodDelete && q.Get("uploadId") != "":
			lab.aborts = append(lab.aborts, r.URL.Path+"|"+q.Get("uploadId"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected call: "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	if code := Execute([]string{"source", "add", "store", "--type", "s3",
		"--endpoint", srv.URL, "--access-key", "k", "--secret-key", "s",
		"--bucket", "lab"}); code != 0 {
		t.Fatalf("source add: exit %d", code)
	}
	return lab
}

func (l *uploadsLab) abortList() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.aborts...)
}

func TestBucketUploadsJSONListsOldestFirst(t *testing.T) {
	cliEnv(t)
	newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab", "--json")
	if code != 0 {
		t.Fatalf("uploads: exit %d (%s)", code, out)
	}
	if trimmed := strings.TrimSpace(out); !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("JSON listing must be an array, got: %s", trimmed)
	}
	for _, want := range []string{"docs/report.bin", "logs/big.log", "id-fresh", "id-old"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q: %s", want, out)
		}
	}
	// the engine answers oldest-first; the CLI keeps that order
	if strings.Index(out, "logs/big.log") > strings.Index(out, "docs/report.bin") {
		t.Errorf("listing is not oldest-first: %s", out)
	}
}

func TestBucketUploadsEmptyBucketIsAnEmptyArrayNeverNull(t *testing.T) {
	cliEnv(t)
	newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://empty", "--json")
	if code != 0 {
		t.Fatalf("uploads (empty): exit %d (%s)", code, out)
	}
	if got := strings.TrimSpace(out); got != "[]" {
		t.Fatalf("empty bucket JSON = %q, want []", got)
	}
}

func TestBucketUploadsOlderThanNarrowsTheMatch(t *testing.T) {
	cliEnv(t)
	newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab", "--older-than", "2h", "--json")
	if code != 0 {
		t.Fatalf("uploads --older-than: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "logs/big.log") {
		t.Errorf("the 3h-old upload must match --older-than 2h: %s", out)
	}
	if strings.Contains(out, "docs/report.bin") || strings.Contains(out, "id-fresh") {
		t.Errorf("the 30m-old upload must not match --older-than 2h: %s", out)
	}
}

func TestBucketUploadsAbortRequiresForce(t *testing.T) {
	cliEnv(t)
	lab := newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab", "--abort", "--older-than", "2h")
	if code != exitUsage {
		t.Fatalf("abort without --force: exit %d, want %d (%s)", code, exitUsage, out)
	}
	if aborts := lab.abortList(); len(aborts) != 0 {
		t.Fatalf("an ungated abort reached the wire: %v", aborts)
	}
}

func TestBucketUploadsForcedAbortSpeaksExactWire(t *testing.T) {
	cliEnv(t)
	lab := newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab",
		"--abort", "--older-than", "2h", "--force", "--json")
	if code != 0 {
		t.Fatalf("forced abort: exit %d (%s)", code, out)
	}
	for _, want := range []string{`"aborted": 1`, `"failed": 0`} {
		if !strings.Contains(out, want) {
			t.Errorf("abort report missing %q: %s", want, out)
		}
	}
	aborts := lab.abortList()
	if len(aborts) != 1 || aborts[0] != "/lab/logs/big.log|id-old" {
		t.Fatalf("aborts = %v, want exactly the stale /lab/logs/big.log|id-old", aborts)
	}
}

func TestBucketUploadsAbortByIDSelectsOne(t *testing.T) {
	cliEnv(t)
	lab := newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab",
		"--abort", "--id", "id-fresh", "--force")
	if code != 0 {
		t.Fatalf("abort by id: exit %d (%s)", code, out)
	}
	aborts := lab.abortList()
	if len(aborts) != 1 || aborts[0] != "/lab/docs/report.bin|id-fresh" {
		t.Fatalf("aborts = %v, want exactly /lab/docs/report.bin|id-fresh", aborts)
	}
}

func TestBucketUploadsUnknownIDFailsHonestly(t *testing.T) {
	cliEnv(t)
	lab := newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab", "--id", "nope")
	if code != exitOpFail {
		t.Fatalf("unknown --id: exit %d, want %d (%s)", code, exitOpFail, out)
	}
	if aborts := lab.abortList(); len(aborts) != 0 {
		t.Fatalf("a failed --id lookup aborted something: %v", aborts)
	}
}

func TestBucketUploadsFiltersAreExclusive(t *testing.T) {
	cliEnv(t)
	newUploadsLab(t)
	out, code := captureOut(t, "bucket", "uploads", "s3://lab", "--id", "id-old", "--older-than", "1h")
	if code != exitUsage {
		t.Fatalf("--id + --older-than: exit %d, want %d (%s)", code, exitUsage, out)
	}
}

func TestParseAge(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"12h", 12 * time.Hour},
		{"90m", 90 * time.Minute},
		{"30s", 30 * time.Second},
	} {
		got, err := parseAge(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"garbage", "3d2h", "", "-7d"} {
		if _, err := parseAge(bad); err == nil {
			t.Errorf("parseAge(%q) accepted a non-duration", bad)
		}
	}
}

func TestTruncCell(t *testing.T) {
	if got := truncCell("short", 44); got != "short" {
		t.Errorf("short cell = %q", got)
	}
	if got := truncCell(strings.Repeat("e", 44), 44); got != strings.Repeat("e", 44) {
		t.Errorf("exact-width cell must pass through: %q", got)
	}
	got := truncCell(strings.Repeat("x", 60), 44)
	if utf8.RuneCountInString(got) != 44 || !strings.HasSuffix(got, "…") {
		t.Errorf("over-width cell = %q, want 44 runes ending in …", got)
	}
}

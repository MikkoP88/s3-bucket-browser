// source_split_test.go pins `source split` against a fake S3: the fan-out
// (one bucket-scoped source per visible bucket, named after the bucket),
// the account source + its mirrored profile going away, --dry-run's
// preview, the connection-match idempotence, and the refusals (scoped
// source, non-s3, bucket-less add).
package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// splitFakeS3 serves ListBuckets over a fixed bucket set — the only call
// a split makes against the endpoint.
func splitFakeS3(t *testing.T, buckets ...string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Trim(r.URL.Path, "/") != "" {
			http.Error(w, "unexpected bucket call: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		var sb strings.Builder
		sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		sb.WriteString("<ListAllMyBucketsResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
		sb.WriteString("<Owner><ID>fake</ID><DisplayName>fake</DisplayName></Owner><Buckets>")
		for _, b := range buckets {
			sb.WriteString("<Bucket><Name>" + b + "</Name></Bucket>")
		}
		sb.WriteString("</Buckets></ListAllMyBucketsResult>")
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, sb.String())
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// splitAddAccount adds a legacy account-wide s3 source through the legacy
// profile face — the real-world entry path of every stored account source.
func splitAddAccount(t *testing.T, name, url string) {
	t.Helper()
	if code := Execute([]string{"profile", "add", name, "--endpoint", url,
		"--access-key", "AK", "--secret-key", "sec"}); code != 0 {
		t.Fatalf("profile add %s: exit %d", name, code)
	}
}

func TestSourceSplit(t *testing.T) {
	cliEnv(t)
	url := splitFakeS3(t, "team-files", "logs-2026")
	splitAddAccount(t, "hetzner", url)

	// dry-run previews, changes nothing
	out, code := captureOut(t, "source", "split", "hetzner", "--dry-run")
	if code != 0 || !strings.Contains(out, "team-files") || !strings.Contains(out, "logs-2026") {
		t.Fatalf("split --dry-run: exit %d, %s", code, out)
	}
	s := reloadStore(t)
	if len(s.Sources) != 1 || s.Sources[0].Bucket != "" {
		t.Fatalf("dry-run mutated the store: %+v", s.Sources)
	}

	// the split fans out and removes the account source + its profile
	out, code = captureOut(t, "source", "split", "hetzner")
	if code != 0 || !strings.Contains(out, "+ team-files") || !strings.Contains(out, "+ logs-2026") {
		t.Fatalf("source split: exit %d, %s", code, out)
	}
	s = reloadStore(t)
	if len(s.Sources) != 2 {
		t.Fatalf("after split: %d sources; want 2", len(s.Sources))
	}
	for _, src := range s.Sources {
		if src.Type != "s3" || src.Bucket == "" || src.Bucket != src.Name {
			t.Fatalf("source %q is not bucket-scoped to its own name: %+v", src.Name, src)
		}
	}
	if _, err := s.Get("hetzner"); err == nil {
		t.Fatal("the account's mirrored profile survived the split")
	}

	// a re-added account on the same connection splits to the SAME
	// sources — matched and refreshed, never duplicated
	splitAddAccount(t, "hetzner", url)
	out, code = captureOut(t, "source", "split", "hetzner")
	if code != 0 || !strings.Contains(out, "= team-files (refreshed)") || strings.Contains(out, "+ ") {
		t.Fatalf("re-split: exit %d, %s", code, out)
	}
	s = reloadStore(t)
	if len(s.Sources) != 2 {
		t.Fatalf("re-split duplicated sources: %d", len(s.Sources))
	}

	// source list speaks the migrated shape: bucket @ endpoint
	out, code = captureOut(t, "source", "list")
	if code != 0 || !strings.Contains(out, "team-files @") {
		t.Fatalf("source list: exit %d, %s", code, out)
	}
}

func TestSourceSplitRefusals(t *testing.T) {
	cliEnv(t)

	// scoped sources have nothing to split
	if code := Execute([]string{"source", "add", "pics", "--type", "s3",
		"--access-key", "AK", "--secret-key", "sec",
		"--endpoint", "https://s3.example.com", "--bucket", "holiday-pics"}); code != 0 {
		t.Fatalf("source add: exit %d", code)
	}
	if code := Execute([]string{"source", "split", "pics"}); code != exitUsage {
		t.Fatalf("scoped split: exit %d (want usage %d)", code, exitUsage)
	}

	// non-s3 sources are refused
	if code := Execute([]string{"source", "add", "lab", "--type", "local", "--root", "."}); code != 0 {
		t.Fatalf("local add: exit %d", code)
	}
	if code := Execute([]string{"source", "split", "lab"}); code != exitUsage {
		t.Fatalf("local split: exit %d (want usage %d)", code, exitUsage)
	}

	// unknown source
	if code := Execute([]string{"source", "split", "nope"}); code != exitUsage {
		t.Fatalf("unknown split: exit %d (want usage %d)", code, exitUsage)
	}
}

// TestSourceAddS3RequiresBucket pins the one-bucket rule at the door: an
// s3 add without --bucket (and without the s3:// shorthand) is a usage
// error — the CLI cannot create account-wide sources anymore.
func TestSourceAddS3RequiresBucket(t *testing.T) {
	cliEnv(t)
	if code := Execute([]string{"source", "add", "acc", "--type", "s3",
		"--access-key", "AK", "--secret-key", "sec",
		"--endpoint", "https://s3.example.com"}); code != exitUsage {
		t.Fatalf("bucket-less s3 add: exit %d (want usage %d)", code, exitUsage)
	}
	if s := reloadStore(t); len(s.Sources) != 0 {
		t.Fatalf("bucket-less add wrote to the store: %+v", s.Sources)
	}
}

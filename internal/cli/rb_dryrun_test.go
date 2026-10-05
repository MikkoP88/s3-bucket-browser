// rb_dryrun_test.go pins rb --dry-run's count-then-act preview against a
// fake S3: the walk totals content objects and bytes (a folder marker is
// not content), the JSON form reports the exact numbers, and the plain
// form names --force when the bucket is not empty — nothing is removed
// either way. A versioned bucket previews the history a forced removal
// would really purge — versions and delete markers, their bytes — not
// just the live objects a plain listing shows.
package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRbDryRun(t *testing.T) {
	cliEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket := strings.Trim(r.URL.Path, "/")
		q := r.URL.Query()
		switch {
		case bucket == "vlab" && q.Has("versioning"):
			io.WriteString(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+
				"<VersioningConfiguration xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">"+
				"<Status>Enabled</Status></VersioningConfiguration>")
		case bucket == "vlab" && q.Has("versions"):
			io.WriteString(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+
				"<ListVersionsResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">"+
				"<Name>vlab</Name><IsTruncated>false</IsTruncated>"+
				"<Version><Key>a.txt</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest>"+
				"<LastModified>2026-10-02T00:00:00.000Z</LastModified><Size>100</Size></Version>"+
				"<Version><Key>a.txt</Key><VersionId>v0</VersionId><IsLatest>false</IsLatest>"+
				"<LastModified>2026-10-01T00:00:00.000Z</LastModified><Size>40</Size></Version>"+
				"<DeleteMarker><Key>b.txt</Key><VersionId>m1</VersionId><IsLatest>true</IsLatest>"+
				"<LastModified>2026-10-02T00:00:00.000Z</LastModified></DeleteMarker>"+
				"</ListVersionsResult>")
		case bucket == "lab" && q.Has("list-type"):
			objects := []struct {
				key  string
				size int64
			}{{"readme.md", 100}, {"docs/", 0}, {"docs/x.txt", 50}}
			prefix := q.Get("prefix")
			var sb strings.Builder
			sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
			sb.WriteString("<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
			sb.WriteString("<Name>lab</Name><IsTruncated>false</IsTruncated>")
			for _, o := range objects {
				if !strings.HasPrefix(o.key, prefix) {
					continue
				}
				sb.WriteString("<Contents><Key>" + o.key + "</Key>")
				sb.WriteString("<LastModified>2026-10-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag>")
				sb.WriteString("<Size>" + strconv.FormatInt(o.size, 10) + "</Size></Contents>")
			}
			sb.WriteString("</ListBucketResult>")
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, sb.String())
		default:
			// every other shape (a DELETE above all) is unexpected: the
			// preview must not remove anything
			http.Error(w, "unexpected call: "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	if code := Execute([]string{"source", "add", "store", "--type", "s3",
		"--endpoint", srv.URL, "--access-key", "k", "--secret-key", "s"}); code != 0 {
		t.Fatalf("source add: exit %d", code)
	}

	// JSON form: two content objects (the docs/ marker is not content),
	// 150 bytes.
	j, code := captureOut(t, "rb", "s3://lab", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("rb --dry-run --json: exit %d (%s)", code, j)
	}
	if !strings.Contains(j, `"objects": 2`) || !strings.Contains(j, `"bytes": 150`) {
		t.Fatalf("dry-run JSON totals: %s", j)
	}

	// Plain form: the not-empty hint names --force, and the fake saw no
	// delete — only listing calls exist on it.
	out, code := captureOut(t, "rb", "s3://lab", "--dry-run")
	if code != 0 {
		t.Fatalf("rb --dry-run: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "removal would need --force") {
		t.Fatalf("not-empty hint missing: %s", out)
	}

	// Versioned shape: two versions of a.txt (100 + 40 bytes) and one
	// delete marker — the whole history the forced removal would purge,
	// with the live-object listing nowhere in the numbers.
	vj, code := captureOut(t, "rb", "s3://vlab", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("rb --dry-run --json (versioned): exit %d (%s)", code, vj)
	}
	if !strings.Contains(vj, `"versions": 2`) || !strings.Contains(vj, `"markers": 1`) || !strings.Contains(vj, `"bytes": 140`) {
		t.Fatalf("dry-run versioned JSON totals: %s", vj)
	}
	vout, code := captureOut(t, "rb", "s3://vlab", "--dry-run")
	if code != 0 {
		t.Fatalf("rb --dry-run (versioned): exit %d (%s)", code, vout)
	}
	if !strings.Contains(vout, "removal would need --force") {
		t.Fatalf("versioned not-empty hint missing: %s", vout)
	}
}

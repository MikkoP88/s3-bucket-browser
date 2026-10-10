// cat_cmd_test.go pins `s3b cat` across its operand matrix: the lab
// engine's source leg (bytes exact, folders refused), the S3 wire leg
// on the edit fake's server (source URI and s3:// grammar, a missing
// key's honest failure), and the workstation path leg.
package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatSourceURILegs(t *testing.T) {
	srcEnv(t)

	out, code := captureOut(t, "cat", "lab://a.txt")
	if code != 0 || out != "alpha" {
		t.Fatalf("cat lab://a.txt: exit %d, out %q", code, out)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("cat must stream bytes unmodified — no added newline: %q", out)
	}

	out, code = captureOut(t, "cat", "lab://docs/b.txt")
	if code != 0 || out != "beta" {
		t.Fatalf("cat lab://docs/b.txt: exit %d, out %q", code, out)
	}

	// folders and roots refuse up front — a stream names one file
	if code := Execute([]string{"cat", "lab://docs"}); code != exitUsage {
		t.Fatalf("cat on a folder: exit %d (want usage %d)", code, exitUsage)
	}
	if code := Execute([]string{"cat", "lab://"}); code != exitUsage {
		t.Fatalf("cat on a source root: exit %d (want usage %d)", code, exitUsage)
	}

	// --json refuses: the stream is raw bytes
	if code := Execute([]string{"cat", "lab://a.txt", "--json"}); code != exitUsage {
		t.Fatalf("cat --json: exit %d (want usage %d)", code, exitUsage)
	}
}

func TestCatS3Legs(t *testing.T) {
	cliEnv(t)
	fake := &fakeEditS3{objs: map[string]string{}, etags: map[string]string{}}
	fake.put("a.txt", "alpha")
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	if code := Execute([]string{"source", "add", "store", "--type", "s3",
		"--endpoint", srv.URL, "--access-key", "k", "--secret-key", "s",
		"--bucket", "lab"}); code != 0 {
		t.Fatalf("source add: exit %d", code)
	}

	// the source-URI grammar: the S3-type source dials its own client
	out, code := captureOut(t, "cat", "store://a.txt")
	if code != 0 || out != "alpha" {
		t.Fatalf("cat store://a.txt: exit %d, out %q", code, out)
	}

	// the s3:// grammar rides the view profile (the source add made it
	// the sole one)
	out, code = captureOut(t, "cat", "s3://lab/a.txt")
	if code != 0 || out != "alpha" {
		t.Fatalf("cat s3://lab/a.txt: exit %d, out %q", code, out)
	}

	// a missing key fails honestly with the wire's own refusal
	if code := Execute([]string{"cat", "s3://lab/no-such.txt"}); code == 0 {
		t.Fatalf("cat on a missing key must fail, printed nothing")
	}

	// folders refuse
	if code := Execute([]string{"cat", "s3://lab"}); code != exitUsage {
		t.Fatalf("cat on a bucket: exit %d (want usage %d)", code, exitUsage)
	}
}

func TestCatLocalLeg(t *testing.T) {
	cliEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(p, []byte("local bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := captureOut(t, "cat", p)
	if code != 0 || out != "local bytes" {
		t.Fatalf("cat local: exit %d, out %q", code, out)
	}
	if code := Execute([]string{"cat", dir}); code != exitUsage {
		t.Fatalf("cat on a directory: exit %d (want usage %d)", code, exitUsage)
	}
}

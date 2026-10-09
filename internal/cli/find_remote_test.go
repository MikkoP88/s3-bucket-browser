// find_remote_test.go pins `s3b find`'s source-URI legs on the hermetic
// lab engine: the same filter pipeline the S3 runs speak, the --class
// refusal, the path-relative hit grammar, the stderr summary and the
// JSON face.
package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestFindSourceURILegs(t *testing.T) {
	srcEnv(t)

	// A nested hit speaks its path relative to the searched root —
	// the S3 leg's FromObject grammar.
	out, code := captureOut(t, "find", "lab://", "--name", "b.txt")
	if code != 0 {
		t.Fatalf("find lab:// --name b.txt: exit %d (%s)", code, out)
	}
	if !strings.Contains(out, "docs/b.txt") {
		t.Fatalf("hit must speak its path relative to the searched root: %s", out)
	}

	// The same search scoped under the folder: the hit is relative to
	// the folder now.
	out, code = captureOut(t, "find", "lab://docs", "--name", "b.txt")
	if code != 0 || !strings.Contains(out, "b.txt") || strings.Contains(out, "docs/") {
		t.Fatalf("find lab://docs: exit %d, %s", code, out)
	}

	// Extension and kind filters ride the shared pipeline.
	out, code = captureOut(t, "find", "lab://", "--kind", "dir")
	if code != 0 || !strings.Contains(out, "docs/") {
		t.Fatalf("find --kind dir: exit %d, %s", code, out)
	}
	if strings.Contains(out, "a.txt") {
		t.Fatalf("--kind dir must not match files: %s", out)
	}
	out, code = captureOut(t, "find", "lab://docs", "--ext", "txt")
	if code != 0 || !strings.Contains(out, "b.txt") {
		t.Fatalf("find --ext txt: exit %d, %s", code, out)
	}

	// The summary line speaks the source URI grammar on stderr: the lab
	// tree walks a.txt, docs, docs/b.txt — three scanned, one matched.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	code = Execute([]string{"find", "lab://", "--name", "b.txt"})
	w.Close()
	os.Stderr = oldStderr
	buf, _ := io.ReadAll(r)
	if code != 0 {
		t.Fatalf("find summary leg: exit %d", code)
	}
	if s := string(buf); !strings.Contains(s, "1 match(es) among 3 scanned under lab://") {
		t.Fatalf("stderr summary = %q, want the lab:// grammar", s)
	}

	// The JSON face carries the source and the relative name.
	j, code := captureOut(t, "find", "lab://", "--name", "b.txt", "--json")
	if code != 0 {
		t.Fatalf("find --json: exit %d, %s", code, j)
	}
	if !strings.Contains(j, `"source": "lab"`) || !strings.Contains(j, `"name": "docs/b.txt"`) {
		t.Fatalf("find --json shape: %s", j)
	}
	if strings.Contains(j, "storageClass") {
		t.Fatalf("remote hits carry no storage class: %s", j)
	}

	// --limit stops the walk after the first match.
	out, code = captureOut(t, "find", "lab://", "--kind", "file", "--limit", "1")
	if code != 0 {
		t.Fatalf("find --limit 1: exit %d, %s", code, out)
	}
	hits := strings.Count(out, "\n")
	if hits != 1 {
		t.Fatalf("--limit 1 must print exactly one hit, got %d: %s", hits, out)
	}
}

func TestFindSourceClassRefused(t *testing.T) {
	srcEnv(t)

	// A --class filter over a source is the GUI search's refusal, spoken
	// up front as a usage error — remote trees carry no storage class.
	if code := Execute([]string{"find", "lab://", "--class", "GLACIER"}); code != exitUsage {
		t.Fatalf("--class on a source: exit %d (want usage %d)", code, exitUsage)
	}
	// The mix refuses under a folder scope too.
	if code := Execute([]string{"find", "lab://docs", "--class", "STANDARD"}); code != exitUsage {
		t.Fatalf("--class under a folder: exit %d (want usage %d)", code, exitUsage)
	}
}

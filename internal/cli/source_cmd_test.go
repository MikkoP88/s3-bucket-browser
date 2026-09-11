package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// cliEnv points the store at a throwaway config dir with the OS keyring
// disabled, so command-level tests are hermetic.
func cliEnv(t *testing.T) {
	t.Helper()
	t.Setenv("S3B_CONFIG", t.TempDir())
	t.Setenv("S3B_NO_KEYRING", "1")
}

// reloadStore re-reads the store under the active $S3B_CONFIG.
func reloadStore(t *testing.T) *profile.Store {
	t.Helper()
	s, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// captureOut swaps the CLI output writer for the duration of one command.
func captureOut(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	old := out
	out = &buf
	code := Execute(args)
	out = old
	return buf.String(), code
}

func TestSourceAddListUseRemove(t *testing.T) {
	cliEnv(t)
	dir := t.TempDir()

	if code := Execute([]string{"source", "add", "data", "--type", "local", "--root", dir}); code != 0 {
		t.Fatalf("add local: exit %d", code)
	}
	if code := Execute([]string{"source", "add", "prod", "--type", "s3",
		"--endpoint", "http://localhost:9000", "--access-key", "a", "--secret-key", "s"}); code != 0 {
		t.Fatalf("add s3: exit %d", code)
	}

	// JSON list shows both; the s3 secret stays masked.
	j, code := captureOut(t, "source", "list", "--json")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	if !strings.Contains(j, `"data"`) || !strings.Contains(j, `"prod"`) {
		t.Fatalf("list output missing sources: %s", j)
	}
	if strings.Contains(j, `"secretKey": "s"`) {
		t.Fatal("list --json leaked the secret key")
	}

	// The s3 source is mirrored as a legacy profile.
	s := reloadStore(t)
	if _, err := s.Get("prod"); err != nil {
		t.Fatalf("mirror profile missing: %v", err)
	}
	if _, err := s.GetSource("data"); err != nil {
		t.Fatalf("local source missing: %v", err)
	}

	// use: default flag lands on both sides.
	if code := Execute([]string{"source", "use", "prod"}); code != 0 {
		t.Fatalf("use: exit %d", code)
	}
	s = reloadStore(t)
	src, err := s.GetSource("prod")
	if err != nil || !src.Default {
		t.Fatalf("prod must be the default source after use: %v %+v", err, src)
	}

	// remove: an s3 source takes its mirror profile with it.
	if code := Execute([]string{"source", "remove", "prod"}); code != 0 {
		t.Fatalf("remove: exit %d", code)
	}
	s = reloadStore(t)
	if _, err := s.GetSource("prod"); err == nil {
		t.Error("prod source still present after remove")
	}
	if _, err := s.Get("prod"); err == nil {
		t.Error("mirror profile still present after remove")
	}
}

func TestSourceAddRemoteAndGuards(t *testing.T) {
	cliEnv(t)
	t.Setenv("S3B_PASSWORD", "pw123")

	if code := Execute([]string{"source", "add", "ftpbox", "--type", "ftp",
		"--host", "files.example.com", "--username", "u"}); code != 0 {
		t.Fatalf("add ftp: exit %d", code)
	}
	s := reloadStore(t)
	src, err := s.GetSource("ftpbox")
	if err != nil {
		t.Fatal(err)
	}
	if src.Password != "pw123" {
		t.Error("$S3B_PASSWORD must be stored as the remote password")
	}
	if got := sourceDetail(src); !strings.Contains(got, "files.example.com:21") {
		t.Errorf("detail must show the per-type default port, got %q", got)
	}

	// use on a non-s3 source is a usage error (exit 2).
	if code := Execute([]string{"source", "use", "ftpbox"}); code != exitUsage {
		t.Errorf("use on non-s3: exit %d, want %d", code, exitUsage)
	}
	// test now really dials the remotefs engine: a local source over a
	// temp directory connects and exits 0…
	if code := Execute([]string{"source", "add", "disk", "--type", "local",
		"--root", t.TempDir()}); code != 0 {
		t.Fatalf("add local: exit %d", code)
	}
	if code := Execute([]string{"source", "test", "disk"}); code != exitOK {
		t.Errorf("test on local: exit %d, want %d", code, exitOK)
	}
	// …and an unreachable remote source fails honestly (exit 1). Port 1 on
	// loopback refuses without depending on DNS behavior.
	if code := Execute([]string{"source", "add", "dead", "--type", "sftp",
		"--host", "127.0.0.1", "--port", "1", "--username", "u", "--password", "x"}); code != 0 {
		t.Fatalf("add dead: exit %d", code)
	}
	if code := Execute([]string{"source", "test", "dead"}); code != exitOpFail {
		t.Errorf("test on unreachable sftp: exit %d, want %d", code, exitOpFail)
	}
	// --default only applies to s3 sources.
	if code := Execute([]string{"source", "add", "s2", "--type", "sftp", "--host", "h", "--default"}); code != exitUsage {
		t.Errorf("--default on non-s3: exit %d, want %d", code, exitUsage)
	}
	// unknown type is a usage error.
	if code := Execute([]string{"source", "add", "x", "--type", "nfs"}); code != exitUsage {
		t.Errorf("unknown type: exit %d, want %d", code, exitUsage)
	}
}

func TestSourceAddURLShorthand(t *testing.T) {
	cliEnv(t)

	// NAME + URL: every component lands in the saved source.
	if code := Execute([]string{"source", "add", "vault", "sftp://deploy:hunter2@files.example.com:2222/srv/data"}); code != 0 {
		t.Fatalf("add NAME URL: exit %d", code)
	}
	src, err := reloadStore(t).GetSource("vault")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != profile.TypeSFTP || src.Host != "files.example.com" || src.Port != 2222 ||
		src.Username != "deploy" || src.Password != "hunter2" || src.Root != "/srv/data" {
		t.Fatalf("URL components not stored: %+v", src)
	}

	// URL only: hostname becomes the name; port 0 keeps the per-type
	// default at dial time; empty path keeps the login directory.
	if code := Execute([]string{"source", "add", "ftp://e2e@127.0.0.1"}); code != 0 {
		t.Fatalf("add URL only: exit %d", code)
	}
	src, err = reloadStore(t).GetSource("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != profile.TypeFTP || src.Port != 0 || src.Username != "e2e" || src.Root != "" {
		t.Fatalf("URL-only form wrong: %+v", src)
	}

	// Percent-encoding carries special characters in the password.
	if code := Execute([]string{"source", "add", "enc", "ftps://u:p%40ss@box.example.com:990"}); code != 0 {
		t.Fatalf("add encoded URL: exit %d", code)
	}
	if src, err = reloadStore(t).GetSource("enc"); err != nil || src.Password != "p@ss" {
		t.Fatalf("percent-decoded password wrong: %+v %v", src, err)
	}

	// Guards: URL wins, conflicting flags are usage errors.
	if code := Execute([]string{"source", "add", "x", "sftp://h.example.com", "--host", "other"}); code != exitUsage {
		t.Errorf("URL + --host: exit %d, want %d", code, exitUsage)
	}
	if code := Execute([]string{"source", "add", "x", "--type", "ftp", "sftp://h.example.com"}); code != exitUsage {
		t.Errorf("conflicting --type: exit %d, want %d", code, exitUsage)
	}
	if code := Execute([]string{"source", "add", "x", "not-a-url"}); code != exitUsage {
		t.Errorf("non-URL second arg: exit %d, want %d", code, exitUsage)
	}
	if code := Execute([]string{"source", "add", "sftp://host:99999"}); code != exitUsage {
		t.Errorf("invalid port: exit %d, want %d", code, exitUsage)
	}
}

func TestProfileCommandsMirrorSources(t *testing.T) {
	cliEnv(t)

	// The deprecated legacy write paths keep the source mirror in sync.
	if code := Execute([]string{"profile", "add", "legacy", "--access-key", "a", "--secret-key", "s"}); code != 0 {
		t.Fatalf("profile add: exit %d", code)
	}
	s := reloadStore(t)
	if _, err := s.GetSource("legacy"); err != nil {
		t.Fatalf("profile add did not mirror a source: %v", err)
	}

	if code := Execute([]string{"profile", "remove", "legacy"}); code != 0 {
		t.Fatalf("profile remove: exit %d", code)
	}
	s = reloadStore(t)
	if _, err := s.GetSource("legacy"); err == nil {
		t.Error("mirror source survived profile remove")
	}
}

func TestSourceExportImportRoundTrip(t *testing.T) {
	cliEnv(t)
	if code := Execute([]string{"source", "add", "data", "--type", "local", "--root", t.TempDir()}); code != 0 {
		t.Fatalf("add local: exit %d", code)
	}
	if code := Execute([]string{"source", "add", "prod", "--type", "s3",
		"--endpoint", "http://localhost:9000", "--access-key", "a", "--secret-key", "s", "--default"}); code != 0 {
		t.Fatalf("add s3: exit %d", code)
	}

	file := filepath.Join(t.TempDir(), "team.s3bprofile")
	if code := Execute([]string{"source", "export", file, "--password", "hunter2"}); code != 0 {
		t.Fatalf("export: exit %d", code)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "s3bpf1|") {
		t.Fatalf("exported file lacks the s3bpf1 magic: %q", string(b[:16]))
	}

	// Fresh store: import restores both sources (and the s3 mirror).
	t.Setenv("S3B_CONFIG", t.TempDir())
	if code := Execute([]string{"source", "import", file, "--password", "hunter2"}); code != 0 {
		t.Fatalf("import: exit %d", code)
	}
	s := reloadStore(t)
	if _, err := s.GetSource("data"); err != nil {
		t.Error("local source not imported")
	}
	src, err := s.GetSource("prod")
	if err != nil {
		t.Fatal("s3 source not imported")
	}
	if !src.Default {
		t.Error("default flag must survive the round trip")
	}
	if _, err := s.Get("prod"); err != nil {
		t.Error("imported s3 source must carry its mirror profile")
	}

	// Re-import skips names that already exist.
	if code := Execute([]string{"source", "import", file, "--password", "hunter2"}); code != 0 {
		t.Fatalf("re-import: exit %d", code)
	}
	// A wrong password is an operation failure (exit 1).
	if code := Execute([]string{"source", "import", file, "--password", "wrong"}); code != exitOpFail {
		t.Errorf("wrong password: exit %d, want %d", code, exitOpFail)
	}
}

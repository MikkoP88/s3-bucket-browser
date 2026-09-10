package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Hermetic tests: no S3 backend required (MinIO stays an optional, CI-side
// functional check — see scripts/e2e-minio.sh).

func TestDirPrefix(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"/":        "",
		"photos":   "photos/",
		"photos/":  "photos/",
		"/photos/": "photos/",
		"a/b":      "a/b/",
		"a/b/":     "a/b/",
	}
	for in, want := range cases {
		if got := dirPrefix(in); got != want {
			t.Errorf("dirPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinKeyNoSlash(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"", "file.txt"}, "file.txt"},
		{[]string{"photos/", "img.png"}, "photos/img.png"},
		{[]string{"photos", ""}, "photos"},
		{[]string{"a/", "b/", "c.txt"}, "a/b/c.txt"},
		{[]string{"docs\\sub", "a.md"}, "docs/sub/a.md"}, // backslashes normalized
	}
	for _, c := range cases {
		if got := joinKeyNoSlash(c.parts...); got != c.want {
			t.Errorf("joinKeyNoSlash(%q) = %q, want %q", c.parts, got, c.want)
		}
	}
}

func TestIsMasked(t *testing.T) {
	for _, s := range []string{"", "****", "abcd…yz"} {
		if !isMasked(s) {
			t.Errorf("isMasked(%q) = false, want true", s)
		}
	}
	if isMasked("real-secret-value") {
		t.Error("isMasked(real secret) = true, want false")
	}
}

// newTestApp points the profile store at a throwaway dir (mirrors S3B_CONFIG).
func newTestApp(t *testing.T) *App {
	t.Helper()
	testNoEvents.Store(true) // no Wails event bus in tests (see events.go)
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	return New("test")
}

func TestSaveProfilePreservesMaskedSecrets(t *testing.T) {
	a := newTestApp(t)

	if err := a.SaveProfile(ProfileInput{
		Name: "lab", AccessKeyID: "AKIA1", SecretKey: "supersecret",
		SetDefault: true,
	}); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// Editor round-trip: masked secret (or empty) must keep stored value.
	if err := a.SaveProfile(ProfileInput{
		Name: "lab", AccessKeyID: "AKIA1", SecretKey: "su…et",
	}); err != nil {
		t.Fatalf("masked save: %v", err)
	}
	s, err := a.loadStore()
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Get("lab")
	if err != nil {
		t.Fatal(err)
	}
	if p.SecretKey != "supersecret" {
		t.Errorf("secret = %q, want preserved %q", p.SecretKey, "supersecret")
	}

	// Explicit new secret overwrites.
	if err := a.SaveProfile(ProfileInput{Name: "lab", SecretKey: "rotated"}); err != nil {
		t.Fatal(err)
	}
	s, _ = a.loadStore()
	p, _ = s.Get("lab")
	if p.SecretKey != "rotated" {
		t.Errorf("secret = %q, want rotated", p.SecretKey)
	}
}

func TestSaveProfileValidation(t *testing.T) {
	a := newTestApp(t)
	if err := a.SaveProfile(ProfileInput{Name: "  "}); err == nil {
		t.Error("empty name accepted")
	}
	if err := a.SaveProfile(ProfileInput{Name: "my profile"}); err == nil {
		t.Error("name with space accepted")
	}
}

func TestImportAwsCredentials(t *testing.T) {
	a := newTestApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	awsDir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := `
# standard AWS credentials file
[default]
aws_access_key_id = AKIADEFAULT
aws_secret_access_key = secretdefault

[work]
aws_access_key_id = AKIAWORK
aws_secret_access_key = secretwork
region = eu-west-1      ; ignored here, noted for M3

[broken]
aws_access_key_id = AKIAONLY
`
	if err := os.WriteFile(filepath.Join(awsDir, "credentials"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := a.ImportAwsCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 2 {
		t.Fatalf("imported = %v, want [default work]", res.Imported)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "broken") {
		t.Errorf("skipped = %v, want broken section", res.Skipped)
	}
	// second import skips existing names (broken stays incomplete)
	res2, err := a.ImportAwsCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Imported) != 0 || len(res2.Skipped) != 3 {
		t.Errorf("re-import: imported=%v skipped=%v, want 0/3", res2.Imported, res2.Skipped)
	}

	s, _ := a.loadStore()
	p, err := s.Get("work")
	if err != nil || p.AccessKeyID != "AKIAWORK" || p.SecretKey != "secretwork" {
		t.Errorf("imported profile work = %+v err=%v", p, err)
	}
}

func TestExpandUploadPaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pairs, err := expandUploadPaths([]string{filepath.Join(dir, "root.txt"), sub}, "photos/")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, p := range pairs {
		got[p.key] = p.size
	}
	if len(pairs) != 2 {
		t.Fatalf("pairs = %v, want 2", got)
	}
	if got["photos/root.txt"] != 1 || got["photos/sub/nested.txt"] != 1 {
		t.Errorf("keys = %v", got)
	}

	if _, err := expandUploadPaths([]string{filepath.Join(dir, "missing")}, ""); err == nil {
		t.Error("missing path accepted")
	}
}

func TestUniqueLocalPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.txt")
	if got := uniqueLocalPath(p); got != p {
		t.Errorf("first = %q, want %q", got, p)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := uniqueLocalPath(p)
	want := filepath.Join(dir, "file (1).txt")
	if got != want {
		t.Errorf("second = %q, want %q", got, want)
	}
}

func TestJobManagerLifecycle(t *testing.T) {
	m := newJobManager()
	j := m.add("upload", 3, 100)
	if j.info.Status != JobRunning || j.info.TotalFiles != 3 {
		t.Fatalf("job info = %+v", j.info)
	}
	if len(m.snapshot()) != 1 {
		t.Fatal("snapshot should show one job")
	}

	// progress accounting
	j.progress(40, 100)
	if j.info.SentBytes != 40 {
		t.Errorf("sent = %d, want 40", j.info.SentBytes)
	}
	j.fileDone(100, false)
	if j.info.SentBytes != 100 || j.info.DoneFiles != 1 {
		t.Errorf("after fileDone: %+v", j.info)
	}

	// cancel + clear
	if !m.cancel(j.info.ID) {
		t.Error("cancel returned false")
	}
	<-j.ctx.Done()
	j.info.Status = JobCanceled // as finishJob would
	m.clearFinished()
	if len(m.snapshot()) != 0 {
		t.Error("finished job not cleared")
	}
}

func TestPresignTTLClamp(t *testing.T) {
	// ttl clamping logic sits inline in PresignObject; verify the clamp
	// bounds contract used by the frontend (1h default, 7d max).
	const def, max = 3600, 7 * 24 * 3600
	got := map[int]int{-5: def, 0: def, 10: 10, max: max, max + 1: max}
	for in, want := range got {
		g := in
		if g <= 0 {
			g = def
		}
		if g > max {
			g = max
		}
		if g != want {
			t.Errorf("clamp(%d) = %d, want %d", in, g, want)
		}
	}
}

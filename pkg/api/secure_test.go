// secure_test.go — deep validation of the GUI-side secure-storage wiring
// (pkg/api/secure.go, clipboard.go, editor/xfer/pick workspace placement):
// per-mode temp locations, the crash-leftover wipe, the Settings
// enable/disable round-trip with a plaintext leak scan, file logging forced
// off, and the pre-signed-URL scrub arming.
package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/zalando/go-keyring"
)

// TestMain keeps the package hermetic like pkg/core/profile: the OS keyring
// is disabled unless S3B_TEST_KEYRING=1, so no test run ever touches the
// real Credential Manager / Keychain.
func TestMain(m *testing.M) {
	if os.Getenv("S3B_TEST_KEYRING") != "1" {
		os.Setenv("S3B_NO_KEYRING", "1")
	}
	os.Exit(m.Run())
}

// mockKeyring swaps in go-keyring's in-memory provider and re-enables
// keyring use for this test (TestMain set S3B_NO_KEYRING=1; keyringOnce has
// not fired because the env check short-circuits before the probe).
func mockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("S3B_NO_KEYRING", "")
}

// writeEnvelope drops a minimal s3bsf1 marker store at path — enough for
// SecureModeOnDisk()/workspaceBase() to see secure mode.
func writeEnvelope(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("s3bsf1|AAAA|BBBB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceBasePerMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)

	// plain mode: the shared system temp dir (owner-only workspaces)
	for name, want := range map[string]string{
		"edit": filepath.Join(os.TempDir(), "s3b-edit"),
		"tmp":  filepath.Join(os.TempDir(), "s3b-tmp"),
		"clip": filepath.Join(os.TempDir(), "s3b-clip"),
	} {
		if got := workspaceBase(name); got != want {
			t.Fatalf("plain workspaceBase(%q) = %q, want %q", name, got, want)
		}
	}

	// secure mode: inside the config dir (0700-protected like profiles.json)
	writeEnvelope(t, filepath.Join(dir, "profiles.json"))
	for name, want := range map[string]string{
		"edit": filepath.Join(dir, "edit"),
		"tmp":  filepath.Join(dir, "tmp"),
		"clip": filepath.Join(dir, "clip"),
	} {
		if got := workspaceBase(name); got != want {
			t.Fatalf("secure workspaceBase(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWipeWorkspacesSweepsEveryLocation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	a := New("test")

	// seed a crash leftover into every location both modes use
	mk := func(p string) string {
		t.Helper()
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(p, "object-content.txt")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}
	leftovers := []string{
		mk(filepath.Join(os.TempDir(), "s3b-edit", "bucket")), // plain editor
		mk(filepath.Join(os.TempDir(), "s3b-tmp")),            // plain spool
		mk(filepath.Join(dir, "edit")),                        // secure editor
		mk(filepath.Join(dir, "tmp")),                         // secure spool
		mk(filepath.Join(dir, "clip")),                        // secure clip
	}
	clip, err := os.MkdirTemp(os.TempDir(), "s3b-clip-") // plain clip staging
	if err != nil {
		t.Fatal(err)
	}
	leftovers = append(leftovers, filepath.Join(clip, "staged.txt"))
	if err := os.WriteFile(filepath.Join(clip, "staged.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	a.wipeWorkspaces()

	for _, p := range leftovers {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("crash leftover survived the wipe: %s (stat err %v)", p, err)
		}
	}
}

func TestStageClipboardDirPerMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	a := New("test")

	plain, err := a.StageClipboardDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(plain)
	if !strings.HasPrefix(plain, filepath.Join(os.TempDir(), "s3b-clip")) {
		t.Fatalf("plain staging dir outside the clip workspace: %s", plain)
	}

	writeEnvelope(t, filepath.Join(dir, "profiles.json"))
	secure, err := a.StageClipboardDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(secure)
	if !strings.HasPrefix(secure, filepath.Join(dir, "clip")) {
		t.Fatalf("secure staging dir outside the config dir: %s", secure)
	}
}

func TestSetSecureStorageRoundTrip(t *testing.T) {
	mockKeyring(t)
	a := newTestApp(t)
	cfg, err := profile.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}

	// seed a plaintext store with recognizable metadata
	s, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(profile.Profile{Name: "demo", Endpoint: "http://localhost:9000", AccessKeyID: "minioadmin"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	storePath, err := profile.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "localhost:9000") {
		t.Fatal("sanity: plaintext store should carry the endpoint")
	}

	// ---- enable: envelope on disk, no metadata leaks, file logging off
	st, err := a.SetSecureStorage(true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.KeyringAvailable {
		t.Fatalf("status after enable: %+v", st)
	}
	if st.EditorDir != filepath.Join(cfg, "edit") || st.SpoolDir != filepath.Join(cfg, "tmp") {
		t.Fatalf("secure workspaces not reported in the config dir: %+v", st)
	}
	raw, err = os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "s3bsf1|") {
		t.Fatalf("store not sealed: %q", raw[:min(40, len(raw))])
	}
	for _, leak := range []string{"demo", "localhost:9000", "minioadmin"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("sealed store leaks %q on disk", leak)
		}
	}
	if ls := eventlog.LoadSettings(); ls.Mode != "off" {
		t.Fatalf("file log mode after enable = %q, want off", ls.Mode)
	}

	// ---- the GUI keeps working on the sealed store
	s2, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get("demo"); err != nil {
		t.Fatalf("sealed store unreadable through Load: %v", err)
	}

	// ---- disable: plain JSON back, master key removed
	st2, err := a.SetSecureStorage(false)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Enabled {
		t.Fatal("status still enabled after disable")
	}
	raw, err = os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(raw), "s3bsf1|") {
		t.Fatal("disable left the store encrypted")
	}
	if !strings.Contains(string(raw), "demo") {
		t.Fatal("decrypted store lost its data")
	}

	// ---- enable-refusal without a keyring stays loud, not destructive
	t.Setenv("S3B_NO_KEYRING", "1")
	if _, err := a.SetSecureStorage(true); err == nil {
		t.Fatal("enable without a keyring must fail")
	}
	raw, err = os.ReadFile(storePath)
	if err != nil || strings.HasPrefix(string(raw), "s3bsf1|") {
		t.Fatal("refused enable still touched the store")
	}
}

func TestPresignURLClassification(t *testing.T) {
	yes := []string{
		"https://b.s3.amazonaws.com/k?X-Amz-Signature=abc",
		"https://b.example.com/k?X-Amz-Expires=60&X-Amz-Signature=xyz",
		"https://legacy.example.com/k?AWSAccessKeyId=ak&Signature=qq",
	}
	no := []string{
		"",
		"readme.md",
		`C:\Users\me\file.txt`,
		"https://b.s3.amazonaws.com/k",
	}
	// note: a text that merely mentions the parameter in prose also matches
	// (substring heuristic) — acceptable: a false positive only clears the
	// clipboard 60 s later, and only under secure storage.
	for _, s := range yes {
		if !isPresignedURL(s) {
			t.Errorf("isPresignedURL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isPresignedURL(s) {
			t.Errorf("isPresignedURL(%q) = true, want false", s)
		}
	}
}

func TestPresignScrubArmedOnlyUnderSecureStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	a := New("test")

	var wrote string
	SetClipboardTextSink(func(s string) { wrote = s })
	t.Cleanup(func() { SetClipboardTextSink(nil) })
	pending := func() string {
		scrubMu.Lock()
		defer scrubMu.Unlock()
		return scrubPending
	}

	url := "https://b.s3.amazonaws.com/k?X-Amz-Signature=abc"

	// plain mode: the copy goes through, nothing is armed
	if err := a.ClipboardSetText(url); err != nil {
		t.Fatal(err)
	}
	if wrote != url {
		t.Fatalf("seam saw %q, want the URL", wrote)
	}
	if p := pending(); p != "" {
		t.Fatalf("plain mode armed a scrub (%q)", p)
	}

	// secure mode: the copy arms the delayed scrub
	writeEnvelope(t, filepath.Join(dir, "profiles.json"))
	if err := a.ClipboardSetText(url); err != nil {
		t.Fatal(err)
	}
	if p := pending(); p != url {
		t.Fatalf("secure mode did not arm the presign scrub (%q)", p)
	}

	// a later non-presigned copy never disturbs the pending scrub
	if err := a.ClipboardSetText("plain path.txt"); err != nil {
		t.Fatal(err)
	}
	if wrote != "plain path.txt" {
		t.Fatalf("seam saw %q, want the plain copy", wrote)
	}
	if p := pending(); p != url {
		t.Fatalf("non-presign copy disturbed the pending scrub (%q)", p)
	}
}

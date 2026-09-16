// secure_test.go — deep validation of the secure-store envelope (M9
// shared-host hardening): crypto round-trips, tamper/wrong-key rejection,
// plaintext leak scans of the raw file bytes, the enable/disable migration,
// and the loud-failure contract for encrypted stores on hosts without an
// OS keyring.
package profile

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// mockKeyring swaps in go-keyring's in-memory provider for this test.
// TestMain set S3B_NO_KEYRING=1; clearing it here lets keyringAvailable()
// reach the probe, which now hits the mock — no real Credential Manager /
// Keychain is ever touched. keyringOnce has not fired before because the
// env check short-circuits first.
func mockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("S3B_NO_KEYRING", "")
}

// randKey returns a fresh random master key.
func randKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, keyLen)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSecureEnvelopeRoundTrip(t *testing.T) {
	key := randKey(t)
	data := []byte(`{"profiles":[{"name":"demo","secretKey":"hunter2"}]}`)

	sealed, err := sealSecure(key, data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(sealed, []byte(secureMagic+"|")) {
		t.Fatalf("envelope lacks the %s magic: %q", secureMagic, sealed[:32])
	}
	// three '|'-separated parts, base64-clean
	parts := strings.Split(strings.TrimSpace(string(sealed)), "|")
	if len(parts) != 3 {
		t.Fatalf("want 3 envelope parts, got %d", len(parts))
	}
	for _, p := range parts[1:] {
		if _, err := base64.StdEncoding.DecodeString(p); err != nil {
			t.Fatalf("part is not base64: %v", err)
		}
	}
	// the plaintext must not leak into the envelope
	for _, secret := range []string{"hunter2", "demo", "secretKey"} {
		if bytes.Contains(sealed, []byte(secret)) {
			t.Fatalf("envelope leaks %q", secret)
		}
	}

	opened, err := openSecure(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, data) {
		t.Fatalf("round-trip mismatch: %q != %q", opened, data)
	}

	// fresh nonce per seal
	again, err := sealSecure(key, data)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals produced identical envelopes (nonce reuse?)")
	}
}

func TestOpenSecureRejectsTampering(t *testing.T) {
	key := randKey(t)
	sealed, err := sealSecure(key, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	// wrong key fails the GCM tag — generic error, no oracle
	wrong := randKey(t)
	if _, err := openSecure(wrong, sealed); err == nil || !strings.Contains(err.Error(), "cannot decrypt") {
		t.Fatalf("wrong key: want cannot-decrypt error, got %v", err)
	}

	// flipped ciphertext byte
	tampered := []byte(sealed)
	tampered[len(tampered)-4] ^= 0x01
	if _, err := openSecure(key, tampered); err == nil {
		t.Fatal("tampered ciphertext must not decrypt")
	}

	// flipped nonce byte
	tampered = []byte(sealed)
	tampered[len(secureMagic)+1] ^= 0x01
	if _, err := openSecure(key, tampered); err == nil {
		t.Fatal("tampered nonce must not decrypt")
	}

	// malformed envelopes
	for name, bad := range map[string]string{
		"bad magic":    "s3bsf0|" + strings.Split(strings.TrimSpace(string(sealed)), "|")[1] + "|" + strings.Split(strings.TrimSpace(string(sealed)), "|")[2],
		"two parts":    secureMagic + "|only-one-part",
		"empty ct":     secureMagic + "|" + base64.StdEncoding.EncodeToString(make([]byte, nonceLen)) + "|",
		"bad nonce":    secureMagic + "|!!not-base64!!|AAAA",
		"bad b64 body": secureMagic + "|" + base64.StdEncoding.EncodeToString(make([]byte, nonceLen)) + "|%%%%",
	} {
		if _, err := openSecure(key, []byte(bad)); err == nil {
			t.Fatalf("%s: openSecure accepted a malformed envelope", name)
		}
	}
}

func TestEnableSecureStorageLifecycle(t *testing.T) {
	mockKeyring(t)
	s := newTestStore(t)
	if err := s.Upsert(Profile{Name: "demo", Endpoint: "http://localhost:9000", AccessKeyID: "minioadmin", SecretKey: "supersecret", PathStyle: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(plain, []byte("http://localhost:9000")) && !bytes.Contains(plain, []byte("demo")) {
		t.Fatal("sanity: plaintext save should carry metadata (mock keyring may move secrets, not names)")
	}
	if bytes.Contains(plain, []byte("supersecret")) {
		t.Fatal("sanity: with a keyring the secret must already live there, not in the file")
	}

	// ---- enable: file becomes one opaque envelope
	if err := s.EnableSecureStorage(); err != nil {
		t.Fatal(err)
	}
	if !s.SecureEnabled() {
		t.Fatal("SecureEnabled() false after enable")
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !isSecureEnvelope(raw) {
		t.Fatalf("store on disk is not an envelope: %q", raw[:min(40, len(raw))])
	}
	// leak scan: names, endpoints, key ids — none may appear in the raw bytes
	for _, leak := range []string{"demo", "localhost:9000", "minioadmin", "profiles", "accessKeyId"} {
		if bytes.Contains(raw, []byte(leak)) {
			t.Fatalf("encrypted store leaks %q on disk", leak)
		}
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(s.Path); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("envelope permissions = %v (want 0600), err %v", st.Mode().Perm(), err)
		}
	}

	// ---- reload: decrypts transparently, data survives
	s2, err := LoadFrom(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.SecureEnabled() {
		t.Fatal("reloaded store lost the secure flag")
	}
	got, err := s2.Get("demo")
	if err != nil || got.AccessKeyID != "minioadmin" {
		t.Fatalf("profile did not survive the encryption round-trip: %v %+v", err, got)
	}

	// ---- a secure save stays an envelope
	if err := s2.Upsert(Profile{Name: "second", AccessKeyID: "ak"}); err != nil {
		t.Fatal(err)
	}
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(s.Path)
	if err != nil || !isSecureEnvelope(raw) {
		t.Fatal("save in secure mode did not keep the envelope")
	}
	if bytes.Contains(raw, []byte("second")) {
		t.Fatal("secure save leaked the new profile name")
	}

	// ---- disable: plaintext JSON returns, master key is deleted
	if err := s2.DisableSecureStorage(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if isSecureEnvelope(raw) {
		t.Fatal("disable left the store encrypted")
	}
	if !bytes.Contains(raw, []byte("demo")) {
		t.Fatal("decrypted store lost its data")
	}
	if k, err := loadMasterKey(); err != nil || k != nil {
		t.Fatalf("master key survived disable: %v %v", k, err)
	}
	s3, err := LoadFrom(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if s3.SecureEnabled() {
		t.Fatal("plain store reports secure mode")
	}
}

func TestEnableSecureStorageWithoutKeyringRefused(t *testing.T) {
	// TestMain's S3B_NO_KEYRING=1 stands in for a headless host.
	s := newTestStore(t)
	if err := s.Upsert(Profile{Name: "demo", AccessKeyID: "ak"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	err := s.EnableSecureStorage()
	if !errors.Is(err, ErrSecureNeedsKeyring) {
		t.Fatalf("enable without a keyring: want ErrSecureNeedsKeyring, got %v", err)
	}
	// the store must be untouched, not half-migrated
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if isSecureEnvelope(raw) {
		t.Fatal("refused enable still rewrote the store")
	}
}

func TestEncryptedStoreWithoutKeyringFailsLoud(t *testing.T) {
	// Build the encrypted store with a mock keyring, then simulate moving
	// the config to a host without one (the S3B_NO_KEYRING=1 stand-in for
	// headless/CI). Loading must fail loudly with remediation — never
	// silently degrade to plaintext, never scramble the file.
	mockKeyring(t)
	s := newTestStore(t)
	if err := s.Upsert(Profile{Name: "demo", AccessKeyID: "ak"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSecureStorage(); err != nil {
		t.Fatal(err)
	}
	path := s.Path

	t.Setenv("S3B_NO_KEYRING", "1")
	_, err := LoadFrom(path)
	if !errors.Is(err, ErrSecureNeedsKeyring) {
		t.Fatalf("want ErrSecureNeedsKeyring, got %v", err)
	}
	for _, want := range []string{"encrypted", "S3B_NO_KEYRING", "secure storage"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error lacks remediation text %q: %v", want, err)
		}
	}
	// and the envelope is still intact for a host WITH a keyring
	raw, err := os.ReadFile(path)
	if err != nil || !isSecureEnvelope(raw) {
		t.Fatal("failed load corrupted the encrypted store")
	}
}

func TestSecureModeOnDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	if SecureModeOnDisk() {
		t.Fatal("no store file, but secure mode reported")
	}
	path := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(path, []byte(`{"profiles":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if SecureModeOnDisk() {
		t.Fatal("plain store reported as secure")
	}
	if err := os.WriteFile(path, []byte(secureMagic+"|AAAA|BBBB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SecureModeOnDisk() {
		t.Fatal("envelope on disk not detected")
	}
}

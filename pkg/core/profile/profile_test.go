package profile

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), "profiles.json")}
}

func TestUpsertGetRemove(t *testing.T) {
	s := newTestStore(t)

	p := Profile{Name: "local", Endpoint: "http://localhost:9000", AccessKeyID: "minioadmin", SecretKey: "minioadmin", PathStyle: true}
	if err := s.Upsert(p); err != nil {
		t.Fatal(err)
	}
	if len(s.Profiles) != 1 {
		t.Fatalf("want 1 profile, got %d", len(s.Profiles))
	}

	// Update preserves CreatedAt and Default.
	s.Profiles[0].Default = true
	p.Region = "us-east-1"
	if err := s.Upsert(p); err != nil {
		t.Fatal(err)
	}
	if len(s.Profiles) != 1 || !s.Profiles[0].Default || s.Profiles[0].Region != "us-east-1" {
		t.Fatalf("upsert failed to update in place: %+v", s.Profiles)
	}

	got, err := s.Get("local")
	if err != nil || got.Region != "us-east-1" {
		t.Fatalf("Get failed: %v %+v", err, got)
	}
	if _, err := s.Get("nope"); err == nil {
		t.Fatal("Get(unknown) should fail")
	}

	if err := s.Remove("local"); err != nil {
		t.Fatal(err)
	}
	if len(s.Profiles) != 0 {
		t.Fatal("Remove left profiles behind")
	}
}

func TestDefaultProfile(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DefaultProfile(); err == nil {
		t.Fatal("empty store should have no default")
	}
	s.Upsert(Profile{Name: "a"})
	if _, err := s.DefaultProfile(); err != nil {
		t.Fatalf("single profile should be implicit default: %v", err)
	}
	s.Upsert(Profile{Name: "b"})
	if _, err := s.DefaultProfile(); err == nil {
		t.Fatal("two profiles without default should error")
	}
	if err := s.SetDefault("b"); err != nil {
		t.Fatal(err)
	}
	p, err := s.DefaultProfile()
	if err != nil || p.Name != "b" {
		t.Fatalf("DefaultProfile = %q, %v", p.Name, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(Profile{Name: "x", AccessKeyID: "AKIAEXAMPLE", SecretKey: "supersecret", PathStyle: true})
	s.SetDefault("x")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFrom(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := got.Get("x")
	if err != nil || p.SecretKey != "supersecret" || !p.Default || !p.PathStyle {
		t.Fatalf("round trip mismatch: %+v (%v)", p, err)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := LoadFrom(filepath.Join(t.TempDir(), "nothing.json"))
	if err != nil || len(s.Profiles) != 0 {
		t.Fatalf("missing file should give empty store, got %v, %v", s, err)
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"abc":      "****",
		"abcdefgh": "abcd…gh",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPublicMasksSecrets(t *testing.T) {
	p := Profile{Name: "n", AccessKeyID: "AKIA1234567890", SecretKey: "verylongsecret", SessionToken: "token123"}
	pub := p.Public()
	if pub.SecretKey == p.SecretKey || pub.SessionToken == p.SessionToken {
		t.Fatal("Public() must mask secrets")
	}
	if pub.AccessKeyID != p.AccessKeyID {
		t.Fatal("AccessKeyID is not a secret and should stay visible")
	}
}

func TestProviderDetection(t *testing.T) {
	p := Profile{Name: "n", Endpoint: "https://acct.r2.cloudflarestorage.com"}
	if p.Provider() != "cloudflare" {
		t.Errorf("Provider() = %q, want cloudflare", p.Provider())
	}
	if (Profile{Name: "aws-only"}).Provider() != "aws" {
		t.Error("empty endpoint should default to aws")
	}
}

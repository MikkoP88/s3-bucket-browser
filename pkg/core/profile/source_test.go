package profile

import (
	"strings"
	"testing"
	"time"
)

func TestSourceDefaultPort(t *testing.T) {
	cases := map[string]int{
		TypeS3: 0, TypeSFTP: 22, TypeSCP: 22, TypeFTP: 21, TypeFTPS: 990, TypeLocal: 0, "weird": 0,
	}
	for typ, want := range cases {
		if got := (Source{Type: typ}).DefaultPort(); got != want {
			t.Errorf("%s: DefaultPort = %d, want %d", typ, got, want)
		}
	}
}

func TestSourceValidate(t *testing.T) {
	cases := []struct {
		label string
		src   Source
		ok    bool
	}{
		{"no name", Source{Type: TypeS3, S3: &Profile{}}, false},
		{"s3 with profile", Source{Name: "a", Type: TypeS3, S3: &Profile{}}, true},
		{"s3 without profile", Source{Name: "a", Type: TypeS3}, false},
		{"sftp with host", Source{Name: "a", Type: TypeSFTP, Host: "h"}, true},
		{"scp with host", Source{Name: "a", Type: TypeSCP, Host: "h"}, true},
		{"ftp with host", Source{Name: "a", Type: TypeFTP, Host: "h"}, true},
		{"ftps with host", Source{Name: "a", Type: TypeFTPS, Host: "h"}, true},
		{"sftp without host", Source{Name: "a", Type: TypeSFTP}, false},
		{"sftp whitespace host", Source{Name: "a", Type: TypeSFTP, Host: "   "}, false},
		{"local with root", Source{Name: "a", Type: TypeLocal, LocalRoot: "/data"}, true},
		{"local without root", Source{Name: "a", Type: TypeLocal}, false},
		{"unknown type", Source{Name: "a", Type: "nfs"}, false},
	}
	for _, c := range cases {
		err := c.src.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: unexpected error %v", c.label, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected error, got nil", c.label)
		}
	}
}

func TestSourcePublicMasksSecrets(t *testing.T) {
	s := Source{
		Name:     "box",
		Type:     TypeSFTP,
		Host:     "h",
		Username: "u",
		Password: "supersecret",
		S3: &Profile{
			Name:        "inner",
			AccessKeyID: "AKIAEXAMPLE",
			SecretKey:   "s3secret",
		},
	}
	p := s.Public()
	if p.Password == "supersecret" || p.Password != Mask("supersecret") {
		t.Errorf("password not masked: %q", p.Password)
	}
	if p.S3 == nil || p.S3.SecretKey == "s3secret" {
		t.Errorf("embedded s3 secret not masked: %+v", p.S3)
	}
	if p.S3.AccessKeyID != "AKIAEXAMPLE" {
		t.Errorf("access key must stay readable: %q", p.S3.AccessKeyID)
	}
	if p.Host != "h" || p.Username != "u" {
		t.Error("Public must not touch non-secret fields")
	}
	if s.Password != "supersecret" || s.S3.SecretKey != "s3secret" {
		t.Error("Public must return a copy, not mutate the receiver")
	}
}

func TestFromProfile(t *testing.T) {
	p := Profile{Name: "legacy", Endpoint: "e", Color: "#123456", Default: true}
	s := FromProfile(p)
	if s.Type != TypeS3 || s.Name != "legacy" || s.Color != "#123456" || !s.Default {
		t.Errorf("FromProfile lost fields: %+v", s)
	}
	if s.S3 == nil || s.S3.Endpoint != "e" {
		t.Errorf("FromProfile did not embed the profile: %+v", s.S3)
	}
}

func TestNormalizeSources(t *testing.T) {
	base := time.Now()
	srcs := []Source{
		{ID: "keep-me", Name: "one", Type: TypeLocal, LocalRoot: "/a", UpdatedAt: base},
		{Name: "two", Type: TypeSFTP, Host: "h"},                 // ID assigned
		{ID: "keep-me", Name: "three", Type: TypeFTP, Host: "h"}, // duplicate ID re-rolled
	}
	out, err := NormalizeSources(srcs)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if out[0].ID != "keep-me" {
		t.Errorf("existing unique ID must be preserved, got %q", out[0].ID)
	}
	if out[1].ID == "" {
		t.Error("missing ID must be assigned")
	}
	if out[2].ID == "keep-me" {
		t.Error("duplicate ID must be re-rolled")
	}

	// Duplicate names are rejected.
	if _, err := NormalizeSources([]Source{
		{Name: "dup", Type: TypeLocal, LocalRoot: "/a"},
		{Name: "dup", Type: TypeLocal, LocalRoot: "/b"},
	}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate name err = %v, want duplicate-name error", err)
	}

	// Invalid entries are rejected with context.
	if _, err := NormalizeSources([]Source{{Name: "x", Type: "nope"}}); err == nil {
		t.Error("invalid type must be rejected")
	}
}

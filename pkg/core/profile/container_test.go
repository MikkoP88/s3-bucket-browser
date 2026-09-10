package profile

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// Note: every Encrypt/Decrypt costs one scrypt derivation (N=32768,
// ~32 MiB); the tests below keep the total count in the single digits.

func containerFixture() (string, []Source) {
	srcs := []Source{
		{
			ID:    "src_1",
			Name:  "main s3",
			Type:  TypeS3,
			Color: "#ff0000",
			S3: &Profile{
				Name:        "main s3",
				Endpoint:    "https://s3.example.com",
				Region:      "us-east-1",
				AccessKeyID: "AKIAEXAMPLE",
				SecretKey:   "topsecret",
			},
			CreatedAt: time.Unix(1700000000, 0).UTC(),
			UpdatedAt: time.Unix(1700000100, 0).UTC(),
		},
		{
			ID:       "src_2",
			Name:     "backup box",
			Type:     TypeSFTP,
			Host:     "backup.example.com",
			Port:     0, // default at dial time
			Username: "deploy",
			Password: "hunter2",
			Root:     "/srv/backup",
		},
	}
	return "work profile", srcs
}

func TestContainerRoundTrip(t *testing.T) {
	name, srcs := containerFixture()
	data, err := EncryptContainer(name, srcs, "correct horse battery staple")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !bytes.HasPrefix(data, []byte(containerMagic+"|")) {
		t.Fatalf("missing magic prefix: %q", data[:20])
	}
	gotName, gotSrcs, err := DecryptContainer(data, "correct horse battery staple")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if gotName != name {
		t.Errorf("name = %q, want %q", gotName, name)
	}
	if len(gotSrcs) != len(srcs) {
		t.Fatalf("got %d sources, want %d", len(gotSrcs), len(srcs))
	}
	if gotSrcs[0].S3 == nil || gotSrcs[0].S3.SecretKey != "topsecret" {
		t.Errorf("embedded s3 secret did not survive the round trip: %+v", gotSrcs[0].S3)
	}
	if gotSrcs[1].Password != "hunter2" {
		t.Errorf("sftp password did not survive the round trip")
	}
	if !gotSrcs[0].CreatedAt.Equal(srcs[0].CreatedAt) {
		t.Errorf("createdAt = %v, want %v", gotSrcs[0].CreatedAt, srcs[0].CreatedAt)
	}
}

func TestContainerWrongPassword(t *testing.T) {
	name, srcs := containerFixture()
	data, err := EncryptContainer(name, srcs, "right")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	_, _, err = DecryptContainer(data, "wrong")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
}

func TestContainerTampered(t *testing.T) {
	name, srcs := containerFixture()
	data, err := EncryptContainer(name, srcs, "pw")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip one byte inside the ciphertext segment.
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	ct, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		t.Fatalf("decode ct: %v", err)
	}
	ct[len(ct)-1] ^= 0x01
	parts[3] = base64.StdEncoding.EncodeToString(ct)
	_, _, err = DecryptContainer([]byte(strings.Join(parts, "|")), "pw")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword (tamper must not leak plaintext)", err)
	}
}

func TestContainerBadFormat(t *testing.T) {
	name, srcs := containerFixture()
	good, err := EncryptContainer(name, srcs, "pw")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	goodParts := strings.Split(strings.TrimSpace(string(good)), "|")

	cases := map[string]string{
		"empty":          "",
		"garbage":        "hello world",
		"bad magic":      "s3bpf2|" + strings.Join(goodParts[1:], "|"),
		"too few parts":  strings.Join(goodParts[:3], "|"),
		"too many parts": strings.Join(append(goodParts, "extra"), "|"),
		"bad salt b64":   containerMagic + "|!!!|" + goodParts[2] + "|" + goodParts[3],
		"bad nonce b64":  containerMagic + "|" + goodParts[1] + "|!!!|" + goodParts[3],
		"bad ct b64":     containerMagic + "|" + goodParts[1] + "|" + goodParts[2] + "|!!!",
		"empty ct":       containerMagic + "|" + goodParts[1] + "|" + goodParts[2] + "|",
	}
	// Valid base64 but wrong lengths (decoded, re-encoded).
	shortSalt := base64.StdEncoding.EncodeToString(make([]byte, 8))
	cases["short salt"] = containerMagic + "|" + shortSalt + "|" + goodParts[2] + "|" + goodParts[3]
	shortNonce := base64.StdEncoding.EncodeToString(make([]byte, 6))
	cases["short nonce"] = containerMagic + "|" + goodParts[1] + "|" + shortNonce + "|" + goodParts[3]

	for label, in := range cases {
		_, _, err := DecryptContainer([]byte(in), "pw")
		if !errors.Is(err, ErrBadFormat) {
			t.Errorf("%s: err = %v, want ErrBadFormat", label, err)
		}
	}
}

func TestContainerEmptyPassword(t *testing.T) {
	if _, err := EncryptContainer("x", nil, ""); err == nil {
		t.Fatal("empty password must be rejected")
	}
}

func TestContainerUniqueSalts(t *testing.T) {
	name, srcs := containerFixture()
	a, err := EncryptContainer(name, srcs, "pw")
	if err != nil {
		t.Fatalf("encrypt a: %v", err)
	}
	b, err := EncryptContainer(name, srcs, "pw")
	if err != nil {
		t.Fatalf("encrypt b: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("two encryptions of the same payload must differ (random salt/nonce)")
	}
}

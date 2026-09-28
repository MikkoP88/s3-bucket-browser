package api

// Live end-to-end check for the Import credentials flow against a real
// S3-compatible provider (developed against Hetzner Object Storage) and a
// real HashiCorp Vault.
//
// Credentials NEVER live in this file or the repo: the tests run only when
// the S3B_E2E_* variables are exported, e.g.
//
//	export S3B_E2E_ENDPOINT=hel1.your-objectstorage.com
//	export S3B_E2E_ACCESS_KEY=…
//	export S3B_E2E_SECRET_KEY=…
//	export S3B_E2E_BUCKET=testijotain   # optional: bucket that must be visible
//	go test ./pkg/api -run TestImportCredentialsE2E -v
//
// Flow A parses a generated AWS INI credentials file; Flow B fetches from a
// local custom-HTTP secrets endpoint (httptest) using a JSON path and a
// custom header. Flows C/D fetch from a REAL Vault dev-mode container:
//
//	docker run -d --name s3b-e2e-vault -p 8200:8200 \
//	  -e VAULT_DEV_ROOT_TOKEN_ID=s3b-e2e-root-token hashicorp/vault:latest
//	export S3B_E2E_VAULT_ADDR=http://127.0.0.1:8200
//	export S3B_E2E_VAULT_TOKEN=s3b-e2e-root-token
//
// (seeding in the scripts/e2e-cross.sh header). All then run the full
// dialog path: parse/fetch → test (live bucket count) → import → buckets
// visible through the new source.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// e2eConfig reads the live provider config or skips the test.
func e2eConfig(t *testing.T) (endpoint, accessKey, secretKey, bucket string) {
	t.Helper()
	endpoint = os.Getenv("S3B_E2E_ENDPOINT")
	accessKey = os.Getenv("S3B_E2E_ACCESS_KEY")
	secretKey = os.Getenv("S3B_E2E_SECRET_KEY")
	bucket = os.Getenv("S3B_E2E_BUCKET")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("S3B_E2E_ENDPOINT / S3B_E2E_ACCESS_KEY / S3B_E2E_SECRET_KEY not set — skipping live import-credentials e2e")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	return endpoint, accessKey, secretKey, bucket
}

// verifyBuckets asserts the imported source really reaches the provider and
// sees bucket information ("end-to-end", not just a saved row).
func verifyBuckets(t *testing.T, a *App, name, wantBucket string) {
	t.Helper()
	buckets, err := a.ListSourceBuckets(name)
	if err != nil {
		t.Fatalf("ListSourceBuckets(%q): %v", name, err)
	}
	if len(buckets) == 0 {
		t.Fatalf("imported source %q lists no buckets", name)
	}
	if wantBucket != "" {
		found := false
		for _, b := range buckets {
			if b.Name == wantBucket {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("bucket %q not visible through %q (%d buckets listed)", wantBucket, name, len(buckets))
		}
	}
}

// noLeakedSecrets fails when candidate metadata carries the credentials —
// only metadata ever crosses to the frontend.
func noLeakedSecrets(t *testing.T, cands []CredCandidate, vals ...string) {
	t.Helper()
	b, err := json.Marshal(cands)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vals {
		if v != "" && strings.Contains(string(b), v) {
			t.Errorf("candidate metadata leaks credential value %q…", v[:4])
		}
	}
}

// assertImportedExpansion checks the import contract for an account-wide
// S3 candidate: every bucket the credential can see becomes its own
// bucket-named source (a scoped-down credential with no visible buckets
// falls back to one account-wide source instead), so the result names
// buckets — the wanted bucket (S3B_E2E_BUCKET) must be among them — and
// one imported source must list buckets live.
func assertImportedExpansion(t *testing.T, a *App, imp ImportResult, wantBucket string) {
	t.Helper()
	if len(imp.Skipped) != 0 {
		t.Errorf("ImportCredentials skipped candidates: %v", imp.Skipped)
	}
	names := append(append([]string{}, imp.Imported...), imp.Updated...)
	if len(names) == 0 {
		t.Fatalf("ImportCredentials imported nothing: %+v", imp)
	}
	target := names[0]
	if wantBucket != "" {
		if !slices.Contains(names, wantBucket) {
			t.Fatalf("bucket %q not among the imported sources %v", wantBucket, names)
		}
		target = wantBucket
	}
	verifyBuckets(t, a, target, wantBucket)
}

func TestImportCredentialsE2EFile(t *testing.T) {
	endpoint, accessKey, secretKey, bucket := e2eConfig(t)
	t.Setenv("S3B_NO_KEYRING", "1") // plaintext secrets in the throwaway store — no OS-keyring footprint
	a := newTestApp(t)
	a.Startup(context.Background()) // live calls need a service context

	ini := fmt.Sprintf(`
[hetzner-e2e]
aws_access_key_id = %s
aws_secret_access_key = %s
endpoint_url = %s
`, accessKey, secretKey, endpoint)
	file := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(file, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	cands, err := a.ParseCredentialFile(file, "")
	if err != nil {
		t.Fatalf("ParseCredentialFile: %v", err)
	}
	var cand *CredCandidate
	for i := range cands {
		if cands[i].Name == "hetzner-e2e" {
			cand = &cands[i]
		}
	}
	if cand == nil {
		t.Fatalf("no hetzner-e2e candidate in %+v", cands)
	}
	if cand.Type != profile.TypeS3 || !cand.HasSecret || cand.Endpoint != endpoint {
		t.Fatalf("candidate = %+v", cand)
	}
	noLeakedSecrets(t, cands, secretKey)

	res := a.TestCredentialDraft(cand.ID)
	if !res.OK || res.BucketCount == 0 {
		t.Fatalf("TestCredentialDraft = %+v", res)
	}

	imp, err := a.ImportCredentials([]string{cand.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertImportedExpansion(t, a, imp, bucket)
}

func TestImportCredentialsE2EKmsHTTP(t *testing.T) {
	endpoint, accessKey, secretKey, bucket := e2eConfig(t)
	t.Setenv("S3B_NO_KEYRING", "1") // plaintext secrets in the throwaway store — no OS-keyring footprint
	a := newTestApp(t)
	a.Startup(context.Background())

	sawHeader := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("X-S3B-Probe") == "e2e"
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"s3":{"name":"hetzner-kms-e2e","type":"s3","endpoint":%q,"aws_access_key_id":%q,"aws_secret_access_key":%q}}}`,
			endpoint, accessKey, secretKey)
	}))
	defer srv.Close()

	cands, err := a.KmsFetch("http", map[string]string{
		"url":          srv.URL + "/secret/s3",
		"jsonPath":     "data.s3",
		"headerName1":  "X-S3B-Probe",
		"headerValue1": "e2e",
	})
	if err != nil {
		t.Fatalf("KmsFetch(http): %v", err)
	}
	if len(cands) != 1 || cands[0].Name != "hetzner-kms-e2e" || cands[0].Type != profile.TypeS3 {
		t.Fatalf("candidates = %+v", cands)
	}
	if !sawHeader {
		t.Error("custom header was not sent to the secrets endpoint")
	}
	if !strings.HasPrefix(cands[0].Origin, srv.URL) {
		t.Errorf("candidate origin = %q, want the endpoint URL", cands[0].Origin)
	}
	noLeakedSecrets(t, cands, secretKey)

	res := a.TestCredentialDraft(cands[0].ID)
	if !res.OK || res.BucketCount == 0 {
		t.Fatalf("TestCredentialDraft = %+v", res)
	}

	imp, err := a.ImportCredentials([]string{cands[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	assertImportedExpansion(t, a, imp, bucket)
}

// vaultE2EConfig reads the real Vault coordinates or skips the test. The
// fixture is one dev-mode container (KV v2 mounted at secret/ out of the
// box), seeded per the scripts/e2e-cross.sh header:
//
//	secret/s3b/e2e/s3      an account-wide S3 credential  (name=vault-e2e-s3)
//	secret/s3b/e2e/sftp    an SFTP credential             (name=vault-e2e-sftp)
//	secret/s3b/e2e/rotated two versions                   (…-v1, then …-v2)
func vaultE2EConfig(t *testing.T) (addr, token string) {
	t.Helper()
	addr = os.Getenv("S3B_E2E_VAULT_ADDR")
	token = os.Getenv("S3B_E2E_VAULT_TOKEN")
	if addr == "" || token == "" {
		t.Skip("S3B_E2E_VAULT_ADDR / S3B_E2E_VAULT_TOKEN not set — skipping live Vault import-credentials e2e")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return addr, token
}

// Flow C: a REAL HashiCorp Vault — the exact KV-v2 wire protocol kmsVault
// speaks (X-Vault-Token auth, the data.data unwrap, ?version= pinning),
// then the full dialog path, then the failure contract: a bad token or a
// missing secret must come back as an ERROR, never as an empty candidate
// list (which would read as "nothing to import" in the dialog).
func TestImportCredentialsE2EVault(t *testing.T) {
	endpoint, _, secretKey, bucket := e2eConfig(t)
	addr, token := vaultE2EConfig(t)
	t.Setenv("S3B_NO_KEYRING", "1") // plaintext secrets in the throwaway store — no OS-keyring footprint
	a := newTestApp(t)
	a.Startup(context.Background())

	cands, err := a.KmsFetch("vault", map[string]string{
		"url": addr, "token": token, "mount": "secret", "path": "s3b/e2e/s3",
	})
	if err != nil {
		t.Fatalf("KmsFetch(vault): %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(cands), cands)
	}
	c := cands[0]
	if c.Name != "vault-e2e-s3" || c.Type != profile.TypeS3 || !c.HasSecret || c.ID == "" {
		t.Fatalf("candidate = %+v", c)
	}
	if c.Endpoint != endpoint || c.Origin != "Vault" {
		t.Fatalf("candidate endpoint/origin = %q / %q, want %q / Vault", c.Endpoint, c.Origin, endpoint)
	}
	noLeakedSecrets(t, cands, secretKey, token)

	// version pinning: rotation must not silently hide history — ?version=1
	// returns the FIRST payload of the rotated secret, not the current one.
	old, err := a.KmsFetch("vault", map[string]string{
		"url": addr, "token": token, "path": "s3b/e2e/rotated", "version": "1",
	})
	if err != nil {
		t.Fatalf("KmsFetch(vault, version=1): %v", err)
	}
	if len(old) != 1 || old[0].Name != "vault-e2e-rotated-v1" {
		t.Fatalf("version=1 candidates = %+v, want the rotated secret's first version", old)
	}

	res := a.TestCredentialDraft(c.ID)
	if !res.OK || res.BucketCount == 0 {
		t.Fatalf("TestCredentialDraft = %+v", res)
	}

	imp, err := a.ImportCredentials([]string{c.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertImportedExpansion(t, a, imp, bucket)

	if _, err := a.KmsFetch("vault", map[string]string{"url": addr, "token": token + "-wrong", "path": "s3b/e2e/s3"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("wrong token: err = %v, want a 403 forbidden", err)
	}
	if _, err := a.KmsFetch("vault", map[string]string{"url": addr, "token": token, "path": "s3b/e2e/absent"}); err == nil {
		t.Error("missing secret: err = nil, want an error (an empty list would read as \"nothing to import\")")
	}
	if _, err := a.KmsFetch("nosuchservice", map[string]string{"url": addr, "token": token}); err == nil || !strings.Contains(err.Error(), "unknown secrets service") {
		t.Errorf("unknown service: err = %v, want the unknown-service error", err)
	}
}

// Flow D: a REAL Vault payload for a NON-S3 engine. The secret carries no
// type field — the fetcher must recognize host+user as SFTP, test the
// candidate against the live engine, and import it as a working remote
// source (the import flow is engine-uniform, S3 is just the common case).
func TestImportCredentialsE2EVaultSftp(t *testing.T) {
	addr, token := vaultE2EConfig(t)
	sftpPort := os.Getenv("S3B_SFTP_PORT")
	if sftpPort == "" {
		sftpPort = "2222"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", sftpPort), time.Second)
	if err != nil {
		t.Skipf("SFTP 127.0.0.1:%s not reachable — skipping the Vault SFTP import e2e", sftpPort)
	}
	conn.Close()

	t.Setenv("S3B_NO_KEYRING", "1") // plaintext secrets in the throwaway store — no OS-keyring footprint
	a := newTestApp(t)
	a.Startup(context.Background())

	cands, err := a.KmsFetch("vault", map[string]string{"url": addr, "token": token, "path": "s3b/e2e/sftp"})
	if err != nil {
		t.Fatalf("KmsFetch(vault, sftp): %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(cands), cands)
	}
	c := cands[0]
	if c.Type != profile.TypeSFTP || c.Name != "vault-e2e-sftp" || !c.HasSecret || c.Username != "e2e" || c.Host != "127.0.0.1" {
		t.Fatalf("candidate = %+v", c)
	}
	noLeakedSecrets(t, cands, "e2epass")

	res := a.TestCredentialDraft(c.ID)
	if !res.OK {
		t.Fatalf("TestCredentialDraft(sftp) = %+v", res)
	}

	imp, err := a.ImportCredentials([]string{c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Imported) != 1 || imp.Imported[0] != "vault-e2e-sftp" {
		t.Fatalf("ImportCredentials = %+v", imp)
	}
	srcs, err := a.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range srcs {
		if s.Name == "vault-e2e-sftp" {
			found = s.Type == profile.TypeSFTP && s.Port != 0
		}
	}
	if !found {
		t.Errorf("the imported SFTP source is not in the store: %+v", srcs)
	}
}

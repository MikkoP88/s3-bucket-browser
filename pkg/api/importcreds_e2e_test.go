package api

// Live end-to-end check for the Import credentials flow against a real
// S3-compatible provider (developed against Hetzner Object Storage).
//
// Credentials NEVER live in this file or the repo: the test runs only when
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
// custom header. Both then run the full dialog path: parse/fetch → test
// (live bucket count) → import → buckets visible through the new source.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestImportCredentialsE2EFile(t *testing.T) {
	endpoint, accessKey, secretKey, bucket := e2eConfig(t)
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
	if len(imp.Imported) != 1 || imp.Imported[0] != "hetzner-e2e" {
		t.Fatalf("ImportCredentials = %+v", imp)
	}
	verifyBuckets(t, a, "hetzner-e2e", bucket)
}

func TestImportCredentialsE2EKmsHTTP(t *testing.T) {
	endpoint, accessKey, secretKey, bucket := e2eConfig(t)
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
	if len(imp.Imported) != 1 || imp.Imported[0] != "hetzner-kms-e2e" {
		t.Fatalf("ImportCredentials = %+v", imp)
	}
	verifyBuckets(t, a, "hetzner-kms-e2e", bucket)
}

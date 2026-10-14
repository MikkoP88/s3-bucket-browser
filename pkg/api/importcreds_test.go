package api

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscardCredentialDrafts pins the import dialog's remove path: a
// discarded candidate is gone from the stash immediately — its secret must
// not outlive the user's choice by the stash's 30-minute TTL. Both
// consumers of a stash entry (test and import) must see it as expired.
func TestDiscardCredentialDrafts(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	ini := filepath.Join(dir, "creds.ini")
	content := "[minio]\naws_access_key_id = e2e\naws_secret_access_key = e2epass\nendpoint = http://localhost:9000\n"
	if err := os.WriteFile(ini, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cs, err := a.ParseCredentialFile(ini, "")
	if err != nil || len(cs) != 1 {
		t.Fatalf("ParseCredentialFile = (%v, %v), want 1 candidate", cs, err)
	}

	a.DiscardCredentialDrafts([]string{cs[0].ID})

	if tr := a.TestCredentialDraft(cs[0].ID); tr.OK {
		t.Fatalf("discarded draft still tests: %+v", tr)
	}
	res, err := a.ImportCredentials([]string{cs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || len(res.Imported) != 0 || len(res.Updated) != 0 {
		t.Fatalf("discarded draft still imports: %+v", res)
	}
}

// TestDiscardCredentialDraftsUnknownIDs keeps the contract cheap for the
// dialog's close path, which blindly discards ids an import already
// consumed: unknown or repeated ids must be silent no-ops.
func TestDiscardCredentialDraftsUnknownIDs(t *testing.T) {
	a := newTestApp(t)
	a.DiscardCredentialDrafts(nil)
	a.DiscardCredentialDrafts([]string{"gone", "gone"})
	if tr := a.TestCredentialDraft("gone"); tr.OK {
		t.Fatalf("unknown id tests: %+v", tr)
	}
}

// TestAddCredentialDraftManualS3 pins the manual-entry road's happy path:
// one flat map of friendly names builds the same stashed candidate a file
// would, with the endpoint name-derived and the secret held behind
// HasSecret — never in the candidate payload itself.
func TestAddCredentialDraftManualS3(t *testing.T) {
	a := newTestApp(t)
	cs, err := a.AddCredentialDraft("s3", map[string]string{
		"endpoint":  "https://files.example.com",
		"region":    "eu-central-1",
		"accessKey": "AKIAMANUAL",
		"secretKey": "manual-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("AddCredentialDraft returned %d candidates, want 1", len(cs))
	}
	c := cs[0]
	if c.Name != "files.example.com" {
		t.Fatalf("Name = %q, want endpoint-derived files.example.com", c.Name)
	}
	if c.Type != "s3" || c.Endpoint != "https://files.example.com" || c.Region != "eu-central-1" {
		t.Fatalf("candidate metadata = %+v", c)
	}
	if !c.HasSecret {
		t.Fatal("HasSecret = false, want true")
	}
	// the stash law holds for manual entries exactly as for files: the id
	// discards clean (the Test/Import legs stay network-bound and belong
	// to the discard rig, which pins them against expired ids)
	a.DiscardCredentialDrafts([]string{c.ID})
	if tr := a.TestCredentialDraft(c.ID); tr.OK {
		t.Fatalf("discarded manual draft still tests: %+v", tr)
	}
}

// TestAddCredentialDraftValidation pins the manual road's refusals: an
// S3 entry without a secret, a remote entry without a host, and an
// unknown kind must fail before anything is stashed.
func TestAddCredentialDraftValidation(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.AddCredentialDraft("s3", map[string]string{"accessKey": "x"}); err == nil {
		t.Fatal("s3 without secret key was accepted")
	}
	if _, err := a.AddCredentialDraft("sftp", map[string]string{"user": "u"}); err == nil {
		t.Fatal("sftp without host was accepted")
	}
	if _, err := a.AddCredentialDraft("ftp", map[string]string{"host": "h"}); err == nil {
		t.Fatal("ftp without user was accepted")
	}
	if _, err := a.AddCredentialDraft("matrix", map[string]string{}); err == nil {
		t.Fatal("unknown kind was accepted")
	}
}

// TestAddCredentialDraftManualRemote pins the remote leg and the name
// defaulting law: a host+user entry becomes a remote candidate named
// after the host, and an S3 entry with only a region becomes s3-<region>.
func TestAddCredentialDraftManualRemote(t *testing.T) {
	a := newTestApp(t)
	cs, err := a.AddCredentialDraft("sftp", map[string]string{
		"host":     "sftp.example.com:2222",
		"user":     "demo",
		"password": "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("AddCredentialDraft returned %d candidates, want 1", len(cs))
	}
	c := cs[0]
	if c.Type != "sftp" || c.Host != "sftp.example.com:2222" || c.Username != "demo" {
		t.Fatalf("remote candidate = %+v", c)
	}
	if c.Name != "sftp.example.com:2222" {
		t.Fatalf("Name = %q, want host-derived name", c.Name)
	}
	if !c.HasSecret {
		t.Fatal("remote HasSecret = false, want true")
	}
	a.DiscardCredentialDrafts([]string{c.ID})

	cs2, err := a.AddCredentialDraft("s3", map[string]string{
		"region":    "us-east-2",
		"accessKey": "AK",
		"secretKey": "SK",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cs2[0].Name != "s3-us-east-2" {
		t.Fatalf("Name = %q, want region-derived s3-us-east-2", cs2[0].Name)
	}
	if cs2[0].Endpoint != "" {
		t.Fatalf("Endpoint = %q, want empty for region-only entry", cs2[0].Endpoint)
	}
	a.DiscardCredentialDrafts([]string{cs2[0].ID})
}

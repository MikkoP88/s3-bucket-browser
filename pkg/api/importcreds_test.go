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

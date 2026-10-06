package api

import (
	"context"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// acctSource is the legacy shape under migration: an account-wide s3
// source (no Bucket) whose credentials see every bucket of the endpoint.
func acctSource(name, url string) profile.Source {
	return profile.Source{Name: name, Type: profile.TypeS3, Color: "#0b63ce",
		S3: &profile.Profile{Name: name, Endpoint: url, Region: "us-east-1",
			PathStyle: true, AccessKeyID: "key", SecretKey: "secret"}}
}

// TestSplitAccountSource pins the one-bucket migration: an account-wide
// source becomes one bucket-scoped source per visible bucket, named after
// the bucket, carrying the account's credentials and color; the account
// source itself is removed.
func TestSplitAccountSource(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("team-files", "logs-2026")
	url := f.serve(t)
	if err := a.SaveSource(acctSource("hetzner", url)); err != nil {
		t.Fatal(err)
	}

	res, err := a.SplitAccountSource("hetzner")
	if err != nil {
		t.Fatalf("SplitAccountSource: %v", err)
	}
	if res.Source != "hetzner" || len(res.Created) != 2 || len(res.Matched) != 0 {
		t.Fatalf("result = %+v; want 2 created, 0 matched", res)
	}

	srcs := a.workspaceSources()
	if len(srcs) != 2 {
		t.Fatalf("after split: %d sources; want 2 (the account gone)", len(srcs))
	}
	for _, s := range srcs {
		if s.Type != profile.TypeS3 || s.Bucket != s.Name {
			t.Fatalf("source %q is not bucket-scoped to its own name: %+v", s.Name, s)
		}
		if s.Bucket != "team-files" && s.Bucket != "logs-2026" {
			t.Fatalf("unexpected bucket source %q", s.Bucket)
		}
		// the clone carries the account's full connection and color — the
		// buckets keep the account tint and dial without a re-prompt
		if s.S3 == nil || s.S3.Endpoint != url || s.S3.AccessKeyID != "key" || s.S3.SecretKey != "secret" {
			t.Fatalf("source %q lost the account's credentials: %+v", s.Name, s.S3)
		}
		if s.Color != "#0b63ce" {
			t.Fatalf("source %q color = %q; want the account's #0b63ce", s.Name, s.Color)
		}
	}
}

// TestSplitAccountSourceIdempotent pins the re-split: when every bucket
// of the account already has a source on the same connection, nothing is
// duplicated — each is refreshed in place and only the account row goes.
func TestSplitAccountSourceIdempotent(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("team-files", "logs-2026")
	url := f.serve(t)
	if err := a.SaveSource(acctSource("hetzner", url)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SplitAccountSource("hetzner"); err != nil {
		t.Fatal(err)
	}
	// the same account connection again (a re-add) still splits to the
	// SAME two sources — no duplicates, no -2 suffixes
	if err := a.SaveSource(acctSource("hetzner", url)); err != nil {
		t.Fatal(err)
	}
	res, err := a.SplitAccountSource("hetzner")
	if err != nil {
		t.Fatalf("re-split: %v", err)
	}
	if len(res.Created) != 0 || len(res.Matched) != 2 {
		t.Fatalf("re-split result = %+v; want 0 created, 2 matched", res)
	}
	if srcs := a.workspaceSources(); len(srcs) != 2 {
		t.Fatalf("after re-split: %d sources; want the same 2", len(srcs))
	}
	// a third pass on a removed account is a plain not-found
	if _, err := a.SplitAccountSource("hetzner"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("split of a removed source: err = %v; want not found", err)
	}
}

// TestSplitAccountSourceUnreachable pins the offline rule: an account
// that cannot be listed keeps its account-wide source untouched — the
// migration only rewrites the workspace when it knows the bucket set.
func TestSplitAccountSourceUnreachable(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	// port 1 on localhost: nothing listens there
	if err := a.SaveSource(acctSource("offline", "http://127.0.0.1:1")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SplitAccountSource("offline"); err == nil {
		t.Fatal("split of an unreachable account: want error")
	}
	srcs := a.workspaceSources()
	if len(srcs) != 1 || srcs[0].Name != "offline" || srcs[0].Bucket != "" {
		t.Fatalf("unreachable split mutated the workspace: %+v", srcs)
	}
}

// TestSplitAccountSourceRefusals pins the guard rails: only a legacy
// account-wide s3 source is splittable.
func TestSplitAccountSourceRefusals(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("team-files")
	url := f.serve(t)

	scoped := acctSource("scoped", url)
	scoped.Bucket = "team-files"
	if err := a.SaveSource(scoped); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SplitAccountSource("scoped"); err == nil || !strings.Contains(err.Error(), "already scoped") {
		t.Fatalf("scoped split: err = %v; want already scoped", err)
	}

	if err := a.SaveSource(profile.Source{Name: "lab", Type: profile.TypeLocal, LocalRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SplitAccountSource("lab"); err == nil || !strings.Contains(err.Error(), "not an s3 source") {
		t.Fatalf("local split: err = %v; want not an s3 source", err)
	}
	if _, err := a.SplitAccountSource("nope"); err == nil {
		t.Fatal("unknown source split: want error")
	}
}

// TestSplitAccountSourceSeesNothing pins the empty-account rule shared
// with credential import: a listing that sees no buckets keeps the
// account-wide source (a scoped-down key must not lose its connection).
func TestSplitAccountSourceSeesNothing(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3() // serves ListBuckets over an empty set
	url := f.serve(t)
	if err := a.SaveSource(acctSource("empty", url)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SplitAccountSource("empty"); err == nil || !strings.Contains(err.Error(), "no buckets") {
		t.Fatalf("empty-account split: err = %v; want sees no buckets", err)
	}
	if srcs := a.workspaceSources(); len(srcs) != 1 || srcs[0].Name != "empty" {
		t.Fatalf("empty-account split mutated the workspace: %+v", srcs)
	}
}

// TestCreateBucketSource pins the New-bucket flow's source half: scoping a
// copy of an s3 connection to one fresh bucket keeps the connection source
// intact, carries the real credentials server-side, and is idempotent by
// connection (a bucket that already has a source refreshes, not duplicates).
func TestCreateBucketSource(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("team-files")
	url := f.serve(t)
	conn := acctSource("team-files", url)
	conn.Bucket = "team-files"
	if err := a.SaveSource(conn); err != nil {
		t.Fatal(err)
	}

	created, err := a.CreateBucketSource("team-files", "media-assets")
	if err != nil {
		t.Fatalf("CreateBucketSource: %v", err)
	}
	if created.Name != "media-assets" || created.Bucket != "media-assets" {
		t.Fatalf("created = %+v; want media-assets", created)
	}
	srcs := a.workspaceSources()
	if len(srcs) != 2 {
		t.Fatalf("after create: %d sources; want the connection plus the clone", len(srcs))
	}
	for _, s := range srcs {
		if s.Name == "media-assets" {
			if s.Bucket != "media-assets" || s.S3 == nil || s.S3.Endpoint != url ||
				s.S3.AccessKeyID != "key" || s.S3.SecretKey != "secret" {
				t.Fatalf("bucket source lost the connection's credentials: %+v", s)
			}
		}
	}

	// idempotence: the same bucket again refreshes in place, never duplicates
	if _, err := a.CreateBucketSource("team-files", "media-assets"); err != nil {
		t.Fatalf("re-create: %v", err)
	}
	if srcs := a.workspaceSources(); len(srcs) != 2 {
		t.Fatalf("re-create duplicated: %d sources", len(srcs))
	}

	// non-s3 sources are refused
	if err := a.SaveSource(profile.Source{Name: "lab", Type: profile.TypeLocal, LocalRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateBucketSource("lab", "x"); err == nil || !strings.Contains(err.Error(), "not an s3 source") {
		t.Fatalf("local create: err = %v; want not an s3 source", err)
	}
}

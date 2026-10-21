// browse_guard_test.go: pins the bucket guard's error law. A transient
// versioning failure must PROPAGATE (the frontend retries instead of
// caching "never configured" as truth — the missing-guard-icon flake of
// 21 Oct 2026), while the provider's honest not-implemented family
// collapses into the zero state. Both legs run over the real SDK wire
// (an httptest S3), so the smithy-code classification is proven end to
// end, not assumed from a fake error.
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// guardStubClient seats an S3 client on the stub server.
func guardStubClient(t *testing.T, url string) *s3client.Client {
	t.Helper()
	c, err := s3client.New(context.Background(), profile.Profile{
		Endpoint: url, Region: "us-east-1", PathStyle: true,
		AccessKeyID: "k", SecretKey: "s",
	}, s3client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// s3Err answers one S3 error wire body — the shape every provider's
// "this API refuses" carries, which the SDK parses into a smithy
// APIError whose code isCode matches.
func s3Err(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `<Error><Code>%s</Code><Message>stub</Message></Error>`, code)
}

func TestBucketGuardPropagatesVersioningFailure(t *testing.T) {
	// The flake's exact shape: versioning fails on the wire (a 500 here;
	// a cold dial or quick-ctx timeout in the wild) and the lock leg is
	// unreachable/irrelevant. The guard must answer the error — swallowing
	// it once answered {versioning:""} as success, the frontend cached
	// the lie, and the icon could never appear for the whole session.
	a := newTestApp(t)
	a.ctx = context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("versioning") {
			s3Err(w, http.StatusInternalServerError, "InternalError")
			return
		}
		s3Err(w, http.StatusNotFound, "NoSuchObjectLockConfiguration")
	}))
	t.Cleanup(srv.Close)

	if _, err := a.bucketGuardC(guardStubClient(t, srv.URL), "b"); err == nil {
		t.Fatal("versioning failure answered success — the zero state would be cached as truth")
	}
}

func TestBucketGuardZeroStateForNotImplementedProvider(t *testing.T) {
	// A provider that never speaks GetBucketVersioning honestly answers
	// "not versioned": nil error, empty guard, the icon renders off.
	a := newTestApp(t)
	a.ctx = context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("versioning") {
			s3Err(w, http.StatusNotImplemented, "NotImplemented")
			return
		}
		s3Err(w, http.StatusNotFound, "NoSuchObjectLockConfiguration")
	}))
	t.Cleanup(srv.Close)

	g, err := a.bucketGuardC(guardStubClient(t, srv.URL), "b")
	if err != nil {
		t.Fatalf("honest not-implemented must be the zero state, got %v", err)
	}
	if g.Versioning != "" {
		t.Fatalf("versioning = %q, want the empty never-configured state", g.Versioning)
	}
	if g.LockEnabled {
		t.Fatal("lock must stay off for a provider without the API")
	}
}

func TestBucketGuardReadsEnabledVersioning(t *testing.T) {
	// The happy wire still lands: Enabled answers the icon's on state and
	// a configured lock rides along.
	a := newTestApp(t)
	a.ctx = context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("versioning"):
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
		case r.URL.Query().Has("object-lock"):
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>COMPLIANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`)
		default:
			s3Err(w, http.StatusBadRequest, "InvalidRequest")
		}
	}))
	t.Cleanup(srv.Close)

	g, err := a.bucketGuardC(guardStubClient(t, srv.URL), "b")
	if err != nil {
		t.Fatal(err)
	}
	if g.Versioning != "Enabled" {
		t.Fatalf("versioning = %q, want Enabled", g.Versioning)
	}
	if !g.LockEnabled || g.LockMode != "COMPLIANCE" || g.LockDays != 30 {
		t.Fatalf("lock leg degraded: %+v", g)
	}
}

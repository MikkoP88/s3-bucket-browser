package api

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
)

// The event-log surface of bucket administration: every setter verb the
// admin panel speaks lands exactly one line in the activity log — scoped
// "admin", sourced to the bucket, info for what changed and warn for what
// was removed — and a verb the wire refused says so as an error. The
// client-resolution failures stay silent on purpose (the caller already
// got the error; there is no bucket to source the line to), the same
// shape CreateBucket set.

// captureLogLines routes EventLogLine into a guarded slice while the test
// runs — the same sink idiom the list rigs use.
func captureLogLines(t *testing.T) (*sync.Mutex, *[]LogLine) {
	t.Helper()
	var mu sync.Mutex
	var lines []LogLine
	prevNoEvents := testNoEvents.Load()
	testNoEvents.Store(false)
	SetEventSink(func(event string, data ...any) {
		if event != EventLogLine || len(data) == 0 {
			return
		}
		if l, ok := data[0].(LogLine); ok {
			mu.Lock()
			lines = append(lines, l)
			mu.Unlock()
		}
	})
	t.Cleanup(func() {
		SetEventSink(nil)
		testNoEvents.Store(prevNoEvents)
	})
	return &mu, &lines
}

// adminLogRig boots an app whose default client serves the in-process fake.
func adminLogRig(t *testing.T) (*App, *fakeS3) {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.mu.Lock()
	f.url = srv.URL
	f.mu.Unlock()
	if err := a.SaveSource(fakeS3Source("adminbox", srv.URL)); err != nil {
		t.Fatal(err)
	}
	return a, f
}

func TestAdminSettersLogEachVerb(t *testing.T) {
	a, f := adminLogRig(t)
	mu, lines := captureLogLines(t)

	steps := []struct {
		name string
		call func() error
		want string
		lvl  string
	}{
		{"versioning enabled", func() error { return a.SetBucketVersioning("docs", true) },
			"versioning enabled", LogInfo},
		{"versioning suspended", func() error { return a.SetBucketVersioning("docs", false) },
			"versioning suspended", LogInfo},
		{"policy updated", func() error {
			return a.PutBucketPolicy("docs", `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::docs/*"}]}`)
		}, "bucket policy updated", LogInfo},
		{"policy removed", func() error { return a.DeleteBucketPolicy("docs") },
			"bucket policy removed", LogWarn},
		{"cors updated", func() error {
			return a.PutBucketCORS("docs", []adminops.CORSRule{{
				Origins: []string{"https://example.com"}, Methods: []string{"GET"},
			}})
		}, "CORS rules updated (1)", LogInfo},
		{"cors removed by empty set", func() error { return a.PutBucketCORS("docs", nil) },
			"CORS rules removed (empty set)", LogWarn},
		{"cors deleted", func() error { return a.DeleteBucketCORS("docs") },
			"CORS rules removed", LogWarn},
		{"lifecycle updated", func() error {
			return a.PutBucketLifecycle("docs", []adminops.LifecycleRule{{
				ID: "expire", Enabled: true, ExpirationDays: 30,
			}})
		}, "lifecycle rules updated (1)", LogInfo},
		{"lifecycle removed by empty set", func() error { return a.PutBucketLifecycle("docs", nil) },
			"lifecycle rules removed (empty set)", LogWarn},
		{"lifecycle deleted", func() error { return a.DeleteBucketLifecycle("docs") },
			"lifecycle rules removed", LogWarn},
		{"encryption set aes256", func() error { return a.PutBucketEncryption("docs", "AES256", "") },
			"default encryption set to AES256", LogInfo},
		{"encryption set kms", func() error {
			return a.PutBucketEncryption("docs", "aws:kms", "arn:aws:kms:us-east-1:1:key/x")
		}, "default encryption set to aws:kms (key arn:aws:kms:us-east-1:1:key/x)", LogInfo},
		{"encryption removed", func() error { return a.DeleteBucketEncryption("docs") },
			"default encryption removed", LogWarn},
		{"pab updated", func() error { return a.PutBucketPAB("docs", adminops.PABInfo{}.All()) },
			"public-access-block settings updated", LogInfo},
		{"website configured", func() error {
			return a.PutBucketWebsite("docs", adminops.WebsiteInfo{IndexSuffix: "index.html"})
		}, "website hosting configured", LogInfo},
		{"website removed", func() error { return a.DeleteBucketWebsite("docs") },
			"website hosting removed", LogWarn},
		{"tags updated", func() error {
			return a.PutBucketTags("docs", []adminops.Tag{{Key: "team", Value: "core"}})
		}, "tags updated (1)", LogInfo},
		{"tags removed", func() error { return a.DeleteBucketTags("docs") },
			"tags removed", LogWarn},
	}
	for _, s := range steps {
		if err := s.call(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, s := range steps {
		n := 0
		for _, l := range *lines {
			if l.Scope == "admin" && l.Source == "docs" && l.Level == s.lvl && l.Message == s.want {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d matching admin line(s), want exactly 1 (all lines: %+v)", s.name, n, *lines)
		}
	}
	if len(*lines) != len(steps) {
		t.Errorf("captured %d admin line(s), want %d — no setter may log more than its one line", len(*lines), len(steps))
	}

	// the wire saw every subresource verb the panel speaks
	for _, want := range []string{
		"PUT versioning docs", "PUT policy docs", "DELETE policy docs",
		"PUT cors docs", "DELETE cors docs", "PUT lifecycle docs", "DELETE lifecycle docs",
		"PUT encryption docs", "DELETE encryption docs", "PUT public-access-block docs",
		"PUT website docs", "DELETE website docs", "PUT tagging docs", "DELETE tagging docs",
	} {
		if !slices.Contains(f.adminLandings(), want) {
			t.Errorf("wire never saw %q (landed: %v)", want, f.adminLandings())
		}
	}
}

// A verb the wire refused says so as an error line.
func TestAdminSetterFailureLogsError(t *testing.T) {
	a, f := adminLogRig(t)
	mu, lines := captureLogLines(t)
	f.fault("PUT", "docs", "policy", 5) // outlasts the SDK retryer's attempts

	if err := a.PutBucketPolicy("docs", `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::docs/*"}]}`); err == nil {
		t.Fatal("faulted policy PUT succeeded — the fault never fired")
	}
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, l := range *lines {
		if l.Scope == "admin" && l.Source == "docs" && l.Level == LogError &&
			strings.Contains(l.Message, "updating bucket policy failed") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d error line(s) for the failed policy PUT, want 1 (all lines: %+v)", n, *lines)
	}
}

// Client-resolution failures stay silent — the house shape CreateBucket
// set: the caller already holds the error, and there is no resolved
// source to hang the line on.
func TestAdminSetterClientFailureStaysSilent(t *testing.T) {
	a := newTestApp(t) // no source saved: client("") cannot resolve
	a.Startup(context.Background())
	mu, lines := captureLogLines(t)

	if err := a.PutBucketPolicy("docs", `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::docs/*"}]}`); err == nil {
		t.Fatal("PutBucketPolicy without a resolvable client succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	if n := len(*lines); n != 0 {
		t.Errorf("client-resolution failure logged %d line(s), want silence (%+v)", n, *lines)
	}
}

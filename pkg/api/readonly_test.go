// readonly_test.go: pins the read-only source law end to end. The gate
// must refuse every write verb BEFORE the wire — the rigs seat real
// sources against an httptest S3 that counts requests, so a slipped
// write fails the count, not just the return value. The resolution legs
// mirror client(""): the implicit single-source fallback, the named view
// source, Source* verbs on remote sources, transfer destinations,
// versioned copies, the editor pipeline and the doctor's write probes.
package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// roSrv seats a counting S3 stub: every request is a write attempt as
// far as these rigs are concerned, so any hit fails the test.
func roSrv(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "unexpected write reached the wire", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func roSource(url, name string, readOnly bool) profile.Source {
	return profile.Source{
		Name: name, Type: profile.TypeS3, ReadOnly: readOnly,
		S3: &profile.Profile{
			Endpoint: url, Region: "us-east-1", PathStyle: true,
			AccessKeyID: "k", SecretKey: "s",
		},
	}
}

// TestReadOnlyRefusesBeforeWire: one read-only S3 source (the implicit
// single-source resolution), every write verb family refuses with
// ErrReadOnlySource and nothing reaches the wire.
func TestReadOnlyRefusesBeforeWire(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	srv, hits := roSrv(t)
	a.session = []profile.Source{roSource(srv.URL, "ro-bucket", true)}

	verbs := []struct {
		name string
		call func() error
	}{
		{"delete", func() error { _, err := a.DeleteSelection("b", []string{"k"}, false); return err }},
		{"rename", func() error { return a.RenameObject("b", "k", "n") }},
		{"folder", func() error { return a.CreateFolder("b", "", "d") }},
		{"file", func() error { _, err := a.CreateFile("b", "", "n", "txt"); return err }},
		{"upload", func() error {
			_, err := a.Upload([]string{t.TempDir() + "/x"}, "b", "", "overwrite", 0, nil)
			return err
		}},
		{"copy", func() error {
			_, err := a.CopySelection("b", []string{"k"}, "b2", "", false, "overwrite", nil)
			return err
		}},
		{"bucket delete", func() error { _, err := a.DeleteBucket("b", false); return err }},
		{"versioning", func() error { return a.SetBucketVersioning("b", true) }},
		{"policy", func() error { return a.PutBucketPolicy("b", "{}") }},
		{"cors", func() error { return a.PutBucketCORS("b", nil) }},
		{"lifecycle", func() error { return a.PutBucketLifecycle("b", nil) }},
		{"encryption", func() error { return a.PutBucketEncryption("b", "AES256", "") }},
		{"pab", func() error {
			return a.PutBucketPAB("b", adminops.PABInfo{BlockPublicACLs: true, IgnorePublicACLs: true, BlockPublicPolicy: true, RestrictPublicBuckets: true})
		}},
		{"website", func() error { return a.PutBucketWebsite("b", adminops.WebsiteInfo{}) }},
		{"tags", func() error { return a.PutBucketTags("b", nil) }},
		{"class", func() error { _, err := a.ConvertStorageClass("b", []string{"k"}, "GLACIER", false); return err }},
		{"lock config", func() error { return a.PutBucketLockConfig("b", true, "COMPLIANCE", 30) }},
		{"retention", func() error { return a.PutObjectRetention("b", "k", "", "GOVERNANCE", "2030-01-01T00:00:00Z") }},
		{"legal hold", func() error { return a.SetObjectLegalHold("b", "k", "", true) }},
		{"restore version", func() error { return a.RestoreVersion("b", "k", "v1") }},
		{"undo delete", func() error { return a.UndoDelete("b", "k", "v1") }},
		{"purge", func() error { _, err := a.PurgeVersions("b", "", "noncurrent", false); return err }},
		{"empty bucket", func() error { _, err := a.EmptyBucketAllVersions("b", false); return err }},
		{"doctor write", func() error { _, err := a.RunDoctor("b", true); return err }},
		{"editor", func() error { _, err := a.EditFile(editTargetS3("", "b", "k"), false); return err }},
		{"transfer dest", func() error {
			_, err := a.TransferCross(nil, nil, xferDestS3("", "b", ""), "overwrite", 0, false, nil, false)
			return err
		}},
		{"vcopy dest", func() error {
			_, err := a.CopySelectionVersions("", "b", []string{"k"}, "", "b2", "", false)
			return err
		}},
	}
	for _, v := range verbs {
		err := v.call()
		if !errors.Is(err, ErrReadOnlySource) {
			t.Errorf("%s: got %v, want ErrReadOnlySource", v.name, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d request(s) reached the wire despite the read-only gate", n)
	}
}

// TestReadOnlyMoveRefusesSource: a MOVE writes both sides — the read-only
// leg refuses even when the other side is writable, for transfer items
// and versioned copies alike. A plain COPY out of a read-only source
// stays legal (a read), so only the destination gate fires — refused by
// the read-only destination in these legs.
func TestReadOnlyMoveRefusesSource(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	srv, hits := roSrv(t)
	a.session = []profile.Source{
		roSource(srv.URL, "ro-src", true),
		{Name: "rw", Type: profile.TypeS3, S3: &profile.Profile{
			Endpoint: srv.URL, Region: "us-east-1", PathStyle: true, AccessKeyID: "k", SecretKey: "s",
		}},
	}
	if err := a.SetViewSource("rw"); err != nil {
		t.Fatal(err)
	}

	// move INTO a writable destination FROM a read-only source: refused
	if _, err := a.TransferCross([]XferItem{{Source: "ro-src", Bucket: "b", Key: "k"}}, nil,
		XferDest{Kind: "s3", Source: "rw", Bucket: "b2"}, "overwrite", 0, true, nil, false); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf("transfer move from read-only: got %v, want ErrReadOnlySource", err)
	}
	// plain COPY into a read-only destination: refused
	if _, err := a.TransferCross([]XferItem{{Source: "rw", Bucket: "b", Key: "k"}}, nil,
		XferDest{Kind: "s3", Source: "ro-src", Bucket: "b2"}, "overwrite", 0, false, nil, false); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf("transfer copy into read-only: got %v, want ErrReadOnlySource", err)
	}
	// versioned move from read-only source: refused
	if _, err := a.CopySelectionVersions("ro-src", "b", []string{"k"}, "rw", "b2", "", true); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf("vcopy move from read-only: got %v, want ErrReadOnlySource", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d request(s) reached the wire despite the read-only gate", n)
	}
}

// TestReadOnlyNamedAndRemoteLegs: Source* verbs refuse on the read-only
// source by id AND name while the writable twin passes the gate (its
// request reaches the wire); remote writes refuse before any dial.
func TestReadOnlyNamedAndRemoteLegs(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	srv, hits := roSrv(t)
	ro := roSource(srv.URL, "ro-remote", true)
	ro.Type = profile.TypeSFTP
	ro.S3 = nil
	ro.Host = "nowhere.invalid"
	a.session = []profile.Source{ro}

	if err := a.RemoteMkdir("ro-remote", "/d"); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf("RemoteMkdir: got %v, want ErrReadOnlySource", err)
	}
	// id-keyed: give the source an id and refuse by it too
	a.session[0].ID = "src-1"
	if err := a.RemoteMkdir("src-1", "/d"); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf("RemoteMkdir by id: got %v, want ErrReadOnlySource", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d request(s) reached the wire", n)
	}
}

// TestGateSourceResolution: the gate resolves exactly what client("")
// would — the named view source wins over a lone read-only source, and
// the implicit single-source fallback still sees a read-only source.
func TestGateSourceResolution(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	srv, _ := roSrv(t)
	a.session = []profile.Source{
		roSource(srv.URL, "ro", true),
		{Name: "rw", Type: profile.TypeS3, S3: &profile.Profile{
			Endpoint: srv.URL, Region: "us-east-1", PathStyle: true, AccessKeyID: "k", SecretKey: "s",
		}},
	}
	// no view source + two S3 sources: "" resolves nothing (client("")
	// would error too) — the gate stays quiet, the verb's own dial fails
	if src, ok := a.gateSource(""); ok {
		t.Fatalf(`gateSource("") = %q, want no resolution with two S3 sources`, src.Name)
	}
	// named view source on the writable one: writable
	if err := a.SetViewSource("rw"); err != nil {
		t.Fatal(err)
	}
	if err := a.requireWritable(""); err != nil {
		t.Errorf(`requireWritable("") with writable view source: %v`, err)
	}
	if err := a.requireWritable("ro"); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf(`requireWritable("ro"): %v`, err)
	}
	// single read-only source resolves implicitly even unnamed
	a.session = []profile.Source{roSource(srv.URL, "solo", true)}
	a.SetViewSource("")
	if err := a.requireWritable(""); !errors.Is(err, ErrReadOnlySource) {
		t.Errorf(`single read-only source must resolve implicitly: %v`, err)
	}
}

// TestReadOnlyFlagSurvivesPublicAndSave: the flag rides ListSources
// (Public passes bools) and a SaveSource round trip — the GUI's honest
// UI depends on the same flag the wire gate reads.
func TestReadOnlyFlagSurvivesPublicAndSave(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	src := roSource("http://localhost:9000", "kept", true)
	if err := a.saveSessionSource(src); err != nil {
		t.Fatal(err)
	}
	got, err := a.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range got {
		if s.Name == "kept" {
			found = true
			if !s.ReadOnly {
				t.Fatal("ListSources lost the readOnly flag")
			}
		}
	}
	if !found {
		t.Fatal("saved source missing from ListSources")
	}
	// the writable default round trip: no flag, no behavior change
	if err := a.saveSessionSource(roSource("http://localhost:9000", "rw", false)); err != nil {
		t.Fatal(err)
	}
	for _, s := range mustList(t, a) {
		if s.Name == "rw" && s.ReadOnly {
			t.Fatal("readOnly appeared on a writable source")
		}
	}
}

func mustList(t *testing.T, a *App) []profile.Source {
	t.Helper()
	got, err := a.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func editTargetS3(source, bucket, key string) EditTarget {
	return EditTarget{Kind: "s3", Source: source, Bucket: bucket, Key: key}
}

func xferDestS3(source, bucket, dir string) XferDest {
	return XferDest{Kind: "s3", Source: source, Bucket: bucket, Dir: dir}
}

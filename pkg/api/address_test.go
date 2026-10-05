package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// addressTestApp seeds one account-wide S3 source, one bucket-scoped S3
// source and one sftp source — the shapes the ladder has to tell apart.
func addressTestApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	for _, s := range []profile.Source{
		{Name: "hetzner", Type: profile.TypeS3, S3: &profile.Profile{Endpoint: "https://hetzner.example.test"}},
		{Name: "website-prod", Type: profile.TypeS3, Bucket: "www-assets", S3: &profile.Profile{Endpoint: "https://aws.example.test"}},
		{Name: "backup-box", Type: profile.TypeSFTP, Host: "backup-box.example.test", Port: 2022, Username: "demo", Root: ""},
	} {
		if err := a.SaveSource(s); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestParseAddressAppPaths(t *testing.T) {
	a := addressTestApp(t)

	// NAME:// for an account-wide S3 source: bucket then prefix, the
	// buckets view at the root, backslashes folding like the old editor
	cases := []struct {
		raw  string
		kind string
		src  string
		bkt  string
		pfx  string
	}{
		{"hetzner://team-files/docs/notes.md", "objects", "hetzner", "team-files", "docs/notes.md/"},
		{"hetzner://team-files/docs/", "objects", "hetzner", "team-files", "docs/"},
		{"hetzner://team-files", "objects", "hetzner", "team-files", ""},
		{"hetzner://", "buckets", "hetzner", "", ""},
		{`HETZNER://team-files\docs`, "objects", "hetzner", "team-files", "docs/"},
	}
	for _, c := range cases {
		got, err := a.ParseAddress(c.raw, "")
		if err != nil {
			t.Fatalf("ParseAddress(%q): %v", c.raw, err)
		}
		if got.Kind != c.kind || got.Source != c.src || got.Bucket != c.bkt || got.Prefix != c.pfx {
			t.Errorf("ParseAddress(%q) = %+v, want %s %s %q %q", c.raw, got, c.kind, c.src, c.bkt, c.pfx)
		}
	}

	// NAME:// for a bucket-scoped source: the rest is content inside the
	// one bucket, and the legacy doubled form folds away
	got, err := a.ParseAddress("website-prod://www-assets/index.html", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "objects" || got.Bucket != "www-assets" || got.Prefix != "index.html/" {
		t.Errorf("scoped doubled form = %+v", got)
	}
	got, err = a.ParseAddress("website-prod://", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "objects" || got.Bucket != "www-assets" || got.Prefix != "" {
		t.Errorf("scoped root = %+v", got)
	}

	// NAME:// by id (ids are assigned at save; look one up)
	srcs, _ := a.ListSources()
	var id string
	for _, s := range srcs {
		if s.Name == "backup-box" {
			id = s.ID
		}
	}
	if id == "" {
		t.Fatal("backup-box has no id")
	}
	got, err = a.ParseAddress(id+":///srv/data", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "remote" || got.Source != "backup-box" || got.Prefix != "/srv/data/" {
		t.Errorf("by-id remote = %+v", got)
	}

	// NAME:// for a remote source: /-anchored path, trailing slash
	got, err = a.ParseAddress("backup-box:///backup.sh", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "remote" || got.Source != "backup-box" || got.Prefix != "/backup.sh/" {
		t.Errorf("remote = %+v", got)
	}
}

func TestParseAddressNormalizedForm(t *testing.T) {
	a := addressTestApp(t)

	// Name/contents — the editors' normalized display form — resolves
	// through the same source grammar NAME:// speaks
	cases := []struct {
		raw  string
		kind string
		src  string
		bkt  string
		pfx  string
	}{
		{"hetzner/team-files/docs/notes.md", "objects", "hetzner", "team-files", "docs/notes.md/"},
		{`HETZNER\team-files\docs`, "objects", "hetzner", "team-files", "docs/"},
		{"website-prod/index.html", "objects", "website-prod", "www-assets", "index.html/"},
		{"backup-box/srv/data", "remote", "backup-box", "", "/srv/data/"},
	}
	for _, c := range cases {
		got, err := a.ParseAddress(c.raw, "")
		if err != nil {
			t.Fatalf("ParseAddress(%q): %v", c.raw, err)
		}
		if got.Kind != c.kind || got.Source != c.src || got.Bucket != c.bkt || got.Prefix != c.pfx {
			t.Errorf("ParseAddress(%q) = %+v, want %s %s %q %q", c.raw, got, c.kind, c.src, c.bkt, c.pfx)
		}
	}

	// the bare name alone is the source's root — buckets for an
	// account-wide S3 source, / for a remote one
	got, err := a.ParseAddress("hetzner", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "buckets" || got.Source != "hetzner" {
		t.Errorf("bare s3 name = %+v", got)
	}
	got, err = a.ParseAddress("backup-box", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "remote" || got.Source != "backup-box" || got.Prefix != "/" {
		t.Errorf("bare remote name = %+v", got)
	}

	// by id (ids are assigned at save; look one up)
	srcs, _ := a.ListSources()
	var id string
	for _, s := range srcs {
		if s.Name == "backup-box" {
			id = s.ID
		}
	}
	if id == "" {
		t.Fatal("backup-box has no id")
	}
	got, err = a.ParseAddress(id+"/srv/data", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "remote" || got.Source != "backup-box" || got.Prefix != "/srv/data/" {
		t.Errorf("by-id remote = %+v", got)
	}
}

func TestParseAddressNormalizedPrecedence(t *testing.T) {
	// the local shape wins over the name step: even a source named "c"
	// cannot shadow a drive-letter path, and a bucket name is not a
	// source name
	a := addressTestApp(t)
	if err := a.SaveSource(profile.Source{Name: "c", Type: profile.TypeSFTP,
		Host: "c.example.test", Port: 22, Username: "demo"}); err != nil {
		t.Fatal(err)
	}
	got, err := a.ParseAddress(`C:\Projects\llm-scaler`, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "local" || got.Prefix != `C:\Projects\llm-scaler` {
		t.Errorf("drive beats source named c = %+v", got)
	}
	// team-files is hetzner's bucket, not a source — no match, error
	if _, err := a.ParseAddress("team-files/docs", ""); err == nil {
		t.Error("bucket-name first segment: want error")
	}
}

func TestParseAddressS3Scheme(t *testing.T) {
	a := addressTestApp(t)

	// a source scoped to the named bucket wins outright — even with a
	// different account as the editor's hint
	got, err := a.ParseAddress("s3://www-assets/index.html", "hetzner")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "website-prod" || got.Bucket != "www-assets" || got.Prefix != "index.html/" {
		t.Errorf("scoped match = %+v", got)
	}

	// the hint breaks ties while it is an account-wide S3 source
	got, err = a.ParseAddress("s3://team-files/readme.md", "hetzner")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "hetzner" || got.Bucket != "team-files" || got.Prefix != "readme.md/" {
		t.Errorf("hint pick = %+v", got)
	}

	// a scoped or non-S3 hint cannot own a foreign bucket — but the
	// workspace's only account-wide source still can
	for _, hint := range []string{"website-prod", "backup-box", ""} {
		got, err = a.ParseAddress("s3://team-files/readme.md", hint)
		if err != nil {
			t.Fatalf("hint %q: %v", hint, err)
		}
		if got.Source != "hetzner" || got.Bucket != "team-files" || got.Prefix != "readme.md/" {
			t.Errorf("hint %q pick = %+v", hint, got)
		}
	}
}

func TestParseAddressTypedScheme(t *testing.T) {
	a := addressTestApp(t)

	// TYPE://Name/contents — the scheme is the source's TYPE and the
	// first segment names a configured source OF THAT TYPE: the
	// canonical address form every app-added data source carries. No
	// NewSource ever stands up — the named source opens outright.
	cases := []struct {
		raw  string
		kind string
		src  string
		bkt  string
		pfx  string
	}{
		{"sftp://backup-box/srv/data", "remote", "backup-box", "", "/srv/data/"},
		{"sftp://backup-box", "remote", "backup-box", "", "/"},
		{"SFTP://BACKUP-BOX/srv/data", "remote", "backup-box", "", "/srv/data/"},
		{"s3://hetzner/team-files/docs/notes.md", "objects", "hetzner", "team-files", "docs/notes.md/"},
		{"s3://hetzner", "buckets", "hetzner", "", ""},
		{"s3://website-prod/index.html", "objects", "website-prod", "www-assets", "index.html/"},
	}
	for _, c := range cases {
		got, err := a.ParseAddress(c.raw, "")
		if err != nil {
			t.Fatalf("ParseAddress(%q): %v", c.raw, err)
		}
		if got.Kind != c.kind || got.Source != c.src || got.Bucket != c.bkt || got.Prefix != c.pfx || got.NewSource != nil {
			t.Errorf("ParseAddress(%q) = %+v, want %s %s %q %q", c.raw, got, c.kind, c.src, c.bkt, c.pfx)
		}
	}

	// by id: the id names the source, the scheme its type
	srcs, _ := a.ListSources()
	var id string
	for _, s := range srcs {
		if s.Name == "backup-box" {
			id = s.ID
		}
	}
	if id == "" {
		t.Fatal("backup-box has no id")
	}
	got, err := a.ParseAddress("sftp://"+id+"/srv/data", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "remote" || got.Source != "backup-box" || got.Prefix != "/srv/data/" {
		t.Errorf("by-id typed remote = %+v", got)
	}

	// an authority segment (user@, :port) means a connection URL, not a
	// name: it falls through to the conn step even when a source is
	// named like the bare host
	got, err = a.ParseAddress("sftp://demo@backup-box:2022/srv/backup", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.NewSource == nil || got.NewSource.Type != "sftp" || got.NewSource.Host != "backup-box" {
		t.Errorf("authority guard = %+v, want the conn family (stand-up)", got)
	}

	// the name must be a source of THAT type: backup-box is sftp, so an
	// ftp-schemed form addressing it is a plain connection URL
	got, err = a.ParseAddress("ftp://backup-box/pub", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.NewSource == nil || got.NewSource.Type != "ftp" {
		t.Errorf("type guard = %+v, want the conn family (stand-up)", got)
	}
}

func TestParseAddressS3Ambiguous(t *testing.T) {
	// two account-wide S3 sources and no hint: an error naming the
	// candidates — NAME:// says which account outright
	a := newTestApp(t)
	a.Startup(context.Background())
	for _, s := range []profile.Source{
		{Name: "hetzner", Type: profile.TypeS3, S3: &profile.Profile{Endpoint: "https://hetzner.example.test"}},
		{Name: "aws-work", Type: profile.TypeS3, S3: &profile.Profile{Endpoint: "https://aws.example.test"}},
	} {
		if err := a.SaveSource(s); err != nil {
			t.Fatal(err)
		}
	}
	_, err := a.ParseAddress("s3://my-bucket/path/to/object.txt", "")
	if err == nil || !strings.Contains(err.Error(), "hetzner") || !strings.Contains(err.Error(), "aws-work") {
		t.Errorf("ambiguous s3:// error = %v, want both candidates named", err)
	}
	// the hint resolves the same address
	got, err := a.ParseAddress("s3://my-bucket/path/to/object.txt", "aws-work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "aws-work" || got.Bucket != "my-bucket" || got.Prefix != "path/to/object.txt/" {
		t.Errorf("hint pick = %+v", got)
	}
}

func TestParseAddressSingleAccount(t *testing.T) {
	// a workspace with exactly ONE account-wide S3 source: bare s3://
	// resolves to it without a hint
	a := newTestApp(t)
	a.Startup(context.Background())
	if err := a.SaveSource(profile.Source{Name: "only", Type: profile.TypeS3,
		S3: &profile.Profile{Endpoint: "https://only.example.test"}}); err != nil {
		t.Fatal(err)
	}
	got, err := a.ParseAddress("s3://my-bucket/path/to/object.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "objects" || got.Source != "only" || got.Bucket != "my-bucket" || got.Prefix != "path/to/object.txt/" {
		t.Errorf("single-account s3:// = %+v", got)
	}
}

func TestParseAddressNoS3Configured(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	if err := a.SaveSource(profile.Source{Name: "backup-box", Type: profile.TypeSFTP,
		Host: "backup-box.example.test", Port: 2022, Username: "demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ParseAddress("s3://my-bucket/x", ""); err == nil || !strings.Contains(err.Error(), "no S3 data source") {
		t.Errorf("s3:// with no s3 sources = %v, want the no-S3-configured error", err)
	}
}

func TestParseAddressConnURIs(t *testing.T) {
	a := addressTestApp(t)

	// a configured source of the same type+host+user opens at the URI's
	// root — no new source
	got, err := a.ParseAddress("sftp://demo@backup-box.example.test:2022/srv/backup", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "remote" || got.Source != "backup-box" || got.Prefix != "/srv/backup/" || got.NewSource != nil {
		t.Errorf("existing conn = %+v", got)
	}

	// nothing matches: the address itself carries the connection
	got, err = a.ParseAddress("ftp://user:password@host:21/path/to/file.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	ns := got.NewSource
	if ns == nil || ns.Type != "ftp" || ns.Host != "host" || ns.Port != 21 ||
		ns.Username != "user" || ns.Password != "password" || ns.Root != "/path/to/file.txt" {
		t.Errorf("new-source fields = %+v", ns)
	}
	if ns.Name != "host - path/to/file.txt" {
		t.Errorf("new-source name = %q", ns.Name)
	}
	if got.Kind != "remote" || got.Source != ns.Name || got.Prefix != "/path/to/file.txt/" {
		t.Errorf("new-source loc = %+v", got)
	}
	// the connection saves like the editor would, password and all
	if err := a.SaveSource(*ns); err != nil {
		t.Fatalf("SaveSource(new): %v", err)
	}
	if full, err := a.sourceByIDOrName(ns.Name); err != nil || full.Password != "password" {
		t.Errorf("saved password = %q (%v), want it stored", full.Password, err)
	}

	// the same URI again now matches the saved source — no second one
	got, err = a.ParseAddress("ftp://user@host:21/path/to/", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.NewSource != nil || got.Source != ns.Name {
		t.Errorf("re-paste after save = %+v", got)
	}
	srcs, _ := a.ListSources()
	for _, s := range srcs {
		if s.Type == "ftp" && s.Name != ns.Name {
			t.Errorf("duplicate ftp source %q beside %q", s.Name, ns.Name)
		}
	}

	// malformed connection URI: the parser's detail surfaces
	if _, err := a.ParseAddress("ftp://host:notaport/x", ""); err == nil {
		t.Error("bad port: want error")
	}
}

func TestParseAddressLocal(t *testing.T) {
	a := addressTestApp(t)

	got, err := a.ParseAddress("file:///C:/Users/mikko/data/file.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	// expectations go through FromSlash like the resolver, so the suite
	// holds on separators-native and pass-through platforms alike
	if want := filepath.FromSlash("C:/Users/mikko/data/file.txt"); got.Kind != "local" || got.Prefix != want {
		t.Errorf("file:/// drive = %+v, want %q", got, want)
	}
	got, err = a.ParseAddress("file://server/share/dir", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.FromSlash(`\\server/share/dir`); got.Kind != "local" || got.Prefix != want {
		t.Errorf("file:// UNC = %+v, want %q", got, want)
	}
	// percent-encoding decodes (file URLs carry %20 for spaces)
	got, err = a.ParseAddress("file:///C:/My%20Docs/", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.FromSlash("C:/My Docs/"); got.Prefix != want {
		t.Errorf("decoded file URL = %q, want %q", got.Prefix, want)
	}
	// local:// — the secondary pane's canonical form round-trips (the
	// roots view is the bare scheme)
	got, err = a.ParseAddress(`local://C:\Users\demo\Downloads`, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "local" || got.Prefix != `C:\Users\demo\Downloads` {
		t.Errorf("local:// dir = %+v", got)
	}
	got, err = a.ParseAddress("local://", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "local" || got.Prefix != "" {
		t.Errorf("local:// roots = %+v", got)
	}
	// the user's own examples
	for _, raw := range []string{`C:\Projects\llm-scaler`, "C:/Projects/llm-scaler", `\\server\share\data`} {
		got, err = a.ParseAddress(raw, "")
		if err != nil {
			t.Fatalf("ParseAddress(%q): %v", raw, err)
		}
		if got.Kind != "local" {
			t.Errorf("ParseAddress(%q) kind = %s", raw, got.Kind)
		}
	}
	if got.Prefix != `\\server\share\data` { // no forward slashes: native on every platform
		t.Errorf("bare UNC = %q", got.Prefix)
	}
	if got2, _ := a.ParseAddress(`C:\Projects\llm-scaler`, ""); got2.Prefix != `C:\Projects\llm-scaler` {
		t.Errorf("bare drive = %q", got2.Prefix)
	}

	// ~ expands under the home dir
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir in this environment")
	}
	got, err = a.ParseAddress(`~\Documents`, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "Documents"); got.Prefix != want {
		t.Errorf("~ expansion = %q, want %q", got.Prefix, want)
	}
}

func TestParseAddressErrors(t *testing.T) {
	a := addressTestApp(t)
	for _, raw := range []string{"", "   ", "nope://void", "just a name", "s3://", "s3:///x", "s3://b@d/x"} {
		if _, err := a.ParseAddress(raw, ""); err == nil {
			t.Errorf("ParseAddress(%q) succeeded, want error", raw)
		}
	}
}

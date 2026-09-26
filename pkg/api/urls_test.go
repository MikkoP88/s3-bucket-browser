package api

import (
	"context"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// RemoteUrls: real addresses for every non-S3 source type — root joined,
// default ports elided, non-default kept, no password, S3 refused.
func TestRemoteUrlsForms(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	// sftp with a root and a non-default port: server path = root + anchored
	box := profile.Source{Name: "box", Type: profile.TypeSFTP,
		Host: "files.example.com", Port: 2022, Username: "demo",
		Password: "hunter2", Root: "/srv/backup"}
	if err := a.SaveSource(box); err != nil {
		t.Fatal(err)
	}
	got, err := a.RemoteUrls("box", []string{"/docs/inventory.csv", "/docs/"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"sftp://demo@files.example.com:2022/srv/backup/docs/inventory.csv",
		"sftp://demo@files.example.com:2022/srv/backup/docs/",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RemoteUrls[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, u := range got {
		if strings.Contains(u, "hunter2") {
			t.Errorf("password leaked into %q", u)
		}
	}

	// webdavs on the conventional 443: port elided; no root → anchored path
	dav := profile.Source{Name: "dav", Type: profile.TypeWebDAVS,
		Host: "dav.example.com", Port: 443, Username: "u@example.com"}
	if err := a.SaveSource(dav); err != nil {
		t.Fatal(err)
	}
	got, err = a.RemoteUrls("dav", []string{"claim.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "webdavs://u%40example.com@dav.example.com/claim.pdf" {
		t.Errorf("webdavs url = %q", got[0])
	}

	// local-root source: file:/// with forward slashes
	lab, root := localSource(t, "lab")
	if err := a.SaveSource(lab); err != nil {
		t.Fatal(err)
	}
	got, err = a.RemoteUrls("lab", []string{"/readme.md"})
	if err != nil {
		t.Fatal(err)
	}
	// Unix roots already start with /; Windows drive paths do not.
	if got[0] != "file:///"+strings.TrimLeft(strings.ReplaceAll(root, "\\", "/"), "/")+"/readme.md" {
		t.Errorf("file url = %q", got[0])
	}

	// S3 sources stay on their pipeline
	if err := a.SaveSource(s3Source("cloud", "s")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoteUrls("cloud", nil); err == nil || !strings.Contains(err.Error(), "S3 pipeline") {
		t.Errorf("s3 via RemoteUrls err = %v", err)
	}
}

// ObjectUrls: the presigner's URL minus its query — custom endpoint and
// path-style honored, no credentials survive in the address.
func TestObjectUrlsStripsSignature(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src := s3Source("lab", "s")
	src.S3.PathStyle = true
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	got, err := a.SourceObjectUrls("lab", "team-files", []string{"readme.md", "docs/notes v2.md"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "https://s3.example.com/team-files/readme.md" {
		t.Errorf("object url = %q", got[0])
	}
	if got[1] != "https://s3.example.com/team-files/docs/notes%20v2.md" {
		t.Errorf("escaped key url = %q", got[1])
	}
	for _, u := range got {
		if strings.Contains(u, "?") || strings.Contains(u, "AKIAEXAMPLE") {
			t.Errorf("signature material survived in %q", u)
		}
	}
}

package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// ListSourceObjectsStream must resolve the named source before anything
// else: unknown sources and non-S3 sources fail fast; a resolvable S3
// source proceeds to the (context-less) stream setup in tests.
func TestListSourceObjectsStreamSourceResolution(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.ListSourceObjectsStream("nope", "b", ""); !errors.Is(err, profile.ErrNotFound) {
		t.Fatalf("unknown source: want ErrNotFound, got %v", err)
	}
	if err := a.SaveSource(profile.Source{Name: "box", Type: profile.TypeSFTP, Host: "h", Username: "u"}); err != nil {
		t.Fatalf("save sftp source: %v", err)
	}
	if _, err := a.ListSourceObjectsStream("box", "b", ""); err == nil || !strings.Contains(err.Error(), "not an S3 source") {
		t.Fatalf("sftp source: want not-an-S3-source error, got %v", err)
	}
	if err := a.SaveSource(s3Source("lab", "s3secret")); err != nil {
		t.Fatalf("save s3 source: %v", err)
	}
	// No Wails context in tests: resolution succeeds, the stream setup
	// stops at errNoContext — proving the source resolved by name.
	if _, err := a.ListSourceObjectsStream("lab", "b", ""); !errors.Is(err, errNoContext) {
		t.Fatalf("valid source without context: want errNoContext, got %v", err)
	}
}

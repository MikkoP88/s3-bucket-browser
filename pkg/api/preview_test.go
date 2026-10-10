// preview_test.go pins the preview read: the S3 leg's Range grammar
// (the served window, ContentRange's true total, the honest
// truncation), the engine legs (a saved source's own engine and the
// workstation), the WHATWG sniff, and the refusals (folders, empty
// operands, unknown kinds).
package api

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// smallPNG stages a real 1×1 PNG: the sniff reads its signature, the
// round trip reads its bytes.
func smallPNG(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The S3 leg rides the Range grammar end to end: the small object
// round-trips its bytes with the sniffed type, the object past the
// bound serves exactly the window with ContentRange speaking the true
// total, a missing key is an error, and folder-shaped operands refuse
// up front.
func TestPreviewS3Leg(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("pics")
	f.seed("pics", "shot.png", string(smallPNG(t)))
	if err := a.SaveSource(fakeS3Source("shots", f.serve(t))); err != nil {
		t.Fatal(err)
	}

	p, err := a.PreviewData(PreviewTarget{Kind: "s3", Source: "shots", Bucket: "pics", Key: "shot.png"})
	if err != nil {
		t.Fatalf("preview s3: %v", err)
	}
	if string(p.Data) != string(smallPNG(t)) {
		t.Errorf("s3 bytes did not round trip: %d bytes", len(p.Data))
	}
	if p.ContentType != "image/png" {
		t.Errorf("sniffed type = %q, want image/png", p.ContentType)
	}
	if p.Truncated || p.Size != int64(len(smallPNG(t))) {
		t.Errorf("small object shape: size %d, truncated %v — want the full size, no cut", p.Size, p.Truncated)
	}

	// the object past the bound: the wire serves the window, ContentRange
	// speaks the total past the cut, and Truncated is the honest word
	big := strings.Repeat("x", PreviewMaxBytes+7)
	f.seed("pics", "huge.png", big)
	p, err = a.PreviewData(PreviewTarget{Kind: "s3", Source: "shots", Bucket: "pics", Key: "huge.png"})
	if err != nil {
		t.Fatalf("preview oversized s3: %v", err)
	}
	if len(p.Data) != PreviewMaxBytes {
		t.Errorf("oversized preview carried %d bytes, want exactly the bound", len(p.Data))
	}
	if !p.Truncated {
		t.Error("oversized preview must say Truncated")
	}
	if p.Size != int64(len(big)) {
		t.Errorf("oversized size = %d, want the ContentRange total %d", p.Size, len(big))
	}

	// a missing key is an error the caller can toast, never an empty preview
	if _, err := a.PreviewData(PreviewTarget{Kind: "s3", Source: "shots", Bucket: "pics", Key: "ghost.png"}); err == nil {
		t.Error("missing key must error")
	}

	for _, c := range []struct {
		t     PreviewTarget
		voice string
	}{
		{PreviewTarget{Kind: "s3", Source: "shots", Bucket: "pics", Key: "docs/"}, "is a folder"},
		{PreviewTarget{Kind: "s3", Source: "shots", Bucket: "pics", Key: ""}, "is a folder"},
		{PreviewTarget{Kind: "s3", Source: "shots", Bucket: "", Key: "x.png"}, "pass an object key"},
	} {
		_, err := a.PreviewData(c.t)
		if err == nil || !strings.Contains(err.Error(), c.voice) {
			t.Errorf("preview %+v err = %v, want the %q refusal", c.t, err, c.voice)
		}
	}
}

// The engine legs: a saved source's own engine (the same path grammar
// RemoteList speaks) and the workstation file — bytes, sniff, size,
// the bound's truncation, the folder refusals.
func TestPreviewRemoteAndLocalLegs(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	src, root := emptyLocalSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	gif := append([]byte("GIF89a"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(root, "eye.gif"), gif, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := a.PreviewData(PreviewTarget{Kind: "remote", Source: "lab", Key: "/eye.gif"})
	if err != nil {
		t.Fatalf("preview remote: %v", err)
	}
	if string(p.Data) != string(gif) {
		t.Errorf("remote bytes did not round trip: %d bytes", len(p.Data))
	}
	if p.ContentType != "image/gif" {
		t.Errorf("remote sniff = %q, want image/gif", p.ContentType)
	}
	if p.Truncated || p.Size != int64(len(gif)) {
		t.Errorf("remote shape: size %d, truncated %v", p.Size, p.Truncated)
	}

	// the bound on the engine leg: the read stops at max, the stat
	// speaks the true size, Truncated is honest
	huge := make([]byte, PreviewMaxBytes+10)
	if err := os.WriteFile(filepath.Join(root, "huge.bin"), huge, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = a.PreviewData(PreviewTarget{Kind: "remote", Source: "lab", Key: "/huge.bin"})
	if err != nil {
		t.Fatalf("preview oversized remote: %v", err)
	}
	if len(p.Data) != PreviewMaxBytes || !p.Truncated || p.Size != int64(len(huge)) {
		t.Errorf("oversized remote shape: %d bytes, size %d, truncated %v", len(p.Data), p.Size, p.Truncated)
	}

	// folder-shaped operands and a source that is not saved
	if _, err := a.PreviewData(PreviewTarget{Kind: "remote", Source: "lab", Key: "/"}); err == nil ||
		!strings.Contains(err.Error(), "is a folder") {
		t.Errorf("remote root err = %v, want the folder refusal", err)
	}
	if _, err := a.PreviewData(PreviewTarget{Kind: "remote", Source: "ghost", Key: "/x.png"}); err == nil {
		t.Error("unknown source must error")
	}

	// the local leg
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	note := filepath.Join(root, "pic.jpg")
	if err := os.WriteFile(note, jpg, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = a.PreviewData(PreviewTarget{Kind: "local", Path: note})
	if err != nil {
		t.Fatalf("preview local: %v", err)
	}
	if string(p.Data) != string(jpg) || p.ContentType != "image/jpeg" {
		t.Errorf("local leg: %d bytes, sniff %q", len(p.Data), p.ContentType)
	}
	if _, err := a.PreviewData(PreviewTarget{Kind: "local", Path: root}); err == nil ||
		!strings.Contains(err.Error(), "is a folder") {
		t.Errorf("local dir err = %v, want the folder refusal", err)
	}
	if _, err := a.PreviewData(PreviewTarget{Kind: "local", Path: ""}); err == nil {
		t.Error("empty local path must error")
	}
}

// The refusals and the sniff: a kind the app does not know is named,
// and the WHATWG sniff reads the signatures the renderer will meet.
func TestPreviewRefusalsAndSniff(t *testing.T) {
	a := newTestApp(t)
	_, err := a.PreviewData(PreviewTarget{Kind: "ftp", Source: "x", Key: "/y"})
	if err == nil || !strings.Contains(err.Error(), `cannot preview kind "ftp"`) {
		t.Errorf("unknown kind err = %v, want the named refusal", err)
	}
	for _, c := range []struct {
		name string
		data []byte
		want string
	}{
		{"png", smallPNG(t), "image/png"},
		{"jpeg", append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...), "image/jpeg"},
		{"gif", append([]byte("GIF89a"), make([]byte, 32)...), "image/gif"},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...), "image/webp"},
		{"binary", []byte("\x00\x01\x02not an image at all"), "application/octet-stream"},
	} {
		if got := sniffContentType(c.data); got != c.want {
			t.Errorf("sniff(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

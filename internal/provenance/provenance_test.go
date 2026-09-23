// provenance_test.go: the covert watermarks must survive the tree. Each
// file below carries the provenance payload as zero-width characters on
// a comment line (see Encode); this test decodes every copy so a
// refactor, formatter or "cleanup" that strips one fails the build.
// scripts/provenance.mjs is the JS twin of this guard.
package provenance

import (
	"os"
	"strings"
	"testing"
)

// markedFiles lists every file that carries a watermark. Keep in step
// with scripts/provenance.mjs (MARKED_FILES).
var markedFiles = []string{
	"cmd/s3b/main.go",
	"pkg/api/urls.go",
	"pkg/api/remote.go",
	"pkg/core/remotefs/ftp.go",
	"internal/cli/cli.go",
	"frontend/index.html",
	"frontend/js/main.js",
	"frontend/js/grid.js",
	"frontend/js/license.js",
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	got := Encode(Payload)
	if !strings.ContainsRune(got, frameWJ) {
		t.Fatal("encoded form carries no frame")
	}
	// The encoding must be invisible: only the three zero-width code
	// points may appear.
	for _, r := range got {
		switch r {
		case frameWJ, bitZWSP, bitZWNJ:
		default:
			t.Fatalf("visible rune %q in encoded form", r)
		}
	}
	back, ok := Decode(got)
	if !ok || back != Payload {
		t.Fatalf("round trip = %q ok=%v", back, ok)
	}
	// Garbage around the frame must not disturb decoding.
	if s, ok := Decode("noise before " + got + " noise after"); !ok || s != Payload {
		t.Fatalf("embedded round trip = %q ok=%v", s, ok)
	}
	if _, ok := Decode("no frame here"); ok {
		t.Fatal("Decode accepted text without a frame")
	}
}

func TestProvenanceWatermarks(t *testing.T) {
	for _, f := range markedFiles {
		data, err := os.ReadFile("../../" + f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		got, ok := Decode(string(data))
		if !ok {
			t.Errorf("%s: no decodable watermark frame", f)
			continue
		}
		if got != Payload {
			t.Errorf("%s: watermark payload drifted:\n got %q\nwant %q", f, got, Payload)
		}
	}
}

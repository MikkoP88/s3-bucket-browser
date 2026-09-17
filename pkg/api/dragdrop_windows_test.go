//go:build windows

package api

import (
	"strings"
	"testing"
	"unsafe"
)

// hdropBytes must produce the exact CF_HDROP layout Explorer parses:
// DROPFILES (pFiles=20, fWide) followed by a double-null-terminated
// UTF-16 path list.
func TestHdropBytesLayout(t *testing.T) {
	b := hdropBytes([]string{`C:\a\x.txt`, `C:\b\y z.txt`})
	if pFiles := *(*uint32)(unsafe.Pointer(&b[0])); pFiles != 20 {
		t.Fatalf("pFiles = %d, want 20", pFiles)
	}
	if fWide := *(*uint32)(unsafe.Pointer(&b[16])); fWide != 1 {
		t.Fatalf("fWide = %d, want 1", fWide)
	}
	u16 := make([]uint16, (len(b)-20)/2)
	for i := range u16 {
		u16[i] = *(*uint16)(unsafe.Pointer(&b[20+i*2]))
	}
	var sb strings.Builder
	for _, c := range u16 {
		sb.WriteRune(rune(c))
	}
	got := sb.String()
	want := "C:\\a\\x.txt\x00C:\\b\\y z.txt\x00\x00"
	if got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
}

func TestFeMatchesHDROP(t *testing.T) {
	ok := formatEtc{cfFormat: cfHDROP, dwAspect: dvaspectContent, tymed: tymedHGlobal, lindex: -1}
	if !feMatchesHDROP(&ok) {
		t.Fatal("canonical CF_HDROP FORMATETC must match")
	}
	any := formatEtc{cfFormat: cfHDROP} // wildcards for aspect/tymed
	if !feMatchesHDROP(&any) {
		t.Fatal("wildcard FORMATETC must match")
	}
	badFormat := formatEtc{cfFormat: 13} // CF_UNICODETEXT
	if feMatchesHDROP(&badFormat) {
		t.Fatal("non-HDROP format must not match")
	}
	badTymed := formatEtc{cfFormat: cfHDROP, tymed: 2} // TYMED_FILE
	if feMatchesHDROP(&badTymed) {
		t.Fatal("non-HGLOBAL medium must not match")
	}
}

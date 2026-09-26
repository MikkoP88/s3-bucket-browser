package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in                         string
		major, minor, patch, build uint32
	}{
		{"1.2.3", 1, 2, 3, 0},
		{"1.2.3.4", 1, 2, 3, 4},
		{"1.2.3-rc1", 1, 2, 3, 0},
		{"2.0.0.7+meta", 2, 0, 0, 7},
		{"10.20.30.40", 10, 20, 30, 40},
		{"dev", 0, 0, 0, 0},
		{"", 0, 0, 0, 0},
	}
	for _, c := range cases {
		ma, mi, pa, bu := parseVersion(c.in)
		if ma != c.major || mi != c.minor || pa != c.patch || bu != c.build {
			t.Errorf("parseVersion(%q) = %d.%d.%d.%d, want %d.%d.%d.%d",
				c.in, ma, mi, pa, bu, c.major, c.minor, c.patch, c.build)
		}
	}
}

// sectionFrom returns the raw bytes of section i of the COFF object, plus
// its name and file offset, so tests can walk the layout the linker sees.
func sectionFrom(t *testing.T, obj []byte, i int) (name string, data []byte) {
	t.Helper()
	le := binary.LittleEndian
	if n := le.Uint16(obj[2:]); int(n) <= i {
		t.Fatalf("object has %d sections, want > %d", n, i)
	}
	off := 20 + 40*i
	name = string(bytes.TrimRight(obj[off:off+8], "\x00"))
	size := le.Uint32(obj[off+16:])
	ptr := le.Uint32(obj[off+20:])
	if int(ptr)+int(size) > len(obj) {
		t.Fatalf("section %q [%d:%d] out of bounds (len %d)", name, ptr, ptr+size, len(obj))
	}
	return name, obj[ptr : ptr+size]
}

func TestBuildSysoStructure(t *testing.T) {
	const ver = "1.2.3.4"
	blob := versionInfoBlob(1, 2, 3, 4, ver)

	for arch, mi := range machines {
		t.Run(arch, func(t *testing.T) {
			le := binary.LittleEndian
			obj := buildSyso(mi, []resource{{rtype: rtVersion, name: 1, lang: stringTableLangID, data: blob}})

			// COFF header
			if got := le.Uint16(obj[0:]); got != mi.machine {
				t.Fatalf("Machine = %#04x, want %#04x", got, mi.machine)
			}
			if got := le.Uint16(obj[2:]); got != 2 {
				t.Fatalf("NumberOfSections = %d, want 2", got)
			}
			if got := le.Uint32(obj[4:]); got != 0 {
				t.Fatalf("TimeDateStamp = %d, want 0 (deterministic)", got)
			}

			// Section order and payload sizes
			n1, d1 := sectionFrom(t, obj, 0)
			n2, d2 := sectionFrom(t, obj, 1)
			if n1 != ".rsrc$01" || n2 != ".rsrc$02" {
				t.Fatalf("sections = %q, %q; want .rsrc$01, .rsrc$02", n1, n2)
			}
			if len(d1) != 0x58 || !bytes.Equal(d2, blob) {
				t.Fatalf("section sizes = %d, %d; want %d, %d", len(d1), len(d2), 0x58, len(blob))
			}

			// Resource directory: root -> RT_VERSION -> id 1 -> en-US -> data.
			// Each dir() window covers the 16-byte header + one 8-byte entry.
			dir := func(off uint32) []byte { return d1[off : off+24] }
			root := dir(0)
			if le.Uint16(root[12:]) != 0 || le.Uint16(root[14:]) != 1 {
				t.Fatal("root: want 0 named + 1 id entry")
			}
			if id := le.Uint32(root[16:]); id != 16 { // RT_VERSION
				t.Fatalf("root entry id = %d, want 16 (RT_VERSION)", id)
			}
			p := le.Uint32(root[20:])
			if p&0x80000000 == 0 || p&^0x80000000 != 0x18 {
				t.Fatalf("root entry offset = %#x, want dir at 0x18", p)
			}
			d2dir := dir(p &^ 0x80000000)
			if id := le.Uint32(d2dir[16:]); id != 1 {
				t.Fatalf("level-2 entry id = %d, want 1", id)
			}
			p = le.Uint32(d2dir[20:])
			if p&0x80000000 == 0 || p&^0x80000000 != 0x30 {
				t.Fatalf("level-2 offset = %#x, want dir at 0x30", p)
			}
			d3dir := dir(p &^ 0x80000000)
			if id := le.Uint32(d3dir[16:]); id != stringTableLangID {
				t.Fatalf("lang id = %#x, want %#x", id, stringTableLangID)
			}
			p = le.Uint32(d3dir[20:])
			if p&0x80000000 != 0 || p != 0x48 {
				t.Fatalf("level-3 offset = %#x, want data entry at 0x48", p)
			}

			// Data entry: OffsetToData is placeholder 0 — the linker's
			// addpersrc rewrites it via the relocation to RVA(.rsrc) +
			// len($01); Size covers the whole blob.
			entry := d1[p : p+16]
			if got := le.Uint32(entry[0:]); got != 0 {
				t.Fatalf("OffsetToData = %d, want 0 (linker-patched placeholder)", got)
			}
			if got := le.Uint32(entry[4:]); got != uint32(len(blob)) {
				t.Fatalf("Size = %d, want %d", got, len(blob))
			}

			// Relocation table of $01: exactly one fixup on the data
			// entry's OffsetToData, targeting symbol 1 (".rsrc$02").
			h1 := obj[20:60] // section header of $01
			if got := le.Uint32(h1[24:]); got == 0 {
				t.Fatal("$01 PointerToRelocations = 0, want a table")
			} else {
				rp := got
				if got := le.Uint16(h1[32:]); got != 1 {
					t.Fatalf("$01 NumberOfRelocations = %d, want 1", got)
				}
				if int(rp)+10 > len(obj) {
					t.Fatalf("reloc table at %d out of bounds", rp)
				}
				r := obj[rp : rp+10]
				if got := le.Uint32(r[0:]); got != 0x48 {
					t.Fatalf("reloc offset = %#x, want 0x48 (OffsetToData)", got)
				}
				if got := le.Uint32(r[4:]); got != 1 {
					t.Fatalf("reloc symbol index = %d, want 1 (.rsrc$02)", got)
				}
				if got := le.Uint16(r[8:]); got != mi.reloc {
					t.Fatalf("reloc type = %#x, want %#x", got, mi.reloc)
				}
			}

			// Symbol table: two STATIC section symbols, exact names.
			if sp, n := le.Uint32(obj[8:]), le.Uint32(obj[12:]); n != 2 || sp == 0 {
				t.Fatalf("symbol table: ptr=%d count=%d, want nonzero ptr and 2 symbols", sp, n)
			} else if int(sp)+2*18 > len(obj) {
				t.Fatalf("symbol table at %d out of bounds", sp)
			}
			sp := le.Uint32(obj[8:])
			for i, want := range []string{".rsrc$01", ".rsrc$02"} {
				s := obj[sp+uint32(18*i):]
				s = s[:18]
				if name := string(bytes.TrimRight(s[:8], "\x00")); name != want {
					t.Fatalf("symbol %d name = %q, want %q", i, name, want)
				}
				if sn := le.Uint16(s[12:]); sn != uint16(i+1) {
					t.Fatalf("symbol %d section = %d, want %d", i, sn, i+1)
				}
				if s[16] != 3 { // IMAGE_SYM_CLASS_STATIC
					t.Fatalf("symbol %d storage class = %d, want 3 (STATIC)", i, s[16])
				}
			}

			// Empty string table after the symbols: one DWORD = 4.
			if got := le.Uint32(obj[sp+36:]); got != 4 {
				t.Fatalf("string table length = %d, want 4 (empty table)", got)
			}

			// Determinism
			if again := buildSyso(mi, []resource{{rtype: rtVersion, name: 1, lang: stringTableLangID, data: blob}}); !bytes.Equal(obj, again) {
				t.Fatal("buildSyso not deterministic")
			}
		})
	}
}

func TestVersionInfoBlob(t *testing.T) {
	const ver = "9.9.9"
	le := binary.LittleEndian
	blob := versionInfoBlob(9, 9, 9, 0, ver)

	// Root header: wLength covers the whole blob, value = 52-byte fixed
	// info, type = binary.
	if got := le.Uint16(blob[0:]); got != uint16(len(blob)) {
		t.Fatalf("wLength = %d, want %d", got, len(blob))
	}
	if got := le.Uint16(blob[2:]); got != 52 {
		t.Fatalf("wValueLength = %d, want 52", got)
	}
	if got := le.Uint16(blob[4:]); got != 0 {
		t.Fatalf("wType = %d, want 0", got)
	}
	if key := blob[6:38]; !bytes.Equal(key, utf16Bytes("VS_VERSION_INFO\x00")) {
		t.Fatalf("root key = %q", key)
	}

	// VS_FIXEDFILEINFO starts after header+key+pad = offset 40.
	if got := le.Uint32(blob[40:]); got != 0xFEEF04BD {
		t.Fatalf("dwSignature = %#x, want FEEF04BD", got)
	}
	if got := le.Uint32(blob[48:]); got != 9<<16|9 { // FileVersionMS
		t.Fatalf("dwFileVersionMS = %#x, want %#x", got, 9<<16|9)
	}
	if got := le.Uint32(blob[52:]); got != 9<<16 { // FileVersionLS
		t.Fatalf("dwFileVersionLS = %#x, want %#x", got, 9<<16)
	}

	// String table content (UTF-16LE in the blob). Values are 32-bit
	// aligned after their keys, so key and value are matched separately.
	for _, s := range []string{
		"MikkoP88", "S3 Bucket Browser", "9.9.9\x00",
		"CompanyName\x00", "FileVersion\x00", "LegalCopyright\x00", "s3b.exe\x00",
	} {
		if !bytes.Contains(blob, utf16Bytes(s)) {
			t.Errorf("blob missing %q", s)
		}
	}
	// Translation var: 0x0409 + 0x04B0, little-endian.
	if !bytes.Contains(blob, []byte{0x09, 0x04, 0xB0, 0x04}) {
		t.Error("blob missing Translation var 0409/04B0")
	}
}

// fakeICO assembles a minimal two-image .ico for parser tests.
func fakeICO() []byte {
	le := binary.LittleEndian
	img1 := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	img2 := []byte{0xAA, 0xBB}
	buf := make([]byte, 6+2*16)
	le.PutUint16(buf[2:], 1)
	le.PutUint16(buf[4:], 2)
	e := buf[6:]
	e[0], e[1] = 16, 16
	le.PutUint16(e[4:], 1)
	le.PutUint16(e[6:], 32)
	le.PutUint32(e[8:], uint32(len(img1)))
	le.PutUint32(e[12:], uint32(6+2*16))
	e = buf[6+16:]
	e[0], e[1] = 0, 0 // 256
	le.PutUint16(e[4:], 1)
	le.PutUint16(e[6:], 32)
	le.PutUint32(e[8:], uint32(len(img2)))
	le.PutUint32(e[12:], uint32(6+2*16+len(img1)))
	return append(buf, append(append([]byte{}, img1...), img2...)...)
}

func TestParseICO(t *testing.T) {
	le := binary.LittleEndian
	imgs, err := parseICO(fakeICO())
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("images = %d, want 2", len(imgs))
	}
	if imgs[0].width != 16 || imgs[0].height != 16 || imgs[0].bits != 32 {
		t.Fatalf("image 0 fields = %d/%d/%d, want 16/16/32", imgs[0].width, imgs[0].height, imgs[0].bits)
	}
	if imgs[1].width != 0 || imgs[1].height != 0 {
		t.Fatalf("image 1 size = %d/%d, want 0/0 (256)", imgs[1].width, imgs[1].height)
	}
	if !bytes.Equal(imgs[0].data, []byte{0x89, 'P', 'N', 'G', 1, 2, 3}) || !bytes.Equal(imgs[1].data, []byte{0xAA, 0xBB}) {
		t.Fatal("payloads not split at the directory offsets")
	}

	mk := func(mut func([]byte)) []byte {
		b := fakeICO()
		mut(b)
		return b
	}
	bad := []struct {
		name string
		b    []byte
	}{
		{"cursor type", mk(func(b []byte) { le.PutUint16(b[2:], 2) })},
		{"nonzero reserved", mk(func(b []byte) { le.PutUint16(b[0:], 7) })},
		{"no images", mk(func(b []byte) { le.PutUint16(b[4:], 0) })},
		{"payload out of bounds", mk(func(b []byte) { le.PutUint32(b[6+8:], 1<<20) })},
		{"truncated", fakeICO()[:10]},
	}
	for _, c := range bad {
		if _, err := parseICO(c.b); err == nil {
			t.Errorf("%s: parseICO accepted a corrupt file", c.name)
		}
	}
	if _, err := parseICO([]byte{1, 2}); err == nil {
		t.Error("parseICO accepted a 2-byte file")
	}
}

func TestGroupIconBlob(t *testing.T) {
	le := binary.LittleEndian
	imgs, _ := parseICO(fakeICO())
	g := groupIconBlob(imgs, firstIconID)
	if le.Uint16(g[2:]) != 1 || le.Uint16(g[4:]) != 2 {
		t.Fatalf("type/count = %d/%d, want 1/2", le.Uint16(g[2:]), le.Uint16(g[4:]))
	}
	e0, e1 := g[6:20], g[20:34]
	if e0[0] != 16 || e0[1] != 16 || e1[0] != 0 || e1[1] != 0 {
		t.Fatal("size fields not carried over")
	}
	if le.Uint32(e0[8:]) != 7 || le.Uint32(e1[8:]) != 2 {
		t.Fatal("dwBytesInRes not carried over")
	}
	if id := le.Uint16(e0[12:]); id != firstIconID {
		t.Fatalf("entry 0 nID = %d, want %d", id, firstIconID)
	}
	if id := le.Uint16(e1[12:]); id != firstIconID+1 {
		t.Fatalf("entry 1 nID = %d, want %d", id, firstIconID+1)
	}
}

func TestBuildSysoWithIcon(t *testing.T) {
	le := binary.LittleEndian
	imgs, _ := parseICO(fakeICO())
	res := []resource{{rtype: rtVersion, name: 1, lang: stringTableLangID, data: []byte("VER")}}
	for i, m := range imgs {
		res = append(res, resource{rtype: rtIcon, name: firstIconID + uint16(i), lang: stringTableLangID, data: m.data})
	}
	res = append(res, resource{rtype: rtGroupIcon, name: groupIconID, lang: stringTableLangID, data: groupIconBlob(imgs, firstIconID)})
	obj := buildSyso(machines["amd64"], res)

	_, d1 := sectionFrom(t, obj, 0)
	_, d2 := sectionFrom(t, obj, 1)

	// Root: three type entries in ascending id order: 3 (RT_ICON),
	// 14 (RT_GROUP_ICON), 16 (RT_VERSION).
	if got := le.Uint16(d1[14:]); got != 3 {
		t.Fatalf("root id entries = %d, want 3", got)
	}
	if ids := []uint16{uint16(le.Uint32(d1[16:]) & 0xffff), uint16(le.Uint32(d1[24:]) & 0xffff), uint16(le.Uint32(d1[32:]) & 0xffff)}; ids[0] != rtIcon || ids[1] != rtGroupIcon || ids[2] != rtVersion {
		t.Fatalf("root type ids = %v, want [%d %d %d]", ids, rtIcon, rtGroupIcon, rtVersion)
	}
	iconDir := le.Uint32(d1[20:]) &^ 0x80000000
	groupDir := le.Uint32(d1[28:]) &^ 0x80000000

	// RT_ICON directory: names 4 and 5 ascending.
	if got := le.Uint16(d1[iconDir+14:]); got != 2 {
		t.Fatalf("RT_ICON entries = %d, want 2", got)
	}
	if n := le.Uint32(d1[iconDir+16:]); n != firstIconID {
		t.Fatalf("RT_ICON name[0] = %d, want %d", n, firstIconID)
	}
	if n := le.Uint32(d1[iconDir+24:]); n != firstIconID+1 {
		t.Fatalf("RT_ICON name[1] = %d, want %d", n, firstIconID+1)
	}

	// Walk one full path: RT_GROUP_ICON dir -> group id 3 -> lang -> data
	// entry -> payload must be the groupIconBlob bytes.
	if n := le.Uint32(d1[groupDir+16:]); n != groupIconID {
		t.Fatalf("group name = %d, want %d", n, groupIconID)
	}
	langDir := le.Uint32(d1[groupDir+20:]) &^ 0x80000000
	entry := le.Uint32(d1[langDir+20:])
	size := le.Uint32(d1[entry+4:])
	off := le.Uint32(d1[entry:]) // addend = offset within $02
	if !bytes.Equal(d2[off:off+size], groupIconBlob(imgs, firstIconID)) {
		t.Fatal("group payload bytes mismatch")
	}

	// Every resource gets exactly one relocation on its OffsetToData.
	h1 := obj[20:60]
	if got := le.Uint16(h1[32:]); got != uint16(len(res)) {
		t.Fatalf("$01 NumberOfRelocations = %d, want %d", got, len(res))
	}

	if again := buildSyso(machines["amd64"], res); !bytes.Equal(obj, again) {
		t.Fatal("buildSyso not deterministic")
	}
}

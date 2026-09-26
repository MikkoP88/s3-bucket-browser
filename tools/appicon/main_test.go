package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// syntheticPNG draws a size x size RGBA image with a transparent left half
// and an opaque right half so the ICO AND-mask logic has both cases.
func syntheticPNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x >= size/2 {
				img.Set(x, y, color.RGBA{0x46, 0x8f, 0xd2, 0xff})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBuildICOStructure(t *testing.T) {
	set := iconSet{16: syntheticPNG(t, 16), 24: syntheticPNG(t, 24), 64: syntheticPNG(t, 64)}
	ico := buildICO(set)

	le := binary.LittleEndian
	if got := le.Uint16(ico[2:]); got != 1 {
		t.Fatalf("type = %d, want 1 (icon)", got)
	}
	if got := le.Uint16(ico[4:]); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}

	var offsets, sizes []int
	for i := 0; i < 3; i++ {
		e := ico[6+16*i : 6+16*(i+1)]
		s := int(e[0])
		if s == 0 {
			s = 256
		}
		if e[1] != e[0] {
			t.Fatalf("entry %d: width %d != height %d", i, e[0], e[1])
		}
		sizes = append(sizes, s)
		offsets = append(offsets, int(le.Uint32(e[12:])))
	}
	if sizes[0] != 16 || sizes[1] != 24 || sizes[2] != 64 {
		t.Fatalf("sizes = %v, want [16 24 64]", sizes)
	}
	// offsets strictly ascending, first past the directory, payloads inside file
	prev := 6 + 16*3
	for i, o := range offsets {
		if o < prev {
			t.Fatalf("entry %d offset %d not past %d", i, o, prev)
		}
		end := o + int(le.Uint32(ico[6+16*i+8:6+16*i+12]))
		if end > len(ico) {
			t.Fatalf("entry %d payload exceeds file", i)
		}
		prev = end
	}
	if prev != len(ico) {
		t.Fatalf("payloads end at %d, file is %d", prev, len(ico))
	}

	// The 16px entry must be a DIB32: BITMAPINFOHEADER with doubled height.
	dib := ico[offsets[0] : offsets[0]+int(le.Uint32(ico[6+16*0+8:6+16*0+12]))]
	if got := le.Uint32(dib[0:]); got != 40 {
		t.Fatalf("biSize = %d, want 40", got)
	}
	if got := le.Uint32(dib[8:]); got != 32 {
		t.Fatalf("biHeight = %d, want 32 (16 XOR + 16 AND)", got)
	}
	if got := le.Uint16(dib[14:]); got != 32 {
		t.Fatalf("biBitCount = %d, want 32", got)
	}
	// Bottom-up BGRA: the top image row (transparent half) is the last XOR
	// row; its first byte must have alpha 0 and the AND mask must mark it.
	xor := dib[40 : 40+16*16*4]
	topRow := xor[(16-1)*16*4:] // last XOR row = top of the image
	if topRow[3] != 0 {
		t.Fatalf("top-left alpha = %d, want 0", topRow[3])
	}
	topRight := topRow[8*4:] // pixel x=8: first opaque one
	if topRight[0] != 0xd2 || topRight[1] != 0x8f || topRight[2] != 0x46 || topRight[3] != 0xff {
		t.Fatalf("top-right BGRA = %v, want d2 8f 46 ff", topRight[:4])
	}
	and := dib[40+16*16*4:]
	// first AND row = bottom of the image: byte 0 spans x0-7 (all
	// transparent -> set), byte 1 spans x8-15 (all opaque -> clear).
	if and[0] != 0xff || and[1] != 0x00 {
		t.Fatalf("AND mask first row = %08b %08b, want 11111111 00000000", and[0], and[1])
	}

	// The 64px entry is the PNG payload verbatim.
	if !bytes.Equal(ico[offsets[2]:offsets[2]+int(le.Uint32(ico[6+16*2+8:6+16*2+12]))], set[64]) {
		t.Fatal("64px payload is not the input PNG bytes")
	}
}

func TestBuildICNS(t *testing.T) {
	set := iconSet{32: syntheticPNG(t, 32), 256: syntheticPNG(t, 256)}
	icns := buildICNS(set)
	if !bytes.Equal(icns[0:4], []byte("icns")) {
		t.Fatalf("magic = %q", icns[0:4])
	}
	if got := binary.BigEndian.Uint32(icns[4:]); got != uint32(len(icns)) {
		t.Fatalf("total length = %d, file is %d", got, len(icns))
	}
	// Walk entries: ic04 (32) and ic08 (256) and ic11 (32, the @2x slot)
	// are present; ic07/ic09/ic10/ic12 skip — sizes missing from the set.
	type ent struct {
		ostype string
		data   []byte
	}
	var got []ent
	for off := 8; off < len(icns); {
		n := int(binary.BigEndian.Uint32(icns[off+4:]))
		if n < 8 || off+n > len(icns) {
			t.Fatalf("bad entry length %d at %d", n, off)
		}
		got = append(got, ent{string(icns[off : off+4]), icns[off+8 : off+n]})
		off += n
	}
	want := []string{"ic04", "ic08", "ic11"}
	if len(got) != len(want) {
		t.Fatalf("entries = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ostype != w {
			t.Fatalf("entry %d = %s, want %s", i, got[i].ostype, w)
		}
	}
	if !bytes.Equal(got[0].data, set[32]) || !bytes.Equal(got[1].data, set[256]) || !bytes.Equal(got[2].data, set[32]) {
		t.Fatal("entries do not carry the input PNG bytes verbatim")
	}
}

func TestDeterministic(t *testing.T) {
	set := iconSet{16: syntheticPNG(t, 16), 64: syntheticPNG(t, 64)}
	if !bytes.Equal(buildICO(set), buildICO(set)) {
		t.Fatal("buildICO not byte-identical across runs")
	}
	if !bytes.Equal(buildICNS(set), buildICNS(set)) {
		t.Fatal("buildICNS not byte-identical across runs")
	}
}

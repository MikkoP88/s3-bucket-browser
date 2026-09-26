// appicon assembles the platform icon containers from the rasterized brand
// set in build/iconset (produced by scripts/gen-icons.mjs):
//
//	build/icon.ico     - Windows icon: BMP (DIB32) images for 16-48px (best
//	                     legacy compatibility) and PNG images for 64-256px.
//	build/AppIcon.icns - macOS icon: PNG entries on the ic04/ic07-ic12 ladder.
//
// Both containers are built deterministically (fixed entry order, no
// timestamps) so the committed binaries only change when the artwork does.
//
// Usage:
//
//	go run ./tools/appicon -icons build/iconset -ico build/icon.ico -icns build/AppIcon.icns
//
// The tool needs no network or cgo; PNG encoding/decoding is stdlib.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// iconSet is every PNG the generator emits, keyed by pixel size.
type iconSet map[int][]byte

// icoSizes is the Windows set, ascending: BMP below 64px, PNG from 64 up.
var icoSizes = []int{16, 24, 32, 48, 64, 128, 256}

// icnsEntries maps the Apple ostype to its pixel size, in emit order.
var icnsEntries = [][2]string{
	{"ic04", "32"},   // small
	{"ic07", "128"},  //
	{"ic08", "256"},  //
	{"ic09", "512"},  //
	{"ic10", "1024"}, // 512@2x
	{"ic11", "32"},   // 16@2x
	{"ic12", "64"},   // 32@2x
}

func main() {
	icons := flag.String("icons", "build/iconset", "directory holding icon_<size>.png files")
	ico := flag.String("ico", "build/icon.ico", "output .ico path")
	icns := flag.String("icns", "build/AppIcon.icns", "output .icns path")
	flag.Parse()

	set, err := loadIconSet(*icons)
	if err != nil {
		die("%v", err)
	}
	if err := os.WriteFile(*ico, buildICO(set), 0o644); err != nil {
		die("write %s: %v", *ico, err)
	}
	if err := os.WriteFile(*icns, buildICNS(set), 0o644); err != nil {
		die("write %s: %v", *icns, err)
	}
	fmt.Printf("wrote %s, %s\n", *ico, *icns)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "appicon: "+format+"\n", args...)
	os.Exit(1)
}

// loadIconSet reads every icon_<size>.png in dir and verifies each decodes
// to a square image of the advertised size.
func loadIconSet(dir string) (iconSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	set := iconSet{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "icon_") || !strings.HasSuffix(name, ".png") {
			continue
		}
		size, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "icon_"), ".png"))
		if err != nil {
			return nil, fmt.Errorf("%s: filename is not icon_<size>.png", name)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		b := img.Bounds()
		if b.Dx() != size || b.Dy() != size {
			return nil, fmt.Errorf("%s: image is %dx%d, filename says %d", name, b.Dx(), b.Dy(), size)
		}
		set[size] = data
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no icon_<size>.png files in %s (run scripts/gen-icons.mjs first)", dir)
	}
	return set, nil
}

// ---------------- ICO ----------------

// buildICO writes a classic ICO: 6-byte header, one 16-byte directory entry
// per size (ascending), then the image payloads. Sizes < 64px are stored as
// uncompressed DIB32 (BITMAPINFOHEADER + bottom-up BGRA rows + AND mask),
// which every Windows shell surface renders; larger sizes as PNG, which
// Vista and later support inside icons.
func buildICO(set iconSet) []byte {
	var sizes []int
	for _, s := range icoSizes {
		if _, ok := set[s]; ok {
			sizes = append(sizes, s)
		}
	}
	var payloads [][]byte
	for _, s := range sizes {
		if s < 64 {
			payloads = append(payloads, dib32(set[s]))
		} else {
			payloads = append(payloads, set[s])
		}
	}

	var buf bytes.Buffer
	var le = binary.LittleEndian
	var hdr [6]byte
	le.PutUint16(hdr[2:], 1) // type: icon
	le.PutUint16(hdr[4:], uint16(len(payloads)))
	buf.Write(hdr[:])

	offset := 6 + 16*len(payloads)
	for i, s := range sizes {
		var e [16]byte
		e[0], e[1] = byte(s), byte(s)
		if s >= 256 {
			e[0], e[1] = 0, 0 // 256 is encoded as 0
		}
		e[4] = 1  // planes
		e[5] = 32 // bit count (informational for PNG entries)
		le.PutUint32(e[8:], uint32(len(payloads[i])))
		le.PutUint32(e[12:], uint32(offset))
		buf.Write(e[:])
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		buf.Write(p)
	}
	return buf.Bytes()
}

// dib32 converts a PNG to the DIB32 icon payload: BITMAPINFOHEADER with
// doubled height (XOR mask above, AND mask below), bottom-up BGRA rows,
// then a 1bpp AND mask that marks alpha-0 pixels transparent.
func dib32(pngData []byte) []byte {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		panic("appicon: embedded png does not decode: " + err.Error())
	}
	s := img.Bounds().Dx()
	rowBytes := (s + 7) / 8          // AND-mask row, 1bpp
	andRow := (rowBytes + 3) / 4 * 4 // padded to DWORD
	xor := s * s * 4                 // BGRA payload
	and := andRow * s

	var buf bytes.Buffer
	var le = binary.LittleEndian
	var ih [40]byte
	le.PutUint32(ih[0:], 40)
	le.PutUint32(ih[4:], uint32(s))
	le.PutUint32(ih[8:], uint32(2*s)) // XOR + AND
	le.PutUint16(ih[12:], 1)
	le.PutUint16(ih[14:], 32)
	le.PutUint32(ih[20:], uint32(xor+and))
	buf.Write(ih[:])

	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		for y := 0; y < s; y++ {
			for x := 0; x < s; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
	}
	for y := s - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < s; x++ {
			i := y*rgba.Stride + x*4
			buf.Write([]byte{rgba.Pix[i+2], rgba.Pix[i+1], rgba.Pix[i], rgba.Pix[i+3]}) // BGRA
		}
	}
	maskRow := make([]byte, andRow)
	for y := s - 1; y >= 0; y-- {
		for j := range maskRow {
			maskRow[j] = 0
		}
		for x := 0; x < s; x++ {
			i := y*rgba.Stride + x*4
			if rgba.Pix[i+3] == 0 {
				maskRow[x/8] |= 0x80 >> (x % 8) // 1 = transparent
			}
		}
		buf.Write(maskRow)
	}
	return buf.Bytes()
}

// ---------------- ICNS ----------------

// buildICNS writes the Apple container: 'icns' magic, total length, then
// per-entry ostype + length + PNG payload. Entries whose size is missing
// from the set are skipped, so an incomplete set still yields a valid file.
func buildICNS(set iconSet) []byte {
	var body bytes.Buffer
	for _, e := range icnsEntries {
		size, _ := strconv.Atoi(e[1])
		data, ok := set[size]
		if !ok {
			continue
		}
		var head [8]byte
		copy(head[0:4], e[0])
		binary.BigEndian.PutUint32(head[4:], uint32(len(data)+8))
		body.Write(head[:])
		body.Write(data)
	}
	out := make([]byte, 8+body.Len())
	copy(out[0:4], "icns")
	binary.BigEndian.PutUint32(out[4:], uint32(len(out)))
	copy(out[8:], body.Bytes())
	return out
}

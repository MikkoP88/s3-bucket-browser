// versioninfo generates a Windows COFF resource object (.syso) carrying the
// RT_VERSION (VERSIONINFO) resource for the s3b executable: CompanyName,
// ProductName, FileVersion, LegalCopyright and friends. With -icon it also
// embeds every image of an .ico file as RT_ICON resources plus an
// RT_GROUP_ICON directory at resource id 3 — the id the Wails v3 window
// shell loads the window icon from (NewIconFromResource(module, 3)), so the
// taskbar, Alt-Tab and the window corner all show the brand mark.
//
// Plain `go build` picks up any *.syso in the package directory, so CI and
// the release pipeline run this tool into cmd/s3b/ right before the Windows
// builds and delete the file afterwards (the machine type must match the
// target arch; a stale cross-arch .syso breaks the other build). Dev builds
// without the file simply lack the resource — no code depends on it.
//
// Usage:
//
//	go run ./tools/versioninfo -version 1.2.3 -arch amd64 -icon build/icon.ico -o cmd/s3b/versioninfo_windows_amd64.syso
//
// The output is deterministic: same inputs, same bytes (TimeDateStamp 0).
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Publisher metadata (single source of truth for Windows properties, the
// NSIS publisher fields and the About dialog keep their own copies in sync).
const (
	companyName       = "MikkoP88"
	productName       = "S3 Bucket Browser"
	fileDescription   = "S3 Bucket Browser - GUI + CLI for S3-compatible storage"
	internalName      = "s3b"
	legalCopyright    = "Copyright (c) MikkoP88 - PolyForm Internal Use License 1.0.0"
	originalFilename  = "s3b.exe"
	stringTableLangID = 0x0409 // en-US
	charSetID         = 0x04B0 // 1200 = Unicode
)

// machine is a Windows target: the COFF machine ID plus the COFF reloc
// type that denotes a 32-bit absolute address (differs per arch).
type machineInfo struct {
	machine uint16 // IMAGE_FILE_MACHINE_*
	reloc   uint16 // IMAGE_REL_*_ADDR32 / _DIR32
}

var machines = map[string]machineInfo{
	"amd64": {0x8664, 0x0002}, // IMAGE_FILE_MACHINE_AMD64, IMAGE_REL_AMD64_ADDR32
	"arm64": {0xAA64, 0x0001}, // IMAGE_FILE_MACHINE_ARM64, IMAGE_REL_ARM64_ADDR32
	"386":   {0x014c, 0x0006}, // IMAGE_FILE_MACHINE_I386, IMAGE_REL_I386_DIR32
}

func main() {
	version := flag.String("version", "0.0.0-dev", "version string: major.minor.patch[.build]")
	arch := flag.String("arch", runtime.GOARCH, "target architecture: amd64, arm64, 386")
	icon := flag.String("icon", "", "optional .ico whose images are embedded as RT_ICON resources plus an RT_GROUP_ICON at resource id 3")
	out := flag.String("o", "", "output path (default cmd/s3b/versioninfo_windows_<arch>.syso)")
	flag.Parse()

	mi, ok := machines[*arch]
	if !ok {
		die("unsupported arch %q (want one of amd64, arm64, 386)", *arch)
	}
	path := *out
	if path == "" {
		path = filepath.Join("cmd", "s3b", fmt.Sprintf("versioninfo_windows_%s.syso", *arch))
	}
	major, minor, patch, build := parseVersion(*version)
	resources := []resource{{rtype: rtVersion, name: 1, lang: stringTableLangID,
		data: versionInfoBlob(major, minor, patch, build, *version)}}
	icons := 0
	if *icon != "" {
		icoBytes, err := os.ReadFile(*icon)
		if err != nil {
			die("read %s: %v", *icon, err)
		}
		imgs, err := parseICO(icoBytes)
		if err != nil {
			die("%s: %v", *icon, err)
		}
		for i, m := range imgs {
			resources = append(resources, resource{rtype: rtIcon, name: firstIconID + uint16(i),
				lang: stringTableLangID, data: m.data})
		}
		resources = append(resources, resource{rtype: rtGroupIcon, name: groupIconID,
			lang: stringTableLangID, data: groupIconBlob(imgs, firstIconID)})
		icons = len(imgs)
	}
	if err := os.WriteFile(path, buildSyso(mi, resources), 0o644); err != nil {
		die("write %s: %v", path, err)
	}
	if icons > 0 {
		fmt.Printf("wrote %s (version %s, arch %s, %d icon images)\n", path, *version, *arch, icons)
	} else {
		fmt.Printf("wrote %s (version %s, arch %s)\n", path, *version, *arch)
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "versioninfo: "+format+"\n", args...)
	os.Exit(1)
}

// ---------------- version parsing ----------------

// parseVersion extracts up to four numeric components; anything unparsable
// contributes 0. "1.2.3" -> (1,2,3,0), "1.2.3.4" -> (1,2,3,4), "dev" -> 0s.
func parseVersion(v string) (major, minor, patch, build uint32) {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 4)
	nums := make([]uint32, 4)
	for i, p := range parts {
		// tolerate trailing junk like "1.2.3-rc1"
		if j := strings.IndexAny(p, "-+"); j >= 0 {
			p = p[:j]
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			break
		}
		nums[i] = uint32(n)
	}
	return nums[0], nums[1], nums[2], nums[3]
}

// ---------------- VERSIONINFO blob ----------------

// block is one VERSIONINFO node: header (length, valueLength, type), a
// UTF-16 key, 32-bit alignment padding and then children or a value.
type block struct {
	valueLength uint16 // Value size in bytes, 0 for containers (goversioninfo-compatible)
	typ         uint16 // 0 = binary, 1 = text
	key         string
	value       []byte  // for leaf blocks (fixed info, translation DWORD)
	children    []block // for container blocks
}

// resource is one leaf of the .rsrc tree: a (type, name, language) triple
// pointing at a payload stored in $02.
type resource struct {
	rtype uint16 // RT_* id
	name  uint16 // resource id within the type
	lang  uint16 // language id
	data  []byte // payload, copied verbatim into $02
}

const (
	rtIcon      = 3  // RT_ICON (single image)
	rtGroupIcon = 14 // RT_GROUP_ICON (directory over RT_ICON entries)
	rtVersion   = 16 // RT_VERSION
	groupIconID = 3  // Wails v3 loads the window icon from group resource id 3
	firstIconID = 4  // member icon ids: version keeps 1, the group keeps 3
)

func buildSyso(mi machineInfo, resources []resource) []byte {
	// Directory entries must be sorted by id within every level.
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].rtype != resources[j].rtype {
			return resources[i].rtype < resources[j].rtype
		}
		if resources[i].name != resources[j].name {
			return resources[i].name < resources[j].name
		}
		return resources[i].lang < resources[j].lang
	})

	// $02 holds every payload concatenated in sorted order; the offset of
	// each payload becomes the addend for its data entry. cmd/link's
	// addpersrc rewrites every relocated dword to RVA(.rsrc) + addend +
	// len($01), i.e. exactly where each payload lands in the merged section.
	var data bytes.Buffer
	offsets := make([]uint32, len(resources))
	for i, r := range resources {
		offsets[i] = uint32(data.Len())
		data.Write(r.data)
	}

	// $01 layout: root directory, one directory per type (ascending), one
	// language directory per resource, then the data entries. All entries
	// are id-based, so NumberOfNamedEntries is 0 everywhere.
	var types []uint16
	counts := map[uint16]int{}
	for i, r := range resources {
		if i == 0 || r.rtype != resources[i-1].rtype {
			types = append(types, r.rtype)
		}
		counts[r.rtype]++
	}
	cur := uint32(16 + 8*len(types))
	typeDirOff := make([]uint32, len(types))
	for j, t := range types {
		typeDirOff[j] = cur
		cur += uint32(16 + 8*counts[t])
	}
	langDirOff := make([]uint32, len(resources))
	for i := range resources {
		langDirOff[i] = cur
		cur += 16 + 8
	}
	entryOff := make([]uint32, len(resources))
	for i := range resources {
		entryOff[i] = cur
		cur += 16
	}

	d1 := make([]byte, cur)
	dir := func(off uint32, n int) {
		le.PutUint16(d1[off+12:], 0) // NumberOfNamedEntries
		le.PutUint16(d1[off+14:], uint16(n))
	}
	// root: one entry per type, ascending type id
	dir(0, len(types))
	for j, t := range types {
		le.PutUint32(d1[16+8*j:], uint32(t))
		le.PutUint32(d1[16+8*j+4:], 0x80000000|typeDirOff[j])
	}
	// type directories: one entry per resource name, ascending within type
	ti, slot := 0, 0
	for i, r := range resources {
		if i > 0 && r.rtype != resources[i-1].rtype {
			ti++
			slot = 0
			dir(typeDirOff[ti], counts[r.rtype])
		} else if i == 0 {
			dir(typeDirOff[0], counts[r.rtype])
		}
		e := typeDirOff[ti] + uint32(16+8*slot)
		le.PutUint32(d1[e:], uint32(r.name))
		le.PutUint32(d1[e+4:], 0x80000000|langDirOff[i])
		slot++
	}
	// language directories and data entries
	for i, r := range resources {
		dir(langDirOff[i], 1)
		le.PutUint32(d1[langDirOff[i]+16:], uint32(r.lang))
		le.PutUint32(d1[langDirOff[i]+20:], entryOff[i]) // no dir flag
		le.PutUint32(d1[entryOff[i]:], offsets[i])       // addend; linker patches
		le.PutUint32(d1[entryOff[i]+4:], uint32(len(r.data)))
		// code page and reserved stay 0
	}

	relocs := make([]coffReloc, len(resources))
	for i := range resources {
		relocs[i] = coffReloc{off: entryOff[i], symIdx: 1, typ: mi.reloc}
	}
	return coff(mi, [2]section{
		{name: ".rsrc$01", data: d1, relocs: relocs},
		{name: ".rsrc$02", data: data.Bytes()},
	})
}

// versionInfoBlob builds the VS_VERSIONINFO tree with the standard string
// table (040904B0) and the Translation var.
func versionInfoBlob(major, minor, patch, build uint32, versionStr string) []byte {
	strs := [][2]string{
		{"CompanyName", companyName},
		{"FileDescription", fileDescription},
		{"FileVersion", versionStr},
		{"InternalName", internalName},
		{"LegalCopyright", legalCopyright},
		{"OriginalFilename", originalFilename},
		{"ProductName", productName},
		{"ProductVersion", versionStr},
	}

	stringTable := block{typ: 1, key: fmt.Sprintf("%04X%04X", stringTableLangID, charSetID)}
	for _, kv := range strs {
		v := utf16.Encode([]rune(kv[1] + "\x00")) // value + terminating NUL
		b := make([]byte, len(v)*2)
		for i, u := range v {
			le.PutUint16(b[i*2:], u)
		}
		stringTable.children = append(stringTable.children, block{
			typ: 1, key: kv[0], valueLength: uint16(len(b)), value: b,
		})
	}

	root := block{
		valueLength: 52, // VS_FIXEDFILEINFO
		typ:         0,
		key:         "VS_VERSION_INFO",
		value:       fixedFileInfo(major, minor, patch, build),
		children: []block{
			{typ: 1, key: "StringFileInfo", children: []block{stringTable}},
			{typ: 1, key: "VarFileInfo", children: []block{
				{typ: 0, key: "Translation", valueLength: 4,
					value: []byte{0x09, 0x04, 0xB0, 0x04}}, // 0x0409, 0x04B0 LE
			}},
		},
	}
	return serialize(root)
}

// fixedFileInfo renders the 52-byte VS_FIXEDFILEINFO structure.
func fixedFileInfo(major, minor, patch, build uint32) []byte {
	b := make([]byte, 52)
	le.PutUint32(b[0:], 0xFEEF04BD)       // dwSignature
	le.PutUint32(b[4:], 0x00010000)       // dwStrucVersion 1.0
	le.PutUint32(b[8:], major<<16|minor)  // dwFileVersionMS
	le.PutUint32(b[12:], patch<<16|build) // dwFileVersionLS
	le.PutUint32(b[16:], major<<16|minor) // dwProductVersionMS
	le.PutUint32(b[20:], patch<<16|build) // dwProductVersionLS
	le.PutUint32(b[24:], 0x3F)            // dwFileFlagsMask
	le.PutUint32(b[28:], 0)               // dwFileFlags
	le.PutUint32(b[32:], 0x00040004)      // dwFileOS = VOS_NT_WINDOWS32
	le.PutUint32(b[36:], 0x00000001)      // dwFileType = VFT_APP
	le.PutUint32(b[40:], 0)               // dwFileSubtype
	le.PutUint32(b[44:], 0)               // dwFileDateMS
	le.PutUint32(b[48:], 0)               // dwFileDateLS
	return b
}

// serialize renders a block tree: header + key + pad + (value | children),
// back-patching wLength. Children must be 32-bit aligned.
func serialize(b block) []byte {
	var buf bytes.Buffer
	hdr := make([]byte, 6)
	buf.Write(hdr) // patched later
	key := utf16Bytes(b.key + "\x00")
	buf.Write(key)
	align4(&buf)

	if b.value != nil || b.children == nil {
		buf.Write(b.value)
	}
	if len(b.children) > 0 {
		for _, c := range b.children {
			align4(&buf)
			buf.Write(serialize(c))
		}
	}
	out := buf.Bytes()
	le.PutUint16(out[0:], uint16(len(out))) // wLength
	le.PutUint16(out[2:], b.valueLength)    // wValueLength
	le.PutUint16(out[4:], b.typ)            // wType
	return out
}

func utf16Bytes(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		le.PutUint16(b[i*2:], c)
	}
	return b
}

func align4(buf *bytes.Buffer) {
	for buf.Len()%4 != 0 {
		buf.WriteByte(0)
	}
}

var le = binary.LittleEndian

// ---------------- COFF object ----------------

type section struct {
	name   string
	data   []byte
	relocs []coffReloc // fixups inside data; symIdx is 0-based into the symbol table
}

// coffReloc is one COFF relocation record (10 bytes on disk: the loader
// reads VirtualAddress+SymbolTableIndex+Type back-to-back, no padding):
// patch the 32-bit word at off in the section's data against symbol symIdx.
type coffReloc struct {
	off    uint32
	symIdx uint32
	typ    uint16
}

// coff assembles a minimal COFF object: header + two section headers +
// .rsrc$01 (resource directory) + .rsrc$02 (resource data) + $01's
// relocation table + a two-entry symbol table holding the section symbols.
//
// The relocation is what makes the whole thing work: cmd/link recognizes
// .rsrc sections, concatenates them in order, and rewrites every relocated
// dword to RVA(.rsrc) + addend + len($01) (its "split resources" rule),
// turning the data entry's placeholder OffsetToData into the final RVA of
// $02. The symbol table exists so the relocation can target a symbol —
// the $02 section symbol — which the loader (cmd/link/internal/loadpe)
// maps back to the section itself.
func coff(mi machineInfo, secs [2]section) []byte {
	var buf bytes.Buffer
	// COFF file header (20 bytes); symbol table fields patched below.
	hdr := make([]byte, 20)
	le.PutUint16(hdr[0:], mi.machine)
	le.PutUint16(hdr[2:], uint16(len(secs)))
	le.PutUint32(hdr[4:], 0) // TimeDateStamp (deterministic output)
	// hdr[8:] PointerToSymbolTable and hdr[12:] NumberOfSymbols: set once
	// the layout is known.
	le.PutUint16(hdr[16:], 0) // SizeOfOptionalHeader
	le.PutUint16(hdr[18:], 0) // Characteristics
	buf.Write(hdr)

	// Layout: section data (in order), then each section's relocation
	// table, then the symbol table.
	dataStart := 20 + 40*len(secs)
	relocStart := dataStart + offsetOf(secs[:])
	symStart := relocStart + offsetOfRelocs(secs[:])

	for i, s := range secs {
		sh := make([]byte, 40)
		copy(sh[:8], s.name)                       // ".rsrc$01" is exactly 8 bytes, no NUL needed
		le.PutUint32(sh[8:], uint32(len(s.data)))  // VirtualSize (physical in objects)
		le.PutUint32(sh[12:], 0)                   // VirtualAddress
		le.PutUint32(sh[16:], uint32(len(s.data))) // SizeOfRawData
		le.PutUint32(sh[20:], uint32(dataStart+offsetOf(secs[:i])))
		if len(s.relocs) > 0 {
			le.PutUint32(sh[24:], uint32(relocStart+offsetOfRelocs(secs[:i]))) // PointerToRelocations
			le.PutUint16(sh[32:], uint16(len(s.relocs)))                       // NumberOfRelocations
		}
		le.PutUint32(sh[36:], 0x40000040) // CNT_INITIALIZED_DATA | MEM_READ
		buf.Write(sh)
	}
	for _, s := range secs {
		buf.Write(s.data)
	}
	for _, s := range secs {
		for _, r := range s.relocs {
			e := make([]byte, 10)
			le.PutUint32(e[0:], r.off)
			le.PutUint32(e[4:], r.symIdx)
			le.PutUint16(e[8:], r.typ)
			buf.Write(e)
		}
	}

	// Symbol table: the two section symbols (STATIC, value 0). Both names
	// are exactly 8 bytes, so no string table is needed. The header fields
	// are patched in place — buf holds the header at offset 0.
	out := buf.Bytes()
	le.PutUint32(out[8:], uint32(symStart))
	le.PutUint32(out[12:], uint32(len(secs)))
	for i, s := range secs {
		e := make([]byte, 18)
		copy(e[:8], s.name)
		le.PutUint16(e[12:], uint16(i+1)) // SectionNumber (1-based)
		// Value 0, Type 0 (not a function), no aux symbols.
		e[16] = 3 // StorageClass IMAGE_SYM_CLASS_STATIC
		buf.Write(e)
	}
	// The format mandates a string table right after the symbols; an empty
	// one is just its own length (4) as a DWORD.
	st := make([]byte, 4)
	le.PutUint32(st, 4)
	buf.Write(st)
	return buf.Bytes()
}

func offsetOf(secs []section) int {
	n := 0
	for _, s := range secs {
		n += len(s.data)
	}
	return n
}

func offsetOfRelocs(secs []section) int {
	n := 0
	for _, s := range secs {
		n += 10 * len(s.relocs)
	}
	return n
}

// ---------------- .ico parsing ----------------

// icoImage is one image of an .ico file: the directory-entry fields the
// RT_GROUP_ICON needs, plus the raw payload (DIB or PNG) for RT_ICON.
type icoImage struct {
	width, height byte // pixels; 0 encodes 256
	planes, bits  uint16
	data          []byte
}

// parseICO validates and splits an .ico into its images, preserving file
// order (which build/icon.ico keeps ascending by size).
func parseICO(b []byte) ([]icoImage, error) {
	if len(b) < 6 {
		return nil, fmt.Errorf("ico: %d bytes, shorter than the 6-byte header", len(b))
	}
	if le.Uint16(b[0:]) != 0 {
		return nil, fmt.Errorf("ico: reserved field = %d, want 0", le.Uint16(b[0:]))
	}
	if le.Uint16(b[2:]) != 1 {
		return nil, fmt.Errorf("ico: type = %d, want 1 (icon)", le.Uint16(b[2:]))
	}
	n := int(le.Uint16(b[4:]))
	if n == 0 {
		return nil, fmt.Errorf("ico: contains no images")
	}
	imgs := make([]icoImage, 0, n)
	for i := 0; i < n; i++ {
		e := 6 + 16*i
		if e+16 > len(b) {
			return nil, fmt.Errorf("ico: entry %d directory out of bounds", i)
		}
		size := le.Uint32(b[e+8:])
		off := le.Uint32(b[e+12:])
		if uint64(off)+uint64(size) > uint64(len(b)) {
			return nil, fmt.Errorf("ico: entry %d payload [%d:%d] out of bounds (len %d)", i, off, off+size, len(b))
		}
		imgs = append(imgs, icoImage{
			width:  b[e],
			height: b[e+1],
			planes: le.Uint16(b[e+4:]),
			bits:   le.Uint16(b[e+6:]),
			data:   b[off : off+size],
		})
	}
	return imgs, nil
}

// groupIconBlob builds the RT_GROUP_ICON payload: the .ico directory with
// every image offset replaced by the RT_ICON resource id that now carries
// the image. LoadImage walks this to pick the best size.
func groupIconBlob(imgs []icoImage, firstID uint16) []byte {
	var buf bytes.Buffer
	var hdr [6]byte
	le.PutUint16(hdr[2:], 1) // type: icon
	le.PutUint16(hdr[4:], uint16(len(imgs)))
	buf.Write(hdr[:])
	for i, m := range imgs {
		var e [14]byte
		e[0], e[1] = m.width, m.height
		le.PutUint16(e[4:], m.planes)
		le.PutUint16(e[6:], m.bits)
		le.PutUint32(e[8:], uint32(len(m.data)))
		le.PutUint16(e[12:], firstID+uint16(i))
		buf.Write(e[:])
	}
	return buf.Bytes()
}

// versioninfo generates a Windows COFF resource object (.syso) carrying the
// RT_VERSION (VERSIONINFO) resource for the s3b executable: CompanyName,
// ProductName, FileVersion, LegalCopyright and friends.
//
// Plain `go build` picks up any *.syso in the package directory, so CI and
// the release pipeline run this tool into cmd/s3b/ right before the Windows
// builds and delete the file afterwards (the machine type must match the
// target arch; a stale cross-arch .syso breaks the other build). Dev builds
// without the file simply lack the resource — no code depends on it.
//
// Usage:
//
//	go run ./tools/versioninfo -version 1.2.3 -arch amd64 -o cmd/s3b/versioninfo_windows_amd64.syso
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
	blob := versionInfoBlob(major, minor, patch, build, *version)
	if err := os.WriteFile(path, buildSyso(mi, blob), 0o644); err != nil {
		die("write %s: %v", path, err)
	}
	fmt.Printf("wrote %s (version %s, arch %s)\n", path, *version, *arch)
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

func buildSyso(mi machineInfo, blob []byte) []byte {
	// Resource directory ($01): root -> RT_VERSION -> resource id 1 -> lang.
	// The data entry's OffsetToData is a placeholder 0 with a relocation on
	// it: cmd/link's addpersrc rewrites it to RVA(.rsrc) + addend + len($01)
	// — i.e. exactly where $02 lands in the merged section.
	const (
		dir2Off  = 0x18
		dir3Off  = 0x30
		entryOff = 0x48
	)
	d1 := make([]byte, 0x58)
	le := binary.LittleEndian
	// root directory: 1 id entry
	le.PutUint16(d1[12:], 0)                  // NumberOfNamedEntries
	le.PutUint16(d1[14:], 1)                  // NumberOfIdEntries
	le.PutUint32(d1[16:], 16)                 // RT_VERSION
	le.PutUint32(d1[20:], 0x80000000|dir2Off) // -> dir2
	// dir2: 1 id entry
	le.PutUint16(d1[dir2Off+12:], 0)
	le.PutUint16(d1[dir2Off+14:], 1)
	le.PutUint32(d1[dir2Off+16:], 1) // resource id 1
	le.PutUint32(d1[dir2Off+20:], 0x80000000|dir3Off)
	// dir3: 1 id entry (language)
	le.PutUint16(d1[dir3Off+12:], 0)
	le.PutUint16(d1[dir3Off+14:], 1)
	le.PutUint32(d1[dir3Off+16:], stringTableLangID)
	le.PutUint32(d1[dir3Off+20:], entryOff) // data entry (no dir flag)
	// data entry; OffsetToData is patched by the linker via the relocation
	le.PutUint32(d1[entryOff:], 0)
	le.PutUint32(d1[entryOff+4:], uint32(len(blob)))
	le.PutUint32(d1[entryOff+8:], 0)  // code page
	le.PutUint32(d1[entryOff+12:], 0) // reserved

	return coff(mi, [2]section{
		{name: ".rsrc$01", data: d1,
			relocs: []coffReloc{{off: entryOff, symIdx: 1, typ: mi.reloc}}},
		{name: ".rsrc$02", data: blob},
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

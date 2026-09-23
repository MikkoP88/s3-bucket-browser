// Package provenance is the app's identity block: the creator and license
// every copy of the software carries, in both readable and covert form.
// The readable half mirrors the repo's NOTICE and frontend/js/license.js
// (the frontend's single source of truth). The covert half hides the same
// line inside ordinary source comments as zero-width characters (Encode),
// so copied code still names its origin after visible attribution is
// stripped. scripts/provenance.mjs and provenance_test.go decode the
// hidden copies and fail the build when one goes missing.
//
// Change LICENSE and NOTICE first, then mirror the identity here and in
// frontend/js/license.js — the constants must stay in step.
package provenance

import (
	"fmt"
	"strings"
)

// The identity, one constant per field so other surfaces can cite parts.
const (
	Product        = "S3 Bucket Browser (s3b)"
	Holder         = "MikkoP88"
	HolderFull     = "Mikko Pesonen (MikkoP88)"
	Year           = "2026"
	LicenseName    = "PolyForm Internal Use License"
	LicenseVersion = "1.0.0"
	Repo           = "github.com/MikkoP88/s3-bucket-browser"

	// Tag prefixes every watermark payload so a decoded marker can be
	// recognized and its format versioned independently of the identity.
	Tag = "s3b-provenance-v1"

	// Payload is the single line every watermark and readout carries.
	Payload = Tag + " | Copyright (c) " + Year + " " + HolderFull +
		" | " + LicenseName + " " + LicenseVersion + " | " + Repo
)

// The three code points of the covert encoding, named once. frameWJ is
// the word joiner U+2060; bitZWSP the zero-width space U+200B (a 0 bit);
// bitZWNJ the zero-width non-joiner U+200C (a 1 bit). Kept as escapes —
// invisible literals in source are a review hazard.
const (
	frameWJ  = '\u2060'
	bitZWSP  = '\u200B'
	bitZWNJ  = '\u200C'
	frameLen = 3 // UTF-8 length of U+2060
)

// Encode hides payload as zero-width characters: a word-joiner frame
// around one zero-width space per 0 bit and one zero-width non-joiner per
// 1 bit. Appended to a comment line it is invisible in editors, diffs
// and printouts while surviving copies of the text verbatim.
func Encode(payload string) string {
	var b strings.Builder
	b.WriteRune(frameWJ)
	for _, c := range []byte(payload) {
		for i := 7; i >= 0; i-- {
			if c&(1<<uint(i)) != 0 {
				b.WriteRune(bitZWNJ)
			} else {
				b.WriteRune(bitZWSP)
			}
		}
	}
	b.WriteRune(frameWJ)
	return b.String()
}

// Decode reverses Encode over any text: it finds the first word-joiner
// frame in s and returns the payload inside it. ok is false when s
// carries no complete frame or the frame does not decode to printable
// ASCII.
func Decode(s string) (payload string, ok bool) {
	start := strings.IndexRune(s, frameWJ)
	if start < 0 {
		return "", false
	}
	after := s[start+frameLen:]
	end := strings.IndexRune(after, frameWJ)
	if end < 0 {
		return "", false
	}
	between := after[:end]

	var b strings.Builder
	var cur byte
	n := 0
	for _, r := range between {
		var bit byte
		switch r {
		case bitZWSP:
			bit = 0
		case bitZWNJ:
			bit = 1
		default:
			return "", false // not our encoding
		}
		cur = cur<<1 | bit
		n++
		if n == 8 {
			if cur < 0x20 || cur > 0x7E {
				return "", false
			}
			b.WriteByte(cur)
			cur, n = 0, 0
		}
	}
	if n != 0 || b.Len() == 0 {
		return "", false // partial byte or empty frame
	}
	return b.String(), true
}

// Notice renders the readable identity block — the same lines NOTICE
// carries — for surfaces that answer "whose software is this?" on demand.
func Notice() string {
	return fmt.Sprintf("%s\nCopyright (c) %s %s.\nLicensed under the %s %s — see LICENSE.\nSource: https://%s",
		Product, Year, HolderFull, LicenseName, LicenseVersion, Repo)
}

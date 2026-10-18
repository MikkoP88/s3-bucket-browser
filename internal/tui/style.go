// Package tui is the interactive terminal face of s3b: a hand-rolled TUI
// toolkit with zero dependencies beyond the module's existing set (x/term,
// x/sys, fatih/color). The design contract:
//
//   - every build that carries the CLI carries the TUI (no wails imports);
//   - rendering is a cell-grid diff flushed as ANSI escapes (alt screen,
//     hidden caret), input is a uniform ANSI/VT parser — SGR-1006 mouse
//     included — so the whole face is drivable over plain pipes;
//   - the screen renders into a buffer, never straight to the terminal,
//     which makes every widget deterministically testable in-process.
package tui

import (
	"strconv"
	"strings"
)

// Attr is a text-attribute bit. Styles compose; the SGR emitter walks the
// diff between the previous and next style so a color change never leaks
// across cells.
type Attr uint16

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrUnderline
	AttrBlink
	AttrReverse
	AttrStrike
)

// ColorKind selects how a Color is carried on the wire.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota // terminal default (never emit an SGR for it)
	Color256                      // 8-bit palette index
	ColorRGB                      // 24-bit truecolor
)

// Color is one palette entry. The zero value is the terminal default.
type Color struct {
	Kind ColorKind
	V    uint8 // palette index when Kind == Color256
	R    uint8
	G    uint8
	B    uint8
}

// C256 builds a palette-index color.
func C256(v uint8) Color { return Color{Kind: Color256, V: v} }

// RGB builds a truecolor entry.
func RGB(r, g, b uint8) Color { return Color{Kind: ColorRGB, R: r, G: g, B: b} }

// Style is the full per-cell presentation: colors and attributes. The zero
// value means "terminal default, no attributes".
type Style struct {
	Fg    Color
	Bg    Color
	Attrs Attr
}

// Bold/dim/... return copies with the attribute set (builder style keeps
// widget draw code terse and allocation-light).
func (s Style) Bold() Style       { s.Attrs |= AttrBold; return s }
func (s Style) Dim() Style        { s.Attrs |= AttrDim; return s }
func (s Style) Italic() Style     { s.Attrs |= AttrItalic; return s }
func (s Style) Under() Style      { s.Attrs |= AttrUnderline; return s }
func (s Style) Reverse() Style    { s.Attrs |= AttrReverse; return s }
func (s Style) Strike() Style     { s.Attrs |= AttrStrike; return s }
func (s Style) FgC(c Color) Style { s.Fg = c; return s }
func (s Style) BgC(c Color) Style { s.Bg = c; return s }

// colorDepth is the negotiated color capability, set once at startup.
type colorDepth uint8

const (
	depthMono colorDepth = iota // NO_COLOR or a dumb terminal: attributes only
	depth16                     // 3-bit ANSI
	depth256                    // 8-bit palette
	depthTrue                   // 24-bit
)

// ansi16 maps an RGB color to the nearest classic ANSI entry (fg 30-37 /
// 90-97 and bg 40-47 / 100-107 are chosen by the emitter; this returns the
// 0-15 index). Nearest by simple Euclidean distance over the 16 anchors —
// good enough for a graceful degradation nobody is meant to see.
func ansi16(c Color) uint8 {
	switch c.Kind {
	case ColorDefault:
		return 0
	case Color256:
		return ansi16(palette256[c.V])
	default:
		return nearest16(c.R, c.G, c.B)
	}
}

// palette256 holds the 256-color palette's RGB anchors (indices 16-255;
// the first 16 are terminal-defined, approximated here by xterm's).
var palette256 = buildPalette256()

func buildPalette256() [256]Color {
	var p [256]Color
	// 0-15: the xterm anchors.
	basic := [16][3]uint8{
		{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
		{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
		{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
		{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	}
	for i, v := range basic {
		p[i] = RGB(v[0], v[1], v[2])
	}
	// 16-231: the 6x6x6 cube at levels {0,95,135,175,215,255}.
	levels := [6]uint8{0, 95, 135, 175, 215, 255}
	i := 16
	for r := range 6 {
		for g := range 6 {
			for b := range 6 {
				p[i] = RGB(levels[r], levels[g], levels[b])
				i++
			}
		}
	}
	// 232-255: the 24-step grayscale ramp 8..238.
	for v := range 24 {
		g := uint8(8 + v*10)
		p[i] = RGB(g, g, g)
		i++
	}
	return p
}

// ansi256 maps a color to the nearest 256-palette index.
func ansi256(c Color) uint8 {
	switch c.Kind {
	case ColorDefault:
		return 0
	case Color256:
		return c.V
	default:
		best, bestD := 0, 1<<30
		for i := range 256 {
			p := palette256[i]
			dr, dg, db := int(c.R)-int(p.R), int(c.G)-int(p.G), int(c.B)-int(p.B)
			if d := dr*dr + dg*dg + db*db; d < bestD {
				best, bestD = i, d
			}
		}
		return uint8(best)
	}
}

var basic16 = [16][3]uint8{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

func nearest16(r, g, b uint8) uint8 {
	best, bestD := 0, 1<<30
	for i, v := range basic16 {
		dr, dg, db := int(r)-int(v[0]), int(g)-int(v[1]), int(b)-int(v[2])
		if d := dr*dr + dg*dg + db*db; d < bestD {
			best, bestD = i, d
		}
	}
	return uint8(best)
}

// sgrBuffer accumulates the escape sequence that moves style `from` to
// style `to`. Both directions are handled: colors set to default are
// cleared, attributes dropped are turned off. The zero-from case emits a
// full reset first (the renderer only ever moves between adjacent cells,
// so a full reset is rare and always correct).
func sgrSequence(from, to Style, depth colorDepth, sb *strings.Builder) {
	if from == to {
		return
	}
	var parts []string

	emitFg := func(c Color) {
		switch c.Kind {
		case ColorDefault:
			parts = append(parts, "39")
		default:
			switch depth {
			case depthTrue:
				if c.Kind == Color256 {
					c = palette256[c.V] // resolve the index to its RGB anchor
				}
				parts = append(parts, "38;2;"+itoa(int(c.R))+";"+itoa(int(c.G))+";"+itoa(int(c.B)))
			case depth256:
				parts = append(parts, "38;5;"+itoa(int(ansi256(c))))
			default:
				i := ansi16(c)
				if i < 8 {
					parts = append(parts, itoa(30+int(i)))
				} else {
					parts = append(parts, itoa(90+int(i)-8))
				}
			}
		}
	}
	emitBg := func(c Color) {
		switch c.Kind {
		case ColorDefault:
			parts = append(parts, "49")
		default:
			switch depth {
			case depthTrue:
				if c.Kind == Color256 {
					c = palette256[c.V] // resolve the index to its RGB anchor
				}
				parts = append(parts, "48;2;"+itoa(int(c.R))+";"+itoa(int(c.G))+";"+itoa(int(c.B)))
			case depth256:
				parts = append(parts, "48;5;"+itoa(int(ansi256(c))))
			default:
				i := ansi16(c)
				if i < 8 {
					parts = append(parts, itoa(40+int(i)))
				} else {
					parts = append(parts, itoa(100+int(i)-8))
				}
			}
		}
	}

	if depth == depthMono {
		// Monochrome: attributes only, colors silently dropped.
		if from.Attrs != to.Attrs {
			parts = append(parts, "0")
			appendAttrParts(&parts, to.Attrs)
		}
		if len(parts) > 0 {
			sb.WriteString("\x1b[" + strings.Join(parts, ";") + "m")
		}
		return
	}

	if from.Fg != to.Fg {
		emitFg(to.Fg)
	}
	if from.Bg != to.Bg {
		emitBg(to.Bg)
	}
	if from.Attrs != to.Attrs {
		// Any attribute change is handled by toggling the differing bits —
		// cheaper than reset+reapply, and reset would also clear colors.
		// 22 turns off bold AND dim together, so whichever of the two
		// survives the transition must be re-applied after it.
		dropBold := from.Attrs&AttrBold != 0 && to.Attrs&AttrBold == 0
		dropDim := from.Attrs&AttrDim != 0 && to.Attrs&AttrDim == 0
		if dropBold || dropDim {
			parts = append(parts, "22")
			if !dropBold && to.Attrs&AttrBold != 0 {
				parts = append(parts, "1")
			}
			if !dropDim && to.Attrs&AttrDim != 0 {
				parts = append(parts, "2")
			}
		}
		if from.Attrs&AttrItalic != 0 && to.Attrs&AttrItalic == 0 {
			parts = append(parts, "23")
		}
		if from.Attrs&AttrUnderline != 0 && to.Attrs&AttrUnderline == 0 {
			parts = append(parts, "24")
		}
		if from.Attrs&AttrBlink != 0 && to.Attrs&AttrBlink == 0 {
			parts = append(parts, "25")
		}
		if from.Attrs&AttrReverse != 0 && to.Attrs&AttrReverse == 0 {
			parts = append(parts, "27")
		}
		if from.Attrs&AttrStrike != 0 && to.Attrs&AttrStrike == 0 {
			parts = append(parts, "29")
		}
		appendAttrParts(&parts, to.Attrs&^from.Attrs)
	}
	if len(parts) > 0 {
		sb.WriteString("\x1b[" + strings.Join(parts, ";") + "m")
	}
}

func appendAttrParts(parts *[]string, a Attr) {
	if a&AttrBold != 0 {
		*parts = append(*parts, "1")
	}
	if a&AttrDim != 0 {
		*parts = append(*parts, "2")
	}
	if a&AttrItalic != 0 {
		*parts = append(*parts, "3")
	}
	if a&AttrUnderline != 0 {
		*parts = append(*parts, "4")
	}
	if a&AttrBlink != 0 {
		*parts = append(*parts, "5")
	}
	if a&AttrReverse != 0 {
		*parts = append(*parts, "7")
	}
	if a&AttrStrike != 0 {
		*parts = append(*parts, "9")
	}
}

func itoa(v int) string { return strconv.Itoa(v) }

// resetSGR returns the reset sequence (used at flush start/end).
func resetSGR() string { return "\x1b[0m" }

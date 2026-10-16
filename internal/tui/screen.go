package tui

import (
	"bufio"
	"io"
	"strings"
	"unicode/utf8"
)

// Cell is one screen cell: a rune and its style. Wide runes (CJK, most
// emoji) occupy the cell plus its right neighbor — the neighbor keeps a
// zero rune as a continuation marker and is skipped by the renderer.
type Cell struct {
	R     rune
	Style Style
	W     uint8 // display width of R (0 = continuation cell, 1 = narrow, 2 = wide)
}

// Screen is the cell grid the whole UI draws into. Widgets paint cells;
// Flush diffs the grid against what the terminal last showed and emits the
// minimal ANSI to converge. The grid is plain memory — which is what makes
// every widget deterministic to test: draw, then read cells back.
type Screen struct {
	w, h  int
	cells []Cell // front grid: what Flush rendered last
	back  []Cell // back grid: what widgets drew this frame

	depth colorDepth
	out   *bufio.Writer

	// last cursor position the renderer moved to (1-based terminal coords)
	lastX, lastY int

	// FrameCapture, when set, receives the plain-text content of every
	// flushed frame — the battery's black-box oracle: drive the binary over
	// pipes, read frames as text.
	FrameCapture func(frame int, w, h int, lines []string)
	frame        int
}

// NewScreen builds a screen of the given size writing to w.
func NewScreen(w, h int, out io.Writer) *Screen {
	s := &Screen{w: w, h: h, depth: detectColorDepth(), out: bufio.NewWriterSize(out, 64*1024)}
	s.cells = make([]Cell, w*h)
	s.back = make([]Cell, w*h)
	s.clearBack()
	return s
}

func (s *Screen) clearBack() {
	for i := range s.back {
		s.back[i] = Cell{R: ' ', W: 1}
	}
}

// Size returns the grid dimensions.
func (s *Screen) Size() (int, int) { return s.w, s.h }

// Resize rebuilds both grids for the new size; the previous frame is
// discarded (the caller redraws everything and Flush clears the remains).
func (s *Screen) Resize(w, h int) {
	if w == s.w && h == s.h {
		return
	}
	s.w, s.h = w, h
	s.cells = make([]Cell, w*h)
	s.back = make([]Cell, w*h)
	s.clearBack()
	s.out.WriteString("\x1b[2J") // full repaint after a resize
	s.out.WriteString(resetSGR())
}

// Clear resets the back grid to blank cells so a frame starts clean.
func (s *Screen) Clear() { s.clearBack() }

// SetCell paints one cell. x/y outside the grid are ignored — widgets can
// draw their full geometry without clamping every coordinate. Painting a
// wide rune also claims the right neighbor (as a continuation cell) when
// it fits; a wide rune at the right edge degrades to a space.
func (s *Screen) SetCell(x, y int, r rune, st Style) {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return
	}
	i := y*s.w + x
	w := runeWidth(r)
	if w == 0 { // combining marks and zero-width runes: paint nothing
		return
	}
	if x+w > s.w {
		s.back[i] = Cell{R: ' ', Style: st, W: 1}
		return
	}
	s.back[i] = Cell{R: r, Style: st, W: uint8(w)}
	if w == 2 {
		s.back[i+1] = Cell{R: 0, Style: st, W: 0}
	}
}

// Cell reads a back-grid cell back (tests and hit-tests).
func (s *Screen) Cell(x, y int) Cell {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return Cell{}
	}
	return s.back[y*s.w+x]
}

// Text draws a left-aligned string starting at x,y clipped to width.
// Returns the x just past the drawn text. Runes wider than the remaining
// clip stop the draw (a half-cut wide rune would corrupt the grid).
func (s *Screen) Text(x, y int, str string, st Style) int {
	clip := s.w - x
	if clip <= 0 || y < 0 || y >= s.h {
		return x
	}
	cx := x
	for _, r := range str {
		w := runeWidth(r)
		if w == 0 {
			continue
		}
		if cx+w > x+clip || cx >= s.w {
			break
		}
		s.SetCell(cx, y, r, st)
		cx += w
	}
	return cx
}

// TextRight draws str so its last glyph lands ON xEnd (right edge
// inclusive — pass the last content column). A string wider than
// xEnd+1 left-truncates: the tail stays anchored at xEnd.
func (s *Screen) TextRight(xEnd, y int, str string, st Style) {
	w := StringWidth(str)
	s.Text(xEnd-w+1, y, str, st)
}

// FillRect paints a rectangle with one style (blank fill, borders, bands).
func (s *Screen) FillRect(x, y, w, h int, st Style) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			s.SetCell(xx, yy, ' ', st)
		}
	}
}

// HLine/VLine draw single runes along a run.
func (s *Screen) HLine(x, y, w int, r rune, st Style) {
	for i := range w {
		s.SetCell(x+i, y, r, st)
	}
}

func (s *Screen) VLine(x, y, h int, r rune, st Style) {
	for i := range h {
		s.SetCell(x, y+i, r, st)
	}
}

// Begin starts the alternate-screen session: hide the caret, enable
// SGR-1006 mouse tracking (with motion — hover highlighting is part of the
// design language) and focus reporting.
func (s *Screen) Begin() {
	s.out.WriteString("\x1b[?1049h") // alternate screen
	s.out.WriteString("\x1b[?25l")   // caret off
	s.out.WriteString("\x1b[?1003h") // any-motion mouse tracking
	s.out.WriteString("\x1b[?1006h") // SGR mouse encoding
	s.out.WriteString("\x1b[?1004h") // focus in/out reporting
	s.out.WriteString(resetSGR())
	s.out.Flush()
}

// End restores the terminal exactly: modes off in reverse order, caret
// back, primary buffer. Deferred by the app so a panic still restores.
func (s *Screen) End() {
	s.out.WriteString(resetSGR())
	s.out.WriteString("\x1b[?1004l")
	s.out.WriteString("\x1b[?1006l")
	s.out.WriteString("\x1b[?1003l")
	s.out.WriteString("\x1b[?25h")
	s.out.WriteString("\x1b[?1049l")
	s.out.Flush()
}

// Flush renders the back grid: cells that differ from the front grid are
// emitted with a cursor move only when the run is not already adjacent,
// and an SGR only when the style changed. Damage tracking keeps a
// full-frame repaint rare (resizes, the first frame).
func (s *Screen) Flush() {
	var sb strings.Builder
	cur := Style{}
	sb.WriteString(resetSGR())
	first := true
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			i := y*s.w + x
			b := s.back[i]
			if b.W == 0 {
				continue // continuation of a wide rune — owned by its lead cell
			}
			if s.cells[i] == b {
				continue // unchanged since the last flush
			}
			// Move the cursor when the run broke (start of frame, after a
			// skip, or a wide-rune jump).
			if first || s.lastX != x || s.lastY != y {
				sb.WriteString("\x1b[" + itoa(y+1) + ";" + itoa(x+1) + "H")
				s.lastX, s.lastY = x, y
			}
			if b.Style != cur || first {
				sgrSequence(cur, b.Style, s.depth, &sb)
				cur = b.Style
			}
			if b.R == 0 {
				sb.WriteRune(' ')
			} else {
				var buf [utf8.UTFMax]byte
				n := utf8.EncodeRune(buf[:], b.R)
				sb.Write(buf[:n])
			}
			s.cells[i] = b
			s.lastX += int(b.W)
			first = false
		}
	}
	sb.WriteString(resetSGR())
	s.out.WriteString(sb.String())
	s.out.Flush()

	s.frame++
	if s.FrameCapture != nil {
		s.FrameCapture(s.frame, s.w, s.h, s.PlainLines())
	}
}

// PlainLines renders the back grid as plain text lines (styles dropped,
// wide runes kept as-is) — the frame-capture format.
func (s *Screen) PlainLines() []string {
	lines := make([]string, s.h)
	for y := 0; y < s.h; y++ {
		var sb strings.Builder
		for x := 0; x < s.w; x++ {
			c := s.back[y*s.w+x]
			if c.W == 0 {
				continue
			}
			if c.R == 0 {
				sb.WriteByte(' ')
			} else {
				sb.WriteRune(c.R)
			}
		}
		lines[y] = strings.TrimRight(sb.String(), " ")
	}
	return lines
}

// FindText returns the 0-based line index whose content contains sub, or
// -1 — the frame-assert helper.
func FindText(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

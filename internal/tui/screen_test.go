package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetCellWideClaimsNeighbor(t *testing.T) {
	s := NewScreen(10, 3, &bytes.Buffer{})
	s.SetCell(5, 2, '中', Style{})
	lead := s.Cell(5, 2)
	if lead.R != '中' || lead.W != 2 {
		t.Fatalf("lead cell: want 中 W2, got %q W%d", lead.R, lead.W)
	}
	cont := s.Cell(6, 2)
	if cont.R != 0 || cont.W != 0 {
		t.Fatalf("continuation cell: want R0 W0, got %q W%d", cont.R, cont.W)
	}
}

func TestSetCellWideAtEdgeDegrades(t *testing.T) {
	s := NewScreen(10, 1, &bytes.Buffer{})
	s.SetCell(9, 0, '中', Style{})
	c := s.Cell(9, 0)
	if c.R != ' ' || c.W != 1 {
		t.Fatalf("edge wide: want space W1, got %q W%d", c.R, c.W)
	}
}

func TestSetCellZeroWidthAndBounds(t *testing.T) {
	s := NewScreen(10, 2, &bytes.Buffer{})
	s.SetCell(2, 0, 0x0301, Style{}.Bold()) // combining acute: paints nothing
	if c := s.Cell(2, 0); c.R != ' ' || c.Style.Attrs != 0 {
		t.Fatalf("zero-width rune must not paint, got %q attrs %d", c.R, c.Style.Attrs)
	}
	s.SetCell(-1, 0, 'x', Style{})
	s.SetCell(0, -1, 'x', Style{})
	s.SetCell(10, 0, 'x', Style{})
	s.SetCell(0, 2, 'x', Style{})
	if c := s.Cell(10, 0); c != (Cell{}) {
		t.Fatalf("out-of-bounds read must return zero cell, got %#v", c)
	}
	for y := range 2 {
		for x := range 10 {
			if c := s.Cell(x, y); c.R != ' ' {
				t.Fatalf("out-of-bounds writes must be ignored; (%d,%d)=%q", x, y, c.R)
			}
		}
	}
}

func TestTextClippingAndReturn(t *testing.T) {
	s := NewScreen(10, 1, &bytes.Buffer{})
	end := s.Text(2, 0, "ab", Style{})
	if end != 4 {
		t.Fatalf("Text return: want 4, got %d", end)
	}
	if s.Cell(2, 0).R != 'a' || s.Cell(3, 0).R != 'b' {
		t.Fatal("Text did not paint its runes")
	}
	// A wide rune that would straddle the edge stops the draw whole.
	end = s.Text(8, 0, "ab中", Style{})
	if end != 10 {
		t.Fatalf("clipped Text return: want 10, got %d", end)
	}
	if s.Cell(8, 0).R != 'a' || s.Cell(9, 0).R != 'b' {
		t.Fatal("clip lost the narrow runes that fit")
	}
	// Past the right edge entirely: nothing drawn, x returned.
	if end := s.Text(10, 0, "x", Style{}); end != 10 {
		t.Fatalf("past-edge Text return: want 10, got %d", end)
	}
	// Zero-width runes inside text occupy nothing.
	if end := s.Text(0, 0, "áb", Style{}); end != 2 {
		t.Fatalf("zero-width inside text: want end 2, got %d", end)
	}
}

func TestTextRightAnchorsAtEnd(t *testing.T) {
	s := NewScreen(10, 1, &bytes.Buffer{})
	s.TextRight(7, 0, "ab", Style{})
	if s.Cell(6, 0).R != 'a' || s.Cell(7, 0).R != 'b' || s.Cell(8, 0).R != ' ' {
		t.Fatal("TextRight did not land its last glyph on xEnd")
	}
	// Wider than the line: left-truncated, tail anchored at xEnd.
	s.TextRight(2, 0, "abcde", Style{})
	if s.Cell(0, 0).R != 'c' || s.Cell(2, 0).R != 'e' {
		t.Fatalf("over-wide TextRight must show the tail, got %q %q %q",
			s.Cell(0, 0).R, s.Cell(1, 0).R, s.Cell(2, 0).R)
	}
}

func TestLinesAndRects(t *testing.T) {
	s := NewScreen(8, 4, &bytes.Buffer{})
	s.FillRect(1, 1, 3, 2, Style{}.Bold())
	if c := s.Cell(2, 2); c.R != ' ' || c.Style.Attrs&AttrBold == 0 {
		t.Fatal("FillRect did not paint its style")
	}
	if c := s.Cell(0, 0); c.Style.Attrs != 0 {
		t.Fatal("FillRect leaked outside its rect")
	}
	s.HLine(0, 3, 4, '─', Style{})
	if s.Cell(3, 3).R != '─' || s.Cell(4, 3).R != ' ' {
		t.Fatal("HLine wrong length")
	}
	s.VLine(7, 0, 2, '│', Style{})
	if s.Cell(7, 1).R != '│' || s.Cell(7, 2).R != ' ' {
		t.Fatal("VLine wrong length")
	}
}

func TestFlushDiff(t *testing.T) {
	t.Setenv("NO_COLOR", "1") // mono: SGR output stays deterministic
	buf := &bytes.Buffer{}
	s := NewScreen(10, 3, buf)
	s.Text(0, 0, "hello", Style{})
	s.Flush()

	// Nothing changed: only the leading and trailing resets.
	buf.Reset()
	s.Flush()
	if got := buf.String(); got != resetSGR()+resetSGR() {
		t.Fatalf("unchanged flush must emit only resets, got %q", got)
	}

	// One changed cell: one cursor move, one rune.
	s.SetCell(3, 1, 'X', Style{})
	buf.Reset()
	s.Flush()
	got := buf.String()
	want := resetSGR() + "\x1b[2;4H" + "X" + resetSGR()
	if got != want {
		t.Fatalf("single-cell flush: want %q, got %q", want, got)
	}
}

func TestFlushAdjacentRunNoRemoves(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	buf := &bytes.Buffer{}
	s := NewScreen(10, 1, buf)
	s.Text(0, 0, "a中b", Style{}) // 4 display cells, one wide
	s.Flush()
	// The whole line is one adjacent run: exactly one cursor move —
	// the wide rune's continuation cell emits nothing.
	if n := strings.Count(buf.String(), "H"); n != 1 {
		t.Fatalf("adjacent run: want 1 cursor move, got %d in %q", n, buf.String())
	}
	if !strings.Contains(buf.String(), "a中b") {
		t.Fatalf("run content missing: %q", buf.String())
	}
}

func TestPlainLinesAndFindText(t *testing.T) {
	s := NewScreen(10, 3, &bytes.Buffer{})
	s.Text(0, 0, "header", Style{})
	s.Text(2, 1, "中b", Style{})
	lines := s.PlainLines()
	if lines[0] != "header" {
		t.Fatalf("line 0: %q", lines[0])
	}
	if lines[1] != "  中b" {
		t.Fatalf("line 1 (wide kept, trailing trimmed): %q", lines[1])
	}
	if lines[2] != "" {
		t.Fatalf("blank line must trim to empty, got %q", lines[2])
	}
	if FindText(lines, "中b") != 1 || FindText(lines, "header") != 0 || FindText(lines, "nope") != -1 {
		t.Fatal("FindText failed")
	}
}

func TestFrameCaptureHook(t *testing.T) {
	s := NewScreen(4, 2, &bytes.Buffer{})
	var frames []int
	var seen [][]string
	s.FrameCapture = func(f, w, h int, lines []string) {
		if w != 4 || h != 2 {
			t.Errorf("capture size: want 4x2, got %dx%d", w, h)
		}
		frames = append(frames, f)
		seen = append(seen, lines)
	}
	s.Text(0, 0, "ok", Style{})
	s.Flush()
	s.Flush()
	if len(frames) != 2 || frames[0] != 1 || frames[1] != 2 {
		t.Fatalf("frame numbers: %v", frames)
	}
	if seen[0][0] != "ok" {
		t.Fatalf("captured content: %q", seen[0][0])
	}
}

func TestResizeDiscardsAndRepaints(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewScreen(10, 2, buf)
	s.Text(0, 0, "wide text", Style{})
	s.Resize(4, 2)
	if w, h := s.Size(); w != 4 || h != 2 {
		t.Fatalf("size after resize: %dx%d", w, h)
	}
	if c := s.Cell(0, 0); c.R != ' ' {
		t.Fatalf("resize must clear the grid, got %q", c.R)
	}
	// The 2J rides the bufio until the next flush lands it.
	s.Text(0, 0, "x", Style{})
	s.Flush()
	if !strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatal("resize must schedule a full repaint")
	}
	// Same size: a no-op.
	buf.Reset()
	s.Resize(4, 2)
	if buf.Len() != 0 {
		t.Fatalf("same-size resize wrote %q", buf.String())
	}
}

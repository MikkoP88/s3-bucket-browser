package tui

// Widget is anything that can paint into the screen and consume input.
// Containers manage their children's geometry and focus themselves —
// the App only routes top-level events to the root (or the top modal)
// and redraws when a handler says the UI changed (by calling
// App.MarkDirty).
type Widget interface {
	SetRect(x, y, w, h int)
	Rect() (x, y, w, h int)
	Draw(s *Screen)
	HandleKey(k KeyEvent) bool
	HandleMouse(m MouseEvent) bool
}

// BaseWidget carries the geometry every widget shares.
type BaseWidget struct{ x, y, w, h int }

func (b *BaseWidget) SetRect(x, y, w, h int)     { b.x, b.y, b.w, b.h = x, y, w, h }
func (b *BaseWidget) Rect() (int, int, int, int) { return b.x, b.y, b.w, b.h }

// Visible reports whether the rect is drawable at all.
func (b *BaseWidget) Visible() bool { return b.w > 0 && b.h > 0 }

// Box draws the rounded panel frame — the face's core visual unit —
// with an optional title left-aligned in the top border and an optional
// hint right-aligned there. Inner returns the content rect.
type Box struct {
	BaseWidget
	Title   string
	TitleSt Style
	Hint    string
	Focused bool
}

func (b *Box) Draw(s *Screen) {
	if !b.Visible() || b.w < 2 || b.h < 2 {
		return
	}
	bs := Style{Fg: theme.Border}
	if b.Focused {
		bs = Style{Fg: theme.BorderFocus}
	}
	x, y, w, h := b.Rect()
	right := x + w - 1
	bottom := y + h - 1
	s.SetCell(x, y, glTopLeft, bs)
	s.SetCell(right, y, glTopRight, bs)
	s.SetCell(x, bottom, glBotLeft, bs)
	s.SetCell(right, bottom, glBotRight, bs)
	s.HLine(x+1, y, w-2, glHoriz, bs)
	s.HLine(x+1, bottom, w-2, glHoriz, bs)
	s.VLine(x, y+1, h-2, glVert, bs)
	s.VLine(right, y+1, h-2, glVert, bs)
	if b.Title != "" {
		ts := b.TitleSt
		if ts == (Style{}) {
			ts = Style{Fg: theme.BorderFocus}
		}
		s.Text(x+2, y, " "+b.Title+" ", ts)
	}
	if b.Hint != "" {
		s.TextRight(right-2, y, " "+b.Hint+" ", Style{Fg: theme.Border})
	}
}

// Inner is the usable content rect inside the frame.
func (b *Box) Inner() (int, int, int, int) {
	return b.x + 1, b.y + 1, b.w - 2, b.h - 2
}

func (b *Box) HandleKey(KeyEvent) bool     { return false }
func (b *Box) HandleMouse(MouseEvent) bool { return false }

// truncateFits clips s to display width max and appends an ellipsis
// when clipping happened.
func truncateFits(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if StringWidth(s) <= max {
		return s
	}
	if max == 1 {
		return string(glyphEllipsis)
	}
	var sb []rune
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > max-1 {
			break
		}
		sb = append(sb, r)
		w += rw
	}
	return string(sb) + string(glyphEllipsis)
}

// drawProgress paints a fraction as an eighth-block ramp.
func drawProgress(s *Screen, x, y, w int, frac float64) int {
	if w <= 0 {
		return x
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	eighths := int(frac * float64(w) * 8)
	for i := range w {
		cell := eighths - i*8
		switch {
		case cell >= 8:
			s.SetCell(x+i, y, progressRamp[7], stAccent())
		case cell > 0:
			s.SetCell(x+i, y, progressRamp[cell-1], stAccent())
		default:
			s.SetCell(x+i, y, progressRamp[7], Style{Fg: theme.Border})
		}
	}
	return x + w
}

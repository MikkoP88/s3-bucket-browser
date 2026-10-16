package tui

import "time"

// ── Table ─────────────────────────────────────────────────────────────────

// Column is one table column. Exactly one column may be Flex — it
// absorbs the remainder of the panel width.
type Column struct {
	Title string
	Width int
	Flex  bool
	Right bool // right-align contents (sizes, counts)
}

// Row is one line of data; Cells line up with the table's columns and
// CellStyles (optional, same length) overrides per-cell presentation.
type Row struct {
	Cells      []string
	CellStyles []Style
	Data       any
}

// Table is the listing widget: header, scrollable rows, full-row
// selection bar, hover, marks (✓) for multi-selection, double-click
// activation, and wheel scrolling.
type Table struct {
	BaseWidget
	Title   string
	Focused bool
	Multi   bool // show the mark gutter

	Cols []Column
	Rows []Row

	Sel   int // selected row
	Off   int // first visible row
	Marks map[int]bool

	OnSelect   func(r *Row)
	OnActivate func(r *Row)
	OnHeader   func(col int)

	hover   int // row under the mouse, -1 none
	lastClk struct {
		row int
		at  time.Time
	}
}

func NewTable(title string, cols ...Column) *Table {
	return &Table{Title: title, Cols: cols, Sel: -1, hover: -1, Marks: map[int]bool{}}
}

// SelectedRow returns the selected row or nil.
func (t *Table) SelectedRow() *Row {
	if t.Sel >= 0 && t.Sel < len(t.Rows) {
		return &t.Rows[t.Sel]
	}
	return nil
}

// MarkedRows returns marked rows (ordered), falling back to a slice of
// just the selected row when nothing is marked — the "acts on what you
// see" law.
func (t *Table) MarkedRows() []int {
	var out []int
	for i := range t.Rows {
		if t.Marks[i] {
			out = append(out, i)
		}
	}
	if len(out) == 0 && t.Sel >= 0 && t.Sel < len(t.Rows) {
		out = append(out, t.Sel)
	}
	return out
}

// ToggleMark flips the mark on the selected row.
func (t *Table) ToggleMark() {
	if t.Sel >= 0 && t.Sel < len(t.Rows) {
		if t.Marks[t.Sel] {
			delete(t.Marks, t.Sel)
		} else {
			t.Marks[t.Sel] = true
		}
	}
}

// SetRows replaces the data, preserving selection by index when it
// still fits.
func (t *Table) SetRows(rows []Row) {
	sel := t.Sel
	t.Rows = rows
	t.Marks = map[int]bool{}
	t.hover = -1
	if sel >= len(rows) {
		sel = len(rows) - 1
	}
	t.Sel = sel
	t.clamp()
}

// colWidths resolves fixed and flex widths against the available width.
func (t *Table) colWidths(avail int) []int {
	ws := make([]int, len(t.Cols))
	flex := -1
	used := 0
	for i, c := range t.Cols {
		if c.Flex {
			flex = i
			continue
		}
		ws[i] = c.Width
		used += c.Width
	}
	gutters := 2 * (len(t.Cols) - 1)
	rest := avail - used - gutters
	if t.Multi {
		rest -= 2
	}
	if flex >= 0 {
		ws[flex] = max(rest, 8)
	}
	return ws
}

func (t *Table) Draw(s *Screen) {
	if !t.Visible() {
		return
	}
	box := Box{BaseWidget: t.BaseWidget, Title: t.Title, Focused: t.Focused}
	if n := len(t.Rows); n > 0 && t.Multi {
		if m := len(t.MarkedRows()); m > 1 {
			box.Hint = itoa(m) + " selected"
		}
	}
	box.Draw(s)
	ix, iy, iw, ih := box.Inner()
	if iw < 4 || ih < 1 {
		return
	}

	ws := t.colWidths(iw)
	x0 := ix
	if t.Multi {
		x0 += 2
		iw -= 2
	}

	// Header: dim column titles over a hairline.
	hx := x0
	for i, c := range t.Cols {
		title := truncateFits(c.Title, ws[i])
		if c.Right {
			s.TextRight(hx+ws[i]-1, iy, title, stDim())
		} else {
			s.Text(hx, iy, title, stDim())
		}
		hx += ws[i] + 2
	}
	s.HLine(ix, iy+1, iw, glHoriz, Style{Fg: theme.Border})
	if ih < 3 {
		return
	}

	rows := t.Rows
	visible := ih - 2
	t.clampTo(visible)
	for vi := range visible {
		ri := t.Off + vi
		if ri >= len(rows) {
			break
		}
		row := &rows[ri]
		yy := iy + 2 + vi

		sel := ri == t.Sel
		hov := ri == t.hover && !sel && t.Focused
		switch {
		case sel:
			s.FillRect(ix, yy, iw, 1, stSel())
		case hov:
			s.FillRect(ix, yy, iw, 1, Style{Bg: theme.SelBg})
		}

		if t.Multi {
			if t.Marks[ri] {
				s.Text(ix, yy, string(glCheck), stAccent())
			}
		}

		base := stPlain()
		if hov {
			base = Style{Fg: theme.Bright}
		}
		if sel {
			base = stSel()
		}
		cx := x0
		for ci, c := range t.Cols {
			cellSt := base
			if row.CellStyles != nil && ci < len(row.CellStyles) && !sel {
				cellSt = row.CellStyles[ci]
			}
			if sel && row.CellStyles != nil && ci < len(row.CellStyles) {
				cellSt = stSel().Bold()
			}
			text := truncateFits(row.Cells[ci], ws[ci])
			if c.Right {
				s.TextRight(cx+ws[ci]-1, yy, text, cellSt)
			} else {
				s.Text(cx, yy, text, cellSt)
			}
			cx += ws[ci] + 2
		}
	}

	// Scrollbar: a hairline rail with the thumb in accent.
	if len(rows) > visible && visible > 0 && iw > 2 {
		railX := ix + iw - 1
		for vi := range visible {
			s.SetCell(railX, iy+2+vi, glVert, Style{Fg: theme.Border})
		}
		thumb := max(visible*visible/len(rows), 1)
		top := t.Off * visible / len(rows)
		for k := range thumb {
			s.SetCell(railX, iy+2+top+k, glVert, stAccent())
		}
	}
}

// rowAtY maps a y coordinate to a row index, -1 outside the rows.
func (t *Table) rowAtY(y int) int {
	_, iy, _, ih := t.InnerRect()
	if ih < 3 || y < iy+2 || y >= iy+ih {
		return -1
	}
	ri := t.Off + (y - iy - 2)
	if ri < 0 || ri >= len(t.Rows) {
		return -1
	}
	return ri
}

// InnerRect exposes the content rect for the face's hit-tests.
func (t *Table) InnerRect() (int, int, int, int) {
	return t.x + 1, t.y + 1, t.w - 2, t.h - 2
}

func (t *Table) visibleRows() int {
	_, _, _, ih := t.InnerRect()
	return max(ih-2, 1)
}

func (t *Table) clampTo(visible int) {
	if visible < 1 {
		visible = 1
	}
	if t.Off > len(t.Rows)-1 {
		t.Off = max(len(t.Rows)-1, 0)
	}
	if t.Off < 0 {
		t.Off = 0
	}
	if t.Sel != -1 {
		if t.Sel < t.Off {
			t.Off = t.Sel
		}
		if t.Sel >= t.Off+visible {
			t.Off = t.Sel - visible + 1
		}
	}
}

func (t *Table) clamp() { t.clampTo(t.visibleRows()) }

// MoveSel moves the selection by delta (0 = re-clamp only) and fires
// OnSelect when it lands somewhere new.
func (t *Table) MoveSel(delta int) {
	if len(t.Rows) == 0 {
		t.Sel = -1
		return
	}
	if t.Sel < 0 && delta > 0 {
		delta-- // standing above the list: the first step down enters at row 0
	}
	ns := min(max(t.Sel+delta, 0), len(t.Rows)-1)
	if ns != t.Sel || delta == 0 {
		t.Sel = ns
		t.clamp()
		if t.OnSelect != nil && t.SelectedRow() != nil {
			t.OnSelect(t.SelectedRow())
		}
	}
}

func (t *Table) HandleKey(k KeyEvent) bool {
	if !t.Focused {
		return false
	}
	switch k.Key {
	case KeyUp:
		t.MoveSel(-1)
	case KeyDown:
		t.MoveSel(1)
	case KeyPgUp:
		t.MoveSel(-t.visibleRows())
	case KeyPgDn:
		t.MoveSel(t.visibleRows())
	case KeyHome:
		if len(t.Rows) > 0 {
			t.Sel, t.Off = 0, 0
			t.fireSelect()
		}
	case KeyEnd:
		if len(t.Rows) > 0 {
			t.Sel = len(t.Rows) - 1
			t.clamp()
			t.fireSelect()
		}
	case KeyEnter:
		if r := t.SelectedRow(); r != nil && t.OnActivate != nil {
			t.OnActivate(r)
		}
	case KeyRune:
		if k.Rune == ' ' && k.Mod == 0 && t.Multi {
			t.ToggleMark()
			t.MoveSel(1)
		} else {
			return false
		}
	default:
		return false
	}
	return true
}

func (t *Table) fireSelect() {
	if t.OnSelect != nil && t.SelectedRow() != nil {
		t.OnSelect(t.SelectedRow())
	}
}

func (t *Table) HandleMouse(m MouseEvent) bool {
	x, y, w, h := t.Rect()
	if m.X < x || m.X >= x+w || m.Y < y || m.Y >= y+h {
		return false
	}
	switch m.Type {
	case MouseMotion:
		if ri := t.rowAtY(m.Y); ri != t.hover {
			t.hover = ri
		}
	case MouseWheelUp:
		t.Off = max(t.Off-3, 0)
		t.clamp()
		return true
	case MouseWheelDown:
		t.Off = min(t.Off+3, max(len(t.Rows)-1, 0))
		t.clamp()
		return true
	case MousePress:
		// Header click → sort hook.
		_, iy, iw, _ := t.InnerRect()
		if m.Y == iy && t.OnHeader != nil {
			hx := t.x + 1
			if t.Multi {
				hx += 2
			}
			ws := t.colWidths(iw)
			for i := range t.Cols {
				if m.X >= hx && m.X < hx+ws[i] {
					t.OnHeader(i)
					return true
				}
				hx += ws[i] + 2
			}
			return true
		}
		ri := t.rowAtY(m.Y)
		if ri < 0 {
			return true
		}
		now := time.Now()
		dbl := ri == t.lastClk.row && now.Sub(t.lastClk.at) < 450*time.Millisecond
		t.lastClk.row, t.lastClk.at = ri, now
		if k, ok := t.Marks[ri]; k && ok && m.Mod == ModCtrl {
			delete(t.Marks, ri)
		} else if m.Mod == ModCtrl && !t.Marks[ri] {
			t.Marks[ri] = true
		}
		t.Sel = ri
		t.clamp()
		t.fireSelect()
		if dbl && t.OnActivate != nil {
			t.OnActivate(t.SelectedRow())
		}
	}
	return true
}

// ── TextInput ─────────────────────────────────────────────────────────────

// TextInput is a single-line editor: block caret, placeholder, kill
// ringline shortcuts, history recall, and paste support.
type TextInput struct {
	BaseWidget
	Prompt      string
	Placeholder string
	value       []rune
	cur         int
	left        int // window offset when the value overflows
	History     []string
	histIdx     int

	OnSubmit func(string)
	OnChange func(string)
	OnCancel func()
}

func (ti *TextInput) Value() string { return string(ti.value) }

func (ti *TextInput) SetValue(v string) {
	ti.value = []rune(v)
	ti.cur = len(ti.value)
	ti.histIdx = len(ti.History)
}

// setFromHistory seats a recalled entry without disturbing the recall
// index (SetValue would reset it, stranding every entry but the last).
func (ti *TextInput) setFromHistory(v string) {
	ti.value = []rune(v)
	ti.cur = len(ti.value)
}

func (ti *TextInput) Draw(s *Screen) {
	if !ti.Visible() {
		return
	}
	x, y, w, _ := ti.Rect()
	if w < 2 {
		return
	}
	cx := x
	if ti.Prompt != "" {
		p := truncateFits(ti.Prompt, w-1)
		cx = s.Text(cx, y, p, stAccent())
	}
	inner := w - (cx - x)
	ti.scrollWindow(inner)
	if len(ti.value) == 0 && ti.Placeholder != "" {
		s.Text(cx, y, truncateFits(ti.Placeholder, inner), stDim())
		s.SetCell(cx, y, ' ', Style{Attrs: AttrReverse})
		return
	}
	// Value with the block caret.
	vx := cx
	for i, r := range ti.value {
		if vx >= x+w {
			break
		}
		if i == ti.cur {
			s.SetCell(vx, y, r, Style{Attrs: AttrReverse})
		} else {
			s.SetCell(vx, y, r, stPlain())
		}
		vx += int(runeWidth(r))
	}
	if ti.cur >= len(ti.value) && vx < x+w {
		s.SetCell(vx, y, ' ', Style{Attrs: AttrReverse})
	}
}

// scrollWindow adjusts the left offset so the cursor stays visible.
func (ti *TextInput) scrollWindow(width int) {
	if ti.cur < ti.left {
		ti.left = ti.cur
	}
	for ti.cur-ti.left >= width {
		ti.left++
	}
}

// runeAtX maps a click x to the nearest rune index.
func (ti *TextInput) runeAtX(px, x0 int) int {
	w := px - x0
	if w <= 0 {
		return 0
	}
	acc := 0
	for i, r := range ti.value {
		acc += int(runeWidth(r))
		if acc > w {
			return i
		}
	}
	return len(ti.value)
}

func (ti *TextInput) HandleKey(k KeyEvent) bool {
	switch k.Key {
	case KeyRune:
		if k.Mod == ModCtrl {
			// Readline-style line editing: Ctrl+U kills to start,
			// Ctrl+K kills to end — the shortcuts every terminal
			// teaches for one-line prompts.
			switch k.Rune {
			case 'u':
				ti.value = append([]rune{}, ti.value[ti.cur:]...)
				ti.cur = 0
				ti.changed()
			case 'k':
				ti.value = append([]rune{}, ti.value[:ti.cur]...)
				ti.changed()
			}
			return true
		}
		if k.Mod != 0 && k.Mod != ModShift {
			return false
		}
		ti.insert(k.Rune)
	case KeyBackspace:
		if ti.cur > 0 {
			ti.value = append(ti.value[:ti.cur-1], ti.value[ti.cur:]...)
			ti.cur--
			ti.changed()
		}
	case KeyDelete:
		if ti.cur < len(ti.value) {
			ti.value = append(ti.value[:ti.cur], ti.value[ti.cur+1:]...)
			ti.changed()
		}
	case KeyLeft:
		if k.Mod == ModCtrl {
			ti.cur = ti.wordLeft()
		} else if ti.cur > 0 {
			ti.cur--
		}
	case KeyRight:
		if k.Mod == ModCtrl {
			ti.cur = ti.wordRight()
		} else if ti.cur < len(ti.value) {
			ti.cur++
		}
	case KeyHome:
		ti.cur = 0
	case KeyEnd:
		ti.cur = len(ti.value)
	case KeyEnter:
		v := ti.Value()
		if v != "" && (len(ti.History) == 0 || ti.History[len(ti.History)-1] != v) {
			ti.History = append(ti.History, v)
		}
		ti.histIdx = len(ti.History)
		if ti.OnSubmit != nil {
			ti.OnSubmit(v)
		}
	case KeyEsc:
		if ti.OnCancel != nil {
			ti.OnCancel()
		}
	case KeyUp:
		if ti.histIdx > 0 {
			ti.histIdx--
			ti.setFromHistory(ti.History[ti.histIdx])
		}
	case KeyDown:
		if ti.histIdx < len(ti.History) {
			ti.histIdx++
			v := ""
			if ti.histIdx < len(ti.History) {
				v = ti.History[ti.histIdx]
			}
			ti.setFromHistory(v)
		}
	default:
		return false
	}
	return true
}

func (ti *TextInput) insert(rs ...rune) {
	v := append([]rune{}, ti.value[:ti.cur]...)
	v = append(v, rs...)
	ti.value = append(v, ti.value[ti.cur:]...)
	ti.cur += len(rs)
	ti.changed()
}

func (ti *TextInput) changed() {
	if ti.OnChange != nil {
		ti.OnChange(ti.Value())
	}
}

func (ti *TextInput) isWordRune(r rune) bool {
	return r == ' ' || r == '/' || r == '\\' || r == '-' || r == '_'
}

func (ti *TextInput) wordLeft() int {
	i := ti.cur
	for i > 0 && ti.isWordRune(ti.value[i-1]) {
		i--
	}
	for i > 0 && !ti.isWordRune(ti.value[i-1]) {
		i--
	}
	return i
}

func (ti *TextInput) wordRight() int {
	i := ti.cur
	for i < len(ti.value) && !ti.isWordRune(ti.value[i]) {
		i++
	}
	for i < len(ti.value) && ti.isWordRune(ti.value[i]) {
		i++
	}
	return i
}

func (ti *TextInput) HandleMouse(m MouseEvent) bool {
	x, y, w, h := ti.Rect()
	if m.X < x || m.X >= x+w || m.Y < y || m.Y >= y+h {
		return false
	}
	if m.Type == MousePress {
		x0 := x
		if ti.Prompt != "" {
			x0 += StringWidth(ti.Prompt)
		}
		ti.cur = ti.runeAtX(m.X, x0)
	}
	return true
}

// HandlePaste inserts pasted text.
func (ti *TextInput) HandlePaste(text string) {
	ti.insert([]rune(text)...)
}

// ── Dialog ────────────────────────────────────────────────────────────────

// DialogButton is one footer verb. Style nil = default; the confirm
// slot gets the accent, destructive verbs the danger tone.
type DialogButton struct {
	Label  string
	Action func()
	Style  Style
}

// Dialog is the modal chassis: title bar, content painter, footer
// buttons with roving focus (Tab/←/→, Enter fires, Esc cancels).
type Dialog struct {
	BaseWidget
	Title  string
	Width  int
	Height int

	Content func(s *Screen, x, y, w, h int)
	OnKey   func(k KeyEvent, d *Dialog) bool // content keys first (inputs)

	Buttons  []DialogButton
	focusBtn int
	OnCancel func()

	Data any // face payload
}

func (d *Dialog) Draw(s *Screen) {
	if !d.Visible() {
		return
	}
	box := Box{BaseWidget: d.BaseWidget, Title: d.Title, Focused: true}
	box.TitleSt = Style{Fg: theme.BorderModal}.Bold()
	box.Draw(s)
	ix, iy, iw, ih := box.Inner()

	// Footer: buttons right-aligned on the bottom row, hint left.
	fy := iy + ih - 1
	if len(d.Buttons) > 0 {
		s.HLine(ix, fy-1, iw, glHoriz, Style{Fg: theme.Border})
		bx := ix + iw
		for bi := len(d.Buttons) - 1; bi >= 0; bi-- {
			b := d.Buttons[bi]
			bx -= StringWidth(b.Label) + 4
			st := Style{Fg: theme.Bright}
			if bi == d.focusBtn {
				st = Style{Fg: theme.Accent}.Bold().Under()
			}
			if b.Style.Fg == theme.Danger {
				st = Style{Fg: theme.Danger}
				if bi == d.focusBtn {
					st = st.Bold().Under()
				}
			}
			s.Text(bx, fy, " "+b.Label+" ", st)
		}
		s.Text(ix, fy, " Tab ", stDim())
	}
	if d.Content != nil {
		ch := fy - iy - 1
		if len(d.Buttons) == 0 {
			ch = ih
		}
		if ch > 0 {
			d.Content(s, ix, iy, iw, ch)
		}
	}
}

// Layout centers the dialog inside availW×availH (called by the App).
func (d *Dialog) Layout(availW, availH int) {
	w := min(d.Width, max(availW-4, 20))
	h := min(d.Height, max(availH-4, 5))
	x := (availW - w) / 2
	y := (availH - h) / 2
	d.SetRect(x, y, w, h)
}

func (d *Dialog) HandleKey(k KeyEvent) bool {
	if d.OnKey != nil && d.OnKey(k, d) {
		return true
	}
	switch k.Key {
	case KeyEsc:
		if d.OnCancel != nil {
			d.OnCancel()
			return true
		}
	case KeyTab, KeyRight:
		if len(d.Buttons) > 0 {
			d.focusBtn = (d.focusBtn + 1) % len(d.Buttons)
			return true
		}
	case KeyBacktab, KeyLeft:
		if len(d.Buttons) > 0 {
			d.focusBtn = (d.focusBtn - 1 + len(d.Buttons)) % len(d.Buttons)
			return true
		}
	case KeyEnter:
		if len(d.Buttons) > 0 {
			d.Buttons[d.focusBtn].Action()
			return true
		}
	}
	return true // modals swallow everything else
}

func (d *Dialog) HandleMouse(m MouseEvent) bool {
	x, y, w, h := d.Rect()
	if m.X < x || m.X >= x+w || m.Y < y || m.Y >= y+h {
		return m.Type != MousePress // clicks outside land on the face
	}
	if m.Type != MousePress || len(d.Buttons) == 0 {
		return true
	}
	// Footer hit: map x back to the button rectangles Draw computed.
	fbox := Box{BaseWidget: d.BaseWidget}
	_, iy, iw, ih := fbox.Inner()
	fy := iy + ih - 1
	if m.Y != fy {
		return true
	}
	bx := x + 1 + iw
	for bi := len(d.Buttons) - 1; bi >= 0; bi-- {
		b := d.Buttons[bi]
		bx -= StringWidth(b.Label) + 4
		if m.X >= bx && m.X < bx+StringWidth(b.Label)+2 {
			d.focusBtn = bi
			b.Action()
			return true
		}
	}
	return true
}

// ── Menu ──────────────────────────────────────────────────────────────────

// MenuItem is one popup row.
type MenuItem struct {
	Label       string
	Accelerator string
	Danger      bool
	Disabled    bool
	Separator   bool
}

// Menu is a context popup: keyboard list + mouse hover/pick.
type Menu struct {
	BaseWidget
	Items  []MenuItem
	sel    int
	OnPick func(i int)
}

func (m *Menu) Width() int {
	w := 0
	for _, it := range m.Items {
		v := StringWidth(it.Label)
		if it.Accelerator != "" {
			v += 3 + StringWidth(it.Accelerator)
		}
		w = max(w, v)
	}
	return w + 4
}

func (m *Menu) Height() int { return len(m.Items) + 2 }

// OpenAt seats the popup with its top-left at (px,py), pulled back
// inside the screen bounds.
func (m *Menu) OpenAt(px, py, screenW, screenH int) {
	w, h := m.Width(), m.Height()
	x, y := px, py
	if x+w > screenW {
		x = max(screenW-w-1, 0)
	}
	if y+h > screenH {
		y = max(py-h, 0)
	}
	m.SetRect(x, y, w, h)
	m.skipDisabled(1)
}

func (m *Menu) skipDisabled(dir int) {
	for len(m.Items) > 0 {
		if !m.Items[m.sel].Disabled && !m.Items[m.sel].Separator {
			return
		}
		m.sel = (m.sel + dir + len(m.Items)) % len(m.Items)
	}
}

func (m *Menu) Draw(s *Screen) {
	if !m.Visible() {
		return
	}
	sw, sh := s.Size()
	m.OpenAt(m.x, m.y, sw, sh) // re-clamp against the CURRENT size
	box := Box{BaseWidget: m.BaseWidget}
	box.Draw(s)
	ix, iy, iw, _ := box.Inner()
	for i, it := range m.Items {
		yy := iy + i
		if it.Separator {
			s.HLine(ix, yy, iw, glHoriz, Style{Fg: theme.Border})
			continue
		}
		st := stPlain()
		if i == m.sel {
			st = stSel()
			s.FillRect(ix, yy, iw, 1, stSel())
		}
		if it.Disabled {
			st = stDim()
		} else if it.Danger {
			st = stDanger()
			if i == m.sel {
				st = stDanger().Bold()
			}
		}
		s.Text(ix, yy, truncateFits(it.Label, iw), st)
		if it.Accelerator != "" {
			s.TextRight(ix+iw-1, yy, truncateFits(it.Accelerator, iw-StringWidth(it.Label)-1), stDim())
		}
	}
}

func (m *Menu) HandleKey(k KeyEvent) bool {
	switch k.Key {
	case KeyUp:
		if len(m.Items) > 0 {
			m.sel = (m.sel - 1 + len(m.Items)) % len(m.Items)
			m.skipDisabled(-1)
		}
	case KeyDown:
		if len(m.Items) > 0 {
			m.sel = (m.sel + 1) % len(m.Items)
			m.skipDisabled(1)
		}
	case KeyEnter:
		if it := m.Items[m.sel]; !it.Disabled && !it.Separator && m.OnPick != nil {
			m.OnPick(m.sel)
		}
	default:
		return false
	}
	return true
}

func (m *Menu) HandleMouse(me MouseEvent) bool {
	x, y, w, h := m.Rect()
	if me.X < x || me.X >= x+w || me.Y < y || me.Y >= y+h {
		return me.Type == MouseMotion
	}
	if me.Y == y || me.Y == y+h-1 {
		return true // border
	}
	i := me.Y - y - 1
	if i < 0 || i >= len(m.Items) {
		return true
	}
	switch me.Type {
	case MouseMotion:
		m.sel = i
	case MousePress:
		if it := m.Items[i]; !it.Disabled && !it.Separator && m.OnPick != nil {
			m.sel = i
			m.OnPick(i)
		}
	}
	return true
}

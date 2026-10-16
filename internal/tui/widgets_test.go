package tui

import (
	"bytes"
	"testing"
)

func testCols() []Column {
	return []Column{
		{Title: "Name", Flex: true},
		{Title: "Size", Width: 8, Right: true},
	}
}

func newTestTable(n int) *Table {
	t := NewTable("Objects", testCols()...)
	t.SetRect(0, 0, 60, 12)
	t.Focused = true
	t.Multi = true
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Cells: []string{"row" + string(rune('a'+i)), "1 KiB"}}
	}
	t.SetRows(rows)
	t.MoveSel(0) // land the selection on row 0
	return t
}

func TestTableNavigationAndHooks(t *testing.T) {
	tbl := newTestTable(20)
	if tbl.Sel != 0 {
		t.Fatalf("MoveSel(0) should land on 0, got %d", tbl.Sel)
	}
	// Standing above the list: the first step down must ENTER at row 0,
	// not skip over it (the -1 clamp used to swallow row zero).
	tbl.Sel = -1
	tbl.MoveSel(1)
	if tbl.Sel != 0 {
		t.Fatalf("down from unselected must land on 0, got %d", tbl.Sel)
	}
	tbl.MoveSel(1)
	if tbl.Sel != 1 {
		t.Fatalf("second down: Sel=%d", tbl.Sel)
	}
	tbl.Sel = 0 // restore the baseline the walk below starts from
	var selected []string
	tbl.OnSelect = func(r *Row) { selected = append(selected, r.Cells[0]) }
	var activated string
	tbl.OnActivate = func(r *Row) { activated = r.Cells[0] }

	tbl.HandleKey(KeyEvent{Key: KeyDown})
	tbl.HandleKey(KeyEvent{Key: KeyDown})
	if tbl.Sel != 2 {
		t.Fatalf("two downs: Sel=%d", tbl.Sel)
	}
	if len(selected) != 2 || selected[1] != "rowc" {
		t.Fatalf("OnSelect fired wrong: %v", selected)
	}
	tbl.HandleKey(KeyEvent{Key: KeyUp})
	if tbl.Sel != 1 {
		t.Fatalf("up: Sel=%d", tbl.Sel)
	}
	tbl.HandleKey(KeyEvent{Key: KeyEnd})
	if tbl.Sel != 19 || tbl.Off <= 0 {
		t.Fatalf("End: Sel=%d Off=%d", tbl.Sel, tbl.Off)
	}
	tbl.HandleKey(KeyEvent{Key: KeyHome})
	if tbl.Sel != 0 || tbl.Off != 0 {
		t.Fatalf("Home: Sel=%d Off=%d", tbl.Sel, tbl.Off)
	}
	// Page down moves by the visible row count (60x12 → 8 rows).
	tbl.HandleKey(KeyEvent{Key: KeyPgDn})
	if tbl.Sel != 8 {
		t.Fatalf("PgDn: Sel=%d", tbl.Sel)
	}
	tbl.HandleKey(KeyEvent{Key: KeyEnter})
	if activated != "rowi" {
		t.Fatalf("Enter activates selected, got %q", activated)
	}
	// At the bottom edge the selection clamps.
	tbl.HandleKey(KeyEvent{Key: KeyEnd})
	tbl.HandleKey(KeyEvent{Key: KeyDown})
	if tbl.Sel != 19 {
		t.Fatalf("down at edge: Sel=%d", tbl.Sel)
	}
	// Unfocused, the table ignores keys.
	tbl.Focused = false
	if tbl.HandleKey(KeyEvent{Key: KeyDown}) {
		t.Fatal("unfocused table must not consume keys")
	}
	tbl.Focused = true
	// Unknown keys fall through.
	if tbl.HandleKey(KeyEvent{Key: KeyF12}) {
		t.Fatal("unknown key must not be consumed")
	}
}

func TestTableMarksAndFallback(t *testing.T) {
	tbl := newTestTable(5)
	tbl.HandleKey(KeyEvent{Key: KeyRune, Rune: ' '}) // mark row 0, move to 1
	if !tbl.Marks[0] || tbl.Sel != 1 {
		t.Fatalf("space: marks=%v sel=%d", tbl.Marks, tbl.Sel)
	}
	tbl.HandleKey(KeyEvent{Key: KeyRune, Rune: ' '}) // mark row 1, move to 2
	if got := tbl.MarkedRows(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("marked rows: %v", got)
	}
	// No marks → falls back to the selected row.
	tbl.Marks = map[int]bool{}
	if got := tbl.MarkedRows(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("fallback: %v", got)
	}
	// Empty table → no fallback at all.
	tbl.SetRows(nil)
	if got := tbl.MarkedRows(); len(got) != 0 {
		t.Fatalf("empty table fallback: %v", got)
	}
	if tbl.SelectedRow() != nil {
		t.Fatal("empty table has no selected row")
	}
}

func TestTableSetRowsPreservesSelection(t *testing.T) {
	tbl := newTestTable(10)
	tbl.Sel = 4
	tbl.Marks[4] = true
	rows := make([]Row, 8)
	for i := range rows {
		rows[i] = Row{Cells: []string{"x"}}
	}
	tbl.SetRows(rows)
	if tbl.Sel != 4 {
		t.Fatalf("selection should survive by index, got %d", tbl.Sel)
	}
	if len(tbl.Marks) != 0 {
		t.Fatal("SetRows must clear marks")
	}
	rows = rows[:2]
	tbl.SetRows(rows)
	if tbl.Sel != 1 {
		t.Fatalf("shrunk data clamps selection, got %d", tbl.Sel)
	}
}

func TestTableColWidths(t *testing.T) {
	tbl := NewTable("t", testCols()...)
	tbl.Multi = true
	ws := tbl.colWidths(58)
	if ws[0] != 46 || ws[1] != 8 {
		t.Fatalf("colWidths: want [46 8], got %v", ws)
	}
	tbl.Multi = false
	ws = tbl.colWidths(58)
	if ws[0] != 48 || ws[1] != 8 {
		t.Fatalf("colWidths no gutter: want [48 8], got %v", ws)
	}
	// Starved flex column still keeps its minimum.
	ws = tbl.colWidths(10)
	if ws[0] < 8 {
		t.Fatalf("flex minimum: %v", ws)
	}
}

func TestTableMouseGrammar(t *testing.T) {
	tbl := newTestTable(20)
	var picked string
	tbl.OnSelect = func(r *Row) { picked = r.Cells[0] }
	var activated int
	tbl.OnActivate = func(r *Row) { activated++ }

	// Click on the third visible row (grid rows start at y=3).
	if !tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Button: ButtonLeft, Type: MousePress}) {
		t.Fatal("press inside table must be consumed")
	}
	if tbl.Sel != 2 || picked != "rowc" {
		t.Fatalf("row click: Sel=%d picked=%q", tbl.Sel, picked)
	}

	// Double-click on the same row activates.
	tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Button: ButtonLeft, Type: MousePress})
	if activated != 1 {
		t.Fatalf("double-click must activate, got %d", activated)
	}

	// Ctrl+click toggles a mark without disturbing others.
	tbl.HandleMouse(MouseEvent{X: 10, Y: 4, Button: ButtonLeft, Type: MousePress, Mod: ModCtrl})
	tbl.HandleMouse(MouseEvent{X: 10, Y: 6, Button: ButtonLeft, Type: MousePress, Mod: ModCtrl})
	if !tbl.Marks[1] || !tbl.Marks[3] {
		t.Fatalf("ctrl-clicks must mark: %v", tbl.Marks)
	}
	if tbl.Sel != 3 {
		t.Fatalf("ctrl-click still seats selection, Sel=%d", tbl.Sel)
	}
	tbl.HandleMouse(MouseEvent{X: 10, Y: 4, Button: ButtonLeft, Type: MousePress, Mod: ModCtrl})
	if tbl.Marks[1] {
		t.Fatal("second ctrl-click must unmark")
	}

	// Header click fires the sort hook with the hit column.
	var hdrCol = -1
	tbl.OnHeader = func(c int) { hdrCol = c }
	tbl.HandleMouse(MouseEvent{X: 52, Y: 1, Button: ButtonLeft, Type: MousePress})
	if hdrCol != 1 {
		t.Fatalf("header click on Size column: got %d", hdrCol)
	}
	tbl.HandleMouse(MouseEvent{X: 5, Y: 1, Button: ButtonLeft, Type: MousePress})
	if hdrCol != 0 {
		t.Fatalf("header click on Name column: got %d", hdrCol)
	}

	// Wheel scrolls by 3 and clamps at both ends (no selection pinned:
	// the visibility clamp would otherwise pull the viewport back).
	tbl.Sel = -1
	tbl.Off = 0
	tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Type: MouseWheelUp})
	if tbl.Off != 0 {
		t.Fatal("wheel up at top must stay")
	}
	tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Type: MouseWheelDown})
	tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Type: MouseWheelDown})
	if tbl.Off != 6 {
		t.Fatalf("two wheel downs: Off=%d", tbl.Off)
	}
	tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Type: MouseWheelUp})
	if tbl.Off != 3 {
		t.Fatalf("wheel up: Off=%d", tbl.Off)
	}
	tbl.Sel = 2
	// Hover only highlights when focused.
	tbl.HandleMouse(MouseEvent{X: 10, Y: 5, Type: MouseMotion})
	if tbl.hover != tbl.Off+2 {
		t.Fatalf("hover row: %d", tbl.hover)
	}

	// Outside the rect: not consumed.
	if tbl.HandleMouse(MouseEvent{X: 61, Y: 5, Type: MousePress}) {
		t.Fatal("press outside must not be consumed")
	}
}

func TestTableDrawStructure(t *testing.T) {
	tbl := newTestTable(3)
	tbl.Marks[0] = true
	tbl.Marks[1] = true
	s := NewScreen(60, 12, &bytes.Buffer{})
	tbl.Draw(s)
	lines := s.PlainLines()
	if FindText(lines, "Objects") != 0 {
		t.Fatal("title missing")
	}
	if FindText(lines, "2 selected") != 0 {
		t.Fatal("multi-selection hint missing")
	}
	if FindText(lines, "Name") < 0 || FindText(lines, "Size") < 0 {
		t.Fatal("column headers missing")
	}
	if FindText(lines, "rowa") < 0 || FindText(lines, "rowc") < 0 {
		t.Fatal("row content missing")
	}
	if FindText(lines, string(glCheck)) != 3 {
		t.Fatalf("mark glyph must sit on the first row line, got line %d", FindText(lines, string(glCheck)))
	}
}

func TestTextInputEditing(t *testing.T) {
	ti := &TextInput{}
	ti.SetRect(0, 0, 40, 1)
	var changed int
	ti.OnChange = func(string) { changed++ }
	for _, r := range "hello" {
		ti.HandleKey(KeyEvent{Key: KeyRune, Rune: r})
	}
	if ti.Value() != "hello" || changed != 5 {
		t.Fatalf("typing: value=%q changed=%d", ti.Value(), changed)
	}
	ti.HandleKey(KeyEvent{Key: KeyLeft})
	ti.HandleKey(KeyEvent{Key: KeyLeft})
	ti.HandleKey(KeyEvent{Key: KeyRune, Rune: 'X'}) // helXlo? no: insert at 3 → helXlo
	if ti.Value() != "helXlo" {
		t.Fatalf("insert at cursor: %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyBackspace})
	if ti.Value() != "hello" {
		t.Fatalf("backspace: %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyEnd})
	ti.HandleKey(KeyEvent{Key: KeyDelete}) // no-op at the end
	if ti.Value() != "hello" {
		t.Fatalf("delete at end must be a no-op: %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyHome})
	ti.HandleKey(KeyEvent{Key: KeyDelete})
	if ti.Value() != "ello" {
		t.Fatalf("delete at start: %q", ti.Value())
	}
	// Kill-line shortcuts.
	ti.SetValue("hello world")
	ti.HandleKey(KeyEvent{Key: KeyLeft, Mod: ModCtrl}) // word-left over "world"
	ti.cur = 6
	ti.HandleKey(KeyEvent{Key: KeyRune, Rune: 'k', Mod: ModCtrl})
	if ti.Value() != "hello " {
		t.Fatalf("Ctrl+K kill to end: %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyEnd})
	ti.HandleKey(KeyEvent{Key: KeyRune, Rune: 'u', Mod: ModCtrl})
	if ti.Value() != "" {
		t.Fatalf("Ctrl+U kill to start: %q", ti.Value())
	}
	// Modified runes other than Ctrl fall through untouched.
	ti.SetValue("abc")
	if ti.HandleKey(KeyEvent{Key: KeyRune, Rune: 'x', Mod: ModAlt}) {
		t.Fatal("Alt+rune must not be consumed by the input")
	}
	if ti.Value() != "abc" {
		t.Fatalf("Alt+rune must not edit: %q", ti.Value())
	}
	// Paste inserts at the cursor.
	ti.SetValue("")
	ti.HandlePaste("pas")
	ti.HandlePaste("ted")
	if ti.Value() != "pasted" {
		t.Fatalf("paste: %q", ti.Value())
	}
}

func TestTextInputWordMotion(t *testing.T) {
	ti := &TextInput{}
	ti.SetValue("ab cd ef")
	if ti.cur != 8 {
		t.Fatalf("SetValue seats cursor at end, got %d", ti.cur)
	}
	if got := ti.wordLeft(); got != 6 {
		t.Fatalf("wordLeft from end: want 6, got %d", got)
	}
	ti.cur = 6
	if got := ti.wordLeft(); got != 3 {
		t.Fatalf("wordLeft mid: want 3, got %d", got)
	}
	ti.cur = 3
	if got := ti.wordRight(); got != 6 {
		t.Fatalf("wordRight: want 6, got %d", got)
	}
}

func TestTextInputHistoryAndSubmit(t *testing.T) {
	ti := &TextInput{}
	ti.SetRect(0, 0, 40, 1)
	var submitted []string
	var cancelled bool
	ti.OnSubmit = func(v string) { submitted = append(submitted, v) }
	ti.OnCancel = func() { cancelled = true }

	for _, v := range []string{"one", "two", "two"} {
		ti.SetValue(v)
		ti.HandleKey(KeyEvent{Key: KeyEnter})
	}
	if len(ti.History) != 2 {
		t.Fatalf("history dedup: %v", ti.History)
	}
	if len(submitted) != 3 {
		t.Fatalf("every Enter submits: %v", submitted)
	}
	ti.HandleKey(KeyEvent{Key: KeyUp})
	if ti.Value() != "two" {
		t.Fatalf("recall last: %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyUp})
	if ti.Value() != "one" {
		t.Fatalf("recall older: %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyDown})
	ti.HandleKey(KeyEvent{Key: KeyDown})
	if ti.Value() != "" {
		t.Fatalf("recall past the newest clears, got %q", ti.Value())
	}
	ti.HandleKey(KeyEvent{Key: KeyEsc})
	if !cancelled {
		t.Fatal("Esc must cancel")
	}
}

func TestTextInputClick(t *testing.T) {
	ti := &TextInput{}
	ti.SetRect(5, 2, 20, 1)
	ti.SetValue("hello")
	ti.HandleMouse(MouseEvent{X: 5 + 3, Y: 2, Button: ButtonLeft, Type: MousePress})
	if ti.cur != 3 {
		t.Fatalf("click maps to rune index, got %d", ti.cur)
	}
	// The field's own left edge clamps to 0.
	ti.HandleMouse(MouseEvent{X: 5, Y: 2, Button: ButtonLeft, Type: MousePress})
	if ti.cur != 0 {
		t.Fatalf("left edge clamp: %d", ti.cur)
	}
	// Outside the row: not consumed.
	if ti.HandleMouse(MouseEvent{X: 6, Y: 3, Button: ButtonLeft, Type: MousePress}) {
		t.Fatal("press outside must not be consumed")
	}
}

func TestDialogKeysAndButtons(t *testing.T) {
	d := &Dialog{Title: "Confirm", Width: 40, Height: 10}
	d.SetRect(0, 0, 40, 10)
	var fired []string
	d.Buttons = []DialogButton{
		{Label: "Cancel", Action: func() { fired = append(fired, "cancel") }},
		{Label: "Delete", Style: Style{Fg: theme.Danger}, Action: func() { fired = append(fired, "delete") }},
	}
	// Content keys get first refusal.
	var sawContent bool
	d.OnKey = func(k KeyEvent, _ *Dialog) bool {
		if k.Key == KeyF5 {
			sawContent = true
			return true
		}
		return false
	}
	d.HandleKey(KeyEvent{Key: KeyF5})
	if !sawContent {
		t.Fatal("OnKey must see keys first")
	}
	// Roving focus: Tab cycles forward, Enter fires the focused verb.
	d.HandleKey(KeyEvent{Key: KeyTab})
	d.HandleKey(KeyEvent{Key: KeyEnter})
	if len(fired) != 1 || fired[0] != "delete" {
		t.Fatalf("tab+enter: %v", fired)
	}
	d.HandleKey(KeyEvent{Key: KeyLeft})
	d.HandleKey(KeyEvent{Key: KeyEnter})
	if fired[1] != "cancel" {
		t.Fatalf("left+enter: %v", fired)
	}
	// Unknown keys are swallowed (modal), Esc cancels.
	if !d.HandleKey(KeyEvent{Key: KeyF12}) {
		t.Fatal("modal must swallow unknown keys")
	}
	var cancelled bool
	d.OnCancel = func() { cancelled = true }
	d.HandleKey(KeyEvent{Key: KeyEsc})
	if !cancelled {
		t.Fatal("Esc must cancel")
	}
}

func TestDialogMouseFooter(t *testing.T) {
	d := &Dialog{Width: 40, Height: 10}
	d.SetRect(0, 0, 40, 10)
	var fired string
	d.Buttons = []DialogButton{
		{Label: "Cancel", Action: func() { fired = "cancel" }},
		{Label: "Delete", Action: func() { fired = "delete" }},
	}
	// Footer row is inner bottom: y = 8. Buttons right-aligned.
	if !d.HandleMouse(MouseEvent{X: 32, Y: 8, Button: ButtonLeft, Type: MousePress}) {
		t.Fatal("footer press must be consumed")
	}
	if fired != "delete" {
		t.Fatalf("footer hit: %q", fired)
	}
	d.HandleMouse(MouseEvent{X: 22, Y: 8, Button: ButtonLeft, Type: MousePress})
	if fired != "cancel" {
		t.Fatalf("second footer hit: %q", fired)
	}
	// Body press is swallowed, outside press falls through to the face.
	if !d.HandleMouse(MouseEvent{X: 20, Y: 4, Button: ButtonLeft, Type: MousePress}) {
		t.Fatal("body press must be swallowed by the modal")
	}
	if d.HandleMouse(MouseEvent{X: 45, Y: 4, Button: ButtonLeft, Type: MousePress}) {
		t.Fatal("outside press must reach the face")
	}
}

func TestDialogLayoutClampsAndCenters(t *testing.T) {
	d := &Dialog{Width: 30, Height: 8}
	d.Layout(100, 40)
	x, y, w, h := d.Rect()
	if x != 35 || y != 16 || w != 30 || h != 8 {
		t.Fatalf("layout: %d,%d %dx%d", x, y, w, h)
	}
	d.Width, d.Height = 500, 500
	d.Layout(100, 40)
	_, _, w, h = d.Rect()
	if w != 96 || h != 36 {
		t.Fatalf("oversize clamp: %dx%d", w, h)
	}
}

func TestMenuGrammar(t *testing.T) {
	m := &Menu{Items: []MenuItem{
		{Label: "Rename", Accelerator: "F2"},
		{Label: "Delete", Accelerator: "Del", Danger: true},
		{Separator: true},
		{Label: "Properties", Accelerator: "Alt+Enter"},
		{Label: "Disabled", Disabled: true},
	}}
	if got := m.Width(); got != 26 {
		t.Fatalf("menu width: want 26, got %d", got)
	}
	if got := m.Height(); got != 7 {
		t.Fatalf("menu height: want 7, got %d", got)
	}

	// OpenAt clamps a near-edge popup inside the screen.
	m.OpenAt(70, 20, 80, 24)
	x, y, w, h := m.Rect()
	if x+w > 80 || y+h > 24 {
		t.Fatalf("clamp failed: %d,%d %dx%d", x, y, w, h)
	}

	// Keyboard: arrows wrap and skip the disabled; Enter picks.
	var picked = -1
	m.OnPick = func(i int) { picked = i }
	m.OpenAt(0, 0, 80, 24)              // skipDisabled lands on 0
	m.HandleKey(KeyEvent{Key: KeyDown}) // → 1
	m.HandleKey(KeyEvent{Key: KeyDown}) // → 3 (skips separator)
	m.HandleKey(KeyEvent{Key: KeyEnter})
	if picked != 3 {
		t.Fatalf("arrow walk pick: %d", picked)
	}
	// Down from 3 skips the disabled 4 and wraps to 0 in one move.
	m.HandleKey(KeyEvent{Key: KeyDown})
	if m.sel != 0 {
		t.Fatalf("disabled item must be skipped: sel=%d", m.sel)
	}
	// The pick guard itself: a selection forced onto a disabled item
	// (no skip in between) still refuses Enter.
	m.sel = 4
	m.HandleKey(KeyEvent{Key: KeyEnter})
	if picked != 3 {
		t.Fatal("disabled item must not pick")
	}
	m.sel = 0

	// Mouse: motion hovers, press picks, separator rows are inert.
	picked = -1
	m.HandleMouse(MouseEvent{X: 2, Y: 2, Type: MouseMotion})
	if m.sel != 1 {
		t.Fatalf("hover: sel=%d", m.sel)
	}
	m.HandleMouse(MouseEvent{X: 2, Y: 2, Button: ButtonLeft, Type: MousePress})
	if picked != 1 {
		t.Fatalf("press pick: %d", picked)
	}
	picked = -1
	m.HandleMouse(MouseEvent{X: 2, Y: 3, Button: ButtonLeft, Type: MousePress}) // separator
	if picked != -1 {
		t.Fatal("separator must not pick")
	}
	if m.HandleMouse(MouseEvent{X: 50, Y: 2, Button: ButtonLeft, Type: MousePress}) {
		t.Fatal("press outside must not be consumed")
	}
}

func TestMenuSkipDisabledOnOpen(t *testing.T) {
	m := &Menu{Items: []MenuItem{
		{Label: "No", Disabled: true},
		{Label: "Yes"},
	}}
	m.OpenAt(0, 0, 80, 24)
	if m.sel != 1 {
		t.Fatalf("opening must skip the disabled item, sel=%d", m.sel)
	}
}

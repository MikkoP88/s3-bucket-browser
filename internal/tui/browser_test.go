package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// seedStore writes a one-local-source store into a fresh config dir and
// points S3B_CONFIG at it (NO_KEYRING: the test host must never touch the
// real credential store).
func seedStore(t *testing.T, root string) {
	t.Helper()
	cfg := t.TempDir()
	blob, err := json.Marshal(map[string]any{
		"sources": []profile.Source{{ID: "lab", Name: "lab", Type: profile.TypeLocal, LocalRoot: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "profiles.json"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3B_CONFIG", cfg)
	t.Setenv("S3B_NO_KEYRING", "1")
}

func seedFiles(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel string, n int) {
		data := strings.Repeat("x", n)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha.txt", 12)
	write("beta.bin", 4096)
	write("gamma.md", 100)
	write("docs/notes.txt", 30)
}

// newTestBrowser builds the full face over a seeded local source at a
// fixed geometry, answering (app, browser, sourceRoot) — the local
// engine absolutizes exactly this path, so it is the disk oracle. The
// test never runs the loop: events are pumped by hand through
// app.handle, and frames are drawn straight off the grid (no Flush —
// nothing may reach the test host's stdout).
func newTestBrowser(t *testing.T) (*App, *Browser, string) {
	t.Helper()
	root := t.TempDir()
	seedFiles(t, root)
	seedStore(t, root)
	t.Setenv("S3B_TUI_SIZE", "100x24")

	app, err := NewApp()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(app, store, "1.2.0-test")
	app.SetRoot(b)
	app.resizeRoot()
	t.Cleanup(b.Close)
	t.Cleanup(app.term.Close)
	return app, b, root
}

// pump drains every queued event through the real routing chain.
func pumpEvents(t *testing.T, app *App) {
	t.Helper()
	for {
		select {
		case ev := <-app.events:
			app.handle(ev)
		default:
			return
		}
	}
}

// waitRows polls until the listing settles at n rows (engine listings
// land from goroutines; the rig waits them out instead of sleeping).
func waitRows(t *testing.T, app *App, b *Browser, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pumpEvents(t, app)
		if len(b.grid.Rows) == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("listing never reached %d rows (now %d: %s)", n, len(b.grid.Rows), rowNames(b))
}

// waitNamed polls until the listing seats the wanted row and has dropped
// the old one — a rename is count-invariant, so no row-count oracle can
// see it land.
func waitNamed(t *testing.T, app *App, b *Browser, want, gone string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pumpEvents(t, app)
		names := rowNames(b)
		if strings.Contains(names, want) && !strings.Contains(names, gone) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("listing never seated %q / dropped %q: %s", want, gone, rowNames(b))
}

func rowNames(b *Browser) string {
	var sb strings.Builder
	for i, r := range b.grid.Rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strings.TrimSpace(r.Cells[0]))
	}
	return sb.String()
}

// nameOf answers the trimmed row name (files: the plain name; folders
// keep their glyph prefix).
func nameOf(b *Browser, i int) string {
	return strings.TrimSpace(b.grid.Rows[i].Cells[0])
}

// draw paints the exact redraw composition — root, then every modal
// over its backdrop — and answers the plain-text frame.
func draw(t *testing.T, app *App, b *Browser) []string {
	t.Helper()
	s := app.Screen()
	s.Clear()
	b.Draw(s)
	w, h := s.Size()
	for _, m := range app.modals {
		s.FillRect(0, 0, w, h, Style{Bg: theme.Overlay})
		m.Layout(w, h)
		m.Draw(s)
	}
	return s.PlainLines()
}

func key(r rune) KeyEvent { return KeyEvent{Key: KeyRune, Rune: r} }

// cellIndexOf finds sub in a drawn row by scanning cells — a plain line
// carries byte offsets (the box borders are multi-byte runes), not
// columns, and mouse coordinates are columns.
func cellIndexOf(t *testing.T, s *Screen, y int, sub string) int {
	t.Helper()
	rs := []rune(sub)
	w, _ := s.Size()
	for x := 0; x+StringWidth(sub) <= w; x++ {
		match := true
		for j, r := range rs {
			if s.Cell(x+j, y).R != r {
				match = false
				break
			}
		}
		if match {
			return x
		}
	}
	return -1
}

// typeKeys sends rune keys through the full chain (command bar, filter,
// modal inputs all live on this road).
func typeKeys(t *testing.T, app *App, s string) {
	t.Helper()
	for _, r := range s {
		app.handle(key(r))
	}
}

// walkTo moves the grid selection with real Down keys until the wanted
// row is selected.
func walkTo(t *testing.T, app *App, b *Browser, name string) {
	t.Helper()
	for range 40 {
		if r := b.grid.SelectedRow(); r != nil && strings.Contains(strings.TrimSpace(r.Cells[0]), name) {
			return
		}
		app.handle(KeyEvent{Key: KeyDown})
	}
	t.Fatalf("walkTo %q never landed (sel=%d rows=%s)", name, b.grid.Sel, rowNames(b))
}

// waitXfer polls until the transfer row reaches a verdict.
func waitXfer(t *testing.T, app *App, b *Browser, i int) *xferRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pumpEvents(t, app)
		if i < len(b.xfer) && (b.xfer[i].Done || b.xfer[i].Err != "") {
			return b.xfer[i]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("transfer row %d never finished: %+v", i, b.xfer)
	return nil
}

func TestBrowserLiveLocalFS(t *testing.T) {
	app, b, root := newTestBrowser(t)

	// ── boot: the source dials and the root listing lands ─────────────
	waitRows(t, app, b, 4)
	if got := rowNames(b); !strings.Contains(got, "docs") || !strings.Contains(got, "alpha.txt") {
		t.Fatalf("root listing wrong: %s", got)
	}
	if b.grid.Rows[0].Cells[0] == "" || !strings.Contains(nameOf(b, 0), "docs") {
		t.Fatalf("folders must sort first, got %s", rowNames(b))
	}
	if b.grid.Rows[1].Cells[1] != "12 B" || b.grid.Rows[2].Cells[1] != "4.0 KiB" {
		t.Fatalf("sizes not humanized: %q %q", b.grid.Rows[1].Cells[1], b.grid.Rows[2].Cells[1])
	}
	if b.loading {
		t.Fatal("loading flag must clear once the listing lands")
	}
	lines := draw(t, app, b)
	if FindText(lines, "Objects") < 0 || FindText(lines, "Sources") < 0 {
		t.Fatalf("panels missing from frame: %q", lines[1])
	}
	if !strings.Contains(lines[0], "lab") || !strings.Contains(lines[0], "1.2.0-test") {
		t.Fatalf("header must carry source crumb and version: %q", lines[0])
	}
	if FindText(lines, "alpha.txt") < 0 {
		t.Fatal("listing rows missing from the drawn frame")
	}

	// ── pane toggle (Tab) ─────────────────────────────────────────────
	app.handle(KeyEvent{Key: KeyTab})
	draw(t, app, b) // Focused flags are a Draw-time projection of pane
	if !b.pane || !b.src.Focused || b.grid.Focused {
		t.Fatal("Tab must focus the source rail")
	}
	app.handle(KeyEvent{Key: KeyTab})
	draw(t, app, b)
	if b.pane || b.src.Focused || !b.grid.Focused {
		t.Fatal("second Tab must hand focus back to the listing")
	}

	// ── stale-seq guard: an out-of-date listing must never land ───────
	app.Post(AppMsg{Kind: MsgList, Data: listResult{seq: b.listSeq - 1,
		entries: []listing.Entry{{Name: "STALE-ROW", IsDir: true}}}})
	pumpEvents(t, app)
	if strings.Contains(rowNames(b), "STALE-ROW") {
		t.Fatal("a stale-seq listing landed over the live one")
	}
	if FindText(draw(t, app, b), "STALE-ROW") >= 0 {
		t.Fatal("stale row leaked into the frame")
	}

	// ── descend into docs, back up, and the honest root refusal ───────
	walkTo(t, app, b, "docs")
	app.handle(KeyEvent{Key: KeyEnter})
	waitRows(t, app, b, 1)
	if !strings.Contains(rowNames(b), "notes.txt") {
		t.Fatalf("docs listing wrong: %s", rowNames(b))
	}
	if !strings.Contains(draw(t, app, b)[0], "docs") {
		t.Fatal("crumb must follow the descent")
	}
	app.handle(KeyEvent{Key: KeyBackspace})
	waitRows(t, app, b, 4)
	app.handle(KeyEvent{Key: KeyBackspace})
	waitRows(t, app, b, 4)
	if b.toast != "at the source root" || b.toastOk {
		t.Fatalf("root up() must warn, got %q ok=%v", b.toast, b.toastOk)
	}

	// ── live filter, commit, and Esc's restore ────────────────────────
	app.handle(key('/'))
	typeKeys(t, app, "alp")
	if len(b.grid.Rows) != 1 || !strings.Contains(nameOf(b, 0), "alpha.txt") {
		t.Fatalf("live filter must narrow to alpha.txt, got %s", rowNames(b))
	}
	app.handle(KeyEvent{Key: KeyEnter})
	if b.filtering {
		t.Fatal("Enter must close the filter bar")
	}
	if b.filter != "alp" {
		t.Fatalf("committed filter must stick, got %q", b.filter)
	}
	if !strings.Contains(draw(t, app, b)[1], "alp") {
		t.Fatal("panel title must echo the filter")
	}
	app.handle(KeyEvent{Key: KeyEsc})
	if b.filter != "" || len(b.grid.Rows) != 4 {
		t.Fatalf("Esc must clear the filter, got %q rows=%s", b.filter, rowNames(b))
	}

	// ── sort by the Size header: click once big-first, again ascending ─
	lines = draw(t, app, b)
	hy := FindText(lines, "Size")
	if hy < 0 {
		t.Fatalf("no header row in frame: %q", lines[2])
	}
	hx := cellIndexOf(t, app.Screen(), hy, "Size")
	if hx < 0 {
		t.Fatalf("Size title not drawn on line %d", hy)
	}
	app.handle(MouseEvent{X: hx + 1, Y: hy, Type: MousePress, Button: ButtonLeft})
	order := rowNames(b)
	if !(strings.Contains(order, "beta.bin") && strings.Index(order, "beta.bin") < strings.Index(order, "gamma.md") &&
		strings.Index(order, "gamma.md") < strings.Index(order, "alpha.txt")) {
		t.Fatalf("size sort must run big-first: %s", order)
	}
	app.handle(MouseEvent{X: hx + 1, Y: hy, Type: MousePress, Button: ButtonLeft})
	order = rowNames(b)
	if !(strings.Index(order, "alpha.txt") < strings.Index(order, "gamma.md") &&
		strings.Index(order, "gamma.md") < strings.Index(order, "beta.bin")) {
		t.Fatalf("second size click must flip to ascending: %s", order)
	}

	// ── command bar: :mkdir zed writes a real folder and reloads ──────
	app.handle(key(':'))
	typeKeys(t, app, "mkdir zed")
	app.handle(KeyEvent{Key: KeyEnter})
	waitRows(t, app, b, 5)
	if _, err := os.Stat(filepath.Join(root, "zed")); err != nil {
		t.Fatalf("mkdir must create the folder on disk: %v", err)
	}
	pumpEvents(t, app)
	if !b.toastOk || !strings.Contains(b.toast, "zed") {
		t.Fatalf("mkdir must toast success, got %q ok=%v", b.toast, b.toastOk)
	}

	// ── F2 rename through the prompt dialog ───────────────────────────
	walkTo(t, app, b, "alpha.txt")
	app.handle(KeyEvent{Key: KeyF2})
	if m := app.TopModal(); m == nil || m.Title != "Rename" {
		t.Fatalf("F2 must open the Rename dialog, got %+v", app.TopModal())
	}
	app.handle(KeyEvent{Key: KeyRune, Rune: 'u', Mod: ModCtrl}) // kill the prefilled name
	typeKeys(t, app, "renamed.txt")
	app.handle(KeyEvent{Key: KeyEnter})
	waitNamed(t, app, b, "renamed.txt", "alpha.txt")
	if _, err := os.Stat(filepath.Join(root, "alpha.txt")); !os.IsNotExist(err) {
		t.Fatal("rename must move the file on disk")
	}
	if st, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil || st.Size() != 12 {
		t.Fatalf("renamed file must keep its content: %v", err)
	}

	// ── Del: Cancel is honest, Delete really deletes ──────────────────
	walkTo(t, app, b, "renamed.txt")
	app.handle(KeyEvent{Key: KeyDelete})
	if m := app.TopModal(); m == nil || m.Title != "Delete" {
		t.Fatalf("Del must open the confirm dialog, got %+v", app.TopModal())
	}
	lines = draw(t, app, b)
	if FindText(lines, "Delete 1 item(s)?") < 0 || FindText(lines, "renamed.txt") < 0 {
		t.Fatalf("delete dialog must name its victim: %q", lines)
	}
	app.handle(KeyEvent{Key: KeyEnter}) // focused button is Cancel
	if app.TopModal() != nil {
		t.Fatal("Cancel must close the dialog")
	}
	pumpEvents(t, app)
	waitRows(t, app, b, 5)
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatalf("Cancel must leave the file on disk: %v", err)
	}
	app.handle(KeyEvent{Key: KeyDelete})
	app.handle(KeyEvent{Key: KeyTab}) // focus → Delete
	app.handle(KeyEvent{Key: KeyEnter})
	waitRows(t, app, b, 4)
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); !os.IsNotExist(err) {
		t.Fatal("Delete must remove the file from disk")
	}
	pumpEvents(t, app)
	if !b.toastOk || !strings.Contains(b.toast, "deleted") {
		t.Fatalf("delete must toast success, got %q ok=%v", b.toast, b.toastOk)
	}

	// ── upload: a real local file lands in the listing ────────────────
	up := filepath.Join(t.TempDir(), "up-source.txt")
	if err := os.WriteFile(up, []byte("payload-123"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.upload(up)
	x := waitXfer(t, app, b, 0)
	if x.Err != "" || !x.Done || !strings.Contains(x.Label, "up-source.txt") {
		t.Fatalf("upload verdict wrong: %+v", x)
	}
	waitRows(t, app, b, 5)
	if !strings.Contains(rowNames(b), "up-source.txt") {
		t.Fatalf("uploaded file must appear in the listing: %s", rowNames(b))
	}
	lines = draw(t, app, b)
	if FindText(lines, "Transfers") < 0 || FindText(lines, "up-source.txt") < 0 {
		t.Fatal("transfers panel must show the finished upload")
	}
	app.handle(KeyEvent{Key: KeyF8})
	if FindText(draw(t, app, b), "Transfers") >= 0 {
		t.Fatal("F8 must hide the transfers panel")
	}

	// ── download: the round trip returns the exact bytes ──────────────
	walkTo(t, app, b, "up-source.txt")
	e := b.selected()
	if e == nil || e.Name != "up-source.txt" {
		t.Fatalf("download selection wrong: %+v", e)
	}
	dl := filepath.Join(t.TempDir(), "dl")
	if err := os.MkdirAll(dl, 0o755); err != nil {
		t.Fatal(err)
	}
	b.download([]listing.Entry{*e}, dl)
	x = waitXfer(t, app, b, 1)
	if x.Err != "" || !x.Done {
		t.Fatalf("download verdict wrong: %+v", x)
	}
	got, err := os.ReadFile(filepath.Join(dl, "up-source.txt"))
	if err != nil || string(got) != "payload-123" {
		t.Fatalf("download round trip must preserve bytes: %q %v", got, err)
	}

	// ── honest refusals: copy needs an S3 bucket ──────────────────────
	app.handle(key('c'))
	if b.toast != "copy needs an S3 bucket" || b.toastOk {
		t.Fatalf("local copy must be refused with a warning, got %q ok=%v", b.toast, b.toastOk)
	}

	// ── Alt+Enter properties ──────────────────────────────────────────
	walkTo(t, app, b, "gamma.md")
	app.handle(KeyEvent{Key: KeyEnter, Mod: ModAlt})
	if m := app.TopModal(); m == nil || m.Title != "Properties" {
		t.Fatalf("Alt+Enter must open Properties, got %+v", app.TopModal())
	}
	lines = draw(t, app, b)
	if FindText(lines, "gamma.md") < 0 || FindText(lines, "Size") < 0 {
		t.Fatalf("properties modal missing its rows: %v", lines)
	}
	app.handle(KeyEvent{Key: KeyEnter}) // Close
	if app.TopModal() != nil {
		t.Fatal("Close must dismiss the properties modal")
	}

	// ── F1 help ───────────────────────────────────────────────────────
	app.handle(KeyEvent{Key: KeyF1})
	if m := app.TopModal(); m == nil || m.Title != "Keys" {
		t.Fatalf("F1 must open Keys, got %+v", app.TopModal())
	}
	if FindText(draw(t, app, b), "switch source rail") < 0 {
		t.Fatal("help modal missing its key map")
	}
	app.handle(KeyEvent{Key: KeyEsc})
	if app.TopModal() != nil {
		t.Fatal("Esc must close the help modal")
	}

	// ── error toasts ride the warn tone ───────────────────────────────
	app.Post(AppMsg{Kind: MsgError, Data: "engine exploded"})
	pumpEvents(t, app)
	if !strings.Contains(b.toast, "engine exploded") || b.toastOk {
		t.Fatalf("MsgError must warn, got %q ok=%v", b.toast, b.toastOk)
	}

	// ── :q closes the loop ────────────────────────────────────────────
	app.handle(key(':'))
	typeKeys(t, app, "q")
	app.handle(KeyEvent{Key: KeyEnter})
	select {
	case <-app.quit:
	default:
		t.Fatal(":q must quit the app")
	}
}

// TestBrowserRailSwitch pins the seat law the battery found: activating
// a rail row must dial the source AND seat b.sel — the crumb and the
// rail highlight name the source being browsed, not the one the face
// booted into (an account-wide dial once listed its buckets live under
// the previous source's name). The keyboard road is driven here — Tab
// to the rail, Down to the row, Enter to activate — the same
// OnActivate the rail's double-click fires.
func TestBrowserRailSwitch(t *testing.T) {
	rootA := t.TempDir()
	seedFiles(t, rootA)
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootB, "only-b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	blob, err := json.Marshal(map[string]any{
		"sources": []profile.Source{
			{ID: "a", Name: "alpha-src", Type: profile.TypeLocal, LocalRoot: rootA},
			{ID: "b", Name: "beta-src", Type: profile.TypeLocal, LocalRoot: rootB},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "profiles.json"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3B_CONFIG", cfg)
	t.Setenv("S3B_NO_KEYRING", "1")
	t.Setenv("S3B_TUI_SIZE", "100x24")
	app, err := NewApp()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(app, store, "1.2.0-test")
	app.SetRoot(b)
	app.resizeRoot()
	t.Cleanup(b.Close)
	t.Cleanup(app.term.Close)

	waitRows(t, app, b, 4) // booted into alpha-src's listing
	app.handle(KeyEvent{Key: KeyTab})
	app.handle(KeyEvent{Key: KeyDown})
	app.handle(KeyEvent{Key: KeyEnter})
	waitRows(t, app, b, 1)
	if !strings.Contains(rowNames(b), "only-b.txt") {
		t.Fatalf("rail activation must list the second source: %s", rowNames(b))
	}
	if b.sel != 1 || b.src.Sel != 1 {
		t.Fatalf("the seat must follow the dial: b.sel=%d rail.Sel=%d", b.sel, b.src.Sel)
	}
	if head := draw(t, app, b)[0]; !strings.Contains(head, "beta-src") {
		t.Fatalf("crumb must name the seated source: %q", head)
	}
	// Re-activating the seated row is a no-op dial — no flash, same listing.
	app.handle(KeyEvent{Key: KeyEnter})
	pumpEvents(t, app)
	waitRows(t, app, b, 1)
	if b.toast != "" {
		t.Fatalf("re-activating the seated source must not flash: %q", b.toast)
	}
}

// TestBrowserS3PrefixArithmetic pins the S3 prefix grammar without a
// store behind it (a dead endpoint — New arms the client, reload just
// errors into a drained toast): JoinKey answers prefix FORM, trailing
// slash included, and every descent that added its own slash sent
// key// to MinIO for a hard 400 (XMinioInvalidObjectName — the battery
// found it; the local-only rigs never speak S3).
func TestBrowserS3PrefixArithmetic(t *testing.T) {
	cfg := t.TempDir()
	blob, err := json.Marshal(map[string]any{
		"sources": []profile.Source{{
			ID: "s", Name: "s", Type: profile.TypeS3,
			S3: &profile.Profile{Endpoint: "http://127.0.0.1:1", AccessKeyID: "k", SecretKey: "s", Region: "us-east-1", PathStyle: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "profiles.json"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3B_CONFIG", cfg)
	t.Setenv("S3B_NO_KEYRING", "1")
	t.Setenv("S3B_TUI_SIZE", "100x24")
	app, err := NewApp()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(app, store, "1.2.0-test")
	app.SetRoot(b)
	app.resizeRoot()
	t.Cleanup(b.Close)
	t.Cleanup(app.term.Close)

	// Seat a bucket by hand (the bucket row's own road needs a live
	// store), then drive the folder descent through the real chain.
	b.bucket, b.prefix = "bkt", ""
	listAs := func(es ...listing.Entry) {
		b.entries = es
		b.applyView()
		pumpEvents(t, app)
	}
	listAs(listing.Entry{Key: "tui/", Name: "tui", IsDir: true})
	app.handle(KeyEvent{Key: KeyEnter})
	if b.prefix != "tui/" {
		t.Fatalf("descent must seat prefix form, got %q", b.prefix)
	}
	listAs(listing.Entry{Key: "tui/sub/", Name: "sub", IsDir: true})
	app.handle(KeyEvent{Key: KeyEnter})
	if b.prefix != "tui/sub/" {
		t.Fatalf("nested descent must stay single-slashed, got %q", b.prefix)
	}

	// :cd s3:// — the URI leg answers bucket + prefix, root when bare.
	typeKeys(t, app, ":cd s3://bkt/docs")
	app.handle(KeyEvent{Key: KeyEnter})
	pumpEvents(t, app)
	if b.bucket != "bkt" || b.prefix != "docs/" {
		t.Fatalf(":cd s3://bkt/docs must seat docs/, got %q %q", b.bucket, b.prefix)
	}
	typeKeys(t, app, ":cd s3://bkt")
	app.handle(KeyEvent{Key: KeyEnter})
	pumpEvents(t, app)
	if b.bucket != "bkt" || b.prefix != "" {
		t.Fatalf("bare :cd s3://bkt must list the root, got %q %q", b.bucket, b.prefix)
	}

	// :cd with a relative target rides the same prefix form.
	typeKeys(t, app, ":cd docs")
	app.handle(KeyEvent{Key: KeyEnter})
	pumpEvents(t, app)
	if b.prefix != "docs/" {
		t.Fatalf("relative :cd must seat docs/, got %q", b.prefix)
	}
	if head := draw(t, app, b)[0]; !strings.Contains(head, "s3://bkt/docs") {
		t.Fatalf("crumb must print the seated prefix: %q", head)
	}
}

// TestBrowserNavFoldReset pins the navigation fold law the battery
// found under the wheel: the fold (grid Off) survives same-view
// reloads — the 100M-object gesture — but every view change (descent,
// ascent, :cd) opens the new listing from its top. A fold carried at
// the wheel floor into a two-row descended listing bounds-clamped into
// hiding the child's first row entirely above the fold. Every leg
// carries the fold at an Off that bounds-clamping alone would keep,
// so the Off==0 asserts name the reset, not the clamp.
func TestBrowserNavFoldReset(t *testing.T) {
	cfg := t.TempDir()
	blob, err := json.Marshal(map[string]any{
		"sources": []profile.Source{{
			ID: "s", Name: "s", Type: profile.TypeS3,
			S3: &profile.Profile{Endpoint: "http://127.0.0.1:1", AccessKeyID: "k", SecretKey: "s", Region: "us-east-1", PathStyle: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "profiles.json"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3B_CONFIG", cfg)
	t.Setenv("S3B_NO_KEYRING", "1")
	t.Setenv("S3B_TUI_SIZE", "100x24")
	app, err := NewApp()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(app, store, "1.2.0-test")
	app.SetRoot(b)
	app.resizeRoot()
	t.Cleanup(b.Close)
	t.Cleanup(app.term.Close)

	b.bucket, b.prefix = "bkt", ""
	listAs := func(es ...listing.Entry) {
		b.entries = es
		b.applyView()
		pumpEvents(t, app)
	}

	// Descent: the fold carried at 1 must open the 3-row child at 0.
	listAs(
		listing.Entry{Key: "tui/", Name: "tui", IsDir: true},
		listing.Entry{Key: "readme.md", Name: "readme.md"},
	)
	b.grid.Sel = 0
	b.grid.Off = 1
	app.handle(KeyEvent{Key: KeyEnter})
	if b.prefix != "tui/" {
		t.Fatalf("descent must seat prefix form, got %q", b.prefix)
	}
	listAs(
		listing.Entry{Key: "tui/seed.txt", Name: "seed.txt"},
		listing.Entry{Key: "tui/one.txt", Name: "one.txt"},
		listing.Entry{Key: "tui/two.txt", Name: "two.txt"},
	)
	if b.grid.Off != 0 {
		t.Fatalf("descent must open the child at the top, Off=%d", b.grid.Off)
	}

	// Ascent: the fold carried at 2 must open the parent at 0.
	b.grid.Off = 2
	app.handle(KeyEvent{Key: KeyBackspace})
	if b.prefix != "" {
		t.Fatalf("ascent must return to the bucket root, got %q", b.prefix)
	}
	listAs(
		listing.Entry{Key: "tui/", Name: "tui", IsDir: true},
		listing.Entry{Key: "readme.md", Name: "readme.md"},
		listing.Entry{Key: "other.txt", Name: "other.txt"},
	)
	if b.grid.Off != 0 {
		t.Fatalf("ascent must open the parent at the top, Off=%d", b.grid.Off)
	}

	// :cd — the command-bar road resets too.
	b.grid.Off = 2
	typeKeys(t, app, ":cd tui")
	app.handle(KeyEvent{Key: KeyEnter})
	pumpEvents(t, app)
	if b.prefix != "tui/" {
		t.Fatalf(":cd must seat the target, got %q", b.prefix)
	}
	if b.grid.Off != 0 {
		t.Fatalf(":cd must open the target at the top, Off=%d", b.grid.Off)
	}

	// Same-view refreshes keep the fold: F5, r, :refresh — the dead
	// endpoint's errored listings never SetRows, so the fold is the
	// only thing that could move it, and none of these do.
	b.grid.Off = 5
	app.handle(KeyEvent{Key: KeyF5})
	app.handle(key('r'))
	typeKeys(t, app, ":refresh")
	app.handle(KeyEvent{Key: KeyEnter})
	pumpEvents(t, app)
	if b.grid.Off != 5 {
		t.Fatalf("same-view refreshes must keep the fold, Off=%d", b.grid.Off)
	}

	// The account-root refusal is a same-view reload — the fold stays.
	b.bucket, b.prefix = "", ""
	b.grid.Off = 5
	app.handle(KeyEvent{Key: KeyBackspace})
	pumpEvents(t, app)
	if b.grid.Off != 5 {
		t.Fatalf("the root refusal must keep the fold, Off=%d", b.grid.Off)
	}
}

// TestTuiObjectKeyLaw pins the object-key half of the JoinKey contract.
// Prefixes ride prefix form (descent, :cd — pinned above), but KEYS must
// not: an upload composed as "dir/name/" lands as a folder marker whose
// bytes hide behind a synthetic directory, and mkdir's marker doubled
// its slash outright. The battery's first S3 write walk (TUI-10) found
// the whole verb family composing slash-tailed keys; this rig pins the
// arithmetic and the one synchronous verb site, the live law rides
// MinIO in verify.mjs.
func TestTuiObjectKeyLaw(t *testing.T) {
	if got := objKey("tui/", "name.txt"); got != "tui/name.txt" {
		t.Fatalf("objKey must trim the prefix form, got %q", got)
	}
	if got := objKey("", "name.txt"); got != "name.txt" {
		t.Fatalf("objKey at the bucket root must stay bare, got %q", got)
	}
	if got := objKey("a/b/", "sub", "f.txt"); got != "a/b/sub/f.txt" {
		t.Fatalf("objKey must join clean, got %q", got)
	}
	if got := objKey("", ""); got != "" {
		t.Fatalf("objKey of nothing must be empty, got %q", got)
	}

	// copyMarked composes clip keys through the real seat — the one
	// synchronous verb site, observable without a store behind it.
	cfg := t.TempDir()
	blob, err := json.Marshal(map[string]any{
		"sources": []profile.Source{{
			ID: "s", Name: "s", Type: profile.TypeS3,
			S3: &profile.Profile{Endpoint: "http://127.0.0.1:1", AccessKeyID: "k", SecretKey: "s", Region: "us-east-1", PathStyle: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "profiles.json"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3B_CONFIG", cfg)
	t.Setenv("S3B_NO_KEYRING", "1")
	t.Setenv("S3B_TUI_SIZE", "100x24")
	app, err := NewApp()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(app, store, "1.2.0-test")
	app.SetRoot(b)
	app.resizeRoot()
	t.Cleanup(b.Close)
	t.Cleanup(app.term.Close)
	b.bucket, b.prefix = "bkt", "tui/"
	b.entries = []listing.Entry{{Key: "tui/f.txt", Name: "f.txt"}}
	b.applyView()
	b.grid.Marks[0] = true
	pumpEvents(t, app)
	b.copyMarked()
	if len(b.clip) != 1 || b.clip[0].key != "tui/f.txt" {
		t.Fatalf("copy must carry a slash-free object key, got %+v", b.clip)
	}
}

func TestBrowserNoSources(t *testing.T) {
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "profiles.json"), []byte(`{"sources":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3B_CONFIG", cfg)
	t.Setenv("S3B_NO_KEYRING", "1")
	t.Setenv("S3B_TUI_SIZE", "100x24")
	app, err := NewApp()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(app, store, "t")
	app.SetRoot(b)
	defer b.Close()
	defer app.term.Close()
	if b.toast == "" || b.toastOk {
		t.Fatalf("empty store must greet with a hint, got %q ok=%v", b.toast, b.toastOk)
	}
	lines := draw(t, app, b)
	if FindText(lines, "No sources yet") < 0 {
		t.Fatal("the empty hint must reach the frame")
	}
	// Keying an engine verb with no source must refuse, not panic.
	app.handle(key('u'))
	if app.TopModal() != nil {
		t.Fatal("upload prompt must not open without a source")
	}
}

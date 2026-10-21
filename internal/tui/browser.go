package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

const (
	toastTTL     = 4 * time.Second
	xferThrottle = 50 * time.Millisecond
	statusHints  = " f1 help · f2 rename · f7 mkdir · del delete · u upload · d download · c copy · v paste · : cmd"
)

// Browser is the interactive face: a source rail on the left, the
// current listing on the right, a dim status bar of key hints, a ':'
// command bar, a '/' filter, and the GUI's verb grammar — F2 rename,
// Del delete, F7 new folder, uploads and downloads with live progress,
// Alt+Enter properties. It rides pkg/core exactly as the CLI does.
//
// Concurrency law: engine calls run on their own goroutines and never
// touch face state — they cross app.Post as AppMsg envelopes, and every
// mutation happens on the loop goroutine (event handlers and onMsg).
type Browser struct {
	BaseWidget
	app    *App
	store  *profile.Store
	ctx    context.Context
	cancel context.CancelFunc

	src  *Table // source rail
	grid *Table // listing
	pane bool   // false = listing focused, true = sources focused

	sources []profile.Source
	sel     int

	// Engine for the current source: exactly one is live. S3 prefixes
	// are kept without the leading slash ("" = bucket root, "photos/"
	// inside); remote prefixes are "/"-anchored paths.
	fs     remotefs.FS
	s3c    *s3client.Client
	bucket string // S3 only; "" = account-wide bucket view
	prefix string

	entries []listing.Entry
	view    []listing.Entry // filtered + sorted projection the grid shows
	sortCol int             // 0 name, 1 size, 2 modified
	sortAsc bool
	loading bool
	listSeq int // stale-listing guard

	filter     string
	filterPrev string
	filtering  bool
	filterIn   *TextInput

	cmdOpen bool
	cmdIn   *TextInput

	toast   string
	toastOk bool
	toastT  time.Time

	xfer   []*xferRow
	xferOn bool
	clip   []clipEntry

	version string
}

// clipEntry is one copied S3 object awaiting paste.
type clipEntry struct{ bucket, key, name string }

// xferRow is one transfer in the panel. It is created on the loop
// goroutine and mutated only there (progress crosses as TransferTick).
type xferRow struct {
	Label string
	Sent  int64
	Totl  int64
	Done  bool
	Err   string
}

// TransferTick is the envelope transfers and background verbs post to
// the loop: progress updates (MsgProgress) and completion (MsgDone).
type TransferTick struct {
	Row    *xferRow // nil for non-transfer verbs
	Sent   int64
	Total  int64
	Err    error
	Done   bool
	Reload bool   // refetch the listing after a clean landing
	OkMsg  string // success toast for verb completions
}

// listResult is the MsgList payload.
type listResult struct {
	seq     int
	entries []listing.Entry
	err     error
}

// NewBrowser builds the face; version lands in the header.
func NewBrowser(app *App, store *profile.Store, version string) *Browser {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Browser{
		app:     app,
		store:   store,
		ctx:     ctx,
		cancel:  cancel,
		sortAsc: true,
		prefix:  "/",
		version: version,
	}
	b.sources = store.SortedSources()

	b.src = NewTable("Sources", Column{Title: "Source", Flex: true})
	b.src.OnActivate = func(r *Row) { b.pickSourceByRow(r) }

	b.grid = NewTable("Objects",
		Column{Title: "Name", Flex: true},
		Column{Title: "Size", Width: 10, Right: true},
		Column{Title: "Modified", Width: 16, Right: true},
	)
	b.grid.Multi = true
	b.grid.OnActivate = func(*Row) { b.openEntry() }
	b.grid.OnHeader = func(col int) { b.sortBy(col) }

	b.cmdIn = &TextInput{Prompt: ":"}
	b.cmdIn.OnSubmit = func(v string) { b.runCommand(v) }
	b.cmdIn.OnCancel = func() { b.cmdOpen = false }
	b.filterIn = &TextInput{Prompt: "/"}
	b.filterIn.OnChange = func(v string) { b.filter = v; b.applyView() }
	b.filterIn.OnSubmit = func(v string) { b.setFilter(v) }
	b.filterIn.OnCancel = func() {
		b.filter = b.filterPrev
		b.filtering = false
		b.applyView()
	}

	app.OnMsg(b.onMsg)
	app.OnQuitHint = func() { b.flash("Ctrl+C again to quit", false) }
	app.OnPasteTarget(func() pasteReceiver {
		if ti := b.activeInput(); ti != nil {
			return ti
		}
		return nil
	})

	b.refreshSources()
	if len(b.sources) > 0 {
		b.dial(0)
	} else {
		b.flash("No sources yet — add one with: s3b source add", false)
	}
	return b
}

// Close tears the engine down (called once the loop exits).
func (b *Browser) Close() {
	b.cancel()
	if b.fs != nil {
		b.fs.Close()
	}
}

// ── drawing ───────────────────────────────────────────────────────────────

func (b *Browser) Draw(s *Screen) {
	w, h := s.Size()
	if w < 40 || h < 8 {
		s.Text(0, 0, "terminal too small", stDanger())
		return
	}
	xferH := 0
	if b.xferOn && len(b.xfer) > 0 {
		xferH = min(max(len(b.xfer)+3, 5), h/2)
	}
	bodyH := h - 2 - xferH

	// Header: identity, breadcrumb, version.
	s.Text(1, 0, " s3b ", stAccent().Bold())
	s.Text(7, 0, truncateFits(b.crumb(), w-StringWidth(" s3b ")-StringWidth(b.version)-10), stDim())
	s.TextRight(w-2, 0, b.version+" ", stDim())

	// Source rail + listing.
	railW := min(30, max(w/4, 18))
	b.src.SetRect(0, 1, railW, bodyH)
	b.src.Focused = b.pane
	b.src.Draw(s)

	b.grid.SetRect(railW, 1, w-railW, bodyH)
	b.grid.Focused = !b.pane
	b.grid.Title = "Objects"
	if b.loading {
		b.grid.Title = "Objects · loading" + string(glyphEllipsis)
	} else if b.filter != "" {
		b.grid.Title = "Objects · " + b.filter
	}
	b.grid.Draw(s)

	// Transfer panel.
	if xferH > 0 {
		bx := Box{BaseWidget: BaseWidget{x: 0, y: h - 1 - xferH, w: w, h: xferH}, Title: "Transfers", Hint: "f8"}
		bx.Draw(s)
		ix, iy, iw, _ := bx.Inner()
		for i, xr := range b.xfer[:min(len(b.xfer), xferH-2)] {
			yy := iy + i
			st := stPlain()
			switch {
			case xr.Err != "":
				st = stDanger()
			case xr.Done:
				st = stOk()
			}
			label := truncateFits(xr.Label, iw/2)
			s.Text(ix, yy, label, st)
			barW := iw - StringWidth(label) - 16
			if barW > 4 && xr.Totl > 0 {
				drawProgress(s, ix+iw-barW-13, yy, barW, float64(xr.Sent)/float64(xr.Totl))
			}
			s.TextRight(ix+iw-2, yy, b.xferTail(xr), stDim())
		}
	}

	b.drawStatus(s, w, h)
}

func (b *Browser) drawStatus(s *Screen, w, h int) {
	y := h - 1
	if b.filtering {
		b.filterIn.SetRect(0, y, w, 1)
		b.filterIn.Draw(s)
		return
	}
	if b.cmdOpen {
		b.cmdIn.SetRect(0, y, w, 1)
		b.cmdIn.Draw(s)
		return
	}
	count := ""
	if n := len(b.view); n > 0 {
		count = fmt.Sprintf("%d item%s ", n, plural(n))
	}
	if b.toast != "" && time.Since(b.toastT) < toastTTL {
		st := stWarn()
		if b.toastOk {
			st = stOk()
		}
		s.Text(1, y, truncateFits(b.toast, w-StringWidth(count)-2), st)
	} else {
		s.Text(0, y, truncateFits(statusHints, w-StringWidth(count)-1), stDim())
	}
	s.TextRight(w-2, y, count, stDim())
}

// xferTail paints the right edge of a transfer row: verdict or percent.
func (b *Browser) xferTail(x *xferRow) string {
	switch {
	case x.Err != "":
		return string(glCross) + " failed "
	case x.Done:
		return string(glCheck) + " done "
	case x.Totl > 0:
		return fmt.Sprintf("%d%% ", int(float64(x.Sent)/float64(x.Totl)*100))
	}
	return ""
}

// activeInput answers the text input that should receive typing.
func (b *Browser) activeInput() *TextInput {
	if m := b.app.TopModal(); m != nil {
		if ti, ok := m.Data.(*TextInput); ok {
			return ti
		}
		return nil
	}
	if b.filtering {
		return b.filterIn
	}
	if b.cmdOpen {
		return b.cmdIn
	}
	return nil
}

// ── input routing ─────────────────────────────────────────────────────────

func (b *Browser) HandleKey(k KeyEvent) bool {
	// Text-entry modes swallow typing (Tab stays inert inside a bar).
	if b.filtering || b.cmdOpen {
		if k.Key == KeyTab || k.Key == KeyBacktab {
			return true
		}
		ti := b.cmdIn
		if b.filtering {
			ti = b.filterIn
		}
		ti.HandleKey(k)
		if k.Key == KeyEnter {
			b.filtering, b.cmdOpen = false, false
		}
		return true
	}

	switch k.Key {
	case KeyTab, KeyBacktab:
		b.pane = !b.pane
		return true
	case KeyEsc:
		if b.filter != "" {
			b.setFilter("")
			return true
		}
		return false
	case KeyF1:
		b.showHelp()
		return true
	case KeyF5:
		b.reload()
		return true
	case KeyF7:
		b.promptMkdir()
		return true
	case KeyF8:
		b.xferOn = !b.xferOn
		return true
	case KeyDelete:
		b.confirmDelete()
		return true
	case KeyF2:
		b.promptRename()
		return true
	case KeyEnter:
		if k.Mod == ModAlt {
			b.showProperties()
			return true
		}
	}

	if k.Key == KeyRune && k.Mod == 0 {
		switch k.Rune {
		case ':':
			b.cmdOpen = true
			b.cmdIn.SetValue("")
			return true
		case '/':
			b.filterPrev = b.filter
			b.filtering = true
			b.filterIn.SetValue(b.filter)
			return true
		case 'u':
			b.promptUpload()
			return true
		case 'd':
			b.promptDownload()
			return true
		case 'c':
			b.copyMarked()
			return true
		case 'v':
			b.pasteInto()
			return true
		case 'r':
			b.reload()
			return true
		}
	}

	b.src.Focused, b.grid.Focused = b.pane, !b.pane
	if b.pane {
		return b.src.HandleKey(k)
	}
	if k.Key == KeyBackspace {
		b.up()
		return true
	}
	return b.grid.HandleKey(k)
}

func (b *Browser) HandleMouse(m MouseEvent) bool {
	if b.src.HandleMouse(m) {
		return true
	}
	return b.grid.HandleMouse(m)
}

// ── sources ───────────────────────────────────────────────────────────────

func (b *Browser) refreshSources() {
	b.sources = b.store.SortedSources()
	rows := make([]Row, len(b.sources))
	for i, src := range b.sources {
		bd, ok := typeBadges[src.Type]
		if !ok {
			bd = badge{Glyph: glDot, Color: theme.Dim}
		}
		scope := ""
		if src.Type == profile.TypeS3 {
			if src.Bucket != "" {
				scope = " /" + src.Bucket
			} else {
				scope = " /*"
			}
		}
		rows[i] = Row{
			Cells:      []string{string(bd.Glyph) + " " + src.Name + scope},
			CellStyles: []Style{{Fg: bd.Color}},
			Data:       i,
		}
	}
	b.src.SetRows(rows)
	if b.sel >= len(b.sources) {
		b.sel = max(len(b.sources)-1, 0)
	}
	b.src.Sel = b.sel
	b.src.MoveSel(0)
}

func (b *Browser) pickSourceByRow(r *Row) {
	if r == nil {
		return
	}
	if i, ok := r.Data.(int); ok && i != b.sel {
		b.dial(i)
	}
}

// dial seats a source: closes the old engine, opens the new, loads.
func (b *Browser) dial(i int) {
	if i < 0 || i >= len(b.sources) {
		return
	}
	src := b.sources[i]
	if b.fs != nil {
		b.fs.Close()
		b.fs = nil
	}
	b.s3c, b.bucket, b.prefix = nil, "", ""
	b.setFilter("")

	switch src.Type {
	case profile.TypeS3:
		if src.S3 == nil {
			b.fail(fmt.Errorf("source %s: no credentials", src.Name))
			return
		}
		c, err := s3client.New(b.ctx, *src.S3, s3client.Options{Timeout: -1})
		if err != nil {
			b.fail(err)
			return
		}
		b.s3c = c
		b.bucket = src.Bucket
	default:
		fs, err := remotefs.Dial(b.ctx, src)
		if err != nil {
			b.fail(err)
			return
		}
		b.fs = fs
		b.prefix = "/"
	}
	// The seat follows the dial: crumb() and the rail read b.sel, so a
	// failed leg returns above with the old seat intact and an honest crumb.
	b.sel = i
	b.src.Sel = i
	b.src.MoveSel(0)
	b.navReload()
}

// crumb answers where we are: source › bucket › path.
func (b *Browser) crumb() string {
	if b.sel >= len(b.sources) {
		return "s3b"
	}
	src := b.sources[b.sel]
	var sb strings.Builder
	sb.WriteString(src.Name)
	if b.s3c != nil {
		if b.bucket == "" {
			sb.WriteString(" › buckets")
		} else {
			sb.WriteString(" › s3://" + b.bucket)
			if b.prefix != "" {
				sb.WriteString("/" + strings.TrimSuffix(b.prefix, "/"))
			}
		}
	} else if b.prefix != "/" && b.prefix != "" {
		sb.WriteString("://" + b.prefix)
	}
	return sb.String()
}

// ── listing ───────────────────────────────────────────────────────────────

func (b *Browser) reload() {
	b.loading = true
	b.listSeq++
	seq := b.listSeq
	bucket, prefix, s3c, fs := b.bucket, b.prefix, b.s3c, b.fs
	go func() {
		var entries []listing.Entry
		var err error
		switch {
		case s3c != nil && bucket == "":
			entries, err = listBucketsAsEntries(b.ctx, s3c)
		case s3c != nil:
			entries, err = listing.List(b.ctx, s3c.S3, bucket, prefix, listing.Options{})
		case fs != nil:
			entries, err = fs.List(b.ctx, prefix)
		default:
			err = fmt.Errorf("no source selected")
		}
		b.app.Post(AppMsg{Kind: MsgList, Data: listResult{seq: seq, entries: entries, err: err}})
	}()
}

// navReload re-lists a CHANGED view from its top: dial, descent,
// ascent, :cd. The fold never survives a view change — an Off carried
// from the previous listing (bounds-clamped into a shorter universe)
// hides the new view's early rows. reload() keeps the fold for
// same-view refreshes (F5, r, post-verb re-lists, the root refusal).
func (b *Browser) navReload() {
	b.grid.Off = 0
	b.reload()
}

// listBucketsAsEntries lists the account's buckets as folder rows.
func listBucketsAsEntries(ctx context.Context, c *s3client.Client) ([]listing.Entry, error) {
	bs, err := listing.ListBuckets(ctx, c.S3)
	if err != nil {
		return nil, err
	}
	out := make([]listing.Entry, len(bs))
	for i, bk := range bs {
		out[i] = listing.Entry{Key: *bk.Name, Name: *bk.Name, IsDir: true}
	}
	return out, nil
}

func (b *Browser) onMsg(m AppMsg) {
	switch m.Kind {
	case MsgList:
		lr := m.Data.(listResult)
		if lr.seq != b.listSeq {
			return // a newer listing already landed
		}
		b.loading = false
		if lr.err != nil {
			b.fail(lr.err)
			return
		}
		b.entries = lr.entries
		b.applyView()
	case MsgProgress:
		if tk, ok := m.Data.(*TransferTick); ok && tk.Row != nil {
			tk.Row.Sent, tk.Row.Totl = tk.Sent, tk.Total
		}
	case MsgDone:
		tk := m.Data.(*TransferTick)
		if tk.Row != nil {
			if tk.Err != nil {
				tk.Row.Err = tk.Err.Error()
				b.flash(string(glCross)+" "+tk.Row.Label+": "+tk.Row.Err, false)
			} else {
				tk.Row.Done, tk.Row.Sent = true, tk.Row.Totl
			}
		}
		if tk.Err == nil {
			if tk.OkMsg != "" {
				b.flash(string(glCheck)+" "+tk.OkMsg, true)
			}
			if tk.Reload {
				b.reload()
			}
		}
	case MsgError:
		if s, ok := m.Data.(string); ok {
			b.flash(string(glCross)+" "+s, false)
		}
	case MsgTick:
		if b.toast != "" && time.Since(b.toastT) >= toastTTL {
			b.toast = ""
		}
	}
}

func (b *Browser) fail(err error) {
	b.loading = false
	b.flash(string(glCross)+" "+err.Error(), false)
}

func (b *Browser) flash(msg string, ok bool) {
	b.toast, b.toastOk, b.toastT = msg, ok, time.Now()
}

// applyView re-filters and re-sorts into grid rows.
func (b *Browser) applyView() {
	view := b.entries
	if f := strings.ToLower(b.filter); f != "" {
		view = nil
		for _, e := range b.entries {
			if strings.Contains(strings.ToLower(e.Name), f) {
				view = append(view, e)
			}
		}
	}
	b.view = view
	b.sortView()
	rows := make([]Row, len(b.view))
	for i, e := range b.view {
		name, nameSt := "  "+e.Name, stPlain()
		if e.IsDir {
			name, nameSt = string(glFolder)+" "+e.Name, Style{Fg: theme.Bright}
		}
		rows[i] = Row{
			Cells:      []string{name, b.sizeCell(e), b.timeCell(e)},
			CellStyles: []Style{nameSt, stInfo(), stDim()},
			Data:       e,
		}
	}
	b.grid.SetRows(rows)
	if b.grid.Sel < 0 && len(rows) > 0 {
		b.grid.Sel = 0 // a listing always seats a selection — verbs need a victim
	}
}

// sortView orders folders-first by the active column.
func (b *Browser) sortView() {
	v := b.view
	sort.SliceStable(v, func(i, j int) bool {
		a, c := v[i], v[j]
		if a.IsDir != c.IsDir {
			return a.IsDir
		}
		switch b.sortCol {
		case 1:
			if a.Size == c.Size {
				return false
			}
			return (a.Size < c.Size) == b.sortAsc
		case 2:
			at, ct := timeOf(a), timeOf(c)
			if at.Equal(ct) {
				return false
			}
			return at.Before(ct) == b.sortAsc
		default:
			an, cn := strings.ToLower(a.Name), strings.ToLower(c.Name)
			if an == cn {
				return false
			}
			return (an < cn) == b.sortAsc
		}
	})
}

func timeOf(e listing.Entry) time.Time {
	if e.LastModified != nil {
		return *e.LastModified
	}
	return time.Time{}
}

func (b *Browser) sizeCell(e listing.Entry) string {
	if e.IsDir {
		return "—"
	}
	return humanBytes(e.Size)
}

func (b *Browser) timeCell(e listing.Entry) string {
	if e.LastModified == nil {
		return ""
	}
	return e.LastModified.Format("2006-01-02 15:04")
}

func humanBytes(n int64) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(k), 0
	for m := n / k; m >= k; m /= k {
		div *= k
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (b *Browser) sortBy(col int) {
	if b.sortCol == col {
		b.sortAsc = !b.sortAsc
	} else {
		b.sortCol, b.sortAsc = col, col != 1 // sizes start big-first
	}
	b.applyView()
}

func (b *Browser) setFilter(v string) {
	b.filter = strings.TrimSpace(v)
	b.filtering = false
	b.applyView()
}

// selectedEntries answers the marked rows (or the selected one).
func (b *Browser) selectedEntries() []listing.Entry {
	idx := b.grid.MarkedRows()
	out := make([]listing.Entry, 0, len(idx))
	for _, i := range idx {
		if i >= 0 && i < len(b.view) {
			out = append(out, b.view[i])
		}
	}
	return out
}

func (b *Browser) selected() *listing.Entry {
	if r := b.grid.SelectedRow(); r != nil {
		if e, ok := r.Data.(listing.Entry); ok {
			return &e
		}
	}
	return nil
}

// ── navigation ────────────────────────────────────────────────────────────

func (b *Browser) openEntry() {
	e := b.selected()
	if e == nil {
		return
	}
	if !e.IsDir {
		b.showProperties()
		return
	}
	if b.s3c != nil {
		if b.bucket == "" {
			b.bucket = e.Name // account-wide: into the bucket
			b.prefix = ""
		} else {
			// JoinKey answers prefix form (trailing slash included) — a
			// second slash was a real MinIO 400 on every descent.
			b.prefix = transfer.JoinKey(b.prefix, e.Name)
		}
	} else {
		b.prefix = remotefs.CleanPath(path.Join(strings.TrimPrefix(b.prefix, "/"), e.Name))
	}
	b.navReload()
}

func (b *Browser) up() {
	if b.s3c != nil {
		switch {
		case b.bucket == "":
			b.flash("already at the account root", false)
			b.reload() // same view — the refusal keeps the fold
			return
		case b.prefix == "":
			b.bucket = ""
		default:
			p := strings.TrimSuffix(b.prefix, "/")
			if i := strings.LastIndex(p, "/"); i > 0 {
				b.prefix = p[:i+1]
			} else {
				b.prefix = ""
			}
		}
	} else {
		if b.prefix == "/" || b.prefix == "" {
			b.flash("at the source root", false)
			return
		}
		b.prefix = remotefs.CleanPath(path.Dir(strings.TrimSuffix(b.prefix, "/")))
	}
	b.navReload()
}

// ── verbs: folders and names ──────────────────────────────────────────────

func (b *Browser) promptMkdir() {
	b.promptInput("New folder", "folder name", "", func(name string) { b.mkdirAt(name) })
}

// objKey joins a seat prefix and one name into an object key — NO
// trailing slash. JoinKey answers prefix form (trailing slash included),
// which is right for prefixes (descent, up, :cd) and wrong for keys: an
// upload at "dir/name/" lands as a folder marker and the file's bytes
// hide behind a synthetic directory.
func objKey(parts ...string) string {
	return strings.TrimSuffix(transfer.JoinKey(parts...), "/")
}

func (b *Browser) mkdirAt(name string) {
	b.runVerb("folder "+name+" created", true, func() error {
		if b.s3c != nil {
			if b.bucket == "" {
				return fmt.Errorf("pick a bucket first")
			}
			key := objKey(b.prefix, name) + "/"
			return transfer.UploadReader(b.ctx, b.s3c.S3, strings.NewReader(""), 0, b.bucket, key, transfer.UploadOptions{})
		}
		if b.fs == nil {
			return fmt.Errorf("no source selected")
		}
		return b.fs.MkdirAll(b.ctx, remotefs.CleanPath(path.Join(strings.TrimPrefix(b.prefix, "/"), name)))
	})
}

func (b *Browser) promptRename() {
	e := b.selected()
	if e == nil {
		b.flash("nothing selected", false)
		return
	}
	b.promptInput("Rename", "new name", e.Name, func(name string) {
		b.runVerb("renamed to "+name, true, func() error {
			if b.fs != nil {
				dir := strings.TrimSuffix(b.prefix, "/")
				return b.fs.Rename(b.ctx,
					remotefs.CleanPath(path.Join(dir, e.Name)),
					remotefs.CleanPath(path.Join(dir, name)))
			}
			if b.s3c == nil || b.bucket == "" {
				return fmt.Errorf("no source selected")
			}
			oldKey := objKey(b.prefix, e.Name)
			newKey := objKey(b.prefix, name)
			if e.IsDir {
				oldKey, newKey = oldKey+"/", newKey+"/"
			}
			// S3 rename = copy every key, then delete the originals.
			keys, err := transfer.CollectPrefixKeys(b.ctx, b.s3c.S3, b.bucket, oldKey)
			if err != nil {
				return err
			}
			for _, k := range keys {
				nk := newKey + strings.TrimPrefix(k, oldKey)
				if err := transfer.Copy(b.ctx, b.s3c.S3, b.bucket, k, b.bucket, nk); err != nil {
					return err
				}
			}
			_, err = transfer.DeleteKeys(b.ctx, b.s3c.S3, b.bucket, transfer.IncludeFolderMarker(keys, oldKey))
			return err
		})
	})
}

func (b *Browser) confirmDelete() {
	ents := b.selectedEntries()
	if len(ents) == 0 {
		b.flash("nothing selected", false)
		return
	}
	shown := min(len(ents), 3)
	d := &Dialog{
		Title:  "Delete",
		Width:  56,
		Height: 6 + shown + boolInt(len(ents) > shown),
		Content: func(s *Screen, x, y, w, _ int) {
			s.Text(x, y+1, truncateFits(fmt.Sprintf("Delete %d item(s)? This cannot be undone.", len(ents)), w), stPlain())
			for i := range shown {
				s.Text(x, y+2+i, truncateFits("  "+string(glCross)+" "+ents[i].Name, w), stDanger())
			}
			if len(ents) > shown {
				s.Text(x, y+2+shown, fmt.Sprintf("  %s and %d more", string(glyphEllipsis), len(ents)-shown), stDim())
			}
		},
		Buttons: []DialogButton{
			{Label: "Cancel", Action: func() { b.app.PopModal() }},
			{Label: "Delete", Style: Style{Fg: theme.Danger}, Action: func() {
				b.app.PopModal()
				b.runVerb("deleted", true, func() error { return b.deleteEntries(ents) })
			}},
		},
		OnCancel: func() { b.app.PopModal() },
	}
	b.app.PushModal(d)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (b *Browser) deleteEntries(ents []listing.Entry) error {
	if b.fs != nil {
		for _, e := range ents {
			p := remotefs.CleanPath(path.Join(strings.TrimSuffix(b.prefix, "/"), e.Name))
			if err := b.fs.Remove(b.ctx, p); err != nil {
				return err
			}
		}
		return nil
	}
	if b.s3c == nil || b.bucket == "" {
		return fmt.Errorf("no deletable location")
	}
	var keys []string
	for _, e := range ents {
		k := objKey(b.prefix, e.Name)
		if e.IsDir {
			sub, err := transfer.CollectPrefixKeys(b.ctx, b.s3c.S3, b.bucket, k+"/")
			if err != nil {
				return err
			}
			keys = append(keys, sub...)
			keys = append(keys, transfer.IncludeFolderMarker(sub, k+"/")...)
		} else {
			keys = append(keys, k)
		}
	}
	_, err := transfer.DeleteKeys(b.ctx, b.s3c.S3, b.bucket, keys)
	return err
}

// ── verbs: transfers ──────────────────────────────────────────────────────

func (b *Browser) promptUpload() {
	if b.fs == nil && b.s3c == nil {
		b.flash("no source selected", false)
		return
	}
	if b.s3c != nil && b.bucket == "" {
		b.flash("pick a bucket first", false)
		return
	}
	cwd, _ := os.Getwd()
	b.promptInput("Upload", "local file path", cwd+string(os.PathSeparator), func(p string) { b.upload(p) })
}

func (b *Browser) upload(localPath string) {
	st, err := os.Stat(localPath)
	if err != nil {
		b.flash(err.Error(), false)
		return
	}
	name := path.Base(strings.ReplaceAll(localPath, "\\", "/"))
	x := &xferRow{Label: "↑ " + name, Totl: st.Size()}
	b.beginXfer(x)
	go func() {
		pr := b.xferProgress(x)
		var err error
		switch {
		case b.s3c != nil:
			key := objKey(b.prefix, name)
			err = transfer.UploadFile(b.ctx, b.s3c.S3, localPath, b.bucket, key, transfer.UploadOptions{Progress: pr})
		case b.fs != nil:
			var f io.Reader
			f, err = os.Open(localPath)
			if err == nil {
				defer f.(*os.File).Close()
				err = b.fs.Create(b.ctx, remotefs.CleanPath(path.Join(strings.TrimPrefix(b.prefix, "/"), name)), transfer.NewProgressReader(f, pr, st.Size(), 0))
			}
		default:
			err = fmt.Errorf("no source selected")
		}
		b.app.Post(AppMsg{Kind: MsgDone, Data: &TransferTick{Row: x, Err: err, Done: true, Reload: err == nil}})
	}()
}

func (b *Browser) promptDownload() {
	var files []listing.Entry
	for _, e := range b.selectedEntries() {
		if !e.IsDir {
			files = append(files, e)
		}
	}
	if len(files) == 0 {
		b.flash("select a file to download", false)
		return
	}
	cwd, _ := os.Getwd()
	b.promptInput("Download", "local directory", cwd, func(dir string) { b.download(files, dir) })
}

func (b *Browser) download(ents []listing.Entry, dir string) {
	rows := make([]*xferRow, len(ents))
	for i, e := range ents {
		rows[i] = &xferRow{Label: "↓ " + e.Name, Totl: e.Size}
		b.beginXfer(rows[i])
	}
	go func() {
		for i, e := range ents {
			x := rows[i]
			pr := b.xferProgress(x)
			local := path.Join(dir, e.Name)
			var err error
			switch {
			case b.s3c != nil:
				err = transfer.DownloadFile(b.ctx, b.s3c.S3, b.bucket, e.Key, local, transfer.DownloadOptions{Progress: pr})
			case b.fs != nil:
				err = b.downloadRemote(e, local, pr)
			default:
				err = fmt.Errorf("no source selected")
			}
			b.app.Post(AppMsg{Kind: MsgDone, Data: &TransferTick{Row: x, Err: err, Done: true, Reload: err == nil}})
			if err != nil {
				return
			}
		}
	}()
}

func (b *Browser) downloadRemote(e listing.Entry, local string, pr transfer.ProgressFn) error {
	rc, size, err := b.fs.Open(b.ctx, path.Join(strings.TrimPrefix(b.prefix, "/"), e.Name))
	if err != nil {
		return err
	}
	defer rc.Close()
	return transfer.StageAndCommit(local, func(f *os.File) error {
		_, err := io.Copy(f, transfer.NewProgressReader(rc, pr, size, 0))
		return err
	})
}

// xferProgress wraps a transfer row into a throttled progress poster.
func (b *Browser) xferProgress(x *xferRow) transfer.ProgressFn {
	var last time.Time
	return func(sent, total int64) {
		if sent < total && time.Since(last) < xferThrottle {
			return
		}
		last = time.Now()
		b.app.Post(AppMsg{Kind: MsgProgress, Data: &TransferTick{Row: x, Sent: sent, Total: total}})
	}
}

func (b *Browser) beginXfer(x *xferRow) {
	b.xfer = append(b.xfer, x)
	b.xferOn = true
}

// runVerb runs an engine verb off-loop and posts its verdict.
func (b *Browser) runVerb(okMsg string, reload bool, fn func() error) {
	go func() {
		err := fn()
		b.app.Post(AppMsg{Kind: MsgDone, Data: &TransferTick{Err: err, Done: true, OkMsg: okMsg, Reload: err == nil && reload}})
	}()
}

// ── verbs: copy and paste (S3 in-account) ────────────────────────────────

func (b *Browser) copyMarked() {
	if b.s3c == nil || b.bucket == "" {
		b.flash("copy needs an S3 bucket", false)
		return
	}
	ents := b.selectedEntries()
	if len(ents) == 0 {
		b.flash("nothing selected", false)
		return
	}
	b.clip = b.clip[:0]
	for _, e := range ents {
		b.clip = append(b.clip, clipEntry{bucket: b.bucket, key: objKey(b.prefix, e.Name), name: e.Name})
	}
	b.flash(fmt.Sprintf("copied %d — v pastes into this folder", len(b.clip)), true)
}

func (b *Browser) pasteInto() {
	if len(b.clip) == 0 {
		b.flash("nothing copied", false)
		return
	}
	if b.s3c == nil || b.bucket == "" {
		b.flash("paste needs an S3 bucket", false)
		return
	}
	clip, dst, prefix := b.clip, b.bucket, b.prefix
	b.runVerb(fmt.Sprintf("pasted %d item(s)", len(clip)), true, func() error {
		for _, c := range clip {
			if err := transfer.Copy(b.ctx, b.s3c.S3, c.bucket, c.key, dst, objKey(prefix, c.name)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ── properties and help ───────────────────────────────────────────────────

func (b *Browser) showProperties() {
	pe := b.selected()
	if pe == nil {
		b.flash("nothing selected", false)
		return
	}
	e := *pe
	rows := [][2]string{
		{"Name", e.Name},
		{"Kind", map[bool]string{true: "folder", false: "file"}[e.IsDir]},
		{"Size", fmt.Sprintf("%s (%d bytes)", humanBytes(e.Size), e.Size)},
		{"Modified", b.timeCell(e)},
		{"Storage class", e.StorageClass},
		{"ETag", e.ETag},
		{"Mode", e.Mode},
		{"Where", b.crumb()},
	}
	d := &Dialog{
		Title:  "Properties",
		Width:  60,
		Height: 12,
		Content: func(s *Screen, x, y, w, _ int) {
			yy := y + 1
			for _, r := range rows {
				if r[1] == "" {
					continue
				}
				s.Text(x, yy, truncateFits(r[0], 14), stDim())
				s.Text(x+16, yy, truncateFits(r[1], w-16), stPlain())
				yy++
			}
		},
		Buttons:  []DialogButton{{Label: "Close", Action: func() { b.app.PopModal() }}},
		OnCancel: func() { b.app.PopModal() },
	}
	b.app.PushModal(d)
}

func (b *Browser) showHelp() {
	keys := [][2]string{
		{"Tab", "switch source rail ↔ listing"},
		{"Enter", "open folder / file properties"},
		{"Backspace", "parent folder"},
		{"/", "filter"},
		{":", "command bar"},
		{"F2", "rename"},
		{"F7", "new folder"},
		{"Del", "delete"},
		{"Space / Ctrl+Click", "mark items"},
		{"c / v", "copy / paste (S3)"},
		{"u / d", "upload / download"},
		{"F8", "transfers panel"},
		{"Alt+Enter", "properties"},
		{"F5 / r", "refresh"},
		{"Ctrl+C ×2 · Ctrl+Q · :q", "quit"},
	}
	d := &Dialog{
		Title:  "Keys",
		Width:  54,
		Height: len(keys) + 6,
		Content: func(s *Screen, x, y, w, _ int) {
			for i, r := range keys {
				s.Text(x, y+1+i, truncateFits(r[0], 26), stAccent())
				s.Text(x+28, y+1+i, truncateFits(r[1], w-28), stPlain())
			}
		},
		Buttons:  []DialogButton{{Label: "Close", Action: func() { b.app.PopModal() }}},
		OnCancel: func() { b.app.PopModal() },
	}
	b.app.PushModal(d)
}

// ── shared input dialog ───────────────────────────────────────────────────

func (b *Browser) promptInput(title, label, initial string, apply func(string)) {
	ti := &TextInput{Placeholder: label}
	ti.SetValue(initial)
	commit := func() {
		v := strings.TrimSpace(ti.Value())
		if v == "" {
			return
		}
		b.app.PopModal()
		apply(v)
	}
	ti.OnSubmit = func(string) { commit() }
	d := &Dialog{
		Title:  title,
		Width:  max(StringWidth(initial)+16, 44),
		Height: 7,
		Data:   ti,
		Content: func(s *Screen, x, y, w, _ int) {
			s.Text(x, y+1, label, stDim())
			ti.SetRect(x, y+2, w, 1)
			ti.Draw(s)
		},
		OnKey: func(k KeyEvent, _ *Dialog) bool {
			switch k.Key {
			case KeyEsc:
				b.app.PopModal()
				return true
			case KeyEnter:
				commit()
				return true
			}
			return ti.HandleKey(k)
		},
		Buttons: []DialogButton{
			{Label: "Cancel", Action: func() { b.app.PopModal() }},
			{Label: title, Action: func() { commit() }},
		},
		OnCancel: func() { b.app.PopModal() },
	}
	b.app.PushModal(d)
}

// ── command bar ───────────────────────────────────────────────────────────

func (b *Browser) runCommand(line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "q", "quit", "exit":
		b.app.Quit()
	case "cd":
		if len(fields) < 2 {
			b.flash("usage: cd s3://bucket/prefix | NAME://dir | sub/dir", false)
			return
		}
		b.navigate(fields[1])
	case "mkdir":
		if len(fields) < 2 {
			b.flash("usage: mkdir NAME", false)
			return
		}
		b.mkdirAt(strings.Join(fields[1:], " "))
	case "rm":
		b.confirmDelete()
	case "refresh", "r":
		b.reload()
	case "sources":
		b.pane = true
	case "xfers":
		b.xferOn = !b.xferOn
	case "help":
		b.showHelp()
	default:
		b.flash("unknown command: "+fields[0]+" — try :help", false)
	}
}

// navigate follows a URI or a relative path.
func (b *Browser) navigate(target string) {
	if i := strings.Index(target, "://"); i > 0 {
		scheme, rest := target[:i], target[i+3:]
		if scheme == "s3" {
			if b.s3c == nil {
				b.flash("the current source is not S3 — pick one first", false)
				return
			}
			bk, p, _ := strings.Cut(rest, "/")
			if p != "" && !strings.HasSuffix(p, "/") {
				p += "/" // prefix form — a bare s3://bucket lists its root
			}
			b.bucket, b.prefix = bk, p
			b.navReload()
			return
		}
		for si, src := range b.sources {
			if strings.EqualFold(src.Name, scheme) {
				b.dial(si) // dial seats the source itself
				b.prefix = remotefs.CleanPath(rest)
				b.navReload()
				return
			}
		}
		b.flash("unknown source: "+scheme, false)
		return
	}
	if b.s3c != nil && b.bucket != "" {
		b.prefix = transfer.JoinKey(b.prefix, target) // prefix form already
	} else {
		b.prefix = remotefs.CleanPath(path.Join(strings.TrimPrefix(b.prefix, "/"), target))
	}
	b.navReload()
}

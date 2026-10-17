package tui

// The design language: modern terminal minimalism in the newest CLI
// idiom — transparent background (the terminal's own theme shows
// through), rounded panels with dim slate borders that brighten on
// focus, one teal accent for selection and action, semantic ok/warn/
// danger tones reserved for state, full-row selection bars, and dim
// key hints in a segmented status bar. Everything degrades gracefully
// through 256 and 16 colors to plain attributes (NO_COLOR).

var theme = struct {
	// Panel chrome.
	Border      Color // resting panel border: dim slate
	BorderFocus Color // focused panel border: brightened slate
	BorderModal Color // dialog border: mid slate
	Overlay     Color // modal backdrop: near-black slate fill

	// Text tones (foregrounds; background stays terminal-default).
	Dim    Color // secondary text: hints, meta columns
	Bright Color // emphasis: titles, totals — painted bold on default fg

	// Semantic tones.
	Accent Color // teal — selection, action, "you are here"
	Ok     Color // green — connected, succeeded
	Warn   Color // amber — warnings, confirmations
	Danger Color // red — errors, destructive verbs
	Info   Color // blue — neutral facts, sizes, counts

	// Selection bar: full-row background with default fg.
	SelBg Color
}{
	Border:      RGB(0x38, 0x44, 0x58),
	BorderFocus: RGB(0x78, 0x91, 0xB4),
	BorderModal: RGB(0x5E, 0x76, 0x98),
	Overlay:     RGB(0x0A, 0x0E, 0x16),
	Dim:         RGB(0x82, 0x8C, 0x9E),
	Bright:      RGB(0xE6, 0xEA, 0xF2),
	Accent:      RGB(0x2D, 0xD4, 0xBF),
	Ok:          RGB(0x34, 0xD3, 0x99),
	Warn:        RGB(0xF5, 0x9E, 0x0B),
	Danger:      RGB(0xF8, 0x71, 0x71),
	Info:        RGB(0x7A, 0xA2, 0xF7),
	SelBg:       RGB(0x1E, 0x29, 0x3B),
}

// Style shorthands the widgets share (kept as functions so callers can
// chain further attributes without mutating shared state).
func stDim() Style    { return Style{Fg: theme.Dim} }
func stAccent() Style { return Style{Fg: theme.Accent} }
func stOk() Style     { return Style{Fg: theme.Ok} }
func stWarn() Style   { return Style{Fg: theme.Warn} }
func stDanger() Style { return Style{Fg: theme.Danger} }
func stInfo() Style   { return Style{Fg: theme.Info} }
func stSel() Style    { return Style{Bg: theme.SelBg, Fg: theme.Bright} }
func stPlain() Style  { return Style{} }

// Source-type badges: a glyph + tone per kind, mirroring the GUI's
// colored source dots.
type badge struct {
	Glyph rune
	Color Color
}

var typeBadges = map[string]badge{
	"s3":     {Glyph: '◈', Color: theme.Accent},
	"local":  {Glyph: '▣', Color: theme.Info},
	"sftp":   {Glyph: '◈', Color: theme.Warn},
	"ftp":    {Glyph: '◈', Color: theme.Warn},
	"webdav": {Glyph: '◈', Color: theme.Info},
}

// Glyph vocabulary (single width — safe in every grid).
const (
	glTopLeft     = '╭'
	glTopRight    = '╮'
	glBotLeft     = '╰'
	glBotRight    = '╯'
	glHoriz       = '─'
	glVert        = '│'
	glyphEllipsis = '…'
	glCheck       = '✓'
	glCross       = '✗'
	glDot         = '•'
	glFolder      = '▸' // closed folder marker
)

// Progress ramp, eighth-blocks left to right.
var progressRamp = [...]rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

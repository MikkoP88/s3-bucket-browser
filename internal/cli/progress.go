package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// Live transfer progress for the CLI face. The transfer engine has always
// reported (sent, total) through ProgressFn — the GUI rides it via events;
// the CLI just never listened, so a multi-gigabyte cp printed one announce
// line and then silence. The printer here renders one rewriting line on
// stderr while a single file streams:
//
//	\r big-iso.img  1.2 GiB / 4.7 GiB (26%)  9.8 MiB/s
//
// Three gates keep it polite: stderr must be a terminal (a piped stderr
// stays byte-clean for scripts and redirections), --json must be off
// (machine output carries no progress), and a transfer only draws once it
// has moved 1 MiB or run a second — a folder of small files never
// flickers, and the line only appears where it answers a real question
// ("is it moving?"). done() erases the line so the transfer's own record
// (the verbose "up s3://…" trail, the summary count) is what history
// keeps. No color: plain text renders identically on every console.

const (
	progressInterval = 100 * time.Millisecond // redraw throttle (10 Hz)
	progressMinBytes = 1 << 20                // draw only past this much data…
	progressMinDelay = time.Second            // …or after this long (slow links)
	progressNameW   = 40                      // display width of the file name
)

// progressLine is one transfer's live line. Construct per item; fn() feeds
// the engine, done() cleans up. A zero gate (non-TTY, --json) makes fn()
// return nil — the engine skips the wrapper entirely.
type progressLine struct {
	w       io.Writer
	name    string
	on      bool
	now     func() time.Time
	start   time.Time
	lastAt  time.Time
	lastLen int
	lastSent int64
	speed   float64 // exponential moving average, bytes/sec
	drawn   bool
}

// newProgressLine builds the printer for a named transfer against the
// process's stderr, gated on terminal + output mode.
func newProgressLine(name string) *progressLine {
	return newProgressLineW(os.Stderr, name, stderrIsTerminal() && !flagJSON)
}

// newProgressLineW is the testable core: explicit writer, clock and gate.
func newProgressLineW(w io.Writer, name string, on bool) *progressLine {
	return &progressLine{w: w, name: name, on: on, now: time.Now}
}

// fn returns the engine hook — nil when gated off.
func (p *progressLine) fn() transfer.ProgressFn {
	if !p.on {
		return nil
	}
	return p.report
}

// report draws the line under the throttle and self-gates: nothing renders
// until the transfer is big enough (or slow enough) to be worth watching.
func (p *progressLine) report(sent, total int64) {
	now := p.now()
	if p.start.IsZero() {
		p.start = now
	}
	// the very first qualifying frame always renders (lastAt still zero);
	// after that the throttle holds redraws to progressInterval
	if !p.lastAt.IsZero() && sent < total && now.Sub(p.lastAt) < progressInterval {
		return
	}
	if !p.drawn && sent < progressMinBytes && now.Sub(p.start) < progressMinDelay {
		return
	}
	// instantaneous window speed, smoothed so a chunk boundary does not
	// spike the readout
	ref := p.lastAt
	if ref.IsZero() {
		ref = p.start
	}
	if dt := now.Sub(ref).Seconds(); dt > 0 {
		inst := float64(sent-p.lastSent) / dt
		if p.speed == 0 {
			p.speed = inst
		} else {
			p.speed = p.speed*0.6 + inst*0.4
		}
	}
	p.lastAt, p.lastSent = now, sent

	var b strings.Builder
	b.WriteString("\r ")
	b.WriteString(progressName(p.name))
	fmt.Fprintf(&b, "  %s", humanSize(sent))
	if total > 0 {
		fmt.Fprintf(&b, " / %s (%d%%)", humanSize(total), sent*100/total)
	}
	if p.speed > 0 {
		fmt.Fprintf(&b, "  %s/s", humanSize(int64(p.speed)))
	}
	line := b.String()
	// pad over the previous line's tail so a shrinking readout leaves no
	// residue (the percentage losing a digit, say)
	if pad := p.lastLen - len(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	p.lastLen = len(line)
	p.drawn = true
	fmt.Fprint(p.w, line)
}

// done erases the line after the transfer settles — success or error, the
// caller's own record line is the artifact history keeps.
func (p *progressLine) done() {
	if !p.drawn {
		return
	}
	fmt.Fprintf(p.w, "\r%s\r", strings.Repeat(" ", p.lastLen))
	p.drawn, p.lastLen = false, 0
}

// progressName fits a display name into progressNameW columns, keeping the
// tail (the file name lives at the end of a path; the ellipsis absorbs the
// long middle). It cuts on rune boundaries — names carry CJK.
func progressName(s string) string {
	r := []rune(s)
	if len(r) <= progressNameW {
		return s
	}
	return "…" + string(r[len(r)-progressNameW+1:])
}

// stderrIsTerminal reports whether stderr is an interactive console —
// ModeCharDevice is the stdlib tell for both POSIX ttys and Windows
// console handles; pipes and files clear it.
func stderrIsTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

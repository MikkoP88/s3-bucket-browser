package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeClock hands report() a controllable now.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Advance(d time.Duration) time.Time {
	c.t = c.t.Add(d)
	return c.t
}

func newTestProgress(buf *bytes.Buffer, on bool, clk *fakeClock) *progressLine {
	p := newProgressLineW(buf, "big-iso.img", on)
	p.now = clk.Now
	return p
}

func TestProgressGatedOffReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	if fn := newTestProgress(&buf, false, &fakeClock{t: time.Unix(0, 0)}).fn(); fn != nil {
		t.Fatalf("gated-off printer must hand the engine a nil hook, got %v", fn)
	}
	if buf.Len() != 0 {
		t.Fatalf("gated-off printer wrote %q", buf.String())
	}
}

func TestProgressSelfGateSmallFastTransferSilent(t *testing.T) {
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestProgress(&buf, true, clk)
	fn := p.fn()
	fn(512*1024, 900*1024) // 0.5 s in…
	clk.Advance(600 * time.Millisecond)
	fn(800*1024, 900*1024) // …still under 1 MiB and under 1 s of wall time
	p.done()
	if buf.Len() != 0 {
		t.Fatalf("a small fast transfer must stay silent, wrote %q", buf.String())
	}
}

func TestProgressDrawsPastSizeGate(t *testing.T) {
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestProgress(&buf, true, clk)
	fn := p.fn()
	const total = int64(5 << 20)
	fn(64*1024, total) // first chunk seeds the clock, far under the gates
	clk.Advance(250 * time.Millisecond)
	fn(2<<20, total)
	p.done()
	out := buf.String()
	for _, want := range []string{"big-iso.img", "2.0 MB", "5.0 MB", "40%", "MB/s"} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress line %q missing %q", out, want)
		}
	}
	if !strings.Contains(out, "\r ") {
		t.Fatalf("progress line must redraw from column 0, got %q", out)
	}
	// done() erases: the artifact ends with a blanking run and a carriage
	// return, leaving the caller's own record line a clean slate
	if !strings.HasSuffix(out, "\r") || !strings.Contains(out, strings.Repeat(" ", 30)[:20]) {
		t.Fatalf("done() must blank the drawn line, got %q", out)
	}
}

func TestProgressThrottle(t *testing.T) {
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestProgress(&buf, true, clk)
	fn := p.fn()
	const total = int64(50 << 20)
	clk.Advance(150 * time.Millisecond)
	fn(10<<20, total)
	first := buf.Len()
	clk.Advance(20 * time.Millisecond) // inside the 100 ms window
	fn(12<<20, total)
	if buf.Len() != first {
		t.Fatalf("a redraw inside the throttle window rendered again: %q", buf.String())
	}
	clk.Advance(120 * time.Millisecond)
	fn(20<<20, total)
	if buf.Len() == first {
		t.Fatalf("a redraw past the window never rendered: %q", buf.String())
	}
	// the completed frame bypasses the throttle even inside the window
	clk.Advance(10 * time.Millisecond)
	fn(total, total)
	if !strings.Contains(buf.String(), "100%") {
		t.Fatalf("the final frame must render 100%%: %q", buf.String())
	}
}

func TestProgressSlowLinkDrawsOnDelayGate(t *testing.T) {
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestProgress(&buf, true, clk)
	fn := p.fn()
	fn(4*1024, 0)                        // first chunk seeds the clock, far under the size gate
	clk.Advance(1100 * time.Millisecond) // past the 1 s gate, still < 1 MiB
	fn(300*1024, 0)                      // unknown total: byte counter, no pct
	p.done()
	out := buf.String()
	if !strings.Contains(out, "300 KB") {
		t.Fatalf("slow-link draw missing sent bytes: %q", out)
	}
	if strings.Contains(out, "%") {
		t.Fatalf("unknown total must not render a percentage: %q", out)
	}
}

func TestProgressNameTruncatesKeepingTail(t *testing.T) {
	long := strings.Repeat("a", 60) + "\\final.bin"
	got := progressName(long)
	if len([]rune(got)) != progressNameW || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "final.bin") {
		t.Fatalf("truncation must keep the tail: %q", got)
	}
	// CJK names must not split a rune at the cut
	cjk := strings.Repeat("資", 30) + "最終.bin"
	if got := progressName(cjk); strings.ToValidUTF8(got, "") != got {
		t.Fatalf("truncation split a rune: %q", got)
	}
	if progressName("short.bin") != "short.bin" {
		t.Fatalf("short names must pass through: %q", progressName("short.bin"))
	}
}

func TestProgressShrinkingLinePadsOverResidue(t *testing.T) {
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestProgress(&buf, true, clk)
	fn := p.fn()
	const total = int64(100 << 20)
	clk.Advance(150 * time.Millisecond)
	fn(99<<20, total) // "99.0 MiB / 100.0 MiB (99%)" — one digit wider…
	clk.Advance(150 * time.Millisecond)
	fn(100<<20, total) // …than "100%": same bytes, but prove padding works
	out := buf.String()
	last := out[strings.LastIndex(out, "\r"):]
	if len(last) < p.lastLen && p.lastLen > 0 && !strings.HasSuffix(strings.TrimRight(last, "\r"), " ") {
		// padding only matters when the readout shrinks (e.g. speed losing a
		// digit); assert the mechanism: lastLen tracks the widest line drawn
		t.Fatalf("line-length tracking broke: last=%q lastLen=%d", last, p.lastLen)
	}
}

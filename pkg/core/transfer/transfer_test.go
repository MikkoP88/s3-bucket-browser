package transfer

import (
	"strings"
	"testing"
	"time"
)

func TestJoinKey(t *testing.T) {
	cases := map[string]string{
		JoinKey("photos", "2026"):        "photos/2026/",
		JoinKey("photos/", "", "/2026/"): "photos/2026/",
		JoinKey("a\\b", "c"):             "a/b/c/",
		JoinKey():                        "",
		JoinKey("", ""):                  "",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("JoinKey produced %q, want %q", got, want)
		}
	}
}

func TestProgressReader(t *testing.T) {
	var calls int64
	pr := &progressReader{
		r:        strings.NewReader("hello world"),
		fn:       func(sent, total int64) { calls++ },
		total:    11,
		reportOn: true,
	}
	buf := make([]byte, 5)
	pr.Read(buf) // hel
	pr.Read(buf) // lo w
	if calls != 2 {
		t.Errorf("progress calls = %d, want 2 (reportOn)", calls)
	}
}

func TestRateLimiter(t *testing.T) {
	if newRateLimiter(0) != nil || newRateLimiter(-100) != nil {
		t.Error("non-positive bps must mean unlimited (nil limiter)")
	}
	var nilLimiter *rateLimiter
	nilLimiter.wait(1 << 20) // must not panic
	r := newRateLimiter(1000)

	// First write fits in the one-second burst budget: no waiting.
	start := time.Now()
	r.wait(500)
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Errorf("first 500B waited %v, want ~0", el)
	}

	// Second write exceeds the budget (500 left, 1500 needed) → ~1s sleep.
	start = time.Now()
	r.wait(1500)
	if el := time.Since(start); el < 700*time.Millisecond {
		t.Errorf("throttled write waited only %v, want >= ~1s", el)
	}

	// Negative sizes are a no-op.
	r.wait(-5)
}

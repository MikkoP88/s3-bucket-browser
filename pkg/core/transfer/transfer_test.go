package transfer

import (
	"strings"
	"testing"
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

package api

import (
	"testing"
	"time"
)

func TestParseUntil(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{in: "+30d", want: now.Add(30 * 24 * time.Hour)},
		{in: "+12h", want: now.Add(12 * time.Hour)},
		{in: "+90m", want: now.Add(90 * time.Minute)},
		{in: "2027-06-01T00:00:00Z", want: time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)},
		{in: "not-a-time", wantErr: true},
		{in: "+30x", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, c := range cases {
		got, err := parseUntil(c.in, now)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseUntil(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseUntil(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseUntil(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

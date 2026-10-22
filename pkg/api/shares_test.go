// shares_test.go: pins the share registry's contract — presign is pure
// local signing (no wire), the TTL clamps hold, the list is newest-first
// with honest expiry, stop removes and unknown ids refuse.
package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

func shareApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.ctx = context.Background()
	a.session = []profile.Source{roSource("http://127.0.0.1:1", "seat", false)}
	return a
}

// TestCreateShareSignsAndLists: a share mints offline (presign needs no
// wire), carries the source name, and lists newest first.
func TestCreateShareSignsAndLists(t *testing.T) {
	a := shareApp(t)
	s1, err := a.CreateShare("", "bkt", "docs/a.txt", 3600)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s1.URL, "X-Amz-Signature=") {
		t.Fatalf("url is not a presign: %s", s1.URL)
	}
	if s1.Source != "seat" {
		t.Errorf("source = %q, want the resolved name seat", s1.Source)
	}
	if s1.Expired {
		t.Error("a fresh share must not be expired")
	}
	want, err := time.Parse(time.RFC3339, s1.Expires)
	if err != nil {
		t.Fatal(err)
	}
	created, _ := time.Parse(time.RFC3339, s1.Created)
	if got := want.Sub(created); got != time.Hour {
		t.Errorf("ttl = %v, want 1h", got)
	}

	time.Sleep(1100 * time.Millisecond) // distinct Created timestamps
	s2, err := a.CreateShare("", "bkt", "docs/b.txt", 3600)
	if err != nil {
		t.Fatal(err)
	}
	list := a.ListShares()
	if len(list) != 2 {
		t.Fatalf("ListShares = %d entries, want 2", len(list))
	}
	if list[0].ID != s2.ID || list[1].ID != s1.ID {
		t.Errorf("list order = [%s, %s], want newest first", list[0].ID, list[1].ID)
	}
}

// TestShareTTLClamps: the presign law holds for shares — <=0 becomes 1h,
// over 7d becomes 7d (the S3 maximum).
func TestShareTTLClamps(t *testing.T) {
	a := shareApp(t)
	for _, tc := range []struct {
		ttl  int
		want time.Duration
	}{{0, time.Hour}, {-5, time.Hour}, {30 * 86400, 7 * 24 * time.Hour}, {7 * 86400, 7 * 24 * time.Hour}} {
		s, err := a.CreateShare("", "b", "k", tc.ttl)
		if err != nil {
			t.Fatal(err)
		}
		created, _ := time.Parse(time.RFC3339, s.Created)
		exp, _ := time.Parse(time.RFC3339, s.Expires)
		if got := exp.Sub(created); got != tc.want {
			t.Errorf("ttl %d: granted %v, want %v", tc.ttl, got, tc.want)
		}
	}
}

// TestStopShareRemovesAndRefuses: stop clears the entry, unknown ids
// answer ErrNotFound, and expiry is recomputed at list time.
func TestStopShareRemovesAndRefuses(t *testing.T) {
	a := shareApp(t)
	s, err := a.CreateShare("", "b", "k", 60)
	if err != nil {
		t.Fatal(err)
	}
	// seat an already-expired entry directly (the registry's own shape)
	a.shareMu.Lock()
	old := time.Now().Add(-time.Minute)
	a.shares["sh-old"] = &shareEntry{info: ShareInfo{
		ID: "sh-old", Kind: "presign", Bucket: "b", Key: "old",
		Created: old.Add(-time.Hour).Format(time.RFC3339),
		Expires: old.Format(time.RFC3339),
	}, expires: old}
	a.shareMu.Unlock()

	list := a.ListShares()
	if len(list) != 2 || !list[1].Expired {
		t.Fatalf("expired flag not recomputed: %+v", list)
	}
	if err := a.StopShare(s.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.ListShares(); len(got) != 1 || got[0].ID != "sh-old" {
		t.Fatalf("stop left the wrong set: %+v", got)
	}
	if err := a.StopShare("nope"); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("StopShare(unknown) = %v, want ErrNotFound", err)
	}
}

// TestPresignObjectStillSigns: the PresignObject refactor onto
// presignGet keeps the quick-copy verb's contract.
func TestPresignObjectStillSigns(t *testing.T) {
	a := shareApp(t)
	url, err := a.PresignObject("b", "k", 900)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "X-Amz-Signature=") || !strings.Contains(url, "X-Amz-Expires=900") {
		t.Fatalf("presign url degraded: %s", url)
	}
	if got := a.ListShares(); len(got) != 0 {
		t.Fatalf("a quick presign must not register a share: %+v", got)
	}
}

package doctor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// hermeticClient builds a client that never leaves localhost: an IP endpoint
// short-circuits DNS, TCP to a closed port fails fast, and the plain-HTTP
// endpoint makes the TLS check a skip. No real S3 access happens.
func hermeticClient() *s3client.Client {
	return &s3client.Client{
		ProviderKey: "minio",
		Endpoint:    "http://127.0.0.1:1",
		Region:      "us-east-1",
	}
}

func TestCheckNames_RegistryCompleteness(t *testing.T) {
	names := CheckNames()
	if len(names) != 6 {
		t.Fatalf("CheckNames returned %d names, want 6: %v", len(names), names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" {
			t.Error("empty check name in CheckNames")
		}
		if seen[n] {
			t.Errorf("duplicate check name %q", n)
		}
		seen[n] = true
	}

	// Every reported check from Run must be a known registry entry.
	r := Run(context.Background(), hermeticClient(), "", false)
	for _, chk := range r.Checks {
		if !knownCheck(chk.Check) {
			t.Errorf("Run produced check %q which CheckNames does not list", chk.Check)
		}
	}

	// And Run's checks appear in CheckNames' stable order.
	pos := map[string]int{}
	for i, n := range names {
		pos[n] = i
	}
	last := -1
	for _, chk := range r.Checks {
		if p, ok := pos[chk.Check]; ok {
			if p < last {
				t.Errorf("check %q out of registry order", chk.Check)
			}
			last = p
		}
	}
}

func TestRunCheck_UnknownName(t *testing.T) {
	// Unknown names must be rejected before any client dereference, so a
	// nil client is safe here and proves the error comes from validation.
	_, err := RunCheck(context.Background(), nil, "bucket", "no such check", false)
	if err == nil {
		t.Fatal("expected error for unknown check name, got nil")
	}
	if !strings.HasPrefix(err.Error(), "unknown check: ") {
		t.Errorf("error = %q, want prefix %q", err.Error(), "unknown check: ")
	}
	if err.Error() != "unknown check: no such check" {
		t.Errorf("error = %q, want %q", err.Error(), "unknown check: no such check")
	}
}

func TestRunCheck_TimestampsSetAndOrdered(t *testing.T) {
	// DNS against an IP host is fully hermetic (no resolver, no network).
	res, err := RunCheck(context.Background(), hermeticClient(), "", "DNS Resolution Check", false)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if res.StartedAt.IsZero() {
		t.Error("StartedAt not set")
	}
	if res.FinishedAt.IsZero() {
		t.Error("FinishedAt not set")
	}
	if res.FinishedAt.Before(res.StartedAt) {
		t.Errorf("FinishedAt %v before StartedAt %v", res.FinishedAt, res.StartedAt)
	}
	if res.Duration < 0 {
		t.Errorf("Duration = %d, want >= 0", res.Duration)
	}
}

func TestRun_FillsTimestampsPerCheck(t *testing.T) {
	r := Run(context.Background(), hermeticClient(), "", false)
	if len(r.Checks) == 0 {
		t.Fatal("Run produced no checks")
	}
	for _, chk := range r.Checks {
		if chk.StartedAt.IsZero() || chk.FinishedAt.IsZero() {
			t.Errorf("check %q missing StartedAt/FinishedAt", chk.Check)
			continue
		}
		if chk.FinishedAt.Before(chk.StartedAt) {
			t.Errorf("check %q: FinishedAt %v before StartedAt %v", chk.Check, chk.FinishedAt, chk.StartedAt)
		}
		if want := chk.FinishedAt.Sub(chk.StartedAt).Milliseconds(); chk.Duration != want {
			t.Errorf("check %q: Duration = %d, want %d", chk.Check, chk.Duration, want)
		}
	}
}

func TestTimedCheck_WrapsFunction(t *testing.T) {
	res := timedCheck(func() CheckResult {
		time.Sleep(2 * time.Millisecond)
		return CheckResult{Check: "x", Status: StatusPass}
	})
	if res.FinishedAt.Before(res.StartedAt) {
		t.Fatalf("timestamps not ordered: %v -> %v", res.StartedAt, res.FinishedAt)
	}
	if res.Duration < 1 {
		t.Errorf("Duration = %d, want >= 1", res.Duration)
	}
}

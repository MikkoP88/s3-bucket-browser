package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
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
	if len(names) != 7 {
		t.Fatalf("CheckNames returned %d names, want 7: %v", len(names), names)
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

// skewServer stages an HTTP server whose every response carries the given
// Date header; a zero time suppresses the header entirely (the net/http
// documented way).
func skewServer(t *testing.T, date time.Time) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if date.IsZero() {
			w.Header()["Date"] = nil
		} else {
			w.Header().Set("Date", date.UTC().Format(http.TimeFormat))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckClockSkew_Verdicts(t *testing.T) {
	cases := []struct {
		name       string
		date       time.Time // zero suppresses the Date header
		wantStatus Status
		wantDetail string
	}{
		{"aligned clocks pass", time.Now(), StatusPass, ""},
		{"local clock 20m ahead fails", time.Now().Add(-20 * time.Minute), StatusFail, "ahead of"},
		{"local clock 8m behind warns", time.Now().Add(8 * time.Minute), StatusWarn, "behind"},
		{"missing Date header warns", time.Time{}, StatusWarn, "no Date header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := skewServer(t, tc.date)
			res := checkClockSkew(context.Background(),
				provider.Hostname(srv.URL), provider.Port(srv.URL), srv.URL, false)
			if res.Check != "Clock Skew Check" {
				t.Errorf("check name = %q", res.Check)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q (detail %q, error %q)", res.Status, tc.wantStatus, res.Detail, res.Error)
			}
			if tc.wantDetail != "" && !strings.Contains(res.Detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", res.Detail, tc.wantDetail)
			}
			if tc.wantStatus == StatusFail {
				if res.Advice == nil {
					t.Fatal("fail verdict should carry the RequestTimeTooSkewed advice")
				}
				if !strings.Contains(strings.ToLower(res.Advice.Cause), "clock") {
					t.Errorf("advice cause = %q, want the clock cause", res.Advice.Cause)
				}
			}
		})
	}
}

func TestRunCheck_ClockSkew(t *testing.T) {
	// A live server through the RunCheck surface: pass verdict, timestamps
	// and the Info payload all land.
	srv := skewServer(t, time.Now())
	c := &s3client.Client{ProviderKey: "minio", Endpoint: srv.URL, Region: "us-east-1"}
	res, err := RunCheck(context.Background(), c, "", "Clock Skew Check", false)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("status = %q, want pass (detail %q)", res.Status, res.Detail)
	}
	if res.StartedAt.IsZero() || res.FinishedAt.IsZero() {
		t.Error("timestamps not set")
	}
	var info struct {
		SkewSeconds int64  `json:"skewSeconds"`
		ServerTime  string `json:"serverTime"`
		LocalTime   string `json:"localTime"`
	}
	if err := json.Unmarshal(res.Info, &info); err != nil {
		t.Fatalf("Info not valid JSON: %v", err)
	}
	if info.ServerTime == "" || info.LocalTime == "" {
		t.Errorf("Info missing server/local time: %+v", info)
	}

	// Unreachable endpoint: the probe cannot complete, and reachability is
	// owned by the TCP/TLS checks — the honest verdict is skip.
	res, err = RunCheck(context.Background(), hermeticClient(), "", "Clock Skew Check", false)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if res.Status != StatusSkip {
		t.Errorf("status = %q, want skip (detail %q)", res.Status, res.Detail)
	}
}

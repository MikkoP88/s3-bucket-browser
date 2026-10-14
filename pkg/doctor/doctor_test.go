package doctor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
	if len(names) != 9 {
		t.Fatalf("CheckNames returned %d names, want 9: %v", len(names), names)
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

// staticCreds satisfies the SDK's credentials provider without touching the
// environment (the default chain would read env vars and config files).
type staticCreds struct{}

func (staticCreds) Retrieve(context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
}

// sdkClient builds a client whose SDK handle dials the given test server.
func sdkClient(url string) *s3client.Client {
	return &s3client.Client{
		ProviderKey: "minio",
		Endpoint:    url,
		Region:      "us-east-1",
		S3: s3.New(s3.Options{
			Region:       "us-east-1",
			BaseEndpoint: aws.String(url),
			Credentials:  staticCreds{},
			UsePathStyle: true,
		}),
	}
}

const listBucketsXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>0123456789abcdef</ID><DisplayName>demo-owner</DisplayName></Owner>
  <Buckets>
    <Bucket><Name>alpha</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket>
    <Bucket><Name>beta</Name><CreationDate>2026-01-02T00:00:00.000Z</CreationDate></Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

// fakeS3Store records what the write probes actually landed.
type fakeS3Store struct{ puts, dels []string }

// fakeS3Server stages a minimal S3-compatible endpoint: ListBuckets answers
// a fixed owner and two buckets, PUT/DELETE record the keys they see, and
// the toggles force the write legs onto their failure roads.
func fakeS3Server(t *testing.T, store *fakeS3Store, forbidPut, failDelete bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, listBucketsXML)
		case r.Method == http.MethodPut:
			if forbidPut {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			store.puts = append(store.puts, strings.TrimPrefix(r.URL.Path, "/"))
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			if failDelete {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			store.dels = append(store.dels, strings.TrimPrefix(r.URL.Path, "/"))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodHead:
			// HeadBucket: every bucket the fake names exists.
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckIdentity_OwnerAndBuckets(t *testing.T) {
	srv := fakeS3Server(t, &fakeS3Store{}, false, false)
	res := checkIdentity(context.Background(), sdkClient(srv.URL))
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (error %q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Detail, "demo-owner") || !strings.Contains(res.Detail, "2 bucket") {
		t.Errorf("detail = %q, want owner and bucket count", res.Detail)
	}
	var info struct {
		Owner   string `json:"owner"`
		Buckets int    `json:"buckets"`
	}
	if err := json.Unmarshal(res.Info, &info); err != nil {
		t.Fatalf("Info not valid JSON: %v", err)
	}
	if info.Owner != "demo-owner" || info.Buckets != 2 {
		t.Errorf("Info = %+v, want demo-owner/2", info)
	}
}

func TestCheckIdentity_ScopedCredsWarn(t *testing.T) {
	// Keys that cannot list buckets still browse a pinned bucket: warn, not fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	res := checkIdentity(context.Background(), sdkClient(srv.URL))
	if res.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (error %q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "403") {
		t.Errorf("error = %q, want the 403 verdict", res.Error)
	}
}

func TestCheckWriteRoundTrip_WritesAndDeletes(t *testing.T) {
	store := &fakeS3Store{}
	srv := fakeS3Server(t, store, false, false)
	res := checkWriteRoundTrip(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (error %q)", res.Status, res.Error)
	}
	if len(store.puts) != 1 || len(store.dels) != 1 {
		t.Fatalf("server saw puts=%v dels=%v, want exactly one of each", store.puts, store.dels)
	}
	if !strings.HasPrefix(store.puts[0], "probe-bucket/.s3b-doctor-probe-") {
		t.Errorf("probe key = %q, want the bucket-prefixed probe key", store.puts[0])
	}
	if store.puts[0] != store.dels[0] {
		t.Errorf("put key %q != deleted key %q — the probe was not cleaned up", store.puts[0], store.dels[0])
	}
	var info struct {
		ProbeKey string `json:"probeKey"`
		Wrote    bool   `json:"wrote"`
		Deleted  bool   `json:"deleted"`
	}
	if err := json.Unmarshal(res.Info, &info); err != nil {
		t.Fatalf("Info not valid JSON: %v", err)
	}
	if !info.Wrote || !info.Deleted || info.ProbeKey == "" {
		t.Errorf("Info = %+v, want wrote+deleted with the probe key", info)
	}
	if got := "probe-bucket/" + info.ProbeKey; got != store.puts[0] {
		t.Errorf("Info probe key = %q, want the key the server saw (%q)", got, store.puts[0])
	}
}

func TestCheckWriteRoundTrip_ReadOnlyWarns(t *testing.T) {
	store := &fakeS3Store{}
	srv := fakeS3Server(t, store, true, false)
	res := checkWriteRoundTrip(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (error %q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "could not write probe object") {
		t.Errorf("error = %q, want the read-only verdict", res.Error)
	}
	if len(store.puts) != 0 {
		t.Errorf("server saw puts=%v, want none recorded (forbidden)", store.puts)
	}
}

func TestCheckWriteRoundTrip_UndeletedProbeWarns(t *testing.T) {
	store := &fakeS3Store{}
	srv := fakeS3Server(t, store, false, true)
	res := checkWriteRoundTrip(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (error %q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Detail, "remains") {
		t.Errorf("detail = %q, want the leftover-key verdict", res.Detail)
	}
}

func TestRunCheck_WriteProbeNeedsBucket(t *testing.T) {
	srv := fakeS3Server(t, &fakeS3Store{}, false, false)
	res, err := RunCheck(context.Background(), sdkClient(srv.URL), "", "Write Round-trip Check", false)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if res.Status != StatusSkip || !strings.Contains(res.Detail, "no bucket selected") {
		t.Errorf("status = %q detail = %q, want skip/no bucket selected", res.Status, res.Detail)
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct{ key, want string }{
		{"AKIAIOSFODNN7EXAMPLE", "AKIA…MPLE"},
		{"minioadmin", "mini…dmin"},
		{"short", "*****"},
		{"12345678", "********"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := maskKey(tc.key); got != tc.want {
			t.Errorf("maskKey(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestRun_ReportMeta(t *testing.T) {
	// Run against a live fake: the report names its own world — masked key,
	// region, timestamps — and the account-level identity lands even with no
	// bucket selected.
	srv := fakeS3Server(t, &fakeS3Store{}, false, false)
	c := sdkClient(srv.URL)
	c.Profile.AccessKeyID = "AKIAIOSFODNN7EXAMPLE"
	r := Run(context.Background(), c, "", false)
	if r.AccessKey != "AKIA…MPLE" {
		t.Errorf("AccessKey = %q, want the masked form", r.AccessKey)
	}
	if r.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", r.Region)
	}
	if r.RanAt.IsZero() {
		t.Error("RanAt not set")
	}
	if r.TotalMs < 0 {
		t.Errorf("TotalMs = %d, want >= 0", r.TotalMs)
	}
	if r.Summary.Fail != 0 {
		t.Errorf("summary fails = %d, want 0 (checks: %+v)", r.Summary.Fail, r.Checks)
	}
	found := map[string]Status{}
	for _, chk := range r.Checks {
		found[chk.Check] = chk.Status
	}
	if found["Credential Identity Check"] != StatusPass {
		t.Errorf("identity = %q, want pass against the fake owner", found["Credential Identity Check"])
	}
	if found["Write Round-trip Check"] != StatusSkip {
		t.Errorf("write probe = %q, want skip without a bucket", found["Write Round-trip Check"])
	}
	// The masked key must never leak the full credential in JSON either.
	blob, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), "AKIAIOSFODNN7EXAMPLE") {
		t.Error("report JSON leaks the unmasked access key")
	}
}

// The doctor's own probe keys never linger: the write round-trip must clean
// up after itself on the happy road (pinned end-to-end through Run).
func TestRun_WriteProbeAgainstLiveFake(t *testing.T) {
	store := &fakeS3Store{}
	srv := fakeS3Server(t, store, false, false)
	c := sdkClient(srv.URL)
	c.Profile.AccessKeyID = "AKIAIOSFODNN7EXAMPLE"
	r := Run(context.Background(), c, "probe-bucket", false)
	if r.Summary.Fail != 0 {
		t.Fatalf("summary fails = %d, want 0 (checks: %+v)", r.Summary.Fail, r.Checks)
	}
	found := map[string]Status{}
	for _, chk := range r.Checks {
		found[chk.Check] = chk.Status
	}
	if found["Write Round-trip Check"] != StatusPass {
		t.Errorf("write probe = %q, want pass (checks: %+v)", found["Write Round-trip Check"], found)
	}
	if len(store.dels) != 1 || store.dels[0] != store.puts[0] {
		t.Errorf("probe cleanup: puts=%v dels=%v, want the probe key deleted", store.puts, store.dels)
	}
}

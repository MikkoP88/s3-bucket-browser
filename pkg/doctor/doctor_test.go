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
	if len(names) != 10 {
		t.Fatalf("CheckNames returned %d names, want 10: %v", len(names), names)
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
	r := Run(context.Background(), hermeticClient(), "", false, false)
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
	_, err := RunCheck(context.Background(), nil, "bucket", "no such check", false, false)
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
	res, err := RunCheck(context.Background(), hermeticClient(), "", "DNS Resolution Check", false, false)
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
	r := Run(context.Background(), hermeticClient(), "", false, false)
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
	res, err := RunCheck(context.Background(), c, "", "Clock Skew Check", false, false)
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
	res, err = RunCheck(context.Background(), hermeticClient(), "", "Clock Skew Check", false, false)
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

// fakeS3Store records what the write probes actually landed. live holds the
// keys the store still lists (PUT adds, DELETE clears, stored path-prefixed
// as the wire sees them), probeLists records the doctor's cleanup
// verifications — each listing the settling ladder made — and
// lieDelete/dropFirstDelete force the delete legs onto their dishonest
// roads: a delete that acknowledges without effect — the field ghost the
// verify ladder exists for.
type fakeS3Store struct {
	puts, dels, probeLists []string
	live                   map[string]bool
	lieDelete              bool
	dropFirstDelete        bool
	// mpus is the incomplete-upload set ListMultipartUploads answers
	// (bucket-scoped check reads it; nil = none, a clean pass).
	mpus []fakeMPU
}

// fakeMPU is one in-progress upload the fake provider holds.
type fakeMPU struct {
	Key, ID   string
	Initiated time.Time
}

// fakeS3Server stages a minimal S3-compatible endpoint: ListBuckets answers
// a fixed owner and two buckets, PUT/DELETE record the keys they see,
// ListObjectsV2 answers from the live set in bare keys (the ladder's
// oracle — the reader's listing, not a HEAD the in-flight write can fool),
// HEAD answers HeadBucket, and the toggles force the write legs onto their
// failure roads.
func fakeS3Server(t *testing.T, store *fakeS3Store, forbidPut, failDelete bool) *httptest.Server {
	t.Helper()
	if store.live == nil {
		store.live = map[string]bool{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, listBucketsXML)
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			// ListObjectsV2: keys live path-prefixed; the listing speaks
			// the bare keys the ladder asked about.
			bucket := key
			prefix := r.URL.Query().Get("prefix")
			if strings.Contains(prefix, ".s3b-doctor-probe-") {
				store.probeLists = append(store.probeLists, prefix)
			}
			w.Header().Set("Content-Type", "application/xml")
			var b strings.Builder
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>`)
			b.WriteString(bucket)
			b.WriteString(`</Name><Prefix>`)
			b.WriteString(prefix)
			b.WriteString(`</Prefix><IsTruncated>false</IsTruncated>`)
			for k, l := range store.live {
				if !l || !strings.HasPrefix(k, bucket+"/") {
					continue
				}
				if bare := k[len(bucket)+1:]; strings.HasPrefix(bare, prefix) {
					b.WriteString(`<Contents><Key>`)
					b.WriteString(bare)
					b.WriteString(`</Key></Contents>`)
				}
			}
			b.WriteString(`</ListBucketResult>`)
			io.WriteString(w, b.String())
		case r.Method == http.MethodGet && r.URL.Query().Has("uploads"):
			// ListMultipartUploads: the incomplete-upload set, oldest first.
			w.Header().Set("Content-Type", "application/xml")
			var b strings.Builder
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListMultipartUploadsResult>`)
			for _, u := range store.mpus {
				b.WriteString(`<Upload><Key>`)
				b.WriteString(u.Key)
				b.WriteString(`</Key><UploadId>`)
				b.WriteString(u.ID)
				b.WriteString(`</UploadId><Initiated>`)
				b.WriteString(u.Initiated.UTC().Format(time.RFC3339Nano))
				b.WriteString(`</Initiated></Upload>`)
			}
			b.WriteString(`<IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`)
			io.WriteString(w, b.String())
		case r.Method == http.MethodPut:
			if forbidPut {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			store.puts = append(store.puts, key)
			store.live[key] = true
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			if failDelete {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			store.dels = append(store.dels, key)
			dels := 0
			for _, k := range store.dels {
				if k == key {
					dels++
				}
			}
			if !store.lieDelete && !(store.dropFirstDelete && dels == 1) {
				store.live[key] = false
			}
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
		ProbeKey  string `json:"probeKey"`
		Wrote     bool   `json:"wrote"`
		Deleted   bool   `json:"deleted"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := json.Unmarshal(res.Info, &info); err != nil {
		t.Fatalf("Info not valid JSON: %v", err)
	}
	if !info.Wrote || !info.Deleted || !info.Confirmed || info.ProbeKey == "" {
		t.Errorf("Info = %+v, want wrote+deleted+confirmed with the probe key", info)
	}
	if got := "probe-bucket/" + info.ProbeKey; got != store.puts[0] {
		t.Errorf("Info probe key = %q, want the key the server saw (%q)", got, store.puts[0])
	}
	if len(store.probeLists) != 1 || !strings.HasSuffix(store.puts[0], store.probeLists[0]) {
		t.Errorf("probe listings = %v, want exactly one verification listing of the probe key", store.probeLists)
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

func TestCheckWriteRoundTrip_LyingDeleteWarns(t *testing.T) {
	// The field ghost: the store acknowledges the delete while the key
	// keeps listing live. The round-trip must not claim PASS on an ack it
	// could not confirm — the leftover key is named in the same run and
	// the settling ladder stays bounded. The window walks fast here; the
	// production settle lives in confirmSettle.
	oldSettle := confirmSettle
	confirmSettle = []time.Duration{0, time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}
	t.Cleanup(func() { confirmSettle = oldSettle })
	store := &fakeS3Store{lieDelete: true}
	srv := fakeS3Server(t, store, false, false)
	res := checkWriteRoundTrip(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (error %q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "unconfirmed") || !strings.Contains(res.Error, "still lists live") {
		t.Errorf("error = %q, want the unconfirmed verdict naming the live key", res.Error)
	}
	if !strings.Contains(res.Detail, "may remain") {
		t.Errorf("detail = %q, want the may-remain verdict", res.Detail)
	}
	if len(store.dels) != 4 || len(store.probeLists) != 4 {
		t.Errorf("server saw dels=%v listings=%v, want the bounded ladder (ack + 3 retries across 4 listings)", store.dels, store.probeLists)
	}
	var info struct {
		ProbeKey  string `json:"probeKey"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := json.Unmarshal(res.Info, &info); err != nil {
		t.Fatalf("Info not valid JSON: %v", err)
	}
	if info.Confirmed || !strings.HasPrefix("probe-bucket/"+info.ProbeKey, "probe-bucket/.s3b-doctor-probe-") {
		t.Errorf("Info = %+v, want the probe key with confirmed=false", info)
	}
}

func TestCheckWriteRoundTrip_RetryLandsLostDelete(t *testing.T) {
	// The cure road: the first delete is acknowledged and lost, the
	// verification finds the key still live, and the retry lands it — the
	// round-trip passes on its own second attempt.
	oldSettle := confirmSettle
	confirmSettle = []time.Duration{0, time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}
	t.Cleanup(func() { confirmSettle = oldSettle })
	store := &fakeS3Store{dropFirstDelete: true}
	srv := fakeS3Server(t, store, false, false)
	res := checkWriteRoundTrip(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (error %q)", res.Status, res.Error)
	}
	if len(store.dels) != 2 || len(store.probeLists) != 2 {
		t.Errorf("server saw dels=%v listings=%v, want lost delete + retry with two listings", store.dels, store.probeLists)
	}
}

func TestRunCheck_WriteProbeNeedsBucket(t *testing.T) {
	srv := fakeS3Server(t, &fakeS3Store{}, false, false)
	res, err := RunCheck(context.Background(), sdkClient(srv.URL), "", "Write Round-trip Check", false, false)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if res.Status != StatusSkip || !strings.Contains(res.Detail, "no bucket selected") {
		t.Errorf("status = %q detail = %q, want skip/no bucket selected", res.Status, res.Detail)
	}
}

// The write test is opt-in on every surface: neither Run nor RunCheck may
// put so much as one probe object unless the caller explicitly allowed it.
func TestRunCheck_WriteCheckOptIn(t *testing.T) {
	store := &fakeS3Store{}
	srv := fakeS3Server(t, store, false, false)
	res, err := RunCheck(context.Background(), sdkClient(srv.URL), "probe-bucket", "Write Round-trip Check", false, false)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if res.Status != StatusSkip || !strings.Contains(res.Detail, "write test disabled") {
		t.Fatalf("status = %q detail = %q, want skip/write test disabled", res.Status, res.Detail)
	}
	if len(store.puts) != 0 {
		t.Errorf("server saw puts=%v, want none (write not allowed)", store.puts)
	}
	// the explicit ask runs it for real
	res, err = RunCheck(context.Background(), sdkClient(srv.URL), "probe-bucket", "Write Round-trip Check", false, true)
	if err != nil {
		t.Fatalf("RunCheck(allowWrite): %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("status = %q, want pass against the writable fake (error %q)", res.Status, res.Error)
	}
}

func TestRun_WriteCheckDisabledByDefault(t *testing.T) {
	store := &fakeS3Store{}
	srv := fakeS3Server(t, store, false, false)
	r := Run(context.Background(), sdkClient(srv.URL), "probe-bucket", false, false)
	found := map[string]CheckResult{}
	for _, chk := range r.Checks {
		found[chk.Check] = chk
	}
	wr := found["Write Round-trip Check"]
	if wr.Status != StatusSkip || !strings.Contains(wr.Detail, "write test disabled") {
		t.Errorf("write check = %+v, want the opt-in skip", wr)
	}
	if len(store.puts) != 0 {
		t.Errorf("server saw puts=%v, want none (write not allowed)", store.puts)
	}
	// two skips are honest here: the write check (opt-in) and TLS (the
	// plain-HTTP fake cannot speak it)
	if r.Summary.Skip != 2 {
		t.Errorf("summary skip = %d, want write + TLS (summary %+v)", r.Summary.Skip, r.Summary)
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
	r := Run(context.Background(), c, "", false, false)
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
	r := Run(context.Background(), c, "probe-bucket", false, true)
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

func TestCheckUploads_CleanBucketPasses(t *testing.T) {
	srv := fakeS3Server(t, &fakeS3Store{}, false, false)
	res := checkUploads(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (error %q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Detail, "no incomplete multipart uploads") {
		t.Errorf("detail = %q", res.Detail)
	}
}

func TestCheckUploads_CountsAndPrescribesTheCure(t *testing.T) {
	store := &fakeS3Store{mpus: []fakeMPU{
		{Key: "big/late.bin", ID: "id-late", Initiated: time.Now().UTC().Add(-2 * time.Hour)},
		{Key: "big/early.bin", ID: "id-early", Initiated: time.Now().UTC().Add(-26 * time.Hour)},
	}}
	srv := fakeS3Server(t, store, false, false)
	res := checkUploads(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusWarn {
		t.Fatalf("status = %q, want warn", res.Status)
	}
	if !strings.Contains(res.Detail, "2 incomplete multipart upload(s)") || !strings.Contains(res.Detail, "h ago") {
		t.Errorf("detail = %q, want the count and oldest age", res.Detail)
	}
	if !strings.Contains(res.Detail, "1d 2h") { // 26h humanized — the OLDEST, not the newest
		t.Errorf("detail = %q, want the oldest upload's age (1d 2h)", res.Detail)
	}
	if res.Advice == nil || len(res.Advice.Commands) != 1 ||
		!strings.Contains(res.Advice.Commands[0], "s3b bucket uploads s3://probe-bucket --abort --force") {
		t.Errorf("advice = %+v, want the cleanup command", res.Advice)
	}
	var info struct {
		Count  int       `json:"count"`
		Oldest time.Time `json:"oldestInitiated"`
	}
	if err := json.Unmarshal(res.Info, &info); err != nil {
		t.Fatalf("Info not valid JSON: %v", err)
	}
	if info.Count != 2 {
		t.Errorf("Info = %+v, want count 2", info)
	}
}

func TestCheckUploads_UnsupportedProviderSkips(t *testing.T) {
	// A provider without the listing wire skips with its reason — the
	// doctor never fails a bucket for a capability its provider lacks.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		io.WriteString(w, `<Error><Code>NotImplemented</Code><Message>no MPU listing</Message></Error>`)
	}))
	t.Cleanup(srv.Close)
	res := checkUploads(context.Background(), sdkClient(srv.URL), "probe-bucket")
	if res.Status != StatusSkip {
		t.Fatalf("status = %q, want skip", res.Status)
	}
	if !strings.Contains(res.Detail, "not supported by this provider/endpoint") {
		t.Errorf("detail = %q, want the unsupported sentence", res.Detail)
	}
}

package adminops

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// These rigs pin the v2 lifecycle contract against an in-process httptest
// S3 (hermetic — no network, no keyring): the read folds the FULL wire
// grammar (And filter with tag and size range, the transitions list with
// both day and date forms, expiration by date, newer-noncurrent retention,
// noncurrent transitions, abort-MPU) while mirroring the legacy
// transitionDays/Class pair, and the write speaks the same grammar back —
// with the validation refusals and the list-beats-legacy law pinned beside.

// lifecycleFixture is a fake S3 speaking Get/PutBucketLifecycleConfiguration.
// It serves one canned read answer and records the exact PUT body.
type lifecycleFixture struct {
	mu      sync.Mutex
	getBody string // served on GET (?lifecycle=); empty = NoSuchLifecycleConfiguration
	puts    []string
}

func (f *lifecycleFixture) client(t testing.TB) *s3.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Has("lifecycle"):
			if f.getBody == "" {
				http.Error(w, `<Error><Code>NoSuchLifecycleConfiguration</Code></Error>`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, f.getBody)
		case r.Method == http.MethodPut && r.URL.Query().Has("lifecycle"):
			b, _ := io.ReadAll(r.Body)
			f.puts = append(f.puts, string(b))
			io.WriteString(w, `<PutBucketLifecycleConfigurationOutput xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		UsePathStyle: true,
	})
}

const fullWireRead = `<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Rule><ID>full</ID><Prefix>logs/</Prefix><Status>Enabled</Status>
<Filter><And><Prefix>logs/</Prefix><Tag><Key>team</Key><Value>core</Value></Tag>
<ObjectSizeGreaterThan>1024</ObjectSizeGreaterThan><ObjectSizeLessThan>1048576</ObjectSizeLessThan></And></Filter>
<Transition><Days>30</Days><StorageClass>STANDARD_IA</StorageClass></Transition>
<Transition><Date>2030-01-01T00:00:00Z</Date><StorageClass>GLACIER</StorageClass></Transition>
<Expiration><Date>2031-06-01T00:00:00Z</Date></Expiration>
<NoncurrentVersionExpiration><NoncurrentDays>14</NoncurrentDays><NewerNoncurrentVersions>3</NewerNoncurrentVersions></NoncurrentVersionExpiration>
<NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>GLACIER_IR</StorageClass></NoncurrentVersionTransition>
<AbortIncompleteMultipartUpload><DaysAfterInitiation>2</DaysAfterInitiation></AbortIncompleteMultipartUpload>
</Rule>
<Rule><ID>markers</ID><Prefix>tmp/</Prefix><Status>Disabled</Status>
<Filter><Prefix>tmp/</Prefix></Filter>
<Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration>
</Rule>
</LifecycleConfiguration>`

func TestGetLifecycleFoldsTheFullContract(t *testing.T) {
	f := &lifecycleFixture{getBody: fullWireRead}
	c := f.client(t)
	rules, err := GetLifecycle(context.Background(), c, "bench")
	if err != nil {
		t.Fatalf("GetLifecycle: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2: %+v", len(rules), rules)
	}
	r := rules[0]
	if !r.Enabled || r.ID != "full" || r.Prefix != "logs/" {
		t.Fatalf("head fold: %+v", r)
	}
	if r.TagKey != "team" || r.TagValue != "core" {
		t.Fatalf("tag fold: %q=%q", r.TagKey, r.TagValue)
	}
	if r.MinBytes != 1024 || r.MaxBytes != 1048576 {
		t.Fatalf("size range fold: %d..%d", r.MinBytes, r.MaxBytes)
	}
	if len(r.Transitions) != 2 {
		t.Fatalf("transitions = %d, want both: %+v", len(r.Transitions), r.Transitions)
	}
	if r.Transitions[0].Days != 30 || r.Transitions[0].StorageClass != "STANDARD_IA" || r.Transitions[0].Date != "" {
		t.Fatalf("transition[0] = %+v", r.Transitions[0])
	}
	if r.Transitions[1].Date != "2030-01-01" || r.Transitions[1].Days != 0 || r.Transitions[1].StorageClass != "GLACIER" {
		t.Fatalf("transition[1] = %+v (date form, not days)", r.Transitions[1])
	}
	// the legacy mirror rides the first days-form transition
	if r.TransitionDays != 30 || r.TransitionClass != "STANDARD_IA" {
		t.Fatalf("legacy mirror = %d/%q, want 30/STANDARD_IA", r.TransitionDays, r.TransitionClass)
	}
	if r.ExpirationDate != "2031-06-01" || r.ExpirationDays != 0 {
		t.Fatalf("expiration fold: days=%d date=%q", r.ExpirationDays, r.ExpirationDate)
	}
	if r.NoncurrentDays != 14 || r.NewerNoncurrent != 3 {
		t.Fatalf("noncurrent fold: %d keep %d", r.NoncurrentDays, r.NewerNoncurrent)
	}
	if len(r.NoncurrentTransitions) != 1 || r.NoncurrentTransitions[0].NoncurrentDays != 7 ||
		r.NoncurrentTransitions[0].StorageClass != "GLACIER_IR" {
		t.Fatalf("noncurrent transitions: %+v", r.NoncurrentTransitions)
	}
	if r.AbortMPUDays != 2 {
		t.Fatalf("abort MPU fold: %d", r.AbortMPUDays)
	}
	if !rules[1].DeleteMarker || rules[1].Enabled {
		// second rule: Disabled, deleteMarker true — the standalone marker law
		t.Fatalf("rule 2 fold: deleteMarker=%v enabled=%v", rules[1].DeleteMarker, rules[1].Enabled)
	}
}

func TestGetLifecycleNoneConfiguredIsEmpty(t *testing.T) {
	f := &lifecycleFixture{}
	c := f.client(t)
	rules, err := GetLifecycle(context.Background(), c, "bench")
	if err != nil || len(rules) != 0 {
		t.Fatalf("none-configured = %v, %v; want empty, nil", rules, err)
	}
}

func TestPutLifecycleSpeaksTheFullWire(t *testing.T) {
	f := &lifecycleFixture{}
	c := f.client(t)
	err := PutLifecycle(context.Background(), c, "bench", []LifecycleRule{{
		ID: "full", Enabled: true, Prefix: "logs/",
		TagKey: "team", TagValue: "core", MinBytes: 1024, MaxBytes: 1048576,
		Transitions: []LifecycleTransition{
			{Days: 30, StorageClass: "STANDARD_IA"},
			{Date: "2030-01-01", StorageClass: "GLACIER"},
		},
		ExpirationDate:        "2031-06-01",
		NoncurrentDays:        14,
		NewerNoncurrent:       3,
		NoncurrentTransitions: []NoncurrentTransition{{NoncurrentDays: 7, StorageClass: "GLACIER_IR"}},
		AbortMPUDays:          2,
	}})
	if err != nil {
		t.Fatalf("PutLifecycle: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.puts) != 1 {
		t.Fatalf("puts = %d, want 1", len(f.puts))
	}
	body := f.puts[0]
	// the SDK serializes XML in schema order — alphabetical within each
	// element — so the exact-wire pins ride that order
	for _, want := range []string{
		"<Status>Enabled</Status>",
		"<And><ObjectSizeGreaterThan>1024</ObjectSizeGreaterThan><ObjectSizeLessThan>1048576</ObjectSizeLessThan>" +
			"<Prefix>logs/</Prefix><Tag><Key>team</Key><Value>core</Value></Tag></And>",
		"<Transition><Days>30</Days><StorageClass>STANDARD_IA</StorageClass></Transition>",
		"<Transition><Date>2030-01-01T00:00:00Z</Date><StorageClass>GLACIER</StorageClass></Transition>",
		"<Expiration><Date>2031-06-01T00:00:00Z</Date><ExpiredObjectDeleteMarker>false</ExpiredObjectDeleteMarker></Expiration>",
		"<NoncurrentVersionExpiration><NewerNoncurrentVersions>3</NewerNoncurrentVersions><NoncurrentDays>14</NoncurrentDays></NoncurrentVersionExpiration>",
		"<NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>GLACIER_IR</StorageClass></NoncurrentVersionTransition>",
		"<AbortIncompleteMultipartUpload><DaysAfterInitiation>2</DaysAfterInitiation></AbortIncompleteMultipartUpload>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("PUT body missing %q\nbody: %s", want, body)
		}
	}
}

func TestPutLifecycleLegacyPairStandsInAndLosesToList(t *testing.T) {
	f := &lifecycleFixture{}
	c := f.client(t)
	// legacy-only rule: exactly one transition on the wire
	if err := PutLifecycle(context.Background(), c, "bench", []LifecycleRule{{
		ID: "legacy", Enabled: true, Prefix: "logs/",
		TransitionDays: 90, TransitionClass: "GLACIER", ExpirationDays: 365,
	}}); err != nil {
		t.Fatalf("PutLifecycle legacy: %v", err)
	}
	// both forms: the LIST is the truth — the legacy pair must not duplicate
	if err := PutLifecycle(context.Background(), c, "bench", []LifecycleRule{{
		ID: "both", Enabled: true, Prefix: "logs/",
		TransitionDays: 90, TransitionClass: "DEEP_ARCHIVE",
		Transitions: []LifecycleTransition{{Days: 10, StorageClass: "STANDARD_IA"}},
	}}); err != nil {
		t.Fatalf("PutLifecycle both: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.puts) != 2 {
		t.Fatalf("puts = %d, want 2", len(f.puts))
	}
	if got := strings.Count(f.puts[0], "<Transition>"); got != 1 {
		t.Fatalf("legacy rule carries %d transitions, want 1: %s", got, f.puts[0])
	}
	if !strings.Contains(f.puts[0], "<Transition><Days>90</Days><StorageClass>GLACIER</StorageClass></Transition>") {
		t.Fatalf("legacy pair did not stand in: %s", f.puts[0])
	}
	if got := strings.Count(f.puts[1], "<Transition>"); got != 1 {
		t.Fatalf("both-forms rule carries %d transitions, want the list's 1 only: %s", got, f.puts[1])
	}
	if !strings.Contains(f.puts[1], "STANDARD_IA") || strings.Contains(f.puts[1], "DEEP_ARCHIVE") {
		t.Fatalf("the transitions list must beat the legacy pair: %s", f.puts[1])
	}
}

func TestPutLifecycleValidationRefusals(t *testing.T) {
	f := &lifecycleFixture{}
	c := f.client(t)
	cases := []struct {
		name string
		rule LifecycleRule
		want string
	}{
		{"does nothing", LifecycleRule{ID: "noop", Enabled: true}, "does nothing"},
		{"expiration days and date", LifecycleRule{ID: "x", Enabled: true, Prefix: "p/",
			ExpirationDays: 10, ExpirationDate: "2030-01-01"}, "mutually exclusive"},
		{"bad expiration date", LifecycleRule{ID: "x", Enabled: true, Prefix: "p/",
			ExpirationDate: "01/02/2030"}, "not YYYY-MM-DD"},
		{"bad transition date", LifecycleRule{ID: "x", Enabled: true, Prefix: "p/",
			Transitions: []LifecycleTransition{{Date: "soon", StorageClass: "GLACIER"}}}, "not YYYY-MM-DD"},
		{"inverted size range", LifecycleRule{ID: "x", Enabled: true,
			MinBytes: 5000, MaxBytes: 1000}, "not below max"},
	}
	for _, tc := range cases {
		err := PutLifecycle(context.Background(), c, "bench", []LifecycleRule{tc.rule})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.puts) != 0 {
		t.Fatalf("validation refusals must touch nothing: %d puts", len(f.puts))
	}
}

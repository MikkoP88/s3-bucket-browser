package s3client

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestNewLocalEndpoint(t *testing.T) {
	p := profile.Profile{
		Name:        "local",
		Endpoint:    "http://localhost:9000",
		AccessKeyID: "minioadmin",
		SecretKey:   "minioadmin",
		PathStyle:   true,
	}
	c, err := New(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "http://localhost:9000" {
		t.Errorf("Endpoint = %q", c.Endpoint)
	}
	if !c.PathStyle {
		t.Error("path-style should be on")
	}
	if c.ProviderKey != "minio" { // loopback endpoints are assumed to be MinIO labs
		t.Errorf("ProviderKey = %q, want minio", c.ProviderKey)
	}
	if c.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1 default", c.Region)
	}
}

func TestNewOverrides(t *testing.T) {
	p := profile.Profile{
		Name:        "minio",
		Endpoint:    "minio.example.com",
		AccessKeyID: "k",
		SecretKey:   "s",
		PathStyle:   true,
	}
	pathStyleFalse := false
	c, err := New(context.Background(), p, Options{
		EndpointURL: "https://other.example.com",
		Region:      "eu-west-1",
		PathStyle:   &pathStyleFalse,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "https://other.example.com" {
		t.Errorf("endpoint override not applied: %q", c.Endpoint)
	}
	if c.Region != "eu-west-1" {
		t.Errorf("region override not applied: %q", c.Region)
	}
	if c.PathStyle {
		t.Error("path-style override not applied")
	}
}

func TestNewAWSDefault(t *testing.T) {
	// No endpoint: AWS default resolution, no explicit credentials needed
	// (will use environment/shared config chain at request time).
	c, err := New(context.Background(), profile.Profile{Name: "aws"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty (AWS default)", c.Endpoint)
	}
	if c.ProviderKey != "aws" {
		t.Errorf("ProviderKey = %q, want aws", c.ProviderKey)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Errorf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty = %q", got)
	}
}

// blackholeLis accepts TCP connections and never answers — a dead endpoint
// (connection established, then silence forever).
func blackholeLis(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c // hold the connection open; never read, never answer
		}
	}()
	return "http://" + l.Addr().String()
}

// The timeout budget must bound the WHOLE SDK call, not each retry attempt:
// with http.Client.Timeout the SDK's standard retryer classified the timeout
// as retryable and paid the full budget again per attempt (3 × budget +
// backoff against a dead endpoint). With the deadline on the request
// context the attempt error is terminal — one budget, one attempt.
func TestTimeoutIsOneBudgetNotPerAttempt(t *testing.T) {
	url := blackholeLis(t)
	c, err := New(context.Background(), profile.Profile{
		Endpoint: url, Region: "us-east-1", PathStyle: true,
		AccessKeyID: "k", SecretKey: "s",
	}, Options{Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	_, err = c.S3.ListBuckets(context.Background(), &s3.ListBucketsInput{})
	elapsed := time.Since(t0)
	if err == nil {
		t.Fatal("ListBuckets unexpectedly succeeded against a dead endpoint")
	}
	if elapsed > 900*time.Millisecond {
		t.Errorf("call took %v — the budget was paid more than once (retried)", elapsed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "deadline") && !strings.Contains(msg, "canceled") && !strings.Contains(msg, "timeout") {
		t.Errorf("error is not timeout-shaped: %v", err)
	}
}

// The budget covers body streaming, not just headers: a response that
// starts and then stalls mid-body must abort within the budget.
func TestTimeoutCoversBodyStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		time.Sleep(30 * time.Second) // stall forever (mid-body)
	}))
	t.Cleanup(srv.CloseClientConnections) // Close() would wait out the handler
	client := &http.Client{Transport: perRequestBudget{base: http.DefaultTransport, budget: 150 * time.Millisecond}}
	t0 := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("headers should arrive within the budget: %v", err)
	}
	defer resp.Body.Close()
	if _, err = io.ReadAll(resp.Body); err == nil {
		t.Fatal("stalled body read unexpectedly succeeded")
	}
	if elapsed := time.Since(t0); elapsed > 2*time.Second {
		t.Errorf("stalled body read took %v — budget not enforced on the stream", elapsed)
	}
}

// A caller deadline sooner than the budget must be kept as-is: the budget
// never extends an existing deadline, it only caps deadline-less requests.
func TestBudgetKeepsSoonerCallerDeadline(t *testing.T) {
	url := blackholeLis(t)
	client := &http.Client{Transport: perRequestBudget{base: http.DefaultTransport, budget: 30 * time.Second}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(req.Context(), 150*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	t0 := time.Now()
	if _, err = client.Do(req); err == nil {
		t.Fatal("request unexpectedly succeeded against a dead endpoint")
	}
	if elapsed := time.Since(t0); elapsed > 2*time.Second {
		t.Errorf("call took %v — the sooner caller deadline was ignored", elapsed)
	}
}

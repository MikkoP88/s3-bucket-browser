// Package s3client builds configured AWS SDK S3 clients from profiles.
package s3client

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Options are per-invocation overrides applied on top of the profile
// (populated from global CLI flags).
type Options struct {
	EndpointURL  string // override profile endpoint
	Region       string // override profile region
	PathStyle    *bool  // override profile addressing
	AccessKey    string // override credentials
	SecretKey    string
	SessionToken string
	// Timeout bounds every HTTP request (headers AND body streaming) with a
	// request-context deadline. 0 = 30s default; negative = no deadline at
	// all (caller bounds ops with contexts/watchdogs).
	Timeout time.Duration
}

// Client bundles the SDK client with the resolved configuration.
type Client struct {
	S3           *s3.Client
	Profile      profile.Profile
	ProviderKey  string
	ProviderCaps provider.Capabilities
	Endpoint     string // resolved endpoint ("" = AWS default)
	Region       string
	PathStyle    bool
}

// New builds an S3 client for the profile with optional overrides.
func New(ctx context.Context, p profile.Profile, opts Options) (*Client, error) {
	endpoint := p.Endpoint
	if opts.EndpointURL != "" {
		endpoint = opts.EndpointURL
	}
	endpoint = provider.NormalizeEndpoint(endpoint, p.Insecure)

	region := p.Region
	if opts.Region != "" {
		region = opts.Region
	}
	if region == "" {
		region = "us-east-1"
	}

	pathStyle := p.PathStyle
	if opts.PathStyle != nil {
		pathStyle = *opts.PathStyle
	}

	accessKey := firstNonEmpty(opts.AccessKey, p.AccessKeyID)
	secretKey := firstNonEmpty(opts.SecretKey, p.SecretKey)
	sessionToken := firstNonEmpty(opts.SessionToken, p.SessionToken)

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	// Negative Timeout asks for NO whole-request deadline: long transfers
	// are bounded by caller-side cancellation/context instead (the GUI
	// watchdog path) — no transport-level budget is installed at all.
	deadlined := timeout > 0

	var credOpts []func(*config.LoadOptions) error
	if accessKey != "" && secretKey != "" {
		credOpts = append(credOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		))
	}

	httpClient := &http.Client{}
	if p.Insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	if deadlined {
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		httpClient.Transport = perRequestBudget{base: base, budget: timeout}
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		append(credOpts,
			config.WithRegion(region),
			config.WithHTTPClient(httpClient),
		)...)
	if err != nil {
		return nil, err
	}

	var sdkClient *s3.Client
	if endpoint != "" {
		sdkClient = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = pathStyle
		})
	} else {
		// AWS default endpoint; path-style still honored when requested.
		sdkClient = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.UsePathStyle = pathStyle
		})
	}

	key := p.Provider()
	if endpoint != "" {
		key = provider.Detect(endpoint)
	}

	return &Client{
		S3:           sdkClient,
		Profile:      p,
		ProviderKey:  key,
		ProviderCaps: provider.Get(key),
		Endpoint:     endpoint,
		Region:       region,
		PathStyle:    pathStyle,
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// perRequestBudget bounds each HTTP request with a request-context deadline
// instead of http.Client.Timeout.
//
// Why not http.Client.Timeout: its "Client.Timeout exceeded" error is a
// plain transport error that the SDK's standard retryer classifies as
// retryable — so every retry attempt paid the whole budget again. Against a
// dead endpoint (accepted, never answering) `--timeout 3s` took 3 attempts
// + backoff ≈ 12s of wall time, and the 5m default meant a ~15-minute hang.
//
// The deadline lives on the request context, and when it fires the error is
// wrapped in budgetExpiredError, which implements CanceledError() — the
// retryer's NoRetryCanceledError classifier (first in its chain) treats
// that as terminal, so the budget is paid exactly once. Smithy's own
// canceled-context override cannot do this for us: it only inspects the
// CALLER's context, which stays alive here. Fast transient failures
// (connection reset, 503) never touch the budget and keep their retries.
type perRequestBudget struct {
	base   http.RoundTripper
	budget time.Duration
}

func (t perRequestBudget) RoundTrip(req *http.Request) (*http.Response, error) {
	// never extend a caller deadline that is already sooner than the budget
	if d, ok := req.Context().Deadline(); ok && time.Until(d) <= t.budget {
		return t.base.RoundTrip(req)
	}
	caller := req.Context()
	ctx, cancel := context.WithTimeout(caller, t.budget)
	req = req.WithContext(ctx)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		cancel()
		if ctx.Err() != nil && caller.Err() == nil {
			return nil, &budgetExpiredError{err: err}
		}
		return nil, err
	}
	// the budget must cover body streaming too; the timer is released when
	// the caller is done reading
	resp.Body = &budgetBody{ReadCloser: resp.Body, ctx: ctx, cancel: cancel}
	return resp, nil
}

// budgetExpiredError marks a request killed by the per-request budget (as
// opposed to caller-side cancellation, which passes through unwrapped).
// CanceledError() makes the SDK retryer treat the attempt as terminal;
// Unwrap and the message keep it deadline-shaped for callers.
type budgetExpiredError struct{ err error }

func (e *budgetExpiredError) Error() string { return "request budget exceeded: " + e.err.Error() }
func (e *budgetExpiredError) Unwrap() error { return e.err }

func (e *budgetExpiredError) CanceledError() bool { return true }
func (e *budgetExpiredError) Timeout() bool       { return true }

// budgetBody carries the budget across body streaming: reads that stall
// past the deadline fail with the same terminal, deadline-shaped error,
// and the deadline timer is released on Close.
type budgetBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (b *budgetBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF && b.ctx.Err() != nil {
		return n, &budgetExpiredError{err: err}
	}
	return n, err
}

func (b *budgetBody) Close() error {
	b.once.Do(b.cancel)
	return b.ReadCloser.Close()
}

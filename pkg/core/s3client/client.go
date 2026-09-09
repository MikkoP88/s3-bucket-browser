// Package s3client builds configured AWS SDK S3 clients from profiles.
package s3client

import (
	"context"
	"crypto/tls"
	"net/http"
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
	Timeout      time.Duration // default 30s
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

	var credOpts []func(*config.LoadOptions) error
	if accessKey != "" && secretKey != "" {
		credOpts = append(credOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		))
	}

	httpClient := &http.Client{Timeout: timeout}
	if p.Insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
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

// Package doctor runs connection diagnostics against an S3 endpoint.
//
// The check pipeline (DNS → TCP → TLS → auth → policy/ACL with plain-language
// remediation) is ported from s3-bucket-tester; the S3 API checks now run
// through the official SDK instead of hand-rolled signed requests.
package doctor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/errhelp"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/policy"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Status is a check outcome.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// CheckResult is one diagnostic result (JSON-friendly, mirroring the
// s3-bucket-tester report contract).
type CheckResult struct {
	Check      string          `json:"check"`
	Status     Status          `json:"status"`
	Duration   int64           `json:"durationMs"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	Detail     string          `json:"detail,omitempty"`
	Error      string          `json:"error,omitempty"`
	Advice     *errhelp.Advice `json:"advice,omitempty"`
	Info       json.RawMessage `json:"info,omitempty"`
}

// Report is the full doctor output.
type Report struct {
	Endpoint string        `json:"endpoint"`
	Bucket   string        `json:"bucket,omitempty"`
	Provider string        `json:"provider"`
	Checks   []CheckResult `json:"checks"`
	Warnings []string      `json:"warnings,omitempty"`
	Summary  Summary       `json:"summary"`
}

// Summary aggregates outcomes.
type Summary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// ExitWorthy reports whether the report contains failures.
func (r *Report) ExitWorthy() bool { return r.Summary.Fail > 0 }

// Run executes all checks for the client and optional bucket.
func Run(ctx context.Context, c *s3client.Client, bucket string, insecureTLS bool) *Report {
	r := &Report{
		Endpoint: c.Endpoint,
		Bucket:   bucket,
		Provider: c.ProviderKey,
	}
	if c.Endpoint == "" {
		r.Endpoint = "https://s3." + c.Region + ".amazonaws.com (AWS default)"
	}

	// Provider capability warnings (ported knowledge base).
	for _, w := range provider.Warnings(c.ProviderKey, c.Endpoint, c.PathStyle) {
		r.Warnings = append(r.Warnings, w)
	}

	host := "s3." + c.Region + ".amazonaws.com"
	var port = 443
	if c.Endpoint != "" {
		host = provider.Hostname(c.Endpoint)
		port = provider.Port(c.Endpoint)
	}

	r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkDNS(host) }))
	if last(r.Checks).Status == StatusFail && net.ParseIP(host) == nil {
		// DNS failed for a hostname: TCP/TLS cannot succeed either.
		r.Checks = append(r.Checks,
			timedCheck(func() CheckResult { return skip("TCP Connectivity Check", "DNS resolution failed") }),
			timedCheck(func() CheckResult { return skip("TLS Certificate Check", "DNS resolution failed") }))
	} else {
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkTCP(host, port) }))
		if provider.InsecureEndpoint(c.Endpoint) {
			r.Checks = append(r.Checks, timedCheck(func() CheckResult { return skip("TLS Certificate Check", "endpoint uses plain HTTP") }))
		} else {
			r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkTLS(host, port, insecureTLS) }))
		}
	}

	if bucket != "" {
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkAuth(ctx, c, bucket) }))
		if last(r.Checks).Status != StatusFail {
			r.Checks = append(r.Checks,
				timedCheck(func() CheckResult { return checkPolicy(ctx, c, bucket) }),
				timedCheck(func() CheckResult { return checkACL(ctx, c, bucket) }))
		} else {
			r.Checks = append(r.Checks,
				timedCheck(func() CheckResult { return skip("Bucket Policy Check", "authentication failed") }),
				timedCheck(func() CheckResult { return skip("Bucket ACL Check", "authentication failed") }))
		}
	}

	for _, chk := range r.Checks {
		switch chk.Status {
		case StatusPass:
			r.Summary.Pass++
		case StatusWarn:
			r.Summary.Warn++
		case StatusFail:
			r.Summary.Fail++
		case StatusSkip:
			r.Summary.Skip++
		}
	}
	return r
}

func last(checks []CheckResult) CheckResult { return checks[len(checks)-1] }

func timedCheck(fn func() CheckResult) CheckResult {
	started := time.Now()
	res := fn()
	res.StartedAt = started
	res.FinishedAt = time.Now()
	res.Duration = res.FinishedAt.Sub(res.StartedAt).Milliseconds()
	return res
}

func skip(name, reason string) CheckResult {
	return CheckResult{Check: name, Status: StatusSkip, Detail: reason}
}

func checkDNS(host string) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "DNS Resolution Check", Status: StatusPass}

	if ip := net.ParseIP(host); ip != nil {
		res.Detail = fmt.Sprintf("host is an IP address (%s), no DNS needed", host)
		res.Duration = ms(time.Since(start))
		return res
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var r net.Resolver
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		res.Advice = errhelp.ForError(err)
		res.Duration = ms(time.Since(start))
		return res
	}
	ips := make([]string, len(addrs))
	for i, a := range addrs {
		ips[i] = a.IP.String()
	}
	info, _ := json.Marshal(map[string]any{"ips": ips})
	res.Info = info
	res.Detail = fmt.Sprintf("resolved %d address(es): %s", len(ips), strings.Join(ips, ", "))
	res.Duration = ms(time.Since(start))
	return res
}

func checkTCP(host string, port int) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "TCP Connectivity Check", Status: StatusPass}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 10*time.Second)
	if err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		res.Advice = errhelp.ForError(err)
		res.Duration = ms(time.Since(start))
		return res
	}
	conn.Close()
	res.Detail = fmt.Sprintf("connected to %s:%d", host, port)
	res.Duration = ms(time.Since(start))
	return res
}

func checkTLS(host string, port int, insecure bool) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "TLS Certificate Check", Status: StatusPass}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp",
		fmt.Sprintf("%s:%d", host, port),
		&tls.Config{ServerName: host, InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12})
	if err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		res.Advice = errhelp.ForError(err)
		res.Duration = ms(time.Since(start))
		return res
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		res.Status = StatusWarn
		res.Detail = "connected without a certificate chain"
		res.Duration = ms(time.Since(start))
		return res
	}
	cert := state.PeerCertificates[0]
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	info, _ := json.Marshal(map[string]any{
		"subject":    cert.Subject.String(),
		"issuer":     cert.Issuer.String(),
		"notAfter":   cert.NotAfter.Format(time.RFC3339),
		"daysLeft":   days,
		"tlsVersion": tlsVersionName(state.Version),
		"verified":   len(state.VerifiedChains) > 0,
	})
	res.Info = info
	res.Detail = fmt.Sprintf("%s issued by %s, expires in %d day(s), %s",
		cert.Subject.CommonName, cert.Issuer.CommonName, days, tlsVersionName(state.Version))

	switch {
	case days < 0:
		res.Status = StatusFail
		res.Detail = "certificate has EXPIRED: " + res.Detail
	case days < 30:
		res.Status = StatusWarn
	case state.Version == tls.VersionTLS10 || state.Version == tls.VersionTLS11:
		res.Status = StatusWarn
		res.Detail += " (deprecated TLS version)"
	}
	res.Duration = ms(time.Since(start))
	return res
}

func checkAuth(ctx context.Context, c *s3client.Client, bucket string) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "Bucket Authentication Check", Status: StatusPass}

	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		res.Detail = "bucket exists and credentials grant access"
		res.Duration = ms(time.Since(start))
		return res
	}

	var nf *s3types.NotFound
	var httpErr *awshttp.ResponseError
	switch {
	case errors.As(err, &nf):
		res.Status = StatusFail
		res.Error = "bucket does not exist (404)"
	case errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == 403:
		// Bucket exists (403 leaks existence) but credentials lack access.
		res.Status = StatusWarn
		res.Error = "bucket exists but access denied (403)"
	case errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == 404:
		res.Status = StatusFail
		res.Error = "bucket does not exist (404)"
	default:
		res.Status = StatusFail
		res.Error = err.Error()
	}
	res.Advice = errhelp.ForError(err)
	res.Duration = ms(time.Since(start))
	return res
}

func checkPolicy(ctx context.Context, c *s3client.Client, bucket string) CheckResult {
	start := time.Now()
	pres := CheckResult{Check: "Bucket Policy Check", Status: StatusPass}
	if !provider.SupportsPolicy(c.ProviderKey) {
		pres.Status = StatusSkip
		pres.Detail = fmt.Sprintf("%s policy support: %s", c.ProviderCaps.Name, c.ProviderCaps.PolicySupport)
		pres.Duration = ms(time.Since(start))
		return pres
	}
	pout, err := c.S3.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	switch {
	case err == nil && pout.Policy != nil:
		doc, perr := policy.ParsePolicy([]byte(*pout.Policy))
		if perr != nil {
			pres.Status = StatusWarn
			pres.Error = perr.Error()
		} else {
			summary := policy.Analyze(doc)
			info, _ := json.Marshal(summary)
			pres.Info = info
			pres.Detail = fmt.Sprintf("%d statement(s)", summary.StatementCount)
			if summary.HasPublicRead || summary.HasPublicWrite {
				pres.Status = StatusWarn
				pres.Detail += "; " + strings.Join(summary.Warnings, "; ")
			}
		}
	case isNoSuchPolicyError(err):
		pres.Detail = "no bucket policy set"
	default:
		pres.Status = StatusWarn
		pres.Error = "could not read policy: " + err.Error()
		pres.Advice = errhelp.ForError(err)
	}
	pres.Duration = ms(time.Since(start))
	return pres
}

func checkACL(ctx context.Context, c *s3client.Client, bucket string) CheckResult {
	start := time.Now()
	ares := CheckResult{Check: "Bucket ACL Check", Status: StatusPass}
	if !provider.SupportsACL(c.ProviderKey) {
		ares.Status = StatusSkip
		ares.Detail = fmt.Sprintf("%s ACL support: %s", c.ProviderCaps.Name, c.ProviderCaps.ACLSupport)
		ares.Duration = ms(time.Since(start))
		return ares
	}
	aout, err := c.S3.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(bucket)})
	if err != nil {
		ares.Status = StatusWarn
		ares.Error = "could not read ACL: " + err.Error()
		ares.Advice = errhelp.ForError(err)
	} else {
		owner := s3types.Owner{}
		if aout.Owner != nil {
			owner = *aout.Owner
		}
		summary := policy.AnalyzeACL(owner, aout.Grants)
		info, _ := json.Marshal(summary)
		ares.Info = info
		ares.Detail = fmt.Sprintf("%d grant(s)", len(aout.Grants))
		if summary.PublicRead || summary.AuthenticatedRead {
			ares.Status = StatusWarn
			ares.Detail += "; " + strings.Join(summary.Warnings, "; ")
		}
		if c.ProviderKey == "minio" {
			ares.Detail += " (MinIO ACLs are synthetic compatibility data)"
		}
	}
	ares.Duration = ms(time.Since(start))
	return ares
}

// CheckNames returns the full list of check names in execution order (stable).
// The GUI uses this to render the available checks before running them.
func CheckNames() []string {
	return []string{
		"DNS Resolution Check",
		"TCP Connectivity Check",
		"TLS Certificate Check",
		"Bucket Authentication Check",
		"Bucket Policy Check",
		"Bucket ACL Check",
	}
}

// RunCheck runs a single doctor check by name for the given bucket.
// Returns an error if the check name is unknown.
func RunCheck(ctx context.Context, c *s3client.Client, bucket, name string, insecureTLS bool) (CheckResult, error) {
	// Validate name before any client dereference.
	if !knownCheck(name) {
		return CheckResult{}, fmt.Errorf("unknown check: %s", name)
	}
	if c == nil {
		return CheckResult{}, fmt.Errorf("client is nil")
	}

	host := "s3." + c.Region + ".amazonaws.com"
	port := 443
	if c.Endpoint != "" {
		host = provider.Hostname(c.Endpoint)
		port = provider.Port(c.Endpoint)
	}

	switch name {
	case "DNS Resolution Check":
		return timedCheck(func() CheckResult { return checkDNS(host) }), nil
	case "TCP Connectivity Check":
		return timedCheck(func() CheckResult { return checkTCP(host, port) }), nil
	case "TLS Certificate Check":
		if provider.InsecureEndpoint(c.Endpoint) {
			return timedCheck(func() CheckResult { return skip("TLS Certificate Check", "endpoint uses plain HTTP") }), nil
		}
		return timedCheck(func() CheckResult { return checkTLS(host, port, insecureTLS) }), nil
	case "Bucket Authentication Check":
		return timedCheck(func() CheckResult { return checkAuth(ctx, c, bucket) }), nil
	case "Bucket Policy Check":
		return timedCheck(func() CheckResult { return checkPolicy(ctx, c, bucket) }), nil
	case "Bucket ACL Check":
		return timedCheck(func() CheckResult { return checkACL(ctx, c, bucket) }), nil
	default:
		// Unreachable (name was validated above), but satisfy the compiler.
		return CheckResult{}, fmt.Errorf("unknown check: %s", name)
	}
}

// knownCheck reports whether name is a check RunCheck can run.
func knownCheck(name string) bool {
	for _, n := range CheckNames() {
		if n == name {
			return true
		}
	}
	return false
}

// isNoSuchPolicyError matches the NoSuchBucketPolicy response.
func isNoSuchPolicyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "nosuchbucketpolicy") || strings.Contains(msg, "no policy")
}

func ms(d time.Duration) int64 { return d.Milliseconds() }

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", v)
	}
}

// SortedCheckNames returns check names in execution order (stable helper for
// tests and the GUI).
func (r *Report) SortedCheckNames() []string {
	names := make([]string, len(r.Checks))
	for i, c := range r.Checks {
		names[i] = c.Check
	}
	sort.Strings(names)
	return names
}

// Package doctor runs connection diagnostics against an S3 endpoint.
//
// The check pipeline (DNS → TCP → TLS → clock skew → auth → policy/ACL with
// plain-language remediation, plus the account-level identity and write
// round-trip probes) is ported from s3-bucket-tester; the S3 API checks now
// run through the official SDK instead of hand-rolled signed requests.
package doctor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
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

// Clock-skew thresholds: SigV4 signs requests with the local clock and
// providers reject anything stamped more than ~15 minutes off the server's
// own — 15 minutes is the failure line, 5 minutes already worth a warning.
const (
	clockSkewWarn = 5 * time.Minute
	clockSkewFail = 15 * time.Minute
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
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket,omitempty"`
	Provider string `json:"provider"`
	// Context the checks ran under: the GUI and the CLI both print these
	// beside the verdicts, so a saved report names its own world.
	Region    string        `json:"region,omitempty"`
	AccessKey string        `json:"accessKey,omitempty"` // masked (first/last 4)
	PathStyle bool          `json:"pathStyle,omitempty"`
	TotalMs   int64         `json:"totalMs,omitempty"`
	RanAt     time.Time     `json:"ranAt"`
	Checks    []CheckResult `json:"checks"`
	Warnings  []string      `json:"warnings,omitempty"`
	Summary   Summary       `json:"summary"`
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

// Run executes all checks for the client and optional bucket. The write
// round-trip is opt-in (allowWrite): a diagnosis must never put a probe
// object into the user's data unless they explicitly asked for it.
func Run(ctx context.Context, c *s3client.Client, bucket string, insecureTLS, allowWrite bool) *Report {
	started := time.Now()
	r := &Report{
		Endpoint:  c.Endpoint,
		Bucket:    bucket,
		Provider:  c.ProviderKey,
		Region:    c.Region,
		PathStyle: c.PathStyle,
	}
	if k := c.Profile.AccessKeyID; k != "" {
		r.AccessKey = maskKey(k)
	}
	if c.Endpoint == "" {
		r.Endpoint = "https://s3." + c.Region + ".amazonaws.com (AWS default)"
	}

	// Provider capability warnings (ported knowledge base).
	r.Warnings = append(r.Warnings, provider.Warnings(c.ProviderKey, c.Endpoint, c.PathStyle)...)

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
			timedCheck(func() CheckResult { return skip("TLS Certificate Check", "DNS resolution failed") }),
			timedCheck(func() CheckResult { return skip("Clock Skew Check", "DNS resolution failed") }))
	} else {
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkTCP(host, port) }))
		if provider.InsecureEndpoint(c.Endpoint) {
			r.Checks = append(r.Checks, timedCheck(func() CheckResult { return skip("TLS Certificate Check", "endpoint uses plain HTTP") }))
		} else {
			r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkTLS(host, port, insecureTLS) }))
		}
		r.Checks = append(r.Checks, timedCheck(func() CheckResult {
			return checkClockSkew(ctx, host, port, c.Endpoint, insecureTLS)
		}))
	}

	// Authentication only meaningfully succeeded with a bucket in hand.
	authFailed := bucket == ""
	if bucket != "" {
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkAuth(ctx, c, bucket) }))
		if last(r.Checks).Status != StatusFail {
			authFailed = false
			r.Checks = append(r.Checks,
				timedCheck(func() CheckResult { return checkPolicy(ctx, c, bucket) }),
				timedCheck(func() CheckResult { return checkACL(ctx, c, bucket) }))
		} else {
			r.Checks = append(r.Checks,
				timedCheck(func() CheckResult { return skip("Bucket Policy Check", "authentication failed") }),
				timedCheck(func() CheckResult { return skip("Bucket ACL Check", "authentication failed") }))
		}
	}

	// Identity is account-level: ListBuckets names the owner behind the
	// credentials even when no bucket is selected.
	r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkIdentity(ctx, c) }))

	switch {
	case bucket == "":
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return skip("Write Round-trip Check", "no bucket selected") }))
	case authFailed:
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return skip("Write Round-trip Check", "authentication failed") }))
	case !allowWrite:
		r.Checks = append(r.Checks, timedCheck(func() CheckResult {
			return skip("Write Round-trip Check", "write test disabled — it is opt-in because it writes (and deletes) a tiny probe object")
		}))
	default:
		r.Checks = append(r.Checks, timedCheck(func() CheckResult { return checkWriteRoundTrip(ctx, c, bucket) }))
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
	r.RanAt = started.UTC()
	r.TotalMs = time.Since(started).Milliseconds()
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

// checkClockSkew compares the local clock against the server's Date response
// header. SigV4 signs requests with the local clock and providers reject
// anything stamped more than ~15 minutes off the server's own — the
// RequestTimeTooSkewed failure this check names before it ever fires.
func checkClockSkew(ctx context.Context, host string, port int, endpoint string, insecureTLS bool) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "Clock Skew Check", Status: StatusPass}

	scheme := "https"
	if provider.InsecureEndpoint(endpoint) {
		scheme = "http"
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureTLS, MinVersion: tls.VersionTLS12},
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		fmt.Sprintf("%s://%s:%d/", scheme, host, port), nil)
	if err != nil {
		res.Status = StatusSkip
		res.Detail = "could not build the probe request"
		res.Duration = ms(time.Since(start))
		return res
	}
	resp, err := client.Do(req)
	if err != nil {
		// Reachability is owned by the TCP/TLS checks; a probe that cannot
		// complete says nothing about the clocks.
		res.Status = StatusSkip
		res.Detail = "probe did not complete (reachability is covered by the TCP and TLS checks)"
		res.Duration = ms(time.Since(start))
		return res
	}
	defer resp.Body.Close()

	dateHdr := resp.Header.Get("Date")
	if dateHdr == "" {
		res.Status = StatusWarn
		res.Detail = "server sent no Date header; clock skew cannot be verified"
		res.Duration = ms(time.Since(start))
		return res
	}
	serverTime, err := time.Parse(http.TimeFormat, dateHdr)
	if err != nil {
		res.Status = StatusWarn
		res.Error = fmt.Sprintf("malformed Date header %q", dateHdr)
		res.Detail = "clock skew cannot be verified"
		res.Duration = ms(time.Since(start))
		return res
	}

	now := time.Now()
	skew := now.Sub(serverTime)
	abs := skew
	if abs < 0 {
		abs = -abs
	}
	direction := "ahead of"
	if skew < 0 {
		direction = "behind"
	}
	info, _ := json.Marshal(map[string]any{
		"skewSeconds": int64(skew.Seconds()),
		"serverTime":  serverTime.UTC().Format(time.RFC3339),
		"localTime":   now.UTC().Format(time.RFC3339),
	})
	res.Info = info
	res.Detail = fmt.Sprintf("local clock is %s the server clock by %s", direction, abs.Truncate(time.Second))

	switch {
	case abs > clockSkewFail:
		res.Status = StatusFail
		res.Detail = "skew beyond the SigV4 signing window (RequestTimeTooSkewed): " + res.Detail
		res.Advice = errhelp.ForCode("RequestTimeTooSkewed")
	case abs > clockSkewWarn:
		res.Status = StatusWarn
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

// checkIdentity asks the account who it is: ListBuckets needs no bucket and
// answers with the canonical owner, so the report can name the identity
// behind the credentials. Keys scoped away from listing warn rather than
// fail — they may still work perfectly against a pinned bucket.
func checkIdentity(ctx context.Context, c *s3client.Client) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "Credential Identity Check", Status: StatusPass}

	if c.S3 == nil {
		// Mirror of RunCheck's nil-client guard: a client without an SDK
		// connection cannot answer who it is.
		res.Status = StatusSkip
		res.Detail = "no S3 client available"
		res.Duration = ms(time.Since(start))
		return res
	}

	out, err := c.S3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		var httpErr *awshttp.ResponseError
		if errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == 403 {
			res.Status = StatusWarn
			res.Error = "credentials cannot list buckets (403)"
			res.Detail = "access may be scoped to a single bucket"
		} else {
			res.Status = StatusFail
			res.Error = err.Error()
		}
		res.Advice = errhelp.ForError(err)
		res.Duration = ms(time.Since(start))
		return res
	}

	owner := "unknown"
	if o := out.Owner; o != nil {
		switch {
		case o.DisplayName != nil && *o.DisplayName != "":
			owner = *o.DisplayName
		case o.ID != nil && *o.ID != "":
			owner = *o.ID
		}
	}
	info, _ := json.Marshal(map[string]any{"owner": owner, "buckets": len(out.Buckets)})
	res.Info = info
	res.Detail = fmt.Sprintf("owner %s, %d bucket(s) visible", owner, len(out.Buckets))
	res.Duration = ms(time.Since(start))
	return res
}

// checkWriteRoundTrip proves the credentials can actually land data: a
// zero-byte probe object is written and deleted again. A read-only policy
// warns (browsing still works); a probe that cannot be cleaned up warns
// separately so the leftover .s3b-doctor-probe-* key is no surprise.
func checkWriteRoundTrip(ctx context.Context, c *s3client.Client, bucket string) CheckResult {
	start := time.Now()
	res := CheckResult{Check: "Write Round-trip Check", Status: StatusPass}

	if c.S3 == nil {
		res.Status = StatusSkip
		res.Detail = "no S3 client available"
		res.Duration = ms(time.Since(start))
		return res
	}

	probe := fmt.Sprintf(".s3b-doctor-probe-%d", time.Now().UnixNano())
	_, err := c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(probe),
		Body:   strings.NewReader(""),
	})
	if err != nil {
		res.Status = StatusWarn
		res.Error = "could not write probe object: " + err.Error()
		res.Advice = errhelp.ForError(err)
		res.Duration = ms(time.Since(start))
		return res
	}

	_, derr := c.S3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(probe)})
	if derr != nil {
		res.Status = StatusWarn
		res.Error = "probe written but not deleted: " + derr.Error()
		res.Detail = "the probe key remains in the bucket"
		res.Advice = errhelp.ForError(derr)
		info, _ := json.Marshal(map[string]any{"probeKey": probe, "wrote": true, "deleted": false})
		res.Info = info
		res.Duration = ms(time.Since(start))
		return res
	}

	info, _ := json.Marshal(map[string]any{"probeKey": probe, "wrote": true, "deleted": true})
	res.Info = info
	res.Detail = "wrote and deleted " + probe
	res.Duration = ms(time.Since(start))
	return res
}

// maskKey renders an access key ID with only its first and last four
// characters visible — enough to recognize, never enough to reuse.
func maskKey(k string) string {
	if len(k) <= 8 {
		return strings.Repeat("*", len(k))
	}
	return k[:4] + "…" + k[len(k)-4:]
}

// CheckNames returns the full list of check names in execution order (stable).
// The GUI uses this to render the available checks before running them.
func CheckNames() []string {
	return []string{
		"DNS Resolution Check",
		"TCP Connectivity Check",
		"TLS Certificate Check",
		"Clock Skew Check",
		"Bucket Authentication Check",
		"Bucket Policy Check",
		"Bucket ACL Check",
		"Credential Identity Check",
		"Write Round-trip Check",
	}
}

// RunCheck runs a single doctor check by name for the given bucket.
// Returns an error if the check name is unknown. The write round-trip
// honors the same opt-in law as Run (allowWrite) — asking for one check
// never grants a write the user did not ask to test.
func RunCheck(ctx context.Context, c *s3client.Client, bucket, name string, insecureTLS, allowWrite bool) (CheckResult, error) {
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
	case "Clock Skew Check":
		return timedCheck(func() CheckResult {
			return checkClockSkew(ctx, host, port, c.Endpoint, insecureTLS)
		}), nil
	case "Bucket Authentication Check":
		return timedCheck(func() CheckResult { return checkAuth(ctx, c, bucket) }), nil
	case "Bucket Policy Check":
		return timedCheck(func() CheckResult { return checkPolicy(ctx, c, bucket) }), nil
	case "Bucket ACL Check":
		return timedCheck(func() CheckResult { return checkACL(ctx, c, bucket) }), nil
	case "Credential Identity Check":
		return timedCheck(func() CheckResult { return checkIdentity(ctx, c) }), nil
	case "Write Round-trip Check":
		if bucket == "" {
			return timedCheck(func() CheckResult { return skip("Write Round-trip Check", "no bucket selected") }), nil
		}
		if !allowWrite {
			return timedCheck(func() CheckResult {
				return skip("Write Round-trip Check", "write test disabled — it is opt-in because it writes (and deletes) a tiny probe object")
			}), nil
		}
		return timedCheck(func() CheckResult { return checkWriteRoundTrip(ctx, c, bucket) }), nil
	default:
		// Unreachable (name was validated above), but satisfy the compiler.
		return CheckResult{}, fmt.Errorf("unknown check: %s", name)
	}
}

// knownCheck reports whether name is a check RunCheck can run.
func knownCheck(name string) bool {
	return slices.Contains(CheckNames(), name)
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

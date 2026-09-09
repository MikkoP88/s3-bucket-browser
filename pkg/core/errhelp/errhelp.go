// Package errhelp maps S3 errors to plain-language causes and suggestions.
//
// Ported and extended from s3-bucket-tester pkg/remediation/suggestions.go.
// It understands AWS SDK error codes (smithy APIError), HTTP status codes and
// raw error strings, so it also works for transport-level failures.
package errhelp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/smithy-go"
)

// Advice is a user-facing explanation of an error.
type Advice struct {
	Code       string   `json:"code"`
	Cause      string   `json:"cause"`
	Suggestion string   `json:"suggestion"`
	Commands   []string `json:"commands,omitempty"`
}

// adviceFor dispatches on an S3 error code (case-insensitive, no punctuation).
func adviceFor(code string) *Advice {
	switch normalize(code) {
	case "invalidaccesskeyid", "invalidaccesskeyidnosuchuser":
		return &Advice{
			Code:       code,
			Cause:      "The access key ID is invalid or does not exist",
			Suggestion: "Verify the access key ID is correct and the user exists in the S3 provider",
			Commands: []string{
				"Verify the access key in your provider's console",
				"aws iam get-user --user-name <username>",
				"aws iam create-access-key --user-name <username>",
			},
		}
	case "signaturedoesnotmatch":
		return &Advice{
			Code:       code,
			Cause:      "Signature calculation failed — wrong secret key, region, or addressing style",
			Suggestion: "Check the secret key, region and endpoint configuration",
			Commands: []string{
				"Verify the secret key is correct (watch for typos and shell quoting)",
				"Verify the region matches the bucket's region",
				"Some providers require path-style addressing: add --path-style",
				"Verify system time is synchronized (w32tm /query / ntpdate -q)",
			},
		}
	case "accessdenied", "forbidden":
		return &Advice{
			Code:       code,
			Cause:      "Access denied — insufficient permissions",
			Suggestion: "Grant the required IAM permissions to the credentials for this bucket",
			Commands: []string{
				"aws iam list-attached-user-policies --user-name <username>",
				"aws s3api get-bucket-policy --bucket <bucket>",
				"s3b doctor --profile <profile>  # diagnose from this tool",
			},
		}
	case "nosuchbucket":
		return &Advice{
			Code:       code,
			Cause:      "The specified bucket does not exist",
			Suggestion: "Verify the bucket name, region and endpoint",
			Commands: []string{
				"s3b ls  # list buckets visible to this profile",
				"Check bucket name spelling",
				"Some providers require path-style addressing: add --path-style",
			},
		}
	case "nosuchkey":
		return &Advice{
			Code:       code,
			Cause:      "The specified object does not exist",
			Suggestion: "Verify the object key; list the prefix to confirm",
			Commands:   []string{"s3b ls s3://<bucket>/<prefix>/ --recursive"},
		}
	case "allaccessdisabled":
		return &Advice{
			Code:       code,
			Cause:      "All access to the bucket has been disabled",
			Suggestion: "Check bucket policy and ACL settings — access may be blocked",
			Commands:   []string{"aws s3api get-bucket-policy --bucket <bucket>"},
		}
	case "requesttimetoolargeskewed", "requesttimeout", "requesttimetoolarge":
		return &Advice{
			Code:       code,
			Cause:      "Request time is too far from the server's clock",
			Suggestion: "Synchronize system time with an NTP server",
			Commands: []string{
				"Windows: w32tm /resync",
				"Linux: ntpdate -u pool.ntp.org",
				"macOS: sntp -s pool.ntp.org",
			},
		}
	case "requestexpired":
		return &Advice{
			Code:       code,
			Cause:      "The request has expired (STS temporary credentials)",
			Suggestion: "Temporary credentials have expired — obtain new ones",
			Commands:   []string{"aws sts get-session-token", "aws sts assume-role --role-arn <arn> ..."},
		}
	case "missingauthenticationtoken", "unauthorized":
		return &Advice{
			Code:       code,
			Cause:      "Authentication is missing or invalid",
			Suggestion: "Provide valid credentials (access key, secret key, session token if required)",
		}
	case "malformedxml":
		return &Advice{
			Code:       code,
			Cause:      "The server returned malformed XML",
			Suggestion: "The endpoint may not be S3-compatible",
			Commands:   []string{"curl -v <endpoint>", "s3b doctor --profile <profile>"},
		}
	case "internalerror":
		return &Advice{
			Code:       code,
			Cause:      "Internal server error on the provider side",
			Suggestion: "Retry later; check the provider status page",
		}
	case "slowdown", "serviceunavailable":
		return &Advice{
			Code:       code,
			Cause:      "The S3 service is throttling requests or is temporarily unavailable",
			Suggestion: "Reduce request rate and retry with backoff",
			Commands:   []string{"Retry after a short wait", "https://status.aws.amazonaws.com/ (AWS)"},
		}
	case "bucketalreadyexists", "bucketalreadyownedbyyou":
		return &Advice{
			Code:       code,
			Cause:      "A bucket with this name already exists",
			Suggestion: "S3 bucket names are globally unique (AWS) — choose another name",
		}
	case "invalidbucketname":
		return &Advice{
			Code:       code,
			Cause:      "The bucket name is not valid",
			Suggestion: "Use 3-63 chars, lowercase letters, numbers, dots and hyphens only",
		}
	case "bucketnotempty":
		return &Advice{
			Code:       code,
			Cause:      "The bucket is not empty",
			Suggestion: "Empty the bucket first, or use force mode: s3b rb s3://bucket --force",
		}
	case "authorizationheadermalformed":
		return &Advice{
			Code:       code,
			Cause:      "The authorization header was malformed — usually a region mismatch",
			Suggestion: "Verify the region matches the bucket's actual region",
		}
	case "invalidendpoint", "endpointconnectionerror", "connectionrefused":
		return &Advice{
			Code:       code,
			Cause:      "The endpoint could not be reached",
			Suggestion: "Verify the endpoint URL, port and that the service is running",
			Commands:   []string{"s3b doctor --profile <profile>"},
		}
	default:
		return nil
	}
}

// ForError returns Advice for err, or nil when nothing specific is known.
// It unwraps smithy APIError codes first, then falls back to string matching
// (transport errors like "no such host", "connection refused", TLS failures).
func ForError(err error) *Advice {
	if err == nil {
		return nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if a := adviceFor(apiErr.ErrorCode()); a != nil {
			return a
		}
		// Fall through to string matching for unknown codes.
		if a := matchTransport(err.Error()); a != nil {
			a.Code = apiErr.ErrorCode()
			return a
		}
		return &Advice{
			Code:       apiErr.ErrorCode(),
			Cause:      apiErr.ErrorMessage(),
			Suggestion: "Check the error details and your configuration; run s3b doctor for a full diagnosis",
		}
	}

	msg := err.Error()
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		switch httpErr.HTTPStatusCode() {
		case http.StatusForbidden:
			return adviceFor("AccessDenied")
		case http.StatusNotFound:
			return adviceFor("NoSuchBucket")
		case http.StatusServiceUnavailable:
			return adviceFor("SlowDown")
		}
	}

	if a := matchTransport(msg); a != nil {
		return a
	}
	return nil
}

// matchTransport recognizes transport-level failures by message content.
func matchTransport(msg string) *Advice {
	m := normalize(msg)
	switch {
	case strings.Contains(m, "nosuchhost") || strings.Contains(m, "nxdomain") || strings.Contains(m, "lookup"):
		return &Advice{
			Code:       "DNSResolution",
			Cause:      "The hostname does not exist or DNS resolution failed",
			Suggestion: "Verify the hostname and DNS configuration",
			Commands:   []string{"nslookup <hostname>", "dig <hostname>"},
		}
	case strings.Contains(m, "connectionrefused"):
		return &Advice{
			Code:       "ConnectionRefused",
			Cause:      "The target port is closed or no service is listening",
			Suggestion: "Verify the service is running and the port is correct",
			Commands:   []string{"nc -zv <host> <port>", "Test-NetConnection <host> -Port <port> (PowerShell)"},
		}
	case strings.Contains(m, "i/o timeout") || strings.Contains(m, "timeout") || strings.Contains(m, "timedout"):
		return &Advice{
			Code:       "Timeout",
			Cause:      "The connection or request timed out",
			Suggestion: "Check firewall rules, network connectivity and endpoint availability",
			Commands:   []string{"s3b doctor --profile <profile>"},
		}
	case strings.Contains(m, "certificatehasexpired"):
		return &Advice{
			Code:       "TLSExpired",
			Cause:      "The server's TLS certificate has expired",
			Suggestion: "Renew the certificate on the server",
		}
	case strings.Contains(m, "certificate") && (strings.Contains(m, "unknownauthority") || strings.Contains(m, "verifyfailed")):
		return &Advice{
			Code:       "TLSUntrusted",
			Cause:      "The certificate is signed by an unknown or untrusted CA",
			Suggestion: "Add the CA to the system trust store, or use --insecure for labs (not for production)",
		}
	case strings.Contains(m, "tls") && strings.Contains(m, "handshake"):
		return &Advice{
			Code:       "TLSHandshake",
			Cause:      "TLS handshake failed",
			Suggestion: "Check the certificate chain and the server's TLS version support",
		}
	case strings.Contains(m, "x509") && (strings.Contains(m, "hostname") ||
		strings.Contains(m, "isvalidfor") || strings.Contains(m, "notvalidfor")):
		return &Advice{
			Code:       "TLSHostnameMismatch",
			Cause:      "Certificate name does not match the hostname",
			Suggestion: "Use the hostname from the certificate's Subject/SANs, or switch to path-style addressing",
		}
	default:
		return nil
	}
}

// normalize lowercases and strips separators so "SignatureDoesNotMatch",
// "signature does not match" and "signaturedoesnotmatch" all compare equal.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '-', '_', '.', ':', '/', '\'':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Format renders Advice for terminal display.
func Format(a *Advice) string {
	if a == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n  Cause:      %s\n", a.Cause)
	fmt.Fprintf(&sb, "  Suggestion: %s", a.Suggestion)
	for _, c := range a.Commands {
		fmt.Fprintf(&sb, "\n    - %s", c)
	}
	return sb.String()
}

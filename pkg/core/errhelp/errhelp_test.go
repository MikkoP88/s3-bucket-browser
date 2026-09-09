package errhelp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

type fakeAPIError struct {
	code string
	msg  string
}

func (e fakeAPIError) Error() string     { return fmt.Sprintf("api error %s: %s", e.code, e.msg) }
func (e fakeAPIError) ErrorCode() string { return e.code }
func (e fakeAPIError) ErrorMessage() string {
	return e.msg
}
func (e fakeAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestForErrorSDKCodes(t *testing.T) {
	cases := []struct {
		code        string
		wantCauseIn string
	}{
		{"SignatureDoesNotMatch", "secret key"},
		{"InvalidAccessKeyId", "access key ID"},
		{"NoSuchBucket", "does not exist"},
		{"AccessDenied", "permissions"},
		{"BucketNotEmpty", "not empty"},
		{"SlowDown", "throttling"},
		{"RequestTimeTooSkewed", "clock"},
	}
	for _, c := range cases {
		a := ForError(fakeAPIError{code: c.code, msg: "boom"})
		if a == nil {
			t.Fatalf("ForError(%s) = nil, want advice", c.code)
		}
		if a.Code != c.code {
			t.Errorf("code = %q, want %q", a.Code, c.code)
		}
	}
	// Spot-check one suggestion body.
	a := ForError(fakeAPIError{code: "SignatureDoesNotMatch"})
	if a == nil || len(a.Commands) == 0 {
		t.Error("SignatureDoesNotMatch advice should include commands")
	}
}

func TestForErrorTransport(t *testing.T) {
	cases := []struct {
		err      error
		wantCode string
	}{
		{errors.New("dial tcp: lookup minio.local: no such host"), "DNSResolution"},
		{errors.New("dial tcp 127.0.0.1:9000: connect: connection refused"), "ConnectionRefused"},
		{errors.New("net/http: TLS handshake timeout"), "Timeout"},
		{errors.New("x509: certificate has expired"), "TLSExpired"},
		{errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority"), "TLSUntrusted"},
		{errors.New("x509: certificate is valid for a.example.com, not b.example.com"), "TLSHostnameMismatch"},
	}
	for _, c := range cases {
		a := ForError(c.err)
		if a == nil {
			t.Fatalf("ForError(%v) = nil, want %s", c.err, c.wantCode)
		}
		if a.Code != c.wantCode {
			t.Errorf("ForError(%v).Code = %q, want %q", c.err, a.Code, c.wantCode)
		}
	}
}

func TestForErrorUnknownCodeStillExplains(t *testing.T) {
	a := ForError(fakeAPIError{code: "SomethingNew", msg: "weird failure"})
	if a == nil || a.Cause != "weird failure" {
		t.Errorf("unknown code should fall back to API message, got %+v", a)
	}
}

func TestForErrorNil(t *testing.T) {
	if ForError(nil) != nil {
		t.Error("ForError(nil) should be nil")
	}
}

func TestNormalize(t *testing.T) {
	got := normalize("Signature-Does Not_Match")
	if got != "signaturedoesnotmatch" {
		t.Errorf("normalize = %q", got)
	}
}

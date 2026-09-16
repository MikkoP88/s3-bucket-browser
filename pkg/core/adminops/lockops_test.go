package adminops

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

// fakeAPIError is a minimal smithy APIError with an HTTP status — the shape
// every SDK operation error carries.
type fakeAPIError struct {
	code   string
	status int
}

func (e fakeAPIError) Error() string        { return fmt.Sprintf("api error %s: boom", e.code) }
func (e fakeAPIError) ErrorCode() string    { return e.code }
func (e fakeAPIError) ErrorMessage() string { return "boom" }
func (e fakeAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }
func (e fakeAPIError) HTTPStatusCode() int  { return e.status }

// TestIsNoLockConfigured pins the provider-dialect contract: "this bucket
// or object has no lock state" is an empty state, not an error — whatever
// code or 404 flavor the provider answers with.
func TestIsNoLockConfigured(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"aws object-level code", fakeAPIError{code: "NoSuchObjectLockConfiguration", status: 404}, true},
		{"aws bucket-level / ceph code", fakeAPIError{code: "ObjectLockConfigurationNotFoundError", status: 404}, true},
		{"unknown dialect on a 404", fakeAPIError{code: "UnknownError", status: 404}, true},
		{"wrapped ceph code", fmt.Errorf("op: %w", fakeAPIError{code: "ObjectLockConfigurationNotFoundError", status: 404}), true},
		{"missing object is not empty state", fakeAPIError{code: "NoSuchKey", status: 404}, false},
		{"missing version is not empty state", fakeAPIError{code: "NoSuchVersion", status: 404}, false},
		{"missing bucket is not empty state", fakeAPIError{code: "NoSuchBucket", status: 404}, false},
		{"denied", fakeAPIError{code: "AccessDenied", status: 403}, false},
		{"not implemented", fakeAPIError{code: "NotImplemented", status: 501}, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		if got := isNoLockConfigured(tc.err); got != tc.want {
			t.Errorf("%s: isNoLockConfigured(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

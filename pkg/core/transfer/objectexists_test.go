package transfer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// The honest existence probe: (true, nil) is the server's word that the
// key is taken, (false, nil) its word that nothing lives there — a bare
// 404 included, the shape servers that skip the XML body answer with —
// and anything the wire could not answer is (false, err), never silence
// a --no-clobber caller would read as free space.
func TestObjectExistsHonest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b/here":
			w.Header().Set("ETag", `"etag"`)
			w.WriteHeader(http.StatusOK)
		case "/b/missing":
			http.Error(w, "fakeS3: no such key", http.StatusNotFound)
		default: // "/b/wedged" — the probe the wire cannot answer
			http.Error(w, "fakeS3: injected fault", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	c := s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL), Region: "us-east-1",
		Credentials: aws.AnonymousCredentials{}, UsePathStyle: true,
	})

	if exists, err := ObjectExists(context.Background(), c, "b", "here"); err != nil || !exists {
		t.Fatalf("ObjectExists(here) = (%v, %v), want (true, nil)", exists, err)
	}
	if exists, err := ObjectExists(context.Background(), c, "b", "missing"); err != nil || exists {
		t.Fatalf("ObjectExists(missing) = (%v, %v), want (false, nil)", exists, err)
	}
	if exists, err := ObjectExists(context.Background(), c, "b", "wedged"); err == nil || exists {
		t.Fatalf("ObjectExists(wedged) = (%v, %v), want (false, err)", exists, err)
	}
}

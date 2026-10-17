package adminops

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// These rigs pin the incomplete-upload lifecycle against an in-process
// httptest S3 (hermetic — no network, no keyring): the listing walks the
// page boundary the paginator rides, answers oldest-first, and the abort
// speaks the exact bucket+key+uploadId wire — the pair of verbs the
// operational cure is built from.

// uploadsFixture is a fake S3 speaking ListMultipartUploads (paginated via
// key-marker) and AbortMultipartUpload. It records every abort request.
type uploadsFixture struct {
	mu     sync.Mutex
	aborts []string // "key|uploadId" per abort request, in order
}

func (f *uploadsFixture) client(t testing.TB) *s3.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Has("uploads"):
			// three uploads across two pages: page 0 holds two (with a
			// next marker), page 1 the last one
			marker := r.URL.Query().Get("key-marker")
			var b []byte
			b = append(b, xml.Header...)
			b = append(b, `<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`...)
			b = append(b, "<Bucket>bench</Bucket>"...)
			if marker == "" {
				b = append(b, `<Upload><Key>docs/late.txt</Key><UploadId>id-late</UploadId><Initiated>2026-10-17T12:00:00.000Z</Initiated><StorageClass>STANDARD</StorageClass></Upload>`...)
				b = append(b, `<Upload><Key>docs/early.txt</Key><UploadId>id-early</UploadId><Initiated>2026-10-17T08:30:00.000Z</Initiated></Upload>`...)
				b = append(b, `<IsTruncated>true</IsTruncated><NextKeyMarker>docs/early.txt</NextKeyMarker><NextUploadIdMarker>id-early</NextUploadIdMarker>`...)
			} else {
				b = append(b, `<Upload><Key>middle.bin</Key><UploadId>id-mid</UploadId><Initiated>2026-10-17T10:00:00.000Z</Initiated></Upload>`...)
				b = append(b, `<IsTruncated>false</IsTruncated>`...)
			}
			b = append(b, "</ListMultipartUploadsResult>"...)
			w.Header().Set("Content-Type", "application/xml")
			w.Write(b)
		case r.Method == http.MethodDelete && r.URL.Query().Get("uploadId") != "":
			f.aborts = append(f.aborts, r.URL.Path+"|"+r.URL.Query().Get("uploadId"))
			w.WriteHeader(http.StatusNoContent)
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

func TestListUploadsPaginatesAndSortsOldestFirst(t *testing.T) {
	f := &uploadsFixture{}
	c := f.client(t)
	ups, err := ListUploads(context.Background(), c, "bench")
	if err != nil {
		t.Fatalf("ListUploads: %v", err)
	}
	if len(ups) != 3 {
		t.Fatalf("uploads = %d, want 3 (the page boundary must be walked): %+v", len(ups), ups)
	}
	wantOrder := []string{"id-early", "id-mid", "id-late"} // 08:30 < 10:00 < 12:00
	for i, want := range wantOrder {
		if ups[i].UploadID != want {
			t.Fatalf("ups[%d].UploadID = %q, want %q (oldest first)", i, ups[i].UploadID, want)
		}
	}
	if ups[0].Key != "docs/early.txt" {
		t.Fatalf("ups[0].Key = %q", ups[0].Key)
	}
	if got := ups[2].Initiated.UTC().Format(time.RFC3339); got != "2026-10-17T12:00:00Z" {
		t.Fatalf("initiated parse = %q", got)
	}
	if ups[2].StorageClass != "STANDARD" || ups[0].StorageClass != "" {
		t.Fatalf("storage class carry: late=%q early=%q", ups[2].StorageClass, ups[0].StorageClass)
	}
}

func TestAbortUploadSpeaksTheExactWire(t *testing.T) {
	f := &uploadsFixture{}
	c := f.client(t)
	if err := AbortUpload(context.Background(), c, "bench", "docs/late.txt", "id-late"); err != nil {
		t.Fatalf("AbortUpload: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.aborts) != 1 || f.aborts[0] != "/bench/docs/late.txt|id-late" {
		t.Fatalf("aborts = %v, want one /bench/docs/late.txt|id-late", f.aborts)
	}
}

func TestListUploadsMapsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		io.WriteString(w, `<Error><Code>NotImplemented</Code><Message>no MPU listing here</Message></Error>`)
	}))
	defer srv.Close()
	c := s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		UsePathStyle: true,
	})
	_, err := ListUploads(context.Background(), c, "bench")
	if err == nil || err.Error() != "incomplete upload listing is not supported by this provider/endpoint" {
		t.Fatalf("err = %v, want the unsupported-provider sentence", err)
	}
	if err := AbortUpload(context.Background(), c, "bench", "k", "id"); err == nil ||
		err.Error() != "incomplete upload abort is not supported by this provider/endpoint" {
		t.Fatalf("abort err = %v, want the unsupported-provider sentence", err)
	}
}

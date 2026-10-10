package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// PreviewMaxBytes bounds one preview read: generous for photographs and
// screenshots, small enough that a mislabeled movie cannot drag its whole
// body over the bridge for a glance.
const PreviewMaxBytes = 16 << 20

// PreviewTarget is the typed grammar of one preview read — the same
// family EditTarget speaks: kind "s3" (Source "" = the view source, a
// pane binding names its own), "remote" (a saved non-S3 source's engine
// path) or "local" (a workstation path, as the local pane sees it).
type PreviewTarget struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Path   string `json:"path"`
	// MaxBytes caps this read below PreviewMaxBytes when the caller
	// speaks it (a thumbnail asks for a sliver where the viewer asks
	// for the whole glance). Zero and negatives mean the full bound;
	// anything past the bound is clamped to it.
	MaxBytes int64 `json:"maxBytes"`
}

// capBytes settles the read bound one target speaks: a positive
// MaxBytes below the preview bound narrows the read (a thumbnail's
// sliver), everything else — zero, negative, oversized — reads the
// full PreviewMaxBytes the viewer has always paid.
func (t PreviewTarget) capBytes() int64 {
	if t.MaxBytes <= 0 || t.MaxBytes > PreviewMaxBytes {
		return PreviewMaxBytes
	}
	return t.MaxBytes
}

// PreviewPayload is what crossed the bridge: at most PreviewMaxBytes of
// the file, the sniffed content type (the browser's own WHATWG sniff
// over the first 512 bytes), the file's full size when the engine
// speaks it, and Truncated for a file the bound cut short — the honest
// shape the overlay renders or refuses aloud on.
type PreviewPayload struct {
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Truncated   bool   `json:"truncated"`
}

// PreviewData reads one bounded preview of any file the panes seat. It
// never guesses: a kind it does not know, a folder-shaped operand or an
// unreadable store is an error the caller can toast.
func (a *App) PreviewData(t PreviewTarget) (PreviewPayload, error) {
	switch t.Kind {
	case "s3":
		return a.previewS3(t.Source, t.Bucket, t.Key, t.capBytes())
	case "remote":
		return a.previewRemote(t.Source, t.Key, t.capBytes())
	case "local":
		return a.previewLocal(t.Path, t.capBytes())
	default:
		return PreviewPayload{}, fmt.Errorf(
			"cannot preview kind %q — S3 objects, remote source files and local files carry previews", t.Kind)
	}
}

// previewS3 rides a Range GET: a preview must never pull a
// multi-gigabyte object for a glance. The wire's ContentRange carries
// the true total past the cut; a full-body 200 means the object fits —
// and the LimitReader backstop caps any server that ignores Range.
func (a *App) previewS3(source, bucket, key string, max int64) (PreviewPayload, error) {
	if bucket == "" {
		return PreviewPayload{}, fmt.Errorf("pass an object key: s3://bucket/key")
	}
	if key == "" || strings.HasSuffix(key, "/") {
		return PreviewPayload{}, fmt.Errorf("s3://%s/%s is a folder", bucket, key)
	}
	c, err := a.client(source)
	if err != nil {
		return PreviewPayload{}, err
	}
	ctx, cancel := a.previewCtx()
	if cancel != nil {
		defer cancel()
	}
	out, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", max-1)),
	})
	if err != nil {
		return PreviewPayload{}, err
	}
	defer out.Body.Close()
	p, err := readBounded(out.Body, out.ContentLength, out.ContentRange, max)
	if err != nil {
		return PreviewPayload{}, err
	}
	return p, nil
}

// previewRemote reads through the source's own engine — the same lock
// discipline every engine call rides (one FTP data connection at a
// time), and the same folder refusal the editor's pull speaks.
func (a *App) previewRemote(idOrName, keyPath string, max int64) (PreviewPayload, error) {
	if keyPath == "" || strings.HasSuffix(keyPath, "/") {
		return PreviewPayload{}, fmt.Errorf("%s is a folder", keyPath)
	}
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return PreviewPayload{}, err
	}
	ctx, cancel := a.previewCtx()
	if cancel != nil {
		defer cancel()
	}
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	rc, size, err := fs.Open(ctx, keyPath)
	if err != nil {
		return PreviewPayload{}, err
	}
	defer rc.Close()
	p, err := readBounded(rc, &size, nil, max)
	if err != nil {
		return PreviewPayload{}, err
	}
	return p, nil
}

// previewLocal stats before it reads: a directory is refused the same
// way, and the stat carries the true size.
func (a *App) previewLocal(path string, max int64) (PreviewPayload, error) {
	if path == "" || strings.HasSuffix(path, "/") {
		return PreviewPayload{}, fmt.Errorf("%s is a folder", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return PreviewPayload{}, err
	}
	if st.IsDir() {
		return PreviewPayload{}, fmt.Errorf("%s is a folder", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return PreviewPayload{}, err
	}
	defer f.Close()
	size := st.Size()
	p, err := readBounded(f, &size, nil, max)
	if err != nil {
		return PreviewPayload{}, err
	}
	return p, nil
}

// previewCtx is the app's live context — no quick-budget deadline, the
// same budget-free read the editor's pull rides (a slow link must not
// have its glance cancelled by the listing timeout).
func (a *App) previewCtx() (context.Context, context.CancelFunc) {
	if a.ctx != nil {
		return context.WithCancel(a.ctx)
	}
	return context.Background(), nil
}

// readBounded reads at most max bytes (PreviewMaxBytes when the caller
// did not narrow it), sniffs the content type, and settles the honest
// shape: reportedSize when the engine speaks it (S3's ContentLength or
// the engine's Open size), contentRange's total when the wire cut the
// read ("bytes 0-N/TOTAL" — the true size past the cut), and Truncated
// whenever the file outgrew the bound — including a server that ignored
// the Range and paid the backstop.
func readBounded(r io.Reader, reportedSize *int64, contentRange *string, max int64) (PreviewPayload, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return PreviewPayload{}, err
	}
	p := PreviewPayload{Data: data, Size: int64(len(data))}
	if reportedSize != nil && *reportedSize > 0 {
		p.Size = *reportedSize
	}
	if contentRange != nil {
		if cr := strings.TrimSpace(*contentRange); cr != "" {
			if i := strings.LastIndexByte(cr, '/'); i >= 0 {
				if total, terr := strconv.ParseInt(cr[i+1:], 10, 64); terr == nil && total > 0 {
					p.Size = total
				}
			}
		}
	}
	if int64(len(data)) > max { // a full-body answer past the bound
		p.Data = data[:max]
		p.Truncated = true
	} else if p.Size > int64(len(data)) {
		p.Truncated = true
	}
	p.ContentType = sniffContentType(p.Data)
	return p, nil
}

// sniffContentType names the bytes the way the renderer's own <img>
// would: the WHATWG sniff over the first 512 bytes.
func sniffContentType(data []byte) string {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	return http.DetectContentType(head)
}

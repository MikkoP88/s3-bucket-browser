// xfer.go: cross-source transfers (M9) — stream copies between any two
// sources: S3 (the default profile or a named S3 source), the remote
// engines (sftp/scp/ftp/ftps/local-dir sources) and the local pane. One
// synchronous planner expands the items (remote dirs via remotefs.Walk,
// S3 dirs via listing.Walk, local dirs via WalkDir, collecting empty
// directories as it goes); the background job then streams each file
// read-side → write-side under the per-source operation locks, acquired
// in sorted-ID order so multi-source transfers can never deadlock.
//
// move = copy then delete, and a source item is only deleted when every
// one of its files verifiably transferred: a skipped file (conflict
// policy "skip") is NOT a success — the source keeps it, so no data is
// ever destroyed by a policy that declined to overwrite.
package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// XferItem is one source-side item: an S3 object (Source "" = the default
// S3 profile; a name = that S3 source's profile), or a file/directory on
// a non-S3 source (Key is the anchored path, directories with trailing
// slash, as the grid ships them).
type XferItem struct {
	Source string `json:"source"` // "" = default S3; else source name/ID
	Bucket string `json:"bucket"` // S3 items only
	Key    string `json:"key"`    // S3 object key or remote anchored path
	Size   int64  `json:"size"`
	IsDir  bool   `json:"isDir"`
}

// XferDest is the destination of a transfer: an S3 bucket/prefix, a
// directory on a remote source, or a local directory (local pane).
type XferDest struct {
	Kind   string `json:"kind"` // "s3" | "remote" | "local"
	Source string `json:"source,omitempty"`
	Bucket string `json:"bucket,omitempty"` // s3 only
	Dir    string `json:"dir"`              // s3: prefix; remote: anchored path; local: OS path
}

// xferSrcSide addresses one read side (shared per item source).
type xferSrcSide struct {
	kind   string // "s3" | "remote"
	lockID string // engine lock (remote only; "" = no lock)
	client *s3client.Client
	fs     remotefs.FS
}

// xferDestSide addresses the write side (one per job).
type xferDestSide struct {
	kind   string // "s3" | "remote" | "local"
	lockID string // engine lock (remote only)
	client *s3client.Client
	bucket string
	fs     remotefs.FS
	dir    string // s3: dirPrefix form; remote: anchored; local: OS path
}

// xferFile is one planned file copy.
type xferFile struct {
	item    int // index of the originating top-level item (move bookkeeping)
	srcKind string
	srcLock string
	client  *s3client.Client // s3 read side
	fs      remotefs.FS      // remote read side
	bucket  string           // s3 read side
	srcPath string           // s3 key / anchored remote path / OS path
	dstPath string           // s3 key / anchored remote path / OS path
	size    int64
}

// pendingDir is a source directory seen during planning; it graduates to
// emptyDirs at plan end when nothing lives under it.
type pendingDir struct {
	item    int
	rel     string
	dstPath string
}

// xferItemDel is the move bookkeeping of one top-level item: what to
// delete on the source side after every file under it transferred.
type xferItemDel struct {
	item   int
	kind   string // "s3" | "remote" | "local" (local pane)
	client *s3client.Client
	fs     remotefs.FS
	lockID string
	bucket string
	isDir  bool
	root   string   // remote/local: the item path itself (removed last)
	keys   []string // s3: object keys (files + folder markers)
	files  []string // remote/local: file paths
	dirs   []string // remote/local: dir paths, parents-first (deleted reversed)
}

// xferPlan is the synchronous output of the planner.
type xferPlan struct {
	files     []xferFile
	emptyDirs []string // destination paths of empty source directories
	pending   []pendingDir
	dels      []xferItemDel
	total     int64
}

// TransferCross starts a background cross-source transfer job and returns
// its ID. items may mix S3 and remote items; localPaths are local-pane
// files/directories. policy is overwrite | skip | rename; maxBPS 0 =
// unlimited; move copies first and deletes the source items that
// transferred cleanly.
func (a *App) TransferCross(items []XferItem, localPaths []string, dest XferDest, policy string, maxBPS int64, move bool) (string, error) {
	switch policy {
	case "", PolicyOverwrite, PolicySkip, PolicyRename:
	default:
		return "", fmt.Errorf("unknown conflict policy %q", policy)
	}
	dst, err := a.resolveXferDest(dest)
	if err != nil {
		return "", err
	}
	if len(items) == 0 && len(localPaths) == 0 {
		return "", fmt.Errorf("nothing to transfer")
	}
	pctx, cancel := a.quickCtx()
	plan, err := a.planXfer(pctx, items, localPaths, dst)
	cancel()
	if err != nil {
		return "", err
	}
	if len(plan.files) == 0 && len(plan.emptyDirs) == 0 {
		return "", fmt.Errorf("nothing to transfer")
	}
	j := a.jobs.add("transfer", len(plan.files), plan.total)
	id := j.info.ID
	verb := "copying"
	if move {
		verb = "moving"
	}
	a.emitLog(LogInfo, "transfer",
		fmt.Sprintf("job %s: %s %d file(s) (%d bytes) to %s", id, verb, len(plan.files), plan.total, xferDestLabel(dest)))
	go a.runXfer(j, plan, dst, policy, maxBPS, move)
	return id, nil
}

func xferDestLabel(dest XferDest) string {
	switch dest.Kind {
	case "s3":
		return fmt.Sprintf("s3 %s/%s", dest.Bucket, dest.Dir)
	case "remote":
		return fmt.Sprintf("%s:%s", dest.Source, dest.Dir)
	default:
		return dest.Dir
	}
}

// resolveXferDest validates and connects the write side once.
func (a *App) resolveXferDest(dest XferDest) (xferDestSide, error) {
	switch dest.Kind {
	case "s3":
		if dest.Bucket == "" {
			return xferDestSide{}, fmt.Errorf("s3 destination needs a bucket")
		}
		c, err := a.s3ClientFor(dest.Source) // "" = default profile
		if err != nil {
			return xferDestSide{}, err
		}
		return xferDestSide{kind: "s3", client: c, bucket: dest.Bucket, dir: dirPrefix(dest.Dir)}, nil
	case "remote":
		src, fs, err := a.remoteSource(dest.Source)
		if err != nil {
			return xferDestSide{}, err
		}
		return xferDestSide{kind: "remote", lockID: src.ID, fs: fs, dir: remotefs.CleanPath(dest.Dir)}, nil
	case "local":
		st, err := os.Stat(dest.Dir)
		if err != nil || !st.IsDir() {
			return xferDestSide{}, fmt.Errorf("destination folder not found: %s", dest.Dir)
		}
		return xferDestSide{kind: "local", dir: dest.Dir}, nil
	default:
		return xferDestSide{}, fmt.Errorf("unknown destination kind %q", dest.Kind)
	}
}

// xferSrcSideFor resolves (and caches per source) one read side.
func (a *App) xferSrcSideFor(ctx context.Context, cache map[string]*xferSrcSide, idOrName string) (*xferSrcSide, error) {
	if s, ok := cache[idOrName]; ok {
		return s, nil
	}
	var side xferSrcSide
	if idOrName == "" {
		c, err := a.client("")
		if err != nil {
			return nil, err
		}
		side = xferSrcSide{kind: "s3", client: c}
	} else {
		src, err := a.sourceByIDOrName(idOrName)
		if err != nil {
			return nil, err
		}
		if src.Type == profile.TypeS3 {
			// S3 sources mirror into the profile store by name.
			c, err := a.client(src.Name)
			if err != nil {
				return nil, err
			}
			side = xferSrcSide{kind: "s3", client: c}
		} else {
			fs, err := a.engine(src.ID)
			if err != nil {
				return nil, err
			}
			side = xferSrcSide{kind: "remote", lockID: src.ID, fs: fs}
		}
	}
	cache[idOrName] = &side
	return &side, nil
}

// xferDstJoin maps an item base + relative path onto the destination
// namespace.
func xferDstJoin(dst xferDestSide, base, rel string) string {
	switch dst.kind {
	case "s3":
		return joinKeyNoSlash(dst.dir, base, rel)
	case "remote":
		return "/" + path.Join(strings.TrimPrefix(dst.dir, "/"), base, rel)
	default:
		return filepath.Join(dst.dir, base, filepath.FromSlash(rel))
	}
}

// leafName returns the last segment of a slash path.
func leafName(p string) string {
	trimmed := strings.TrimSuffix(p, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

// planXfer expands every item into file copies (plus empty dirs and move
// bookkeeping). Listing walks use the quick context — the same budget
// DownloadRefs planning uses.
func (a *App) planXfer(ctx context.Context, items []XferItem, localPaths []string, dst xferDestSide) (*xferPlan, error) {
	p := &xferPlan{}
	srcCache := map[string]*xferSrcSide{}
	// nonEmpty is keyed "item|rel": rel's parent chain has content. A
	// directory graduates to emptyDirs only if nothing ever marks it.
	nonEmpty := map[string]bool{}
	addDir := func(item int, base, rel string) {
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" {
			return
		}
		markNonEmpty(nonEmpty, item, rel)
		p.pending = append(p.pending, pendingDir{item: item, rel: rel, dstPath: xferDstJoin(dst, base, rel)})
	}

	for idx, it := range items {
		side, err := a.xferSrcSideFor(ctx, srcCache, it.Source)
		if err != nil {
			return nil, err
		}
		if side.kind == "s3" {
			if it.Bucket == "" {
				return nil, fmt.Errorf("item %q: S3 items need a bucket", it.Key)
			}
			if err := a.planS3Item(ctx, p, addDir, nonEmpty, idx, it, side, dst); err != nil {
				return nil, err
			}
			continue
		}
		if err := a.planRemoteItem(ctx, p, addDir, nonEmpty, idx, it, side, dst); err != nil {
			return nil, err
		}
	}
	for _, lp := range localPaths {
		if err := a.planLocalItem(p, addDir, nonEmpty, len(items), lp, dst); err != nil {
			return nil, err
		}
	}
	// Prune: only directories with no content anywhere under them are
	// materialized at the destination.
	for _, d := range p.pending {
		if !nonEmpty[fmt.Sprintf("%d|%s", d.item, d.rel)] {
			p.emptyDirs = append(p.emptyDirs, d.dstPath)
		}
	}
	p.pending = nil
	return p, nil
}

// planS3Item expands one S3 object or prefix. Folder markers (0-byte
// keys ending in "/") count as directories: they are recreated as real
// directories at remote/local destinations and deleted on move, but no
// marker objects are ever written.
func (a *App) planS3Item(ctx context.Context, p *xferPlan, addDir func(int, string, string), nonEmpty map[string]bool, idx int, it XferItem, side *xferSrcSide, dst xferDestSide) error {
	if !it.IsDir {
		key := strings.TrimSuffix(it.Key, "/")
		if key == "" {
			return fmt.Errorf("invalid S3 item key %q", it.Key)
		}
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "s3", client: side.client,
			bucket: it.Bucket, srcPath: key,
			dstPath: xferDstJoin(dst, leafName(key), ""), size: it.Size,
		})
		p.total += it.Size
		p.dels = append(p.dels, xferItemDel{item: idx, kind: "s3", client: side.client, bucket: it.Bucket, keys: []string{it.Key}})
		return nil
	}
	prefix := dirPrefix(it.Key)
	if prefix == "" {
		return fmt.Errorf("refusing to transfer a whole S3 bucket — pick folders")
	}
	base := leafName(it.Key)
	// The item's own folder always exists at the destination: an empty
	// dir item (or one holding only markers) plans no files and no child
	// dirs ever register the root. MkdirAll is idempotent when content
	// follows.
	p.emptyDirs = append(p.emptyDirs, xferDstJoin(dst, base, ""))
	del := xferItemDel{item: idx, kind: "s3", client: side.client, bucket: it.Bucket, isDir: true, root: it.Key}
	err := listing.Walk(ctx, side.client.S3, it.Bucket, prefix, func(o s3types.Object) error {
		key := aws.ToString(o.Key)
		size := aws.ToInt64(o.Size)
		del.keys = append(del.keys, key)
		rel := strings.TrimPrefix(key, prefix)
		if strings.HasSuffix(rel, "/") { // folder marker → directory
			addDir(idx, base, rel)
			return nil
		}
		markNonEmpty(nonEmpty, idx, rel)
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "s3", client: side.client,
			bucket: it.Bucket, srcPath: key,
			dstPath: xferDstJoin(dst, base, rel), size: size,
		})
		p.total += size
		return nil
	})
	if err != nil {
		return err
	}
	p.dels = append(p.dels, del)
	return nil
}

// planRemoteItem expands one file or directory of a remote source.
func (a *App) planRemoteItem(ctx context.Context, p *xferPlan, addDir func(int, string, string), nonEmpty map[string]bool, idx int, it XferItem, side *xferSrcSide, dst xferDestSide) error {
	rootP := remotefs.CleanPath(it.Key)
	if rootP == "/" {
		return fmt.Errorf("refusing to transfer the source root — pick folders")
	}
	trimmed := strings.TrimSuffix(rootP, "/")
	base := leafName(trimmed)
	if !it.IsDir {
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "remote", srcLock: side.lockID, fs: side.fs,
			srcPath: trimmed, dstPath: xferDstJoin(dst, base, ""), size: it.Size,
		})
		p.total += it.Size
		p.dels = append(p.dels, xferItemDel{
			item: idx, kind: "remote", fs: side.fs, lockID: side.lockID,
			files: []string{trimmed},
		})
		return nil
	}
	// The item's own folder always exists at the destination (empty dirs
	// plan no files); MkdirAll is idempotent when content follows.
	p.emptyDirs = append(p.emptyDirs, xferDstJoin(dst, base, ""))
	del := xferItemDel{item: idx, kind: "remote", fs: side.fs, lockID: side.lockID, isDir: true, root: trimmed}
	prefix := trimmed + "/"
	err := remotefs.Walk(ctx, side.fs, rootP, func(e listing.Entry) error {
		rel := strings.TrimSuffix(strings.TrimPrefix(e.Key, prefix), "/")
		if rel == "" {
			return nil
		}
		if e.IsDir {
			addDir(idx, base, rel)
			del.dirs = append(del.dirs, trimmed+"/"+rel)
			return nil
		}
		markNonEmpty(nonEmpty, idx, rel)
		del.files = append(del.files, trimmed+"/"+rel)
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "remote", srcLock: side.lockID, fs: side.fs,
			srcPath: trimmed + "/" + rel, dstPath: xferDstJoin(dst, base, rel), size: e.Size,
		})
		p.total += e.Size
		return nil
	})
	if err != nil {
		return err
	}
	p.dels = append(p.dels, del)
	return nil
}

// planLocalItem expands one local-pane file or directory.
func (a *App) planLocalItem(p *xferPlan, addDir func(int, string, string), nonEmpty map[string]bool, idx int, lp string, dst xferDestSide) error {
	st, err := os.Stat(lp)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		base := filepath.Base(lp)
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "local", srcPath: lp,
			dstPath: xferDstJoin(dst, base, ""), size: st.Size(),
		})
		p.total += st.Size()
		p.dels = append(p.dels, xferItemDel{item: idx, kind: "local", files: []string{lp}})
		return nil
	}
	root := filepath.Clean(lp)
	base := filepath.Base(root)
	// The item's own folder always exists at the destination (empty dirs
	// plan no files); MkdirAll is idempotent when content follows.
	p.emptyDirs = append(p.emptyDirs, xferDstJoin(dst, base, ""))
	del := xferItemDel{item: idx, kind: "local", isDir: true, root: root}
	err = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp == root {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(fp, root), string(filepath.Separator)))
		if d.IsDir() {
			addDir(idx, base, rel)
			del.dirs = append(del.dirs, fp)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		markNonEmpty(nonEmpty, idx, rel)
		del.files = append(del.files, fp)
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "local", srcPath: fp,
			dstPath: xferDstJoin(dst, base, rel), size: info.Size(),
		})
		p.total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	p.dels = append(p.dels, del)
	return nil
}

// markNonEmpty records that rel's parent chain (per item) has content.
func markNonEmpty(nonEmpty map[string]bool, item int, rel string) {
	for {
		i := strings.LastIndex(rel, "/")
		if i <= 0 {
			return
		}
		rel = rel[:i]
		nonEmpty[fmt.Sprintf("%d|%s", item, rel)] = true
	}
}

// runXfer executes the planned copies and (for moves) the source
// deletions. One goroutine, mirroring runUpload's shape.
func (a *App) runXfer(j *jobHandle, plan *xferPlan, dst xferDestSide, policy string, maxBPS int64, move bool) {
	ctx := j.ctx

	// Empty directories first (remote/local dests only; S3 has no real
	// folders and no marker objects are written).
	if dst.kind != "s3" {
		unlock := a.lockSrcs(dst.lockID)
		for _, d := range plan.emptyDirs {
			var err error
			if dst.kind == "remote" {
				err = dst.fs.MkdirAll(ctx, d)
			} else {
				err = os.MkdirAll(d, 0o755)
			}
			if err != nil {
				j.mu.Lock()
				j.info.Error = fmt.Sprintf("mkdir %s: %v", d, err)
				j.mu.Unlock()
			}
		}
		unlock()
	}

	failed := map[int]bool{}
	skipped := map[int]bool{}
	madeDirs := map[string]bool{}
	touched := map[string]bool{}

	for i := range plan.files {
		f := &plan.files[i]
		if ctx.Err() != nil {
			a.finishJob(j, JobCanceled, "canceled")
			return
		}
		j.mu.Lock()
		j.info.CurrentFile = f.srcPath
		j.mu.Unlock()
		j.emit(a.jobs, true)

		unlock := a.lockSrcs(f.srcLock, dst.lockID)
		status, err := a.xferOne(ctx, f, dst, policy, maxBPS, j.progress, madeDirs)
		unlock()

		switch {
		case err != nil:
			if ctx.Err() != nil {
				a.finishJob(j, JobCanceled, "canceled")
				return
			}
			j.mu.Lock()
			j.info.Error = fmt.Sprintf("%s: %v", leafName(f.srcPath), err)
			j.mu.Unlock()
			failed[f.item] = true
			j.fileDone(f.size, true)
		case status == "skipped":
			skipped[f.item] = true
			j.fileDone(0, false)
		default:
			j.fileDone(f.size, false)
			if f.srcKind == "s3" {
				touched[f.bucket] = true
			}
			if dst.kind == "s3" {
				touched[dst.bucket] = true
			}
		}
		j.emit(a.jobs, true)
	}

	// Move: delete source items whose every file transferred cleanly. A
	// skipped or failed file taints the whole item — nothing is removed.
	if move {
		for k := range plan.dels {
			d := &plan.dels[k]
			if failed[d.item] || skipped[d.item] || ctx.Err() != nil {
				continue
			}
			unlock := a.lockSrcs(d.lockID)
			err := a.xferDeleteSource(ctx, d)
			unlock()
			if err != nil {
				j.mu.Lock()
				j.info.Error = fmt.Sprintf("delete source %s: %v", d.root, err)
				j.mu.Unlock()
				a.emitLog(LogError, "transfer", fmt.Sprintf("job %s: source cleanup failed: %v", j.info.ID, err))
			}
		}
	}

	j.mu.Lock()
	failedN := j.info.FailedFiles
	lastErr := j.info.Error
	totalN := j.info.TotalFiles
	j.mu.Unlock()
	if failedN > 0 {
		a.finishJob(j, JobError, fmt.Sprintf("%d of %d file(s) failed — last error: %s", failedN, totalN, lastErr))
	} else {
		a.finishJob(j, JobDone, "")
	}
	for b := range touched {
		a.emit(EventS3Changed, map[string]string{"bucket": b})
	}
}

// xferOne copies one planned file and reports "copied" or "skipped".
func (a *App) xferOne(ctx context.Context, f *xferFile, dst xferDestSide, policy string, maxBPS int64, fn transfer.ProgressFn, madeDirs map[string]bool) (string, error) {
	switch policy {
	case PolicySkip:
		if a.xferDestExists(ctx, dst, f.dstPath) {
			return "skipped", nil
		}
	case PolicyRename:
		if a.xferDestExists(ctx, dst, f.dstPath) {
			f.dstPath = a.xferUniqueDst(ctx, dst, f.dstPath)
		}
	}

	// Fast path: same-profile S3 → S3 is a server-side copy.
	if f.srcKind == "s3" && dst.kind == "s3" && f.client == dst.client {
		if err := transfer.Copy(ctx, f.client.S3, f.bucket, f.srcPath, dst.bucket, f.dstPath); err != nil {
			return "", err
		}
		return "copied", nil
	}
	// Local pane → S3 reuses the battle-tested UploadFile.
	if f.srcKind == "local" && dst.kind == "s3" {
		if err := transfer.UploadFile(ctx, dst.client.S3, f.srcPath, dst.bucket, f.dstPath, transfer.UploadOptions{MaxBPS: maxBPS, Progress: fn}); err != nil {
			return "", err
		}
		return "copied", nil
	}

	// Everything else streams reader → writer.
	var r io.ReadCloser
	var size int64
	switch f.srcKind {
	case "s3":
		body, sz, err := s3Open(ctx, f.client, f.bucket, f.srcPath)
		if err != nil {
			return "", err
		}
		r, size = body, sz
	case "remote":
		rc, sz, err := f.fs.Open(ctx, f.srcPath)
		if err != nil {
			return "", err
		}
		r, size = rc, sz
	default:
		fh, err := os.Open(f.srcPath)
		if err != nil {
			return "", err
		}
		st, serr := fh.Stat()
		if serr != nil {
			fh.Close()
			return "", serr
		}
		r, size = fh, st.Size()
	}
	if size <= 0 {
		size = f.size
	}
	defer r.Close()

	// Same remote engine on both sides: engines allow exactly one data
	// connection (FTP) — stream through a temp file so the read and write
	// halves never overlap on the wire.
	if f.srcKind == "remote" && dst.kind == "remote" && f.srcLock != "" && f.srcLock == dst.lockID {
		return a.xferViaTemp(ctx, r, size, f, dst, fn, maxBPS, madeDirs)
	}

	var err error
	switch dst.kind {
	case "s3":
		err = transfer.UploadReader(ctx, dst.client.S3, r, size, dst.bucket, f.dstPath,
			transfer.UploadOptions{MaxBPS: maxBPS, Progress: fn})
	case "remote":
		err = a.xferRemoteCreate(ctx, dst, f.dstPath, r, size, fn, maxBPS, madeDirs)
	default:
		err = xferLocalCreate(f.dstPath, r, size, fn, maxBPS)
	}
	if err != nil {
		return "", err
	}
	return "copied", nil
}

// s3Open returns a stream over one object and its content length.
func s3Open(ctx context.Context, c *s3client.Client, bucket, key string) (io.ReadCloser, int64, error) {
	resp, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, aws.ToInt64(resp.ContentLength), nil
}

// xferViaTemp: read half drains the source into a temp file (progress +
// throttle on this half), write half streams the temp file into the
// destination (throttle only — progress was already reported).
func (a *App) xferViaTemp(ctx context.Context, r io.ReadCloser, size int64, f *xferFile, dst xferDestSide, fn transfer.ProgressFn, maxBPS int64, madeDirs map[string]bool) (string, error) {
	tmp, err := os.CreateTemp("", "s3b-xfer-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = io.Copy(tmp, transfer.NewProgressReader(r, fn, size, maxBPS))
	cerr := tmp.Close()
	r.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	tf, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer tf.Close()
	if err := a.xferRemoteCreate(ctx, dst, f.dstPath, tf, size, nil, 0, madeDirs); err != nil {
		return "", err
	}
	return "copied", nil
}

// xferRemoteCreate writes one file on a remote destination, creating the
// parent directory once per job (madeDirs cache).
func (a *App) xferRemoteCreate(ctx context.Context, dst xferDestSide, p string, r io.Reader, size int64, fn transfer.ProgressFn, maxBPS int64, madeDirs map[string]bool) error {
	if dir := strings.TrimSuffix(path.Dir(p), "/"); dir != "" && dir != "." && !madeDirs[dir] {
		if err := dst.fs.MkdirAll(ctx, dir); err != nil {
			return err
		}
		madeDirs[dir] = true
	}
	return dst.fs.Create(ctx, p, transfer.NewProgressReader(r, fn, size, maxBPS))
}

// xferLocalCreate writes one file on the local destination.
func xferLocalCreate(p string, r io.Reader, size int64, fn transfer.ProgressFn, maxBPS int64) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, transfer.NewProgressReader(r, fn, size, maxBPS))
	return err
}

// xferDestExists reports whether a destination path is taken.
func (a *App) xferDestExists(ctx context.Context, dst xferDestSide, p string) bool {
	switch dst.kind {
	case "s3":
		return remoteExists(ctx, dst.client, dst.bucket, p)
	case "remote":
		_, err := dst.fs.Stat(ctx, p)
		return err == nil
	default:
		_, err := os.Stat(p)
		return err == nil
	}
}

// xferUniqueDst returns p, or a "name (n).ext" variant, that is free.
func (a *App) xferUniqueDst(ctx context.Context, dst xferDestSide, p string) string {
	if !a.xferDestExists(ctx, dst, p) {
		return p
	}
	ext := path.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for n := 1; n < 1000; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if !a.xferDestExists(ctx, dst, cand) {
			return cand
		}
	}
	return p + ".new"
}

// xferDeleteSource removes one fully-transferred item from its source.
func (a *App) xferDeleteSource(ctx context.Context, d *xferItemDel) error {
	switch d.kind {
	case "s3":
		_, err := transfer.DeleteKeys(ctx, d.client.S3, d.bucket, d.keys)
		return err
	case "remote":
		for _, p := range d.files {
			if err := d.fs.Remove(ctx, p); err != nil {
				return err
			}
		}
		for i := len(d.dirs) - 1; i >= 0; i-- {
			if err := d.fs.Remove(ctx, d.dirs[i]); err != nil {
				return err
			}
		}
		if d.isDir && d.root != "" && d.root != "/" {
			return d.fs.Remove(ctx, d.root)
		}
		return nil
	default: // local pane
		for _, p := range d.files {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
		for i := len(d.dirs) - 1; i >= 0; i-- {
			if err := os.Remove(d.dirs[i]); err != nil {
				return err
			}
		}
		if d.isDir && d.root != "" {
			return os.Remove(d.root)
		}
		return nil
	}
}

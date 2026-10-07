// xfer.go: cross-source transfers (M9) — stream copies between any two
// sources: S3 (the view source or a named S3 source), the remote
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
// ever destroyed by a policy that declined to overwrite. And before any
// of that runs, the cycle guard refuses a destination that lives inside
// the item's own source — a move beneath itself would delete the fresh
// copies with the originals, so the whole family never starts.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// XferItem is one source-side item: an S3 object (Source "" = the view
// source; a name = that S3 source's profile), or a file/directory on
// a non-S3 source (Key is the anchored path, directories with trailing
// slash, as the grid ships them).
type XferItem struct {
	Source string `json:"source"` // "" = view source; else source name/ID
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
	mtime   time.Time // source modification time (conflict pre-check; zero = unknown)
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
// transferred cleanly. decisions optionally overrides the policy per
// planned destination path (the conflict dialog's decisionKeys). hidden
// registers the job as internal staging — it runs through the engine but
// never appears in the transfers/tasks UI (the drag-out scratch download
// uses it).
func (a *App) TransferCross(items []XferItem, localPaths []string, dest XferDest, policy string, maxBPS int64, move bool, decisions map[string]string, hidden bool) (string, error) {
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
	if err := a.xferCycleRefusal(items, localPaths, dest, dst); err != nil {
		return "", err
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
	j.src = xferDestSource(dest)
	j.setMeta(xferTitle(items, localPaths), xferFromLabel(items, localPaths), xferDestLabel(dest), len(items)+len(localPaths), move)
	its := make([]TransferItem, 0, len(items)+len(localPaths))
	for _, it := range items {
		its = append(its, TransferItem{Name: it.Key})
	}
	for _, lp := range localPaths {
		its = append(its, TransferItem{Name: lp})
	}
	for i := range plan.files {
		its[plan.files[i].item].Files++
		its[plan.files[i].item].Total += plan.files[i].size
	}
	j.setItems(its)
	if hidden {
		j.mu.Lock()
		j.info.Hidden = true
		j.mu.Unlock()
	}
	id := j.info.ID
	verb := "copying"
	if move {
		verb = "moving"
	}
	a.emitLogSrc(LogInfo, "transfer", j.src,
		fmt.Sprintf("job %s: %s %d file(s) (%d bytes) to %s", id, verb, len(plan.files), plan.total, xferDestLabel(dest)))
	logDecisions(a, "transfer", decisions)
	go a.runXfer(j, plan, dst, policy, maxBPS, move, decisions)
	return id, nil
}

// xferTitle names a cross-source job after the first dropped item (S3
// object/prefix, remote path or local-pane path).
func xferTitle(items []XferItem, localPaths []string) string {
	if len(items) > 0 {
		return leafName(items[0].Key)
	}
	return filepath.Base(localPaths[0])
}

// xferFromLabel renders the read side of the route line: the view source
// as s3://bucket, a named source as "name:dir", the local pane as a
// folder.
func xferFromLabel(items []XferItem, localPaths []string) string {
	if len(items) > 0 {
		it := items[0]
		if it.Source == "" {
			return s3Label(it.Bucket, "")
		}
		dir := path.Dir(strings.TrimSuffix(it.Key, "/"))
		if dir == "." || dir == "/" {
			dir = ""
		}
		if dir != "" {
			return it.Source + ":" + dir
		}
		return it.Source
	}
	return filepath.Dir(localPaths[0])
}

// xferDestSource is the log source tag of a transfer destination: the S3
// bucket or the remote source name ("" for local folders).
func xferDestSource(dest XferDest) string {
	switch dest.Kind {
	case "s3":
		return dest.Bucket
	case "remote":
		return dest.Source
	default:
		return ""
	}
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
		c, err := a.s3ClientFor(dest.Source) // "" = view source
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
func (a *App) xferSrcSideFor(cache map[string]*xferSrcSide, idOrName string) (*xferSrcSide, error) {
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

// xferCycleRefusal is the transfer engine's last line of defense against
// a destination inside the item's own source. Copying a folder beneath
// itself and then (on a move) deleting the sources takes the fresh copies
// with them — nothing would survive — and a file dropped onto its own
// folder lands on its own key, which a move then deletes. The frontend
// refuses both shapes, but the engine re-verifies for every caller (pane
// drops, paste, the battery, anything yet unwritten): the whole family is
// rejected before a single byte transfers. Cross-store pairings can never
// nest and pass untouched.
func (a *App) xferCycleRefusal(items []XferItem, localPaths []string, dest XferDest, dst xferDestSide) error {
	switch dst.kind {
	case "s3":
		dstStore, haveDst := a.s3StoreOf(dest.Source)
		if !haveDst {
			return nil // resolveXferDest already refused unmapped destinations
		}
		for _, it := range items {
			if it.Bucket == "" || it.Bucket != dst.bucket {
				continue
			}
			itStore, ok := a.s3StoreOf(it.Source) // "" = view source, as the planner resolves it
			if !ok || !sameS3Store(itStore, dstStore) {
				continue // different store: the copy can never land inside its source
			}
			landing := joinKeyNoSlash(dst.dir, leafName(it.Key))
			if !it.IsDir && !strings.HasSuffix(it.Key, "/") {
				if landing == it.Key {
					return fmt.Errorf("source and destination are the same: %s", it.Key)
				}
				continue
			}
			if root := dirPrefix(it.Key); landing == strings.TrimSuffix(root, "/") || strings.HasPrefix(landing+"/", root) {
				return fmt.Errorf("%s: a folder cannot be moved or copied into itself", strings.TrimSuffix(it.Key, "/"))
			}
		}
	case "remote":
		for _, it := range items {
			if it.Source == "" {
				continue // the S3 view source, never a remote engine item
			}
			src, err := a.sourceByIDOrName(it.Source)
			if err != nil || src.Type == profile.TypeS3 || src.ID != dst.lockID {
				continue
			}
			landing := "/" + path.Join(strings.TrimPrefix(dst.dir, "/"), leafName(it.Key))
			root := remotefs.CleanPath(it.Key)
			if !it.IsDir && !strings.HasSuffix(it.Key, "/") {
				if landing == root {
					return fmt.Errorf("source and destination are the same: %s", root)
				}
				continue
			}
			if landing == root || strings.HasPrefix(landing+"/", root+"/") {
				return fmt.Errorf("%s: a folder cannot be moved or copied into itself", root)
			}
		}
	default: // local destination: only local-pane sources can nest inside it
		for _, lp := range localPaths {
			landing := filepath.Join(dst.dir, filepath.Base(lp))
			st, serr := os.Stat(lp)
			isDir := serr == nil && st.IsDir()
			if !isDir {
				if strings.EqualFold(landing, lp) {
					return fmt.Errorf("source and destination are the same: %s", lp)
				}
				continue
			}
			if p, c := filepath.Clean(lp), filepath.Clean(landing); strings.EqualFold(p, c) ||
				strings.HasPrefix(strings.ToLower(c), strings.ToLower(p)+string(filepath.Separator)) {
				return fmt.Errorf("%s: a folder cannot be moved or copied into itself", lp)
			}
		}
	}
	return nil
}

// s3StoreOf resolves the source record an S3 client dial would use for
// name ("" = the view source, as client() resolves it). sameS3Store over
// two resolutions is the same-store test — however the caller spelled it
// (name, ID, or the view-source default).
func (a *App) s3StoreOf(name string) (profile.Source, bool) {
	if name == "" {
		name = a.currentViewSource()
	}
	if cSrc, ok := a.containerS3Source(name); ok {
		return cSrc, true
	}
	sSrc, ok := a.sessionS3Source(name)
	return sSrc, ok
}

// sameS3Store reports whether two resolved S3 source records address one
// logical store: the same record, or two names over the same endpoint
// (the bucket is compared by the caller — an alias of the account still
// hosts the same data, so a cycle through it is refused like any other).
func sameS3Store(a, b profile.Source) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	return a.S3 != nil && b.S3 != nil && a.S3.Endpoint != "" && a.S3.Endpoint == b.S3.Endpoint
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
		side, err := a.xferSrcSideFor(srcCache, it.Source)
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
		size := it.Size
		if size <= 0 {
			// Clipboard-cut paste items carry no size (only keys survive
			// the clipboard) — read it back so the progress totals and the
			// transfer log tell the truth. Directory items re-read sizes
			// through their walk below; this is the single-file twin.
			head, err := side.client.S3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(it.Bucket), Key: aws.String(key)})
			if err != nil {
				return err
			}
			size = aws.ToInt64(head.ContentLength)
		}
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "s3", client: side.client,
			bucket: it.Bucket, srcPath: key,
			dstPath: xferDstJoin(dst, leafName(key), ""), size: size,
		})
		p.total += size
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
		if rel == "" {
			// key == prefix: the item's own root (an explicit folder
			// marker object). It is the folder itself, never a file —
			// planning it as one aims a file write at the directory
			// path, which fails on paste with "is a directory".
			return nil
		}
		if strings.HasSuffix(rel, "/") { // folder marker → directory
			addDir(idx, base, rel)
			return nil
		}
		markNonEmpty(nonEmpty, idx, rel)
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "s3", client: side.client,
			bucket: it.Bucket, srcPath: key,
			dstPath: xferDstJoin(dst, base, rel), size: size,
			mtime: aws.ToTime(o.LastModified),
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
		size := it.Size
		if size <= 0 {
			// Same re-read as the S3 side: clipboard-cut items carry keys
			// only, so the single-file path stats the engine for the real
			// size instead of planning a zero-byte total.
			st, err := side.fs.Stat(ctx, trimmed)
			if err != nil {
				return err
			}
			size = st.Size
		}
		p.files = append(p.files, xferFile{
			item: idx, srcKind: "remote", srcLock: side.lockID, fs: side.fs,
			srcPath: trimmed, dstPath: xferDstJoin(dst, base, ""), size: size,
		})
		p.total += size
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
			mtime: aws.ToTime(e.LastModified),
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
			dstPath: xferDstJoin(dst, base, ""), size: st.Size(), mtime: st.ModTime(),
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
			dstPath: xferDstJoin(dst, base, rel), size: info.Size(), mtime: info.ModTime(),
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
func (a *App) runXfer(j *jobHandle, plan *xferPlan, dst xferDestSide, policy string, maxBPS int64, move bool, decisions map[string]string) {
	defer a.guardJob("transfer", j) // the worker panic net (guard.go)
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
		// A same-profile S3→S3 hop is a server-side CopyObject: no byte
		// stream ever crosses the wire, so the per-file line gets no size
		// to grind against — announcing it without one lights the row's
		// server-side chip instead of a frozen "0%".
		sz := f.size
		if f.srcKind == "s3" && dst.kind == "s3" && f.client == dst.client {
			sz = 0
		}
		j.startFile(f.item, i+1, f.srcPath, sz)
		j.emit(true)

		unlock := a.lockSrcs(f.srcLock, dst.lockID)
		pol := filePolicy(decisions, f.dstPath, policy)
		status, err := a.xferOne(ctx, f, dst, pol, maxBPS, j.progress, madeDirs)
		unlock()

		switch {
		case err != nil:
			if ctx.Err() != nil {
				a.finishJob(j, JobCanceled, "canceled")
				return
			}
			j.mu.Lock()
			j.info.Error = fmt.Sprintf("%s: %v", leafName(f.srcPath), err)
			j.info.ErrorKind = timeoutKind(err.Error())
			j.mu.Unlock()
			failed[f.item] = true
			j.fileDone(f.item, f.size, ItemFailed)
		case status == "skipped":
			skipped[f.item] = true
			j.fileDone(f.item, 0, ItemSkipped)
		default:
			j.fileDone(f.item, f.size, ItemDone)
			if f.srcKind == "s3" {
				touched[f.bucket] = true
			}
			if dst.kind == "s3" {
				touched[dst.bucket] = true
			}
		}
		j.emit(true)
	}

	// Move: delete source items whose every file transferred cleanly. A
	// skipped or failed file taints the whole item — nothing is removed.
	if move {
		j.setPhase(PhaseCleanup)
		j.emit(true)
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
				j.info.ErrorKind = timeoutKind(err.Error())
				j.mu.Unlock()
				a.emitLogSrc(LogError, "transfer", j.src, fmt.Sprintf("job %s: source cleanup failed: %v", j.info.ID, err))
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
		partSize, conc := a.partTunables() // Settings → Transfers engine tuning
		if err := transfer.UploadFile(ctx, dst.client.S3, f.srcPath, dst.bucket, f.dstPath,
			transfer.UploadOptions{MaxBPS: maxBPS, Progress: fn, PartSize: partSize, Concurrency: conc}); err != nil {
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
	partSize, conc := a.partTunables() // Settings → Transfers engine tuning
	switch dst.kind {
	case "s3":
		err = transfer.UploadReader(ctx, dst.client.S3, r, size, dst.bucket, f.dstPath,
			transfer.UploadOptions{MaxBPS: maxBPS, Progress: fn, PartSize: partSize, Concurrency: conc})
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
	spool := workspaceBase("tmp") // secure.go: config dir (0700) under secure storage
	if err := os.MkdirAll(spool, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(spool, spoolPattern)
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
// parent directory once per job (madeDirs cache). The payload is staged to
// a temp sibling (s3b-part- prefix) and swapped into place on success, so a
// canceled or failed write can never leave a partial at the final name —
// remote engines have no version history to recover a truncated file from.
// The dest-side lock serializes jobs into one engine, so the fixed temp
// name cannot collide between concurrent writers.
func (a *App) xferRemoteCreate(ctx context.Context, dst xferDestSide, p string, r io.Reader, size int64, fn transfer.ProgressFn, maxBPS int64, madeDirs map[string]bool) error {
	if dir := strings.TrimSuffix(path.Dir(p), "/"); dir != "" && dir != "." && !madeDirs[dir] {
		if err := dst.fs.MkdirAll(ctx, dir); err != nil {
			return err
		}
		madeDirs[dir] = true
	}
	dir, base := path.Split(p)
	// No leading dot in the stage name: FTP servers hide dotfiles from
	// LIST, and an invisible stage is unremovable — the discard Remove
	// below stats through the same listing, aborts with "no such file",
	// and strands the partial forever (it also blocks RMDIR of the parent,
	// since the server counts what listings don't show). SFTP and WebDAV
	// list dotfiles anyway, and S3 destinations don't take this path, so
	// the rename only changes what vsftpd-class servers can see — making
	// the stage visible everywhere is what makes it cleanable everywhere.
	tmp := path.Join(dir, "s3b-part-"+base)
	// Discarding the stage must survive the task's own cancellation: an
	// engine that honors a canceled context (WebDAV's HTTP requests fail
	// outright, before a byte is sent) would refuse the cleanup Remove and
	// strand the staging file on the server. WithoutCancel keeps the
	// request's values while dropping the done channel; the timeout bounds
	// a hung connection. A single shot is not enough either — the aborted
	// write can poison the very connection the discard rides (a desynced
	// FTP control channel reading the dead command's stale reply), so a
	// short retry window also covers engines without self-healing probes:
	// a canceled write must never strand its stage on the server.
	discard := func(name string) {
		for attempt := 0; ; attempt++ {
			dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			err := dst.fs.Remove(dctx, name)
			dcancel()
			if err == nil || errors.Is(err, fs.ErrNotExist) || attempt >= 2 {
				return
			}
			select {
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
			}
		}
	}
	if err := dst.fs.Create(ctx, tmp, transfer.NewProgressReader(r, fn, size, maxBPS)); err != nil {
		discard(tmp) // a half-written stage file is garbage, never data
		return err
	}
	// Engines' Rename overwrites (posix-rename / MOVE Overwrite:T / RNTO);
	// servers that refuse an overwrite target get the explicit two-step.
	if err := dst.fs.Rename(ctx, tmp, p); err != nil {
		if rmErr := dst.fs.Remove(ctx, p); rmErr == nil {
			if err = dst.fs.Rename(ctx, tmp, p); err == nil {
				return nil
			}
		}
		discard(tmp)
		return fmt.Errorf("commit %s: %w", p, err)
	}
	return nil
}

// xferLocalCreate writes one file on the local destination, staged through
// transfer.StageAndCommit: a canceled or failed copy can never truncate a
// pre-existing file at p and never leaves a partial at the final name.
func xferLocalCreate(p string, r io.Reader, size int64, fn transfer.ProgressFn, maxBPS int64) error {
	return transfer.StageAndCommit(p, func(f *os.File) error {
		_, err := io.Copy(f, transfer.NewProgressReader(r, fn, size, maxBPS))
		return err
	})
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

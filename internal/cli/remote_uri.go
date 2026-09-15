// remote_uri.go: "source://" URIs on the CLI — every saved data source gets a
// scheme. Any saved non-S3 data source (local/sftp/scp/ftp/ftps) can be
// addressed as NAME://path, e.g. `s3b ls vault://media` or
// `s3b cp vault://media/a.txt s3://b/`. S3 sources keep their s3:// URIs
// for browsing (they resolve by name via --profile; the store mirrors
// them), but cp/mv accept them as NAME:// operands too: a per-bucket
// source scopes the path to its bucket (NAME://dir/file), an account-wide
// source reads the bucket from the first segment (NAME://bucket/dir/file).
package cli

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

// remoteRef is a dialed source URI: the engine plus the anchored path.
type remoteRef struct {
	src  profile.Source
	path string
	fs   remotefs.FS
}

func (r *remoteRef) Close() {
	if r != nil && r.fs != nil {
		r.fs.Close()
	}
}

// uri renders a display URI ("lab", "/a/b" → "lab://a/b" — anchored
// paths carry a leading slash the URI form does not).
func uri(name, anchored string) string {
	return name + "://" + strings.TrimPrefix(anchored, "/")
}

// rprintf writes through the swappable CLI writer (tests capture it).
func rprintf(format string, a ...any) {
	fmt.Fprintf(out, format, a...)
}

// sourceURI reports whether arg is a "name://path" source URI and splits
// it (ok=false for plain paths and s3:// URIs).
func sourceURI(arg string) (name, rest string, ok bool) {
	i := strings.Index(arg, "://")
	if i <= 0 {
		return "", "", false
	}
	name, rest = arg[:i], arg[i+3:]
	if rest == "" {
		rest = "/"
	}
	return name, rest, true
}

// dialSourceURI resolves a source URI against the store and dials the
// engine. Returns (nil, nil) when arg is not a source URI (plain path or
// s3://), so callers can fall through to their existing handling.
func dialSourceURI(ctx context.Context, arg string) (*remoteRef, error) {
	name, rest, ok := sourceURI(arg)
	if !ok || name == "s3" {
		return nil, nil
	}
	s, err := store()
	if err != nil {
		return nil, err
	}
	src, err := s.GetSource(name)
	if err != nil {
		return nil, usageErr("%q is not a saved data source (see `s3b source list`)", name)
	}
	if src.Type == profile.TypeS3 {
		return nil, usageErr("source %q is S3 — browse it as s3://bucket/prefix with --profile %q (cp/mv also take %s:// operands)", name, name, name)
	}
	fs, err := remotefs.Dial(ctx, src)
	if err != nil {
		return nil, opErr(err)
	}
	return &remoteRef{src: src, path: remotefs.CleanPath(rest), fs: fs}, nil
}

// ---- S3 source URIs (cp/mv operands) ----

// s3SourceRef is a dialed S3 source URI: the source's own client plus the
// anchored bucket/key. A per-bucket source scopes the whole path to its
// bucket; an account-wide source reads the bucket from the first segment.
type s3SourceRef struct {
	src    profile.Source
	c      *s3client.Client
	bucket string
	key    string // "" = bucket root; never slash-terminated
	folder bool   // operand named a folder (trailing slash)
}

// s3URI mirrors the ref into the s3:// shape every copy path already
// understands, so enumeration and destination semantics stay identical
// for source URIs and plain s3:// operands.
func (r *s3SourceRef) s3URI() s3URI {
	u := s3URI{Bucket: r.bucket, Key: r.key}
	if u.Key != "" {
		u.HasPrefix = true
		u.IsPrefix = r.folder
	}
	return u
}

// uriStr renders the ref as an s3:// URI string — copyS3ToS3 parses its
// operands, and the synthesized URI drives the exact same folder/exact
// destination rules as a typed s3:// one.
func (r *s3SourceRef) uriStr() string {
	s := "s3://" + r.bucket
	if r.key != "" {
		s += "/" + r.key
		if r.folder {
			s += "/"
		}
	}
	return s
}

// s3SourcePath splits the URI path per the source's scope: a per-bucket
// source uses the whole path as the key, an account-wide source takes the
// bucket from the first path segment.
func s3SourcePath(src profile.Source, rest string) (bucket, key string, err error) {
	rest = strings.Trim(rest, "/")
	if src.Bucket != "" {
		return src.Bucket, rest, nil
	}
	bucket, key, _ = strings.Cut(rest, "/")
	if bucket == "" {
		return "", "", usageErr("source %q spans the whole account — address it as %s://bucket[/key]", src.Name, src.Name)
	}
	return bucket, key, nil
}

// dialS3SourceURI resolves NAME://path for an S3-type source into a
// dialed client plus bucket/key. Returns (nil, nil) for everything else
// (plain paths, s3:// URIs, non-S3 sources — dialSourceURI takes those),
// letting cp/mv probe both dialers on each operand.
func dialS3SourceURI(ctx context.Context, arg string) (*s3SourceRef, error) {
	name, rest, ok := sourceURI(arg)
	if !ok || name == "s3" {
		return nil, nil
	}
	s, err := store()
	if err != nil {
		return nil, err
	}
	src, err := s.GetSource(name)
	if err != nil {
		return nil, usageErr("%q is not a saved data source (see `s3b source list`)", name)
	}
	if src.Type != profile.TypeS3 || src.S3 == nil {
		return nil, nil
	}
	bucket, key, err := s3SourcePath(src, rest)
	if err != nil {
		return nil, err
	}
	c, err := s3client.New(ctx, *src.S3, s3client.Options{Timeout: flagTimeout})
	if err != nil {
		return nil, opErr(err)
	}
	return &s3SourceRef{
		src:    src,
		c:      c,
		bucket: bucket,
		key:    key,
		folder: strings.HasSuffix(rest, "/"),
	}, nil
}

// remoteLs implements `ls NAME://dir` (one view; --recursive walks).
func remoteLs(ctx context.Context, r *remoteRef, recursive bool) error {
	if recursive {
		var rows []listing.Entry
		if err := remotefs.Walk(ctx, r.fs, r.path, func(e listing.Entry) error {
			e.Name = strings.TrimPrefix(e.Key, "/")
			rows = append(rows, e)
			return nil
		}); err != nil {
			return opErr(err)
		}
		if flagJSON {
			return printJSON(rows)
		}
		for _, e := range rows {
			printEntry(e)
		}
		return nil
	}
	entries, err := r.fs.List(ctx, r.path)
	if err != nil {
		return opErr(err)
	}
	if flagJSON {
		return printJSON(entries)
	}
	for _, e := range entries {
		printEntry(e)
	}
	return nil
}

// remoteTree implements `tree NAME://dir`.
func remoteTree(ctx context.Context, r *remoteRef) error {
	var keys []string
	if !flagJSON {
		rprintf("%s: %s%s\n", r.src.Name, r.src.Type, r.path)
	}
	var rec func(dir, indent string) error
	rec = func(dir, indent string) error {
		entries, err := r.fs.List(ctx, dir)
		if err != nil {
			return err
		}
		for i, e := range entries {
			connector, child := "├── ", indent+"│   "
			if i == len(entries)-1 {
				connector, child = "└── ", indent+"    "
			}
			if flagJSON {
				keys = append(keys, e.Key)
			} else if e.IsDir {
				rprintf("%s%s%s/\n", indent, connector, e.Name)
			} else {
				rprintf("%s%s%s\n", indent, connector, e.Name)
			}
			if e.IsDir {
				if err := rec(e.Key, child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := rec(r.path, ""); err != nil {
		return opErr(err)
	}
	if flagJSON {
		return printJSON(keys)
	}
	return nil
}

// remoteDu implements `du NAME://dir`.
func remoteDu(ctx context.Context, r *remoteRef) error {
	var usage listing.Usage
	err := remotefs.Walk(ctx, r.fs, r.path, func(e listing.Entry) error {
		if e.IsDir {
			return nil
		}
		usage.ObjectCount++
		usage.TotalBytes += e.Size
		return nil
	})
	if err != nil {
		return opErr(err)
	}
	if flagJSON {
		return printJSON(usage)
	}
	rprintf("%10s  %6d object(s)  %s\n",
		humanSize(usage.TotalBytes), usage.ObjectCount, uri(r.src.Name, r.path))
	return nil
}

// remoteStat implements `stat NAME://path`.
func remoteStat(ctx context.Context, r *remoteRef) error {
	e, err := r.fs.Stat(ctx, r.path)
	if err != nil {
		return opErr(err)
	}
	if flagJSON {
		return printJSON(e)
	}
	rprintf("path:     %s\n", strings.TrimSuffix(e.Key, "/"))
	rprintf("source:   %s (%s)\n", r.src.Name, r.src.Type)
	rprintf("type:     %s\n", map[bool]string{true: "Folder", false: "File"}[e.IsDir])
	if !e.IsDir {
		rprintf("size:     %s (%d bytes)\n", humanSize(e.Size), e.Size)
	}
	if e.LastModified != nil {
		rprintf("modified: %s\n", e.LastModified.Local().Format("2006-01-02 15:04:05"))
	}
	return nil
}

// remoteMkdir implements `mkdir NAME://dir`.
func remoteMkdir(ctx context.Context, r *remoteRef) error {
	if err := r.fs.MkdirAll(ctx, r.path); err != nil {
		return opErr(err)
	}
	if flagJSON {
		return printJSON(map[string]string{"created": uri(r.src.Name, r.path)})
	}
	col.ok.Fprintf(out, "created folder %s\n", uri(r.src.Name, r.path))
	return nil
}

// remoteRm implements `rm NAME://path`. Folders take their whole tree
// (engine contract) behind the same L1 gates as S3 prefixes.
func remoteRm(ctx context.Context, r *remoteRef, recursive, force, dryRun bool) error {
	e, err := r.fs.Stat(ctx, r.path)
	if err != nil {
		return opErr(err)
	}
	if !e.IsDir {
		if dryRun {
			rprintf("would delete %s\n", uri(r.src.Name, r.path))
			return nil
		}
		if err := r.fs.Remove(ctx, r.path); err != nil {
			return opErr(err)
		}
		if flagJSON {
			return printJSON(map[string]any{"deleted": 1})
		}
		col.ok.Fprintf(out, "deleted %s\n", uri(r.src.Name, r.path))
		return nil
	}
	if !recursive {
		return usageErr("%s is a folder — add --recursive to delete its tree", uri(r.src.Name, r.path))
	}
	n := 0
	err = remotefs.Walk(ctx, r.fs, r.path, func(e listing.Entry) error {
		if !e.IsDir {
			n++
			if dryRun {
				rprintf("would delete %s\n", uri(r.src.Name, strings.TrimSuffix(e.Key, "/")))
			}
		}
		return nil
	})
	if err != nil {
		return opErr(err)
	}
	if dryRun {
		rprintf("total: %d file(s)\n", n)
		if n > rmForceThreshold && !force {
			rprintf("note: deleting requires --force (> %d files)\n", rmForceThreshold)
		}
		return nil
	}
	if n > rmForceThreshold && !force {
		return opErr(fmt.Errorf("%d file(s) under %s — pass --force to delete them all",
			n, uri(r.src.Name, r.path)))
	}
	if err := r.fs.Remove(ctx, r.path); err != nil {
		return opErr(err)
	}
	if flagJSON {
		return printJSON(map[string]any{"deleted": n})
	}
	col.ok.Fprintf(out, "deleted %s (%d file(s))\n", uri(r.src.Name, r.path), n)
	return nil
}

// ---- copy integration (cp/mv with source URIs) ----

// copyFile is one file to move: its slash path relative to the copy root
// plus how to open/delete it (delete runs only for mv, after the copy).
type copyFile struct {
	rel    string
	size   int64
	open   func(ctx context.Context) (io.ReadCloser, int64, error)
	remove func(ctx context.Context) error
}

// s3OpenCLI streams one object and its content length.
func s3OpenCLI(ctx context.Context, c *s3client.Client, bucket, key string) (io.ReadCloser, int64, error) {
	resp, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, aws.ToInt64(resp.ContentLength), nil
}

// s3Copier builds open/remove closures for one object.
func s3Copier(c *s3client.Client, bucket, key string) copyFile {
	return copyFile{
		rel: path.Base(key),
		open: func(ctx context.Context) (io.ReadCloser, int64, error) {
			return s3OpenCLI(ctx, c, bucket, key)
		},
		remove: func(ctx context.Context) error {
			_, err := transfer.DeleteKeys(ctx, c.S3, bucket, []string{key})
			return err
		},
	}
}

// remoteCopyFiles enumerates a remote operand: one file, or a directory
// tree behind --recursive (mirrors the local-path rules).
func remoteCopyFiles(ctx context.Context, r *remoteRef, recursive bool) ([]copyFile, bool, error) {
	st, err := r.fs.Stat(ctx, r.path)
	if err != nil {
		return nil, false, opErr(err)
	}
	if !st.IsDir {
		return []copyFile{{
			rel:  path.Base(strings.TrimSuffix(r.path, "/")),
			size: st.Size,
			open: func(ctx context.Context) (io.ReadCloser, int64, error) {
				return r.fs.Open(ctx, r.path)
			},
			remove: func(ctx context.Context) error { return r.fs.Remove(ctx, r.path) },
		}}, false, nil
	}
	if !recursive {
		return nil, true, usageErr("%s is a folder: add --recursive to copy its contents", uri(r.src.Name, r.path))
	}
	root := strings.TrimSuffix(r.path, "/")
	var files []copyFile
	err = remotefs.Walk(ctx, r.fs, r.path, func(e listing.Entry) error {
		if e.IsDir {
			return nil
		}
		key := strings.TrimSuffix(e.Key, "/")
		files = append(files, copyFile{
			rel:  strings.TrimPrefix(key, root+"/"),
			size: e.Size,
			open: func(ctx context.Context) (io.ReadCloser, int64, error) {
				return r.fs.Open(ctx, key)
			},
			remove: func(ctx context.Context) error { return r.fs.Remove(ctx, key) },
		})
		return nil
	})
	if err != nil {
		return nil, true, opErr(err)
	}
	return files, true, nil
}

// localCopyFiles enumerates a local operand: one file, or a directory
// walk behind --recursive.
func localCopyFiles(root string, recursive bool) ([]copyFile, bool, error) {
	st, err := os.Stat(root)
	if err != nil {
		return nil, false, err
	}
	if !st.IsDir() {
		return []copyFile{{
			rel:  path.Base(filepath.ToSlash(root)),
			size: st.Size(),
			open: func(ctx context.Context) (io.ReadCloser, int64, error) {
				f, err := os.Open(root)
				if err != nil {
					return nil, 0, err
				}
				return f, st.Size(), nil
			},
			remove: func(ctx context.Context) error { return os.Remove(root) },
		}}, false, nil
	}
	if !recursive {
		return nil, true, usageErr("%s is a directory: add --recursive to copy its contents", root)
	}
	var files []copyFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, copyFile{
			rel:  filepath.ToSlash(rel),
			size: info.Size(),
			open: func(ctx context.Context) (io.ReadCloser, int64, error) {
				f, err := os.Open(p)
				if err != nil {
					return nil, 0, err
				}
				return f, info.Size(), nil
			},
			remove: func(ctx context.Context) error { return os.Remove(p) },
		})
		return nil
	})
	if err != nil {
		return nil, true, err
	}
	return files, true, nil
}

// s3CopyFiles enumerates an S3 operand: one object, or a prefix walk
// behind --recursive / a trailing slash.
func s3CopyFiles(ctx context.Context, c *s3client.Client, u s3URI, recursive bool) ([]copyFile, bool, error) {
	if !u.IsPrefix && !recursive {
		return []copyFile{s3Copier(c, u.Bucket, u.Key)}, false, nil
	}
	prefix := dirPrefix(u)
	var files []copyFile
	err := listing.Walk(ctx, c.S3, u.Bucket, prefix, func(o s3types.Object) error {
		key := aws.ToString(o.Key)
		if strings.HasSuffix(key, "/") {
			return nil // folder markers
		}
		files = append(files, s3Copier(c, u.Bucket, key))
		files[len(files)-1].rel = strings.TrimPrefix(key, prefix)
		return nil
	})
	if err != nil {
		return nil, true, err
	}
	return files, true, nil
}

// writeRemoteFile streams one file into a remote destination, creating
// parent directories as needed.
func writeRemoteFile(ctx context.Context, r *remoteRef, p string, src io.Reader) error {
	if dir := strings.TrimSuffix(path.Dir(p), "/"); dir != "" && dir != "." && dir != "/" {
		if err := r.fs.MkdirAll(ctx, dir); err != nil {
			return err
		}
	}
	return r.fs.Create(ctx, p, src)
}

// copyViaTemp drains src into a temp file, then streams it to the remote
// destination (read and write halves never share the wire — required for
// single-connection engines such as FTP copying onto themselves).
func copyViaTemp(ctx context.Context, src io.Reader, r *remoteRef, dst string) error {
	tmp, err := os.CreateTemp("", "s3b-cli-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = io.Copy(tmp, src)
	cerr := tmp.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeRemoteFile(ctx, r, dst, f)
}

// remoteDstTarget decides the remote destination semantics: folder mode
// lands every file under the target (rel appended by the caller); a
// single file without folder markers is an exact file destination.
func remoteDstTarget(ctx context.Context, r *remoteRef, uri string, files []copyFile, isDir bool) (string, bool /*folder mode*/, error) {
	if isDir || len(files) > 1 {
		return r.path, true, nil
	}
	if strings.HasSuffix(uri, "/") {
		return r.path, true, nil
	}
	if st, err := r.fs.Stat(ctx, r.path); err == nil && st.IsDir {
		return r.path, true, nil
	}
	return r.path, false, nil // exact file destination
}

// copyRemoteDispatch handles every cp/mv with at least one source-URI
// operand; the other side may be a source URI (remote or S3), s3:// or a
// local path.
func copyRemoteDispatch(ctx context.Context, c *s3client.Client, src, dst string, srcRef, dstRef *remoteRef, srcS3, dstS3 *s3SourceRef, opts copyOptions) (int, error) {
	var files []copyFile
	var srcIsDir bool
	var err error
	switch {
	case srcRef != nil:
		files, srcIsDir, err = remoteCopyFiles(ctx, srcRef, opts.Recursive)
	case srcS3 != nil:
		files, srcIsDir, err = s3CopyFiles(ctx, srcS3.c, srcS3.s3URI(), opts.Recursive)
	case strings.HasPrefix(src, "s3://"):
		var u s3URI
		u, err = parseS3URI(src)
		if err != nil {
			return 0, err
		}
		files, srcIsDir, err = s3CopyFiles(ctx, c, u, opts.Recursive)
	default:
		files, srcIsDir, err = localCopyFiles(src, opts.Recursive)
	}
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil // empty tree — nothing to copy is success
	}

	if dstRef != nil {
		target, folderMode, err := remoteDstTarget(ctx, dstRef, dst, files, srcIsDir)
		if err != nil {
			return 0, err
		}
		sameEngine := srcRef != nil && srcRef.src.ID == dstRef.src.ID
		n := 0
		for _, f := range files {
			dstPath := target
			if folderMode {
				dstPath = remotefs.CleanPath(target + "/" + f.rel)
			}
			if opts.DryRun {
				rprintf("copy -> %s (%s)\n", uri(dstRef.src.Name, dstPath), humanSize(f.size))
				n++
				continue
			}
			rc, _, err := f.open(ctx)
			if err != nil {
				return n, err
			}
			if sameEngine {
				err = copyViaTemp(ctx, rc, dstRef, dstPath)
			} else {
				err = writeRemoteFile(ctx, dstRef, dstPath, rc)
			}
			rc.Close()
			if err != nil {
				return n, err
			}
			n++
			if flagVerbose {
				col.dim.Fprintf(out, "put %s\n", uri(dstRef.src.Name, dstPath))
			}
			if opts.Move {
				if err := f.remove(ctx); err != nil {
					return n, err
				}
			}
		}
		return pruneMovedDir(ctx, srcRef, srcIsDir, opts, n)
	}

	if dstS3 != nil {
		n, err := copyFilesToS3(ctx, dstS3.c, files, srcIsDir, dstS3.s3URI(), opts)
		if err != nil {
			return n, err
		}
		return pruneMovedDir(ctx, srcRef, srcIsDir, opts, n)
	}
	if strings.HasPrefix(dst, "s3://") {
		du, err := parseS3URI(dst)
		if err != nil {
			return 0, err
		}
		n, err := copyFilesToS3(ctx, c, files, srcIsDir, du, opts)
		if err != nil {
			return n, err
		}
		return pruneMovedDir(ctx, srcRef, srcIsDir, opts, n)
	}
	n, err := copyFilesToLocal(ctx, files, srcIsDir, dst, opts)
	if err != nil {
		return n, err
	}
	return pruneMovedDir(ctx, srcRef, srcIsDir, opts, n)
}

// pruneMovedDir completes mv semantics for remote directory sources: the
// per-file removes delete files only, so the emptied directory skeleton
// stays behind. Every engine's Remove(dir) takes a whole tree — by this
// point only empty dirs remain, so removing the root prunes the skeleton
// without touching moved data.
func pruneMovedDir(ctx context.Context, srcRef *remoteRef, srcIsDir bool, opts copyOptions, n int) (int, error) {
	if opts.Move && !opts.DryRun && srcRef != nil && srcIsDir {
		if err := srcRef.fs.Remove(ctx, srcRef.path); err != nil {
			return n, opErr(err)
		}
	}
	return n, nil
}

// copyFilesToS3 uploads enumerated files; folder batches land under the
// destination prefix, a single file honors an exact object destination.
func copyFilesToS3(ctx context.Context, c *s3client.Client, files []copyFile, isDir bool, du s3URI, opts copyOptions) (int, error) {
	for i, f := range files {
		key := joinKeyNoSlash(du.Key, f.rel)
		if !isDir && len(files) == 1 && du.HasPrefix && !du.IsPrefix {
			key = du.Key // exact object destination
		}
		if opts.DryRun {
			rprintf("copy -> s3://%s/%s (%s)\n", du.Bucket, key, humanSize(f.size))
			continue
		}
		rc, size, err := f.open(ctx)
		if err != nil {
			return i, err
		}
		if size <= 0 {
			size = f.size
		}
		err = transfer.UploadReader(ctx, c.S3, rc, size, du.Bucket, key, opts.uploadOptions())
		rc.Close()
		if err != nil {
			return i, fmt.Errorf("%s: %w", f.rel, err)
		}
		if flagVerbose {
			col.dim.Fprintf(out, "put s3://%s/%s\n", du.Bucket, key)
		}
		if opts.Move {
			if err := f.remove(ctx); err != nil {
				return i + 1, err
			}
		}
	}
	return len(files), nil
}

// copyFilesToLocal writes enumerated files under a local folder (or one
// file to a leaf path).
func copyFilesToLocal(ctx context.Context, files []copyFile, isDir bool, dst string, opts copyOptions) (int, error) {
	for i, f := range files {
		local := dst
		if isDir || len(files) > 1 {
			local = filepath.Join(dst, filepath.FromSlash(f.rel))
		} else if isDirPath(dst) || strings.HasSuffix(dst, "/") || strings.HasSuffix(dst, string(os.PathSeparator)) {
			local = filepath.Join(dst, path.Base(f.rel))
		}
		if opts.DryRun {
			rprintf("copy -> %s (%s)\n", local, humanSize(f.size))
			continue
		}
		rc, _, err := f.open(ctx)
		if err != nil {
			return i, err
		}
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			rc.Close()
			return i, err
		}
		out, err := os.Create(local)
		if err != nil {
			rc.Close()
			return i, err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return i, copyErr
		}
		if closeErr != nil {
			return i, closeErr
		}
		if flagVerbose {
			col.dim.Fprintf(out, "got %s\n", local)
		}
		if opts.Move {
			if err := f.remove(ctx); err != nil {
				return i + 1, err
			}
		}
	}
	return len(files), nil
}

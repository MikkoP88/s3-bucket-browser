package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

// The CLI face of the editor's guarded round trip: pull one file to a
// staging copy, run $VISUAL/$EDITOR on it, and push the saved result
// back under the lost-update cure — S3 legs carry the pulled object's
// ETag (If-Match), engine legs compare size and mtime, so a teammate's
// overwrite mid-edit is refused with the staged copy kept for recovery,
// never clobbered (--force is the informed consent, the GUI Push-anyway
// twin). Local paths edit in place: the file itself is the store, there
// is nothing to pull or push. A operand naming a file that does not
// exist yet opens an empty buffer and creates it on exit — the same
// shape the GUI's New-file-then-edit flow rides.

// editEditorFlag carries --editor through to launchEditor (the flag is
// resolved at edit time, after the pull, in the command's own order).
var editEditorFlag string

// editConflict names the refusal: the remote moved under the edit.
var editConflict = errors.New("the file changed on the server since the download")

// launchEditor runs the editor on the staged copy, attached to this
// terminal so interactive editors behave. A var so the rigs can pin the
// round trip without spawning real editors on the test machine.
var launchEditor = func(p string) error {
	argv := append(splitCommand(resolveEditor(editEditorFlag)), p)
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// resolveEditor picks the editor command: the --editor flag, then
// $VISUAL, then $EDITOR, then the platform default (notepad on Windows,
// vi elsewhere — the fallback every unix ships).
func resolveEditor(flagged string) string {
	return orDefault(orDefault(orDefault(flagged,
		os.Getenv("VISUAL")), os.Getenv("EDITOR")), defaultEditor())
}

func defaultEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// splitCommand splits an editor command string on whitespace outside
// quotes, so EDITOR='code -w' and installed paths with spaces both
// survive as one argv ("C:\Program Files\ed.exe" --wait → 3 fields).
func splitCommand(s string) []string {
	var parts []string
	var cur strings.Builder
	quote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return parts
}

func editCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "edit PATH",
		Short: "Edit a file with $VISUAL/$EDITOR and push it back guarded",
		Long: "Pulls one file to a staging copy, opens it in your editor ($VISUAL,\n" +
			"$EDITOR, or the platform default), and pushes the saved result back\n" +
			"guarded: S3 legs carry the pulled object's ETag (If-Match), source legs\n" +
			"compare size and mtime, so a teammate's overwrite mid-edit is refused\n" +
			"and your edit is kept in the staging copy — pass --force to overwrite.\n" +
			"Local paths edit in place. A file that does not exist yet is created\n" +
			"on exit. Operands: ./file, s3://bucket/key, or NAME://path into any\n" +
			"saved data source.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTransferURIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd.Context(), args[0], force)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "push past a conflict refusal (overwrite the changed remote)")
	f.StringVar(&editEditorFlag, "editor", "", "editor command (default: $VISUAL, $EDITOR, then the platform default)")
	return cmd
}

// runEdit routes the operand the way cp does: s3:// to the view
// profile, NAME:// to the source it names (S3-type sources dial their
// own client), anything else is a local path.
func runEdit(ctx context.Context, arg string, force bool) error {
	if strings.HasPrefix(arg, "s3://") {
		u, err := parseS3URI(arg)
		if err != nil {
			return err
		}
		if u.IsPrefix || u.Key == "" {
			return usageErr("%s is a bucket or folder — edit names one file", arg)
		}
		c, err := resolveClient(ctx)
		if err != nil {
			return err
		}
		return editS3(ctx, c, u.Bucket, u.Key, "s3://"+u.Bucket+"/"+u.Key, force)
	}
	if r, err := dialS3SourceURI(ctx, arg); err != nil {
		return err
	} else if r != nil {
		if r.folder || r.key == "" {
			return usageErr("%s is a bucket or folder — edit names one file", arg)
		}
		return editS3(ctx, r.c, r.bucket, r.key, arg, force)
	}
	if r, err := dialSourceURI(ctx, arg); err != nil {
		return err
	} else if r != nil {
		defer r.Close()
		return editRemote(ctx, r, force)
	}
	return editLocal(arg)
}

// editS3 is the S3 round trip: HEAD for the guard baseline (a 404 is a
// file being born — empty buffer, plain create, nothing to guard), pull
// to the staging copy, editor, then the guarded conditional PUT.
func editS3(ctx context.Context, c *s3client.Client, bucket, key, operand string, force bool) error {
	head, herr := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	existed, etag := true, ""
	if herr != nil {
		if !httpStatusIs(herr, http.StatusNotFound) {
			return opErr(herr)
		}
		existed = false
	} else {
		etag = strings.Trim(aws.ToString(head.ETag), `"`)
	}

	staged, keep, err := stageEditCopy(path.Base(key))
	if err != nil {
		return opErr(err)
	}
	defer func() {
		if !keep {
			os.RemoveAll(filepath.Dir(staged))
		}
	}()
	if existed {
		if err := transfer.DownloadFile(ctx, c.S3, bucket, key, staged, transfer.DownloadOptions{}); err != nil {
			return opErr(fmt.Errorf("pulling %s: %w", operand, err))
		}
	}
	stSize, stMod, err := stampStaged(staged)
	if err != nil {
		return opErr(err)
	}

	changed, err := runEditorAndWait(staged, stSize, stMod)
	if err != nil {
		return err // editor failures keep nothing worth keeping — the pull is repeatable
	}
	if !changed {
		rprintf("no changes in %s — nothing pushed\n", operand)
		return nil
	}

	// the guard: If-Match on the pulled ETag, the server's own atomic
	// verdict. A missing ETag degrades the guard off (the SDK law the
	// GUI rides too); --force writes unconditionally.
	ifMatch := ""
	if existed && !force {
		ifMatch = etag
	}
	if _, err := transfer.UploadFileIfMatch(ctx, c.S3, staged, bucket, key, ifMatch); err != nil {
		if ifMatch != "" && preconditionRefused(err) {
			keep = true
			return editConflictErr(operand, fmt.Sprintf("(pulled etag %q)", etag), staged)
		}
		keep = true
		return opErr(fmt.Errorf("pushing %s (your edit is kept at %s): %w", operand, staged, err))
	}
	if existed {
		rprintf("pushed %s\n", operand)
	} else {
		rprintf("created %s\n", operand)
	}
	return nil
}

// editRemote is the engine round trip — the size+mtime twin of the ETag
// law: the baseline comes from the pull's own Stat, the push re-Stats
// and refuses when the source moved (mtime only when both sides report
// a clock — engines differ in precision, and a 0 is no verdict).
func editRemote(ctx context.Context, r *remoteRef, force bool) error {
	operand := uri(r.src.Name, r.path)
	st, serr := r.fs.Stat(ctx, r.path)
	existed, size, mtime := true, int64(0), int64(0)
	if serr != nil {
		if !errors.Is(serr, fs.ErrNotExist) {
			return opErr(serr)
		}
		existed = false // every engine maps a clean miss onto fs.ErrNotExist
	} else {
		if st.IsDir {
			return usageErr("%s is a folder — edit names one file", operand)
		}
		size = st.Size
		if st.LastModified != nil {
			mtime = st.LastModified.UnixMilli()
		}
	}

	staged, keep, err := stageEditCopy(path.Base(strings.TrimPrefix(r.path, "/")))
	if err != nil {
		return opErr(err)
	}
	defer func() {
		if !keep {
			os.RemoveAll(filepath.Dir(staged))
		}
	}()
	if existed {
		rc, _, err := r.fs.Open(ctx, r.path)
		if err != nil {
			return opErr(fmt.Errorf("pulling %s: %w", operand, err))
		}
		err = copyToStaged(staged, rc)
		rc.Close()
		if err != nil {
			return opErr(err)
		}
	}
	stSize, stMod, err := stampStaged(staged)
	if err != nil {
		return opErr(err)
	}

	changed, err := runEditorAndWait(staged, stSize, stMod)
	if err != nil {
		return err
	}
	if !changed {
		rprintf("no changes in %s — nothing pushed\n", operand)
		return nil
	}

	if existed && !force {
		cur, err := r.fs.Stat(ctx, r.path)
		if err != nil {
			keep = true
			return opErr(fmt.Errorf("pushing %s (your edit is kept at %s): %w", operand, staged, err))
		}
		if cur.Size != size || (mtime != 0 && cur.LastModified != nil && cur.LastModified.UnixMilli() != mtime) {
			keep = true
			detail := fmt.Sprintf("(size %s → %s)", humanSize(size), humanSize(cur.Size))
			return editConflictErr(operand, detail, staged)
		}
	}
	f, err := os.Open(staged)
	if err != nil {
		return opErr(err)
	}
	defer f.Close()
	if err := writeRemoteFile(ctx, r, r.path, f); err != nil {
		keep = true
		return opErr(fmt.Errorf("pushing %s (your edit is kept at %s): %w", operand, staged, err))
	}
	if existed {
		rprintf("pushed %s\n", operand)
	} else {
		rprintf("created %s\n", operand)
	}
	return nil
}

// editLocal runs the editor on the path itself — the file is the store.
func editLocal(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return opErr(err)
		}
		// a file being born: an empty buffer the editor's save creates
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return opErr(err)
		}
		f.Close()
		fi, err = os.Stat(p)
		if err != nil {
			return opErr(err)
		}
	}
	if fi.IsDir() {
		return usageErr("%s is a folder — edit names one file", p)
	}
	if err := launchEditor(p); err != nil {
		return opErr(fmt.Errorf("editor: %w", err))
	}
	rprintf("edited %s in place\n", p)
	return nil
}

// stageEditCopy makes the staging file (its own temp dir, the operand's
// basename, touched empty so a file being born has a buffer to edit)
// and reports whether the caller should keep it after the run — kept
// only when it holds an edit worth recovering.
func stageEditCopy(name string) (staged string, keep bool, err error) {
	dir, err := os.MkdirTemp("", "s3b-edit-*")
	if err != nil {
		return "", false, err
	}
	staged = filepath.Join(dir, name)
	if err := os.WriteFile(staged, nil, 0o600); err != nil {
		os.RemoveAll(dir)
		return "", false, err
	}
	return staged, false, nil
}

// copyToStaged drains the pulled stream into the staging copy.
func copyToStaged(staged string, rc io.Reader) error {
	f, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := io.Copy(f, rc)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// stampStaged records the staging copy's size and mtime right after the
// pull — the editor's own save is the only hand that moves them.
func stampStaged(staged string) (size, mod int64, err error) {
	fi, err := os.Stat(staged)
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), fi.ModTime().UnixMilli(), nil
}

// runEditorAndWait opens the editor and reports whether the staged copy
// moved off its pull stamp. Same-size same-millisecond saves are the
// one blind spot (a human editor round trip never lands there — the
// launch alone takes longer than the clock's tick).
func runEditorAndWait(staged string, size, mod int64) (changed bool, err error) {
	if err := launchEditor(staged); err != nil {
		return false, opErr(fmt.Errorf("editor: %w", err))
	}
	fi, err := os.Stat(staged)
	if err != nil {
		return false, opErr(fmt.Errorf("the editor left no file at %s: %w", staged, err))
	}
	return fi.Size() != size || fi.ModTime().UnixMilli() != mod, nil
}

// editConflictErr is the refusal's voice: what moved, where the edit
// lives, and the two honest ways out.
func editConflictErr(operand, detail, staged string) error {
	return opErr(fmt.Errorf("%w: %s %s — your edit is kept at %s (re-run with --force to overwrite it)",
		editConflict, operand, detail, staged))
}

// httpStatusIs reports whether err carries the HTTP status code — the
// SDK wraps every wire error in a ResponseError, but bare-bodies
// servers still classify by the number.
func httpStatusIs(err error, code int) bool {
	var re *awshttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == code
}

// preconditionRefused reports whether err is the S3 conditional-write
// refusal (If-Match): the PreconditionFailed code well-behaved servers
// answer with, or a bare 412 from ones that skip the XML body — the
// same dual identity the GUI's editor classifies on.
func preconditionRefused(err error) bool {
	var re *awshttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusPreconditionFailed {
		return true
	}
	var api interface {
		ErrorCode() string
	}
	if errors.As(err, &api) && api.ErrorCode() == "PreconditionFailed" {
		return true
	}
	return false
}

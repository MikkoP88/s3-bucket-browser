package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	awshttp "github.com/aws/smithy-go/transport/http"
)

// EventEditorSaved fires when an edited file was uploaded back (payload:
// {bucket, key}).
const EventEditorSaved = "editor:saved"

// EventEditorPushFailed fires when a watched edit could not be uploaded
// back — once per failing streak, not per attempt (payload:
// {bucket, key, error}). The session stays dirty and retries with backoff
// until a push lands.
const EventEditorPushFailed = "editor:push-failed"

// EventEditorConflict fires when an edit push was refused because the
// object changed on the server since the session pulled it — the
// lost-update guard speaking (payload: {bucket, key}; once per stale
// transition). The session stays dirty until the user decides: push
// anyway, reload from the server, or stop and discard.
const EventEditorConflict = "editor:conflict"

// EventRemoteChanged fires when an edit push landed on a remote engine
// source (payload: {source, dir}) — the remote twin of s3:changed: the
// views seating that source's directory refresh on it.
const EventRemoteChanged = "remote:changed"

// errEditConflict names the editor's lost-update refusal: the push met a
// remote object that is no longer the version this session pulled.
var errEditConflict = errors.New("the object changed on the server since editing began")

// isPreconditionFailed reports whether err is the S3 conditional-write
// refusal: an API error carrying the PreconditionFailed code AWS and
// well-behaved S3-compatible servers answer with (this SDK vintage ships
// no typed error for it — the wire code is the identity), or a bare 412
// from a server that skips the XML body.
func isPreconditionFailed(err error) bool {
	var api smithy.APIError
	if errors.As(err, &api) && api.ErrorCode() == "PreconditionFailed" {
		return true
	}
	var re *awshttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == http.StatusPreconditionFailed
}

// editSession tracks one file opened in an external editor.
type editSession struct {
	Bucket string
	Key    string
	Source string // the engine leg's source NAME; S3: the source name, "" = the view source
	remote bool   // the engine leg (FTP/SFTP/WebDAV/local-root sources)
	Local  string
	mu     sync.Mutex
	// the S3 leg's push guard: the object's ETag at pull, "" = degraded off
	etag string
	// the engine leg's push guard: the file's size and mtime at pull. rMod 0
	// = the engine reported no mtime (or Stat failed — rGuard off, the same
	// honest degradation as a missing ETag); mtime is compared only when both
	// the baseline and the engine's answer are nonzero, because engines
	// differ in precision (FTP minutes, SFTP seconds, WebDAV sub-second).
	rSize  int64
	rMod   int64
	rGuard bool
	// the staged file's identity, driving change detection and the dirty flag
	origSize int64
	origMod  int64
	lastSize int64
	lastMod  int64
	dirty    bool      // changed since last upload
	stale    bool      // the guard refused a push: the remote changed underneath
	fails    int       // consecutive push failures (drives the backoff)
	nextTry  time.Time // earliest retry after a failure
	done     bool
}

// editKey is the session's registry key, the target grammar the typed
// family speaks: S3 sessions keep the historical bucket\x00key; engine
// sessions sit in their own namespace — a NUL can occur in neither a
// bucket name nor an engine path, so no S3 bucket (even one literally
// named "remote") can ever collide with an engine session.
func (s *editSession) editKey() string {
	if s.remote {
		return "remote\x00" + s.Source + "\x00" + s.Key
	}
	return s.Bucket + "\x00" + s.Key
}

// label is the session's human name in logs and the manager rows —
// source:path for engine files (the crumb grammar), bucket/key for S3.
func (s *editSession) label() string {
	if s.remote {
		return s.Source + ":" + s.Key
	}
	return s.Bucket + "/" + s.Key
}

// EditInfo is the status view of an edit session.
type EditInfo struct {
	Bucket     string `json:"bucket"`
	Key        string `json:"key"`
	Source     string `json:"source"` // engine leg: the source name; S3 rows: the named source, "" = the view source
	Kind       string `json:"kind"`   // "s3" | "remote"; legacy shim rows without it read as s3
	Local      string `json:"local"`
	Dirty      bool   `json:"dirty"`
	PushFailed bool   `json:"pushFailed"` // last push failed; retrying with backoff
	Stale      bool   `json:"stale"`      // the object changed on the server; waiting for a decision
}

// EditTarget names one editable file anywhere the app seats files — the
// typed grammar the transfer matrix's dests already speak. Kind "s3" is
// an object of the view source (Source "") or of a named source (the
// dual pane's bindings); kind "remote" is a file of an engine source
// (FTP/SFTP/WebDAV/local-root), Key the engine path anchored at "/".
// Local files never cross the bridge — the frontend opens them in
// place, so kind "local" is refused here.
type EditTarget struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// id is the target's session-registry key — the session grammar above.
func (t EditTarget) id() string {
	if t.Kind == "remote" {
		return "remote\x00" + t.Source + "\x00" + t.Key
	}
	return t.Bucket + "\x00" + t.Key
}

// editDir is the temp workspace for edited files: the config dir
// (0700, secure.go) under secure storage, else the system temp dir.
// S3 objects stage under their bucket name, engine files under
// "remote/<source name>" — a slash cannot occur in a bucket name, so
// the two trees can never collide — and every segment is server-
// supplied, riding the containment join like every other listing-
// derived path segment.
func editDir(parts ...string) string {
	return transfer.SafeLocalJoin(workspaceBase("edit"), parts...)
}

// watcherPoll is the file-watch interval; a change is uploaded after it
// stays stable for two consecutive polls (editor save jitters). Atomic
// nanoseconds so tests can shrink the cadence with plain stores while
// watchers from earlier tests are still mid-flight: the atomic pair is
// the only ordering a test's write and a live watcher's read ever share.
var watcherPoll atomic.Int64

func init() { watcherPoll.Store(int64(1200 * time.Millisecond)) }

// editPushBackoff spaces out retries of a failing push: one poll's grace
// at first, doubling per consecutive failure and capped at sixteen — a
// dead endpoint is retried forever, but gently (~19s at the cap with the
// shipped cadence).
func editPushBackoff(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	if fails > 4 {
		fails = 4
	}
	return time.Duration(watcherPoll.Load()) * time.Duration(1<<fails)
}

// editPushTimeout bounds one editor push. The push rides no transport
// budget (s3Opts installs Timeout:-1 so a slow link never kills a transfer)
// and no registry task either — its caller is the watcher goroutine, whose
// only cancellation is app shutdown — so without a deadline of its own, a
// peer that takes the body and goes silent (no FIN, no RST — the wedge
// class the wire engines were hardened against) parks the response wait
// for the life of the process: the watcher dies inside one push, the
// failure never reaches notePushFailed, and the session just sits dirty
// with no voice. The budget turns the wedge into an ordinary failed push
// — voiced, backed off, retried — which is the contract everything
// downstream already knows. A push that legitimately needs longer than
// this is not an editor edit anymore; the failure says so honestly. A
// var so tests can shorten it; read once per push in the caller's
// goroutine (the retire-grace discipline).
var editPushTimeout = 15 * time.Minute

// notePushFailed records one failed upload attempt on a session: the
// backoff bookkeeping, the per-attempt error line and the once-per-streak
// event the status-bar pill escalates on. The watcher's retries and
// StopEdit's explicit save ride the same voice.
func (a *App) notePushFailed(s *editSession, err error) {
	s.mu.Lock()
	first := s.fails == 0
	s.fails++
	wait := editPushBackoff(s.fails)
	s.nextTry = time.Now().Add(wait)
	s.mu.Unlock()
	a.emitLog(LogError, "edit", fmt.Sprintf(
		"upload of %s failed (retry in %s; edits stay pending): %v",
		s.label(), wait, err))
	if first {
		a.emit(EventEditorPushFailed, map[string]string{
			"bucket": s.Bucket, "key": s.Key, "source": s.Source, "error": err.Error(),
		})
	}
}

// openInEditor launches the OS editor for the staged file (or, chooseApp,
// the OS "Open with…" picker). A var so tests can pin the pull's task
// lifecycle without spawning real editors on the test machine.
var openInEditor = func(a *App, path string, chooseApp bool) error {
	if chooseApp {
		return a.OpenLocalWith(path)
	}
	return a.OpenLocal(path)
}

// refocus surfaces the editor for an already-open session. A second
// Edit on a live session must not re-download over the staged file —
// that would destroy edits saved but not yet uploaded (the pull writes
// the REMOTE bytes) — and must not leak a second watcher on it.
func (a *App) refocus(s *editSession, chooseApp bool) (EditInfo, error) {
	if err := openInEditor(a, s.Local, chooseApp); err != nil {
		return s.info(), fmt.Errorf("already being edited — could not open editor: %w", err)
	}
	return s.info(), nil
}

// EditObject downloads bucket/key of the browsing view source into a
// temp workspace, opens it with the OS default editor (or, chooseApp=
// true, the OS "Open with…" picker so the user selects the editing
// application per file) and keeps watching: every saved change is
// uploaded back automatically (WinSCP-style "keep remote up to date").
// The historical entry point; the typed EditFile grammar covers every
// seat a file has in this app.
func (a *App) EditObject(bucket, key string, chooseApp bool) (EditInfo, error) {
	return a.editS3("", bucket, key, chooseApp)
}

// EditFile opens any file the app seats for editing — the typed target
// grammar: an S3 object of the view source (Source "") or of any named
// source (the dual pane's bindings), or a file of a remote engine
// source (FTP/SFTP/WebDAV/local-root; the Key is the engine path
// anchored at "/"). Local files are refused here on purpose: they
// never stage and never push — the file IS the store, and the
// frontend opens it in place with the OS.
func (a *App) EditFile(t EditTarget, chooseApp bool) (EditInfo, error) {
	switch t.Kind {
	case "", "s3":
		return a.editS3(t.Source, t.Bucket, t.Key, chooseApp)
	case "remote":
		return a.editRemote(t.Source, t.Key, chooseApp)
	default:
		return EditInfo{}, fmt.Errorf(
			"cannot edit kind %q — S3 objects and remote source files edit in place; local files open directly", t.Kind)
	}
}

// editS3 is the S3 leg: source "" is the browsing view source, any
// other name the saved source the dual pane's bindings carry — the
// same client grammar every transfer leg already speaks.
func (a *App) editS3(source, bucket, key string, chooseApp bool) (EditInfo, error) {
	a.editorsMu.Lock()
	if s := a.editors[bucket+"\x00"+key]; s != nil {
		a.editorsMu.Unlock()
		// already being edited: re-focus the live session (idempotent
		// per object — the staged file, with any pending edits, is what
		// the editor gets; no re-download, no second watcher)
		return a.refocus(s, chooseApp)
	}
	a.editorsMu.Unlock()
	c, err := a.client(source)
	if err != nil {
		return EditInfo{}, err
	}
	dir := editDir(bucket)
	if err := os.MkdirAll(dir, 0o700); err != nil { // owner-only even on shared /tmp
		return EditInfo{}, err
	}
	local := transfer.SafeLocalJoin(dir, key)
	if st, err := os.Stat(local); err == nil && st.IsDir() {
		return EditInfo{}, fmt.Errorf("%s is a folder", key)
	}

	// engine tuning (Settings → Transfers) applies to editor pulls too
	partSize, conc := a.partTunables()
	// The pull rides the task registry (kind "edit"): a transient row —
	// visible with a Cancel in the Running tasks window while it runs,
	// gone when it lands, the status-bar editor indicator being the
	// session's lasting surface. Before this the pull was an invisible,
	// uncancellable bound call: a multi-gigabyte object just spun, and
	// the exit gate's task check now covers a quit mid-pull too.
	taskLabel := fmt.Sprintf("s3://%s/%s", bucket, key)
	if source != "" {
		taskLabel = fmt.Sprintf("s3://%s/%s/%s", source, bucket, key)
	}
	task := a.tasks.add("edit", taskLabel)
	task.setTotal(1, "")
	// Idempotent backstop: no-op when the explicit finishes below already
	// ran — and it still settles the row if a panic unwinds through here.
	defer task.finish(nil, true)
	if err := transfer.DownloadFile(task.ctx, c.S3, bucket, key, local,
		transfer.DownloadOptions{PartSize: partSize, Concurrency: conc}); err != nil {
		task.finish(err, true)
		return EditInfo{}, err
	}
	task.progress(1)
	_ = os.Chmod(local, 0o600) // object contents: owner-only in every mode
	st, err := os.Stat(local)
	if err != nil {
		return EditInfo{}, err
	}
	// The push guard's baseline: the remote identity this session pulled,
	// verbatim in the wire's own grammar (ETags travel quoted — "abc" — on
	// the wire and inside If-Match; storing and sending the same shape
	// keeps the comparison honest against every S3-compatible server). A
	// head that fails — or serves no ETag — degrades honestly: no
	// identity, no guard, exactly the pre-guard behavior. Never a false
	// alarm over an identity the wire never gave.
	etag := ""
	if ho, herr := c.S3.HeadObject(task.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}); herr == nil && ho.ETag != nil {
		etag = *ho.ETag
	}

	s := &editSession{
		Bucket: bucket, Key: key, Source: source, Local: local,
		origSize: st.Size(), origMod: st.ModTime().UnixMilli(),
		lastSize: st.Size(), lastMod: st.ModTime().UnixMilli(),
		etag: etag,
	}
	a.editorsMu.Lock()
	if prev := a.editors[s.editKey()]; prev != nil {
		// a concurrent Edit for the same object registered first (both
		// pulls were in flight): keep its session — the staged file is
		// the same path holding the same fresh bytes — and surface the
		// editor once more instead of stacking a second watcher on it
		a.editorsMu.Unlock()
		return a.refocus(prev, chooseApp)
	}
	a.editors[s.editKey()] = s
	a.editorsMu.Unlock()

	if err := openInEditor(a, local, chooseApp); err != nil {
		return s.info(), fmt.Errorf("downloaded but could not open editor: %w", err)
	}
	go a.watchEditor(s)
	return s.info(), nil
}

// editRemote is the engine leg of the editor: pull the file of a
// remote source (FTP/SFTP/WebDAV/local-root; name or ID resolves the
// same way every engine call does) into the edit workspace, open it,
// and push every stable save back through the engine — the same
// watcher, the same dirty/stale voice, with the guard the engine wire
// can actually speak: FTP/SFTP/WebDAV carry no ETags, so the baseline
// is the file's own size and mtime at pull, compared before every
// push. Check-then-write, both legs under the source's engine lock —
// the wire has no conditional store; the window is the milliseconds
// between the guard's Stat and the Create, and no honest engine can
// close it.
func (a *App) editRemote(idOrName, keyPath string, chooseApp bool) (EditInfo, error) {
	src, fs, err := a.remoteSource(idOrName)
	if err != nil {
		return EditInfo{}, err
	}
	if keyPath == "" || strings.HasSuffix(keyPath, "/") {
		return EditInfo{}, fmt.Errorf("%s is a folder", keyPath)
	}
	sid := "remote\x00" + src.Name + "\x00" + keyPath
	a.editorsMu.Lock()
	if s := a.editors[sid]; s != nil {
		a.editorsMu.Unlock()
		return a.refocus(s, chooseApp) // the live session's staged edits win
	}
	a.editorsMu.Unlock()
	dir := editDir("remote", src.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil { // owner-only even on shared /tmp
		return EditInfo{}, err
	}
	local := transfer.SafeLocalJoin(dir, strings.TrimPrefix(keyPath, "/"))
	if st, err := os.Stat(local); err == nil && st.IsDir() {
		return EditInfo{}, fmt.Errorf("%s is a folder", keyPath)
	}
	// the pull rides the task registry like the S3 leg's, labeled in the
	// source:path grammar the crumb uses
	task := a.tasks.add("edit", fmt.Sprintf("%s:%s", src.Name, keyPath))
	task.setTotal(1, "")
	defer task.finish(nil, true)
	// one engine data connection at a time (FTP allows exactly one) —
	// the pull and the guard's Stat share the source's lock hold
	unlock := a.lockSrcs(src.ID)
	rc, _, err := fs.Open(task.ctx, keyPath)
	if err != nil {
		unlock()
		task.finish(err, true)
		return EditInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		rc.Close()
		unlock()
		task.finish(err, true)
		return EditInfo{}, err
	}
	copyErr := func() error {
		defer rc.Close()
		f, err := os.OpenFile(local, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, rc)
		return err
	}()
	if copyErr != nil {
		unlock()
		task.finish(copyErr, true)
		return EditInfo{}, copyErr
	}
	// The push guard's baseline — the engine file's own identity at pull:
	// size, and mtime when the engine reports one (rMod 0 = it did not,
	// and the guard compares size only). A Stat that fails degrades
	// honestly — no baseline, no guard — exactly the S3 leg's missing-ETag
	// degradation; never a false alarm over an identity the wire never
	// gave. A directory answer is the IsDir refusal, honestly late.
	var rSize, rMod int64
	rGuard := false
	if st, serr := fs.Stat(task.ctx, keyPath); serr == nil {
		if st.IsDir {
			unlock()
			err := fmt.Errorf("%s is a folder", keyPath)
			task.finish(err, true)
			return EditInfo{}, err
		}
		rGuard = true
		rSize = st.Size
		if st.LastModified != nil {
			rMod = st.LastModified.UnixMilli()
		}
	}
	unlock()
	task.progress(1)
	st, err := os.Stat(local)
	if err != nil {
		return EditInfo{}, err
	}
	s := &editSession{
		remote: true, Source: src.Name, Key: keyPath, Local: local,
		origSize: st.Size(), origMod: st.ModTime().UnixMilli(),
		lastSize: st.Size(), lastMod: st.ModTime().UnixMilli(),
		rSize: rSize, rMod: rMod, rGuard: rGuard,
	}
	a.editorsMu.Lock()
	if prev := a.editors[s.editKey()]; prev != nil {
		// the concurrent-edit double-check, same law as the S3 leg
		a.editorsMu.Unlock()
		return a.refocus(prev, chooseApp)
	}
	a.editors[s.editKey()] = s
	a.editorsMu.Unlock()

	if err := openInEditor(a, local, chooseApp); err != nil {
		return s.info(), fmt.Errorf("downloaded but could not open editor: %w", err)
	}
	go a.watchEditor(s)
	return s.info(), nil
}

// watchEditor polls the file and uploads stable changes back.
func (a *App) watchEditor(s *editSession) {
	defer a.guardWorker("app", nil) // the worker panic net (guard.go)
	t := time.NewTicker(time.Duration(watcherPoll.Load()))
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-a.done():
			return
		}
		// StopEdit ended the session: no more auto-uploads. Without this
		// check the goroutine outlives the session and would keep pushing
		// edits the user explicitly discarded (StopEdit upload=false) to
		// the object.
		s.mu.Lock()
		done := s.done
		s.mu.Unlock()
		if done {
			return
		}
		st, err := os.Stat(s.Local)
		if os.IsNotExist(err) {
			// deleted: session over — and the registry must reflect it,
			// or a zombie entry pins the indicator (and, when dirty,
			// the exit gate) on a file that no longer exists
			a.editorsMu.Lock()
			if a.editors[s.editKey()] == s {
				delete(a.editors, s.editKey())
			}
			a.editorsMu.Unlock()
			a.emitLog(LogInfo, "edit", fmt.Sprintf(
				"session for %s ended: staged file removed", s.label()))
			return
		}
		if err != nil {
			// transient — an editor's atomic save (write temp, rename
			// over) can briefly hide the file; killing the watcher here
			// would silently stop every future auto-upload
			continue
		}
		size, mod := st.Size(), st.ModTime().UnixMilli()
		s.mu.Lock()
		changed := size != s.lastSize || mod != s.lastMod
		wasDirty := s.dirty
		if changed {
			s.lastSize, s.lastMod, s.dirty = size, mod, true
		}
		stableUpload := s.dirty && !changed && wasDirty && (size != s.origSize || mod != s.origMod)
		s.mu.Unlock()

		if stableUpload {
			now := time.Now()
			s.mu.Lock()
			ready := (s.fails == 0 || !now.Before(s.nextTry)) && !s.stale
			s.mu.Unlock()
			if !ready {
				continue // a failing push waits out its backoff; a stale one waits for its user
			}
			if err := a.pushSession(s, false); err == nil {
				s.mu.Lock()
				s.dirty = false
				s.fails = 0
				s.nextTry = time.Time{}
				s.mu.Unlock()
				a.noteEditSaved(s)
			} else if errors.Is(err, errEditConflict) {
				// The guard refused: the remote is no longer the version
				// this session pulled. Sticky — the condition cannot heal
				// on a timer, so no backoff ladder — voiced once, and the
				// session stays dirty until the user decides (push anyway,
				// reload from the server, or stop and discard).
				a.markEditStale(s, err)
			} else {
				// A failing push gets a voice and a gentler cadence: every
				// attempt logs, the first of a streak toasts (the session
				// stays visibly dirty — indicator and dialog carry the
				// failure — until a push lands), and the retries back off
				// instead of hammering a dead endpoint every poll.
				a.notePushFailed(s, err)
			}
		}
	}
}

// uploadEdit is the S3 push leg. The push is conditional while the
// session knows the remote's identity (the ETag it pulled): a remote
// change in flight fails with errEditConflict instead of silently
// overwriting it — the lost-update cure. force is the explicit
// informed consent the dialog's Push anyway carries, the one writer
// allowed past the guard. A landed push rebases the guard on the ETag
// it just wrote, so the next save guards against the new baseline.
// One direct conditional PUT, not the multipart manager: past its part
// size the manager rides CreateMultipartUpload, a wire that does not
// carry the condition — the guard would silently vanish exactly on the
// biggest files (UploadFileIfMatch owns the reasoning).
func (a *App) uploadEdit(s *editSession, force bool) error {
	c, err := a.client(s.Source)
	if err != nil {
		return err
	}
	// Shutdown stops an in-flight push through the app context; the exit
	// gate already refuses a quit while a session is dirty, so a push
	// canceled here is an explicitly confirmed discard. The deadline is the
	// push's own (editPushTimeout owns the reasoning): it is the only bound
	// standing between a silently vanished peer and a watcher parked
	// forever inside one unanswered response wait.
	budget := editPushTimeout
	var base context.Context = context.Background()
	if a.ctx != nil {
		base = a.ctx
	}
	ctx, cancel := context.WithTimeout(base, budget)
	defer cancel()
	s.mu.Lock()
	guard := s.etag
	s.mu.Unlock()
	if force {
		guard = "" // the explicit informed consent writes past the guard
	}
	newEtag, err := transfer.UploadFileIfMatch(ctx, c.S3, s.Local, s.Bucket, s.Key, guard)
	if err != nil {
		if isPreconditionFailed(err) {
			return fmt.Errorf("%w: %v", errEditConflict, err)
		}
		return err
	}
	if newEtag != "" {
		s.mu.Lock()
		s.etag = newEtag // verbatim, the wire's own quoted grammar
		s.mu.Unlock()
	}
	return nil
}

// pushSession is the session's own push leg — the S3 conditional PUT
// for object sessions, the guarded engine Create for engine sessions.
// One law for every caller: the watcher's auto-saves, PushEdit's and
// StopEditFile's explicit saves.
func (a *App) pushSession(s *editSession, force bool) error {
	if s.remote {
		return a.uploadEditRemote(s, force)
	}
	return a.uploadEdit(s, force)
}

// uploadEditRemote pushes the staged file back through the engine,
// guarded by the pull's baseline: the file's size, and its mtime when
// both the baseline and the engine's current answer are nonzero
// (engines differ in precision — FTP minutes, SFTP seconds, WebDAV
// sub-second — and a baseline of 0 means the engine reported none).
// Engines have no conditional store; the guard is a Stat compared and
// the Create written under the same source lock, so only the wire
// itself can slip a write between them — the honest limit, the
// milliseconds a check-then-write cannot close. A Stat error is
// endpoint trouble, not a green light: it fails the push into the
// backoff voice, never past the guard. A landed push rebases the
// baseline on what the engine now reports — the mtime twin of the
// ETag rebase, so the next save guards against the new baseline.
func (a *App) uploadEditRemote(s *editSession, force bool) error {
	src, fs, err := a.remoteSource(s.Source)
	if err != nil {
		return err
	}
	// the same budget the S3 leg rides (editPushTimeout owns the
	// reasoning): app shutdown cancels, and a peer that takes the body
	// and goes silent must not park the watcher forever
	budget := editPushTimeout
	var base context.Context = context.Background()
	if a.ctx != nil {
		base = a.ctx
	}
	ctx, cancel := context.WithTimeout(base, budget)
	defer cancel()
	f, err := os.Open(s.Local)
	if err != nil {
		return err
	}
	defer f.Close()
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	s.mu.Lock()
	guard, wantSize, wantMod := s.rGuard, s.rSize, s.rMod
	s.mu.Unlock()
	if guard && !force {
		cur, serr := fs.Stat(ctx, s.Key)
		if serr != nil {
			return serr
		}
		if cur.IsDir {
			return fmt.Errorf("%s became a folder", s.Key)
		}
		if cur.Size != wantSize || (wantMod != 0 && cur.LastModified != nil && cur.LastModified.UnixMilli() != wantMod) {
			return fmt.Errorf("%w: %s changed on the server since editing began", errEditConflict, s.Key)
		}
	}
	if err := fs.Create(ctx, s.Key, f); err != nil {
		return err
	}
	if cur, serr := fs.Stat(ctx, s.Key); serr == nil && !cur.IsDir {
		s.mu.Lock()
		s.rSize = cur.Size
		s.rMod = 0
		if cur.LastModified != nil {
			s.rMod = cur.LastModified.UnixMilli()
		}
		s.mu.Unlock()
	}
	return nil
}

// noteEditLanded voices a landed push to the views seating the file's
// location — the refresh nudge (s3:changed / remote:changed, the
// remote twin). The saved event itself rides noteEditSaved.
func (a *App) noteEditLanded(s *editSession) {
	if s.remote {
		a.emit(EventRemoteChanged, map[string]string{"source": s.Source, "dir": path.Dir(s.Key)})
		return
	}
	a.emit(EventS3Changed, map[string]string{"bucket": s.Bucket, "source": s.Source})
}

// noteEditSaved voices a landed push in full: the saved event (payload
// in the session's own identity grammar — bucket for objects, source
// for engine files, key basename-friendly in both, so today's toasts
// speak unchanged) and the refresh nudge (noteEditLanded).
func (a *App) noteEditSaved(s *editSession) {
	if s.remote {
		a.emit(EventEditorSaved, map[string]string{"source": s.Source, "key": s.Key})
	} else {
		a.emit(EventEditorSaved, map[string]string{"bucket": s.Bucket, "key": s.Key})
	}
	a.noteEditLanded(s)
}

// markEditStale flags a session whose guard refused a push — the remote
// is no longer the version this session pulled. Sticky by design: the
// condition cannot heal on a timer, so it never rides the backoff ladder.
// Every refusal logs; the event fires once per transition (the escalation
// the status pill and dialog carry from there).
func (a *App) markEditStale(s *editSession, err error) {
	s.mu.Lock()
	was := s.stale
	s.stale = true
	s.mu.Unlock()
	a.emitLog(LogError, "edit", fmt.Sprintf(
		"push of %s refused: %v (edits stay pending — push anyway or reload)",
		s.label(), err))
	if !was {
		a.emit(EventEditorConflict, map[string]string{"bucket": s.Bucket, "key": s.Key, "source": s.Source})
	}
}

// PushEdit pushes a session's pending edits now — the stale session's way
// out. force=true writes over the remotely changed object (the dialog's
// Push anyway — the informed consent); force=false retries the guard, for
// after the remote side was put back. A clean session has nothing
// pending and costs no upload (the same economy as StopEdit). The
// session survives every outcome: a landed push settles the dirty state
// and rebases the guard on what it wrote, a conflict re-marks it stale,
// an ordinary failure enters the same backoff voice the watcher rides.
// The historical entry point (view-source objects); PushEditFile is the
// typed grammar covering every seat.
func (a *App) PushEdit(bucket, key string, force bool) error {
	return a.pushEdit(bucket+"\x00"+key, key, force)
}

// PushEditFile pushes a typed target's pending edits now — the typed
// grammar (see PushEdit).
func (a *App) PushEditFile(t EditTarget, force bool) error {
	return a.pushEdit(t.id(), t.Key, force)
}

func (a *App) pushEdit(sid, displayKey string, force bool) error {
	a.editorsMu.Lock()
	s := a.editors[sid]
	a.editorsMu.Unlock()
	if s == nil {
		return fmt.Errorf("not being edited: %s", displayKey)
	}
	// Like StopEdit's explicit save: the watcher only notices changes on
	// its poll — an explicit push right after an edit must not depend on
	// that timing. What is on disk is the verdict.
	s.mu.Lock()
	if st, err := os.Stat(s.Local); err == nil {
		if st.Size() != s.lastSize || st.ModTime().UnixMilli() != s.lastMod {
			s.dirty = true
		}
	}
	dirty := s.dirty
	s.mu.Unlock()
	if !dirty {
		return nil
	}
	if err := a.pushSession(s, force); err != nil {
		if errors.Is(err, errEditConflict) {
			a.markEditStale(s, err)
		} else {
			a.notePushFailed(s, err)
		}
		return err
	}
	s.mu.Lock()
	s.dirty = false
	s.fails = 0
	s.nextTry = time.Time{}
	s.stale = false
	s.mu.Unlock()
	a.noteEditSaved(s)
	return nil
}

// editDiffCap bounds each side of an EditDiff sample: the manager's
// destructive decisions (Push anyway, Reload from server) are gated by
// content, but no gate may stream a multi-gigabyte object across the
// bridge to draw one — the first editDiffCap bytes tell the user what
// they are about to overwrite or discard, and a truncation flag says
// honestly that there is more. A var so rigs can shrink it.
var editDiffCap = 256 << 10

// EditDiffInfo is the two-sided view of an open edit session: the staged
// local edit versus the server's current bytes — the content the dialog's
// destructive decisions are consented against. Local/Remote carry the
// SAMPLED text (editDiffCap-bounded, trunc flags naming the cut); the
// byte counts carry the full sizes; RemoteMissing is the teammate-deleted
// case (the diff says so instead of failing — Push anyway then recreates,
// a reload fails honestly); Binary marks NUL-carrying samples, where the
// dialog shows sizes instead of pretending a line diff means anything.
type EditDiffInfo struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
	// full sizes even when the samples are cut (the framing's own honesty)
	LocalBytes    int64 `json:"localBytes"`
	RemoteBytes   int64 `json:"remoteBytes"`
	LocalTrunc    bool  `json:"localTrunc"`
	RemoteTrunc   bool  `json:"remoteTrunc"`
	RemoteMissing bool  `json:"remoteMissing"`
	Binary        bool  `json:"binary"`
	// the sample bound itself, so the truncation note names the real cut
	CapBytes int64 `json:"capBytes"`
}

// isNoSuchKey reports whether err is the S3 missing-object refusal: the
// typed NoSuchKey code, or a bare 404 from a server that skips the XML
// body — the same dual read isPreconditionFailed carries.
func isNoSuchKey(err error) bool {
	var api smithy.APIError
	if errors.As(err, &api) && api.ErrorCode() == "NoSuchKey" {
		return true
	}
	var re *awshttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound
}

// sampleEditDiff reads up to editDiffCap+1 bytes of one side (the +1 is
// the truncation probe) and reports the sampled text and whether the cut
// happened. A read error mid-sample is the caller's to voice.
func sampleEditDiff(r io.Reader) (string, bool, error) {
	buf := make([]byte, editDiffCap+1)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false, err
	}
	if n > editDiffCap {
		n = editDiffCap
		return string(buf[:n]), true, nil
	}
	return string(buf[:n]), false, nil
}

// hasNUL reports whether the sample's first 8 KiB carries a NUL byte —
// the cheap binary verdict every text tool agrees on.
func hasNUL(sample string) bool {
	probe := sample
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	return strings.ContainsRune(probe, 0)
}

// EditDiff shows both sides of an open edit session — the staged edit on
// this machine versus the server's current bytes — so the manager's
// destructive decisions consent against content, not labels: Push anyway
// means overwriting what the Remote side shows, Reload from server means
// discarding what the Local side holds. One typed grammar for both legs
// (the manager's rows already speak it); a session that is not open has
// nothing to diff, and a fetch that fails says so instead of gating the
// decision on silence — the caller toasts and the way out stays reachable
// by retry. The samples are editDiffCap-bounded: a diff view must never
// become a second full pull.
func (a *App) EditDiff(t EditTarget) (EditDiffInfo, error) {
	switch t.Kind {
	case "", "s3", "remote":
	default:
		return EditDiffInfo{}, fmt.Errorf(
			"cannot diff kind %q — S3 objects and remote source files diff in place; local files never cross the bridge", t.Kind)
	}
	a.editorsMu.Lock()
	s := a.editors[t.id()]
	a.editorsMu.Unlock()
	if s == nil {
		return EditDiffInfo{}, fmt.Errorf("not being edited: %s", t.Key)
	}
	kind := "s3"
	if s.remote {
		kind = "remote"
	}
	d := EditDiffInfo{Kind: kind, Source: s.Source, Bucket: s.Bucket, Key: s.Key}

	// the local side: what the staged file holds right now — the edit the
	// user would force onto the server, or throw away on a reload
	if lst, err := os.Stat(s.Local); err == nil {
		d.LocalBytes = lst.Size()
	} else {
		return EditDiffInfo{}, err
	}
	lf, err := os.Open(s.Local)
	if err != nil {
		return EditDiffInfo{}, err
	}
	d.Local, d.LocalTrunc, err = sampleEditDiff(lf)
	lf.Close()
	if err != nil {
		return EditDiffInfo{}, err
	}

	// the sample budget: a wedge-class peer must not park the dialog —
	// one bounded minute, the diff's own (a push rides the push budget)
	var base context.Context = context.Background()
	if a.ctx != nil {
		base = a.ctx
	}
	ctx, cancel := context.WithTimeout(base, time.Minute)
	defer cancel()

	if s.remote {
		src, fsx, err := a.remoteSource(s.Source)
		if err != nil {
			return EditDiffInfo{}, err
		}
		unlock := a.lockSrcs(src.ID)
		rc, size, err := fsx.Open(ctx, s.Key)
		if err != nil {
			unlock()
			if errors.Is(err, fs.ErrNotExist) {
				d.RemoteMissing = true
				return d, nil
			}
			return EditDiffInfo{}, err
		}
		d.Remote, d.RemoteTrunc, err = sampleEditDiff(rc)
		rc.Close()
		unlock()
		if err != nil {
			return EditDiffInfo{}, err
		}
		d.RemoteBytes = size
		if size < 0 {
			d.RemoteBytes = int64(len(d.Remote))
		}
	} else {
		c, err := a.client(s.Source)
		if err != nil {
			return EditDiffInfo{}, err
		}
		out, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.Bucket), Key: aws.String(s.Key),
		})
		if err != nil {
			if isNoSuchKey(err) {
				d.RemoteMissing = true
				return d, nil
			}
			return EditDiffInfo{}, err
		}
		d.Remote, d.RemoteTrunc, err = sampleEditDiff(out.Body)
		out.Body.Close()
		if err != nil {
			return EditDiffInfo{}, err
		}
		d.RemoteBytes = int64(len(d.Remote))
		if out.ContentLength != nil && *out.ContentLength >= 0 {
			d.RemoteBytes = *out.ContentLength
		}
	}
	d.Binary = hasNUL(d.Local) || hasNUL(d.Remote)
	d.CapBytes = int64(editDiffCap)
	return d, nil
}

// done signals app shutdown (nil ctx before Startup = never).
func (a *App) done() <-chan struct{} {
	if a.ctx == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return a.ctx.Done()
}

// info snapshots a session (locks appropriately).
func (s *editSession) info() EditInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind := "s3"
	if s.remote {
		kind = "remote"
	}
	return EditInfo{
		Bucket: s.Bucket, Key: s.Key, Source: s.Source, Kind: kind, Local: s.Local,
		Dirty: s.dirty, PushFailed: s.fails > 0, Stale: s.stale,
	}
}

// EditingFiles lists open edit sessions (status bar indicator).
func (a *App) EditingFiles() []EditInfo {
	a.editorsMu.Lock()
	defer a.editorsMu.Unlock()
	out := make([]EditInfo, 0, len(a.editors))
	for _, s := range a.editors {
		out = append(out, s.info())
	}
	return out
}

// StopEdit ends a session; upload=true pushes pending changes first.
// The session is only ended when that push lands: "Stop & upload" is a
// save request, not a discard — destroying the session on a failed
// upload would strand the edits on a staged file the next boot's
// workspace wipe deletes, with the indicator gone and the exit gate
// no longer guarding. A failing explicit save keeps the session alive
// (still dirty, flagged, retried by the watcher with backoff) and
// returns the error. The historical entry point (view-source
// objects); StopEditFile is the typed grammar covering every seat.
func (a *App) StopEdit(bucket, key string, upload bool) error {
	return a.stopEdit(bucket+"\x00"+key, key, upload)
}

// StopEditFile ends a typed target's session (the typed grammar; see
// StopEdit).
func (a *App) StopEditFile(t EditTarget, upload bool) error {
	return a.stopEdit(t.id(), t.Key, upload)
}

func (a *App) stopEdit(sid, displayKey string, upload bool) error {
	a.editorsMu.Lock()
	s := a.editors[sid]
	a.editorsMu.Unlock()
	if s == nil {
		return fmt.Errorf("not being edited: %s", displayKey)
	}
	// The watcher only notices changes on its poll (1.2s cadence); an
	// explicit save right after an edit must not depend on that timing.
	// Re-check the staged file against the last state the app knew, so
	// "save and close" pushes what is on disk while an untouched file
	// still costs no upload (and no extra object version).
	s.mu.Lock()
	if upload {
		if st, err := os.Stat(s.Local); err == nil {
			if st.Size() != s.lastSize || st.ModTime().UnixMilli() != s.lastMod {
				s.dirty = true
			}
		}
	}
	dirty := s.dirty
	s.mu.Unlock()
	if upload && dirty {
		// The explicit save keeps the guard — never a side-door clobber
		// past the very condition the auto-push refuses. On a stale
		// session the push fails with the conflict and the session
		// survives (the dialog's Push anyway is the informed consent).
		if err := a.pushSession(s, false); err != nil {
			if errors.Is(err, errEditConflict) {
				a.markEditStale(s, err)
			} else {
				a.notePushFailed(s, err)
			}
			return err
		}
		a.noteEditLanded(s)
	}
	// settle — only now, with the push landed (or none wanted): the
	// watcher stops, the session leaves the registry (by identity, so a
	// session re-opened mid-stop survives), and the staged file retires.
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	a.editorsMu.Lock()
	if a.editors[sid] == s {
		delete(a.editors, sid)
	}
	a.editorsMu.Unlock()
	a.retireEditFile(s.Local)
	return nil
}

// retireEditFile schedules the staged file's removal once its session
// settles — the drag-out stage's grace, for the same reasons: the editor
// process may still hold the file open (without FILE_SHARE_DELETE the
// removal simply fails), and a slow write-behind may still land.
// Best-effort by design; the launch-time workspace wipe (secure.go)
// backstops what the grace leaves behind. Before this, every stopped
// session left its downloaded bytes in the edit workspace until the
// NEXT boot wiped it.
func (a *App) retireEditFile(path string) {
	if path == "" {
		return
	}
	// Capture the grace BEFORE the goroutine: a later seam swap (tests
	// shorten it) must not race the sleeper's read of the package var.
	grace := stageRetireGrace
	go func() {
		defer a.guardWorker("edit", nil) // the worker panic net (guard.go)
		time.Sleep(grace)
		_ = os.Remove(path)
	}()
}

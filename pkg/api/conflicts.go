// conflicts.go: destination collision pre-check. The GUI plans a transfer
// exactly like TransferCross would (same planner, same destination-side
// stat), reports which files already exist at the destination, and only
// then asks for a conflict policy — a clean destination never shows a
// conflict dialog at all.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ConflictInfo is one source/destination collision shown in the conflict
// dialog: both sides' size and modification time so the user can decide
// per file.
type ConflictInfo struct {
	Key         string `json:"key"`         // display name (destination-relative)
	DecisionKey string `json:"decisionKey"` // engine key for per-file decisions
	SrcSize     int64  `json:"srcSize"`
	SrcTime     string `json:"srcTime,omitempty"` // RFC3339, "" when unknown
	DstSize     int64  `json:"dstSize"`
	DstTime     string `json:"dstTime,omitempty"`
}

// Pre-check budgets. The planner walks everything (the same walk the
// transfer itself performs); destination stats are one HEAD/Stat per file,
// so both the planned-file count and the returned row list are capped.
// Beyond the caps the caller falls back to the classic whole-transfer
// policy dialog.
const (
	conflictMaxFiles = 500 // planned files worth stat-ing
	conflictMaxRows  = 200 // rows returned to the dialog

	// conflictProbeWidth bounds the destination probes where the wire
	// is concurrency-safe: the aws client serves parallel HEADs, plain
	// filesystem stats parallelize for free. A remote engine stays at
	// width 1 — the ServerConn has zero synchronization, so its lock
	// law keeps one probe in flight.
	conflictProbeWidth = 8
)

// The plan memo: the pre-check's walk is exactly the walk the transfer
// performs again before its first byte. For the paste that immediately
// follows a clean pre-check — the common gesture — the second walk is
// pure latency, so CheckConflicts hands its finished plan to the
// follow-up start. Single-use (the first taker consumes it), bounded in
// time, count and size, and keyed on the exact argument tuple: policy,
// throttle, decisions and move ride the plan's files, not its identity.
const (
	planMemoTTL      = 2 * time.Minute
	planMemoEntries  = 6
	planMemoMaxFiles = 20000 // a monster walk parks tens of MB for a window the provider's pace already dominates
)

type xferPlanMemo struct {
	mu      sync.Mutex
	entries []xferPlanMemoEntry
}

type xferPlanMemoEntry struct {
	key  string
	plan *xferPlan
	at   time.Time
}

// xferPlanKey is the canonical identity of one planning request: the
// same items, local paths and destination plan the same bytes.
func xferPlanKey(items []XferItem, localPaths []string, dest XferDest) string {
	b, _ := json.Marshal(struct {
		Items      []XferItem `json:"i"`
		LocalPaths []string   `json:"l"`
		Dest       XferDest   `json:"d"`
	}{items, localPaths, dest})
	return string(b)
}

func (m *xferPlanMemo) store(key string, plan *xferPlan) {
	if plan == nil || len(plan.files) > planMemoMaxFiles {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.entries[:0]
	for _, e := range m.entries {
		if time.Since(e.at) < planMemoTTL && e.key != key {
			kept = append(kept, e)
		}
	}
	m.entries = append(kept, xferPlanMemoEntry{key: key, plan: plan, at: time.Now()})
	if len(m.entries) > planMemoEntries {
		m.entries = m.entries[len(m.entries)-planMemoEntries:]
	}
}

// take consumes the memo entry for key when one is live. Expired
// entries are dropped as they pass by.
func (m *xferPlanMemo) take(key string) (*xferPlan, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.entries[:0]
	var found *xferPlan
	for _, e := range m.entries {
		if e.key == key && time.Since(e.at) < planMemoTTL {
			found = e.plan
			continue // single-use: consumed by its taker
		}
		if time.Since(e.at) < planMemoTTL {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	return found, found != nil
}

// CheckConflicts plans the transfer described by (items, localPaths, dest)
// and reports the planned files that already exist at the destination.
// decisionKey is the key the engine's per-file decision map understands:
// for Upload the destination object key, for Download the source object
// key, for TransferCross the planned destination path.
func (a *App) CheckConflicts(items []XferItem, localPaths []string, dest XferDest) ([]ConflictInfo, error) {
	dst, err := a.resolveXferDest(dest)
	if err != nil {
		return nil, err
	}
	pctx, cancel := a.quickCtx()
	plan, err := a.planXfer(pctx, items, localPaths, dst)
	cancel()
	if err != nil {
		return nil, err
	}
	// Hand the finished walk to the follow-up start: a clean pre-check
	// is almost always followed by TransferCross over the same
	// arguments, and its fresh walk would be pure latency before the
	// first byte. Store before the caps refuse — a transfer too big to
	// pre-check still starts, and its plan is exactly what it needs.
	a.planMemo.store(xferPlanKey(items, localPaths, dest), plan)
	// Success must serialize as [], not null: the frontend treats a null
	// probe result as "pre-check failed" and falls back to the classic
	// whole-transfer dialog — a clean destination must stay silent.
	if len(plan.files) == 0 {
		return []ConflictInfo{}, nil
	}
	if len(plan.files) > conflictMaxFiles {
		return nil, fmt.Errorf("too many files to pre-check (%d)", len(plan.files))
	}
	sctx, scancel := a.quickCtx()
	defer scancel()

	// The destination probes ride the same per-source law the planner
	// holds: a remote destination's Stat is an engine command like any
	// other, serialized on the source's lock (S3 and local probes carry
	// no lock id and lock nothing). Where the wire is concurrency-safe
	// the probes fan out — one HEAD per file, N×RTT folded to N/width —
	// while results keep the plan's walk order for the dialog.
	probeUnlock := a.lockSrcs(dst.lockID)
	defer probeUnlock()

	workers := 1
	if dst.kind == "s3" || dst.kind == "local" {
		workers = conflictProbeWidth
	}
	type probeRes struct {
		size   int64
		mtime  time.Time
		exists bool
	}
	probes := make([]probeRes, len(plan.files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := range plan.files {
		f := &plan.files[i]
		wg.Add(1)
		go func(slot int, f *xferFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			size, mtime, exists := xferDestStat(sctx, dst, f.dstPath)
			probes[slot] = probeRes{size: size, mtime: mtime, exists: exists}
		}(i, f)
	}
	wg.Wait()

	var out = []ConflictInfo{}
	var truncated bool
	for i := range plan.files {
		if !probes[i].exists {
			continue
		}
		if len(out) == conflictMaxRows {
			truncated = true
			break
		}
		out = append(out, ConflictInfo{
			Key:         conflictDisplayKey(dst, plan.files[i].dstPath),
			DecisionKey: plan.files[i].dstPath,
			SrcSize:     plan.files[i].size,
			SrcTime:     rfc3339(plan.files[i].mtime),
			DstSize:     probes[i].size,
			DstTime:     rfc3339(probes[i].mtime),
		})
	}
	// A truncated list cannot carry per-file decisions — the dialog then
	// applies the chosen action to the WHOLE transfer (classic behavior).
	if truncated {
		return nil, fmt.Errorf("too many conflicting files to list")
	}
	return out, nil
}

// xferDestStat returns the destination file's size and mtime when the path
// is taken. An unreadable destination reports not-taken: a failed probe
// must never fake a conflict (or block one). The local probe is a bounded
// step — the dialog's preview is advisory, so a wedged destination volume
// costs one budget and reads as not-taken, never a parked conflict check.
func xferDestStat(ctx context.Context, dst xferDestSide, p string) (int64, time.Time, bool) {
	switch dst.kind {
	case "s3":
		out, err := dst.client.S3.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(dst.bucket), Key: aws.String(p),
		})
		if err != nil {
			return 0, time.Time{}, false
		}
		return aws.ToInt64(out.ContentLength), aws.ToTime(out.LastModified), true
	case "remote":
		e, err := dst.fs.Stat(ctx, p)
		if err != nil || e.IsDir {
			return 0, time.Time{}, false
		}
		return e.Size, aws.ToTime(e.LastModified), true
	default:
		stat := localStat // captured before the step spawns (seam discipline)
		st, err := remotefs.LocalStep(ctx, func() (os.FileInfo, error) { return stat(p) })
		if err != nil || st.IsDir() {
			return 0, time.Time{}, false
		}
		return st.Size(), st.ModTime(), true
	}
}

// conflictDisplayKey renders a destination path relative to the transfer's
// destination directory (what the dialog lists after the common prefix).
func conflictDisplayKey(dst xferDestSide, p string) string {
	switch dst.kind {
	case "s3":
		if dst.dir != "" {
			return strings.TrimPrefix(p, dst.dir)
		}
		return p
	case "remote":
		base := strings.TrimSuffix(dst.dir, "/")
		if base != "" && strings.HasPrefix(p, base+"/") {
			return p[len(base)+1:]
		}
		return strings.TrimPrefix(p, "/")
	default:
		if rel, err := filepath.Rel(dst.dir, p); err == nil {
			return filepath.ToSlash(rel)
		}
		return filepath.Base(p)
	}
}

// filePolicy resolves the effective policy for one file: a per-file
// decision from the conflict dialog wins over the job-wide policy.
func filePolicy(decisions map[string]string, key, fallback string) string {
	if d, ok := decisions[key]; ok {
		switch d {
		case PolicyOverwrite, PolicySkip, PolicyRename:
			return d
		}
	}
	return fallback
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

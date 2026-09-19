// jobinfo_test.go pins the full-picture transfer-row contract: live
// byte state from progress(), the stall flag, the finish stamps, the
// timeout classification, and the Running-tasks label join.
package api

import (
	"context"
	"testing"
	"time"
)

func TestProgressFeedsLiveStateAndEmits(t *testing.T) {
	a := newTestApp(t)
	a.jobs.setContext(context.Background())
	j := a.jobs.add("upload", 1, 1000)
	j.setMeta("video.mp4", `C:\shoot`, "s3://team-files/shoot", 1, false)
	j.startFile(1, `C:\shoot\video.mp4`, 1000)

	// The 0%→100% bug: progress() used to update SentBytes silently and
	// only file boundaries emitted. Now every read both updates the
	// live state AND pushes an event (throttled), synchronously.
	j.progress(400, 1000)
	snap := a.jobs.snapshot()[0]
	if snap.SentBytes != 400 || snap.CurrentSent != 400 || snap.CurrentTotal != 1000 {
		t.Fatalf("live state after progress = %+v", snap)
	}
	if snap.Name != "video.mp4" || snap.From != `C:\shoot` || snap.To != "s3://team-files/shoot" {
		t.Fatalf("route meta = %+v", snap)
	}
	if snap.FileIndex != 1 || snap.Phase != PhaseTransfer {
		t.Fatalf("file ordinal/phase = %d/%q", snap.FileIndex, snap.Phase)
	}

	j.fileDone(1000, false)
	snap = a.jobs.snapshot()[0]
	if snap.DoneFiles != 1 || snap.CurrentSent != 0 || snap.CurrentTotal != 0 {
		t.Fatalf("post-fileDone state = %+v", snap)
	}
}

func TestStallFlagFollowsByteMovement(t *testing.T) {
	a := newTestApp(t)
	j := a.jobs.add("download", 1, 100)
	j.startFile(1, "big.bin", 100)
	j.progress(10, 100)
	if got := a.jobs.snapshot()[0].Stalled; got {
		t.Fatal("fresh movement flagged stalled")
	}

	// Simulate a hung read: rewind the last byte time beyond the job's
	// captured stall window (the Settings → Transfers threshold, default
	// 10s) and let the heartbeat's own logic judge it.
	j.mu.Lock()
	j.lastByteAt = time.Now().Add(-j.stallAfter - time.Second)
	j.mu.Unlock()
	j.mu.Lock()
	stalled := j.info.Status == JobRunning && j.info.Phase == PhaseTransfer &&
		j.info.CurrentTotal > 0 && time.Since(j.lastByteAt) > j.stallAfter
	j.mu.Unlock()
	if !stalled {
		t.Fatal("no movement beyond the stall threshold did not count as stalled")
	}

	// Bytes moving again clears it (progress + startFile both refresh).
	j.progress(20, 100)
	if got := a.jobs.snapshot()[0].Stalled; got {
		t.Fatal("movement resumed but stall flag stuck")
	}
}

func TestFinishStampsElapsedAndClearsCurrent(t *testing.T) {
	a := newTestApp(t)
	j := a.jobs.add("transfer", 2, 50)
	j.setMeta("docs", "s3://a", "s3://b", 2, true)
	j.startFile(1, "docs/x.txt", 50)
	j.progress(50, 50)
	j.fileDone(50, false)
	a.finishJob(j, JobDone, "")

	snap := a.jobs.snapshot()[0]
	if snap.Status != JobDone || snap.ElapsedMs < 0 {
		t.Fatalf("finished stamp = %+v", snap)
	}
	if snap.CurrentFile != "" || snap.CurrentSent != 0 || snap.CurrentTotal != 0 || snap.Stalled {
		t.Fatalf("in-flight fields survived finish: %+v", snap)
	}
}

func TestTimeoutKindClassification(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"getsockopt: i/o timeout":         "timeout",
		"context deadline exceeded":       "timeout",
		"Client.Timeout exceeded":         "timeout",
		"readObject: operation timed out": "timeout",
		" NoSuchBucket: 404":              "",
	}
	for msg, want := range cases {
		if got := timeoutKind(msg); got != want {
			t.Fatalf("timeoutKind(%q) = %q, want %q", msg, got, want)
		}
	}

	a := newTestApp(t)
	j := a.jobs.add("download", 1, 10)
	a.finishJob(j, JobError, "big.bin: read tcp: i/o timeout")
	if got := a.jobs.snapshot()[0].ErrorKind; got != "timeout" {
		t.Fatalf("timeout error not classified: %q", got)
	}
}

func TestRunningTasksLabelsJobsByName(t *testing.T) {
	a := newTestApp(t)
	a.jobs.setContext(context.Background())
	j := a.jobs.add("upload", 3, 100)
	j.setMeta("photos", `D:\pics`, "s3://team-files", 3, false)
	got := a.RunningTasks()
	if len(got) != 1 || got[0].Label != "photos +2" {
		t.Fatalf("merged transfer label = %+v", got)
	}
	// name-less jobs fall back to the current file, then the ID
	k := a.jobs.add("download", 1, 1)
	k.startFile(1, "a.txt", 1)
	got = a.RunningTasks()
	if got[1].Label != "a.txt" {
		t.Fatalf("fallback label = %+v", got[1])
	}
}

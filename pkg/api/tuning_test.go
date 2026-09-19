// tuning_test.go pins the Settings engine-tuning contract: SetTuning
// persists through appsettings.json (<=0 args mean "default"), the stored
// values drive quickCtx's budget, the S3 client options and the multipart
// tunables, and a change retunes future jobs' stall threshold without
// touching running ones.
package api

import (
	"context"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
)

func TestSetTuningDefaultsOnZeroArgs(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	got, err := a.SetTuning(0, 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != appsettings.Default() {
		t.Fatalf("SetTuning(0…) = %+v, want defaults", got)
	}
	if a.GetTuning() != appsettings.Default() {
		t.Fatalf("GetTuning() = %+v, want defaults", a.GetTuning())
	}
}

func TestSetTuningClampsOutOfRange(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	// positive-but-absurd values clamp; 0/negative mean "default"
	got, err := a.SetTuning(5, 99_999_999, 500, 4096, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := appsettings.Default()
	want.ListingTimeoutMS = 100 // clamped up from 5ms
	want.CompareTimeoutMS = 3_600_000
	want.RetryAttempts = 16
	want.PartSizeMiB = 64
	want.StallAfterMS = 2_000
	if got != want {
		t.Fatalf("SetTuning clamped = %+v, want %+v", got, want)
	}
	got, err = a.SetTuning(-5, 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != appsettings.Default() {
		t.Fatalf("SetTuning(-5…) = %+v, want defaults (negative = default)", got)
	}
}

func TestTuningDrivesBudgetsAndOptions(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	if _, err := a.SetTuning(8_000, 60_000, 5, 8, 4, 30_000); err != nil {
		t.Fatal(err)
	}
	// quickCtx budget = listing timeout (8s)
	ctx, cancel := a.quickCtx()
	defer cancel()
	dl, _ := ctx.Deadline()
	remaining := time.Until(dl)
	if remaining > 8*time.Second || remaining < 6*time.Second {
		t.Fatalf("quickCtx budget = %v, want ~8s", remaining)
	}
	// s3Opts carries the retry count and the no-deadline sentinel
	opts := a.s3Opts()
	if opts.RetryAttempts != 5 || opts.Timeout != -1 {
		t.Fatalf("s3Opts() = %+v, want retries 5, timeout -1", opts)
	}
	// part tunables resolve to bytes + count
	ps, conc := a.partTunables()
	if ps != 8*1024*1024 || conc != 4 {
		t.Fatalf("partTunables() = (%d, %d), want (8 MiB, 4)", ps, conc)
	}
	// future jobs capture the stored stall threshold (30s)
	j := a.jobs.add("download", 1, 10)
	if j.stallAfter != 30*time.Second {
		t.Fatalf("job stall threshold = %v, want 30s", j.stallAfter)
	}
	// a later retune must not touch the running job
	if _, err := a.SetTuning(0, 0, 0, 0, 0, 60_000); err != nil {
		t.Fatal(err)
	}
	if j.stallAfter != 30*time.Second {
		t.Fatalf("running job's stall threshold moved to %v", j.stallAfter)
	}
	if j2 := a.jobs.add("download2", 1, 10); j2.stallAfter != 60*time.Second {
		t.Fatalf("new job after retune = %v, want the stored 60s", j2.stallAfter)
	}
	// full reset returns new jobs to the default threshold
	if _, err := a.SetTuning(0, 0, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if j3 := a.jobs.add("upload", 1, 10); j3.stallAfter != 10*time.Second {
		t.Fatalf("new job after reset = %v, want the 10s default", j3.stallAfter)
	}
}

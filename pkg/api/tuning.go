// Engine tuning (Settings → Network / Transfers, persisted by the GUI via
// appsettings.json in the config dir). The six knobs are read live at
// every consumer — quickCtx (listings, stat, presign, source tests), the
// listing-stream watchdog, deep compares, the transfer heartbeat's
// Stalled flag, the S3 clients' retryer and every multipart upload and
// download — so a change applies to the next operation without a restart.
// S3 clients are the one cached artifact: SetTuning drops the client
// cache so a new retry count applies immediately too.
package api

import (
	"fmt"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// GetTuning returns the persisted engine tuning (fully resolved — never
// zero fields).
func (a *App) GetTuning() appsettings.Tuning {
	return appsettings.Load()
}

// SetTuning persists the engine tuning. Any value <= 0 means "use the
// default for this field" (the Settings dialog's Default/Auto options);
// everything else is clamped into range. Returns the stored truth.
func (a *App) SetTuning(listingMs, compareMs, retries, partMiB, conc, stallMs int) (appsettings.Tuning, error) {
	t := appsettings.Tuning{
		ListingTimeoutMS: listingMs,
		CompareTimeoutMS: compareMs,
		RetryAttempts:    retries,
		PartSizeMiB:      partMiB,
		PartConcurrency:  conc,
		StallAfterMS:     stallMs,
	}
	if err := appsettings.Save(t); err != nil {
		return appsettings.Load(), err
	}
	stored := appsettings.Load()
	// Cached S3 clients carry the old retryer; drop them so the new count
	// applies to the next request (they rebuild on demand).
	a.mu.Lock()
	a.clients = map[string]*s3client.Client{}
	a.mu.Unlock()
	// Jobs already running keep the stall threshold they started with;
	// new jobs pick the stored one up in add().
	a.jobs.setStallAfter(stored.StallAfter())
	a.emitLog(LogInfo, "settings", tuningLogLine(stored))
	return stored, nil
}

// tuningLogLine renders the one-line summary the log drawer (and the live
// GUI walk) sees on every tuning change:
//
//	engine tuning: listing timeout 30s · compare 5m0s · S3 attempts 3 ·
//	8 MiB parts × 4 · stall 10s
func tuningLogLine(t appsettings.Tuning) string {
	part := "auto parts"
	if t.PartSizeBytes() > 0 {
		part = fmt.Sprintf("%d MiB parts", t.PartSizeMiB)
	}
	conc := "auto"
	if t.PartConcurrency > 0 {
		conc = fmt.Sprintf("× %d", t.PartConcurrency)
	}
	return fmt.Sprintf("engine tuning: listing timeout %s · compare %s · S3 attempts %d · %s %s · stall %s",
		t.ListingTimeout(), t.CompareTimeout(), t.RetryAttempts, part, conc, t.StallAfter())
}

// tuning is the single read point for consumers.
func (a *App) tuning() appsettings.Tuning { return appsettings.Load() }

// s3Opts builds the client options every GUI-side S3 client uses: no
// whole-request HTTP deadline (quick ops are bounded by quickCtx's
// context, transfers by per-job cancellation) plus the configured retryer.
func (a *App) s3Opts() s3client.Options {
	return s3client.Options{Timeout: -1, RetryAttempts: a.tuning().RetryAttempts}
}

// partTunables returns the multipart part size (bytes; 0 = SDK default)
// and parts-in-flight count (0 = SDK default) for transfer option structs.
func (a *App) partTunables() (int64, int) {
	t := a.tuning()
	return t.PartSizeBytes(), t.PartConcurrency
}

// quickBudget is the context budget for quick operations.
func (a *App) quickBudget() time.Duration { return a.tuning().ListingTimeout() }

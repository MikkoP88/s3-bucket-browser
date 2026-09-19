// Package appsettings persists the engine-tuning preference shown in the
// Settings dialog (appsettings.json in the config dir, written by the GUI
// via App.SetTuning). Every value is stored resolved — Load always returns
// a fully valid Tuning — and read per call (no cache) so live changes and
// tests with isolated config dirs take effect immediately, exactly like
// eventlog's log-file preference.
package appsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// Tuning is the persisted engine tuning. The zero-value fields mean
// "unset"; Load overlays Default() with the file's valid values and clamps
// everything into range, so consumers can trust the numbers as-is:
//
//	ListingTimeoutMS  bounds quick ops (listings, stat, presign, source
//	                  tests) and the listing-stream watchdog
//	CompareTimeoutMS  bounds one deep pane-to-pane compare walk
//	RetryAttempts     SDK retry attempts per S3 request (retryer max)
//	PartSizeMiB       multipart part size; 0 = SDK default (5 MiB)
//	PartConcurrency   parts in flight per file; 0 = SDK default (5)
//	StallAfterMS      how long a known-size in-flight transfer may sit
//	                  without byte progress before the row flags Stalled
type Tuning struct {
	ListingTimeoutMS int `json:"listingTimeoutMs"`
	CompareTimeoutMS int `json:"compareTimeoutMs"`
	RetryAttempts    int `json:"retryAttempts"`
	PartSizeMiB      int `json:"partSizeMiB"`
	PartConcurrency  int `json:"partConcurrency"`
	StallAfterMS     int `json:"stallAfterMs"`
}

// Defaults — the values a fresh install runs with. They are also the
// fallback for any field that arrives unset, out of range or unreadable,
// so a torn settings file can never take the engine down.
func Default() Tuning {
	return Tuning{
		ListingTimeoutMS: 30_000,
		CompareTimeoutMS: 300_000,
		RetryAttempts:    3,
		PartSizeMiB:      0, // auto (SDK 5 MiB)
		PartConcurrency:  0, // auto (SDK 5)
		StallAfterMS:     10_000,
	}
}

// Ranges the GUI and SetTuning clamp to. Deliberately generous: the steps
// the Settings dialog offers are a curated subset; anything inside the
// range is still honored (a hand-edited file, a future finer UI). The
// listing floor is low so tests (and daring users) can shrink the
// watchdog hard — the reset-on-every-page semantics keep even 100ms
// viable against a fast local endpoint.
const (
	listingMinMS, listingMaxMS = 100, 600_000      // 0.1 s .. 10 min
	compareMinMS, compareMaxMS = 10_000, 3_600_000 // 10 s .. 60 min
	retryMin, retryMax         = 1, 16
	partMaxMiB                 = 64             // min is 0 = auto
	concMax                    = 64             // min is 0 = auto
	stallMinMS, stallMaxMS     = 2_000, 300_000 // 2 s .. 5 min
)

// normalize overlays def with t's valid fields, clamped.
func normalize(t, def Tuning) Tuning {
	if v := t.ListingTimeoutMS; v > 0 {
		def.ListingTimeoutMS = clamp(v, listingMinMS, listingMaxMS)
	}
	if v := t.CompareTimeoutMS; v > 0 {
		def.CompareTimeoutMS = clamp(v, compareMinMS, compareMaxMS)
	}
	if v := t.RetryAttempts; v > 0 {
		def.RetryAttempts = clamp(v, retryMin, retryMax)
	}
	if v := t.PartSizeMiB; v > 0 {
		def.PartSizeMiB = clamp(v, 1, partMaxMiB)
	}
	if v := t.PartConcurrency; v > 0 {
		def.PartConcurrency = clamp(v, 1, concMax)
	}
	if v := t.StallAfterMS; v > 0 {
		def.StallAfterMS = clamp(v, stallMinMS, stallMaxMS)
	}
	return def
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ListingTimeout / CompareTimeout / StallAfter return the durations
// callers budget with — the conversions live here so every consumer
// resolves them the same way.
func (t Tuning) ListingTimeout() time.Duration {
	return time.Duration(t.ListingTimeoutMS) * time.Millisecond
}
func (t Tuning) CompareTimeout() time.Duration {
	return time.Duration(t.CompareTimeoutMS) * time.Millisecond
}
func (t Tuning) StallAfter() time.Duration { return time.Duration(t.StallAfterMS) * time.Millisecond }

// PartSizeBytes returns the multipart part size in bytes (0 = SDK default).
func (t Tuning) PartSizeBytes() int64 {
	if t.PartSizeMiB <= 0 {
		return 0
	}
	return int64(t.PartSizeMiB) * 1024 * 1024
}

func path() (string, error) {
	dir, err := profile.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "appsettings.json"), nil
}

// Load reads the tuning; Default() is returned when the file is missing
// or unreadable — the engine must keep running on a torn settings file.
// Every field is clamped into range, so a hand-edited file cannot ask for
// a 0 ms listing timeout or a terabyte part.
func Load() Tuning {
	def := Default()
	p, err := path()
	if err != nil {
		return def
	}
	var t Tuning
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &t) != nil {
		return def
	}
	return normalize(t, def)
}

// Save persists the tuning (0600, like profiles.json).
func Save(t Tuning) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(normalize(t, Default()), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

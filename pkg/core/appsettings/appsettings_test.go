package appsettings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("S3B_CONFIG", dir)
	return dir
}

func TestLoadDefaultsWhenFileMissing(t *testing.T) {
	isolate(t)
	got := Load()
	want := Default()
	if got != want {
		t.Fatalf("Load() = %+v, want defaults %+v", got, want)
	}
	if want.ListingTimeout() != 30*time.Second || want.CompareTimeout() != 5*time.Minute || want.StallAfter() != 10*time.Second {
		t.Fatalf("defaults changed: %+v", want)
	}
	if want.PartSizeBytes() != 0 || want.PartConcurrency != 0 {
		t.Fatalf("part tunables must default to auto: %+v", want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)
	want := Tuning{ListingTimeoutMS: 60_000, CompareTimeoutMS: 900_000, RetryAttempts: 5,
		PartSizeMiB: 8, PartConcurrency: 4, StallAfterMS: 30_000}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
	if got.PartSizeBytes() != 8*1024*1024 {
		t.Fatalf("PartSizeBytes() = %d, want 8 MiB", got.PartSizeBytes())
	}
}

func TestTornFileFallsBackToDefaults(t *testing.T) {
	dir := isolate(t)
	if err := os.WriteFile(filepath.Join(dir, "appsettings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got != Default() {
		t.Fatalf("torn file: Load() = %+v, want defaults", got)
	}
}

func TestNormalizeClampsAndFills(t *testing.T) {
	cases := []struct {
		in, want Tuning
	}{
		// zero fields fall back to defaults
		{Tuning{}, Default()},
		// out-of-range values clamp
		{Tuning{ListingTimeoutMS: 5, CompareTimeoutMS: 99_999_999, RetryAttempts: 500,
			PartSizeMiB: 4096, PartConcurrency: -7, StallAfterMS: 1},
			Tuning{ListingTimeoutMS: 100, CompareTimeoutMS: 3_600_000, RetryAttempts: 16,
				PartSizeMiB: 64, PartConcurrency: 0, StallAfterMS: 2_000}},
		// in-range values pass through
		{Tuning{ListingTimeoutMS: 120_000, RetryAttempts: 1, PartSizeMiB: 5, PartConcurrency: 1},
			Tuning{ListingTimeoutMS: 120_000, CompareTimeoutMS: 300_000, RetryAttempts: 1,
				PartSizeMiB: 5, PartConcurrency: 1, StallAfterMS: 10_000}},
	}
	for _, c := range cases {
		if got := normalize(c.in, Default()); got != c.want {
			t.Errorf("normalize(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

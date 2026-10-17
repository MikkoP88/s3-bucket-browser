// uploads_cmd.go implements `s3b bucket uploads …` — the operational face
// of the incomplete-multipart-upload lifecycle: list what a killed or
// canceled large transfer left behind, abort it to reclaim the parts (the
// prevention face is the lifecycle rule's AbortIncompleteMultipartUpload,
// served by `s3b bucket lifecycle put`).
package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/doctor"
	"github.com/spf13/cobra"
)

func bucketUploadsCmd() *cobra.Command {
	var abort, force bool
	var id, olderThan string
	cmd := &cobra.Command{
		Use:   "uploads s3://bucket [--abort] [--id ID | --older-than DUR] [--force]",
		Short: "List (or abort) incomplete multipart uploads",
		Long: "Lists the bucket's incomplete multipart uploads — in-progress uploads whose transfer\n" +
			"died mid-flight (process kill, cancel, link failure). The parts already streamed\n" +
			"stay on the provider, invisible to object listings, until aborted or expired by a\n" +
			"lifecycle rule. With --abort the matched uploads are aborted (releasing their parts);\n" +
			"aborting never touches completed objects. --older-than accepts e.g. 90m, 12h, 7d.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if abort && !force {
				return usageErr("--abort releases the parts of every matched upload — pass --force to act")
			}
			var minAge time.Duration
			if olderThan != "" {
				d, err := parseAge(olderThan)
				if err != nil {
					return usageErr("--older-than: %v", err)
				}
				minAge = d
			}
			if id != "" && olderThan != "" {
				return usageErr("--id and --older-than are mutually exclusive filters")
			}
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			ups, err := adminops.ListUploads(cmd.Context(), c.S3, bucket)
			if err != nil {
				return opErr(err)
			}
			// Filters select from the live listing (--id resolves its key
			// there — the abort wire takes bucket+key+uploadId together).
			var selected []adminops.IncompleteUpload
			for _, u := range ups {
				switch {
				case id != "" && u.UploadID != id:
					continue
				case olderThan != "" && time.Since(u.Initiated) < minAge:
					continue
				}
				selected = append(selected, u)
			}
			if id != "" && len(selected) == 0 {
				return opErr(fmt.Errorf("no incomplete upload with id %s in s3://%s", id, bucket))
			}
			if !abort {
				if flagJSON {
					if selected == nil {
						selected = []adminops.IncompleteUpload{} // the --json law: an array, never null
					}
					return printJSON(selected)
				}
				if len(selected) == 0 && id == "" && olderThan == "" {
					col.dim.Println("no incomplete multipart uploads")
					return nil
				}
				printUploads(selected)
				return nil
			}
			var n, failed int
			var errs []string
			for _, u := range selected {
				if err := adminops.AbortUpload(cmd.Context(), c.S3, bucket, u.Key, u.UploadID); err != nil {
					failed++
					errs = append(errs, fmt.Sprintf("%s: %v", u.Key, err))
					continue
				}
				n++
			}
			if flagJSON {
				return printJSON(map[string]any{"aborted": n, "failed": failed, "errors": errs})
			}
			for _, e := range errs {
				col.warn.Printf("abort failed — %s\n", e)
			}
			if n > 0 {
				col.ok.Printf("aborted %d incomplete upload(s) on s3://%s — parts released\n", n, bucket)
			}
			if n == 0 && failed == 0 {
				col.dim.Println("no matching incomplete uploads to abort")
			}
			if failed > 0 {
				return opErr(fmt.Errorf("%d abort(s) failed", failed))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&abort, "abort", false, "abort the matched uploads (releases their parts)")
	f.BoolVar(&force, "force", false, "required with --abort")
	f.StringVar(&id, "id", "", "select one upload by its upload id")
	f.StringVar(&olderThan, "older-than", "", "select uploads initiated at least this long ago (90m, 12h, 7d)")
	return cmd
}

// printUploads renders the listing: key, short id, age and the RFC3339
// initiation the machines can sort on.
func printUploads(ups []adminops.IncompleteUpload) {
	fmt.Printf("%-44s %-14s %-8s %s\n", "KEY", "UPLOAD ID", "AGE", "INITIATED")
	for _, u := range ups {
		fmt.Printf("%-44s %-14s %-8s %s\n",
			truncCell(u.Key, 44),
			truncCell(u.UploadID, 14),
			doctor.HumanAge(time.Since(u.Initiated)),
			u.Initiated.Local().Format(time.RFC3339))
	}
	fmt.Printf("%d incomplete multipart upload(s)\n", len(ups))
}

// truncCell parks a too-long cell behind an ellipsis so the table's
// columns stay aligned against hostile keys.
func truncCell(s string, w int) string {
	if len(s) <= w {
		return s
	}
	return s[:w-1] + "…"
}

// parseAge accepts Go durations plus the "7d" day form people write for
// storage ages. A negative age is nonsense for a filter, so it refuses.
func parseAge(s string) (time.Duration, error) {
	if d, dErr := strconv.Atoi(strings.TrimSuffix(s, "d")); dErr == nil && strings.HasSuffix(s, "d") {
		if d < 0 {
			return 0, fmt.Errorf("%q: an age cannot be negative", s)
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err == nil && d < 0 {
		return 0, fmt.Errorf("%q: an age cannot be negative", s)
	}
	return d, err
}

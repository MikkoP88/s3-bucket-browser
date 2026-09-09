// lock_cmd.go implements `s3b lock` (M5): per-object retention and legal
// hold on versioned, object-lock-enabled buckets. Retention in COMPLIANCE
// mode cannot be shortened or removed — only GOVERNANCE can be cleared.
package cli

import (
	"fmt"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/spf13/cobra"
)

func lockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Object lock: retention and legal hold per object version",
		Long: "Works on buckets with object lock enabled (see `s3b bucket lock`).\n" +
			"retention: no flags shows current state; --mode GOVERNANCE|COMPLIANCE + --until sets it;\n" +
			"--clear removes GOVERNANCE retention. legalhold: --on / --off toggles, no flag shows.",
	}
	cmd.AddCommand(lockRetentionCmd(), lockLegalHoldCmd())
	return cmd
}

func lockRetentionCmd() *cobra.Command {
	var versionID, mode, until string
	var clear, bypass bool
	cmd := &cobra.Command{
		Use:   "retention s3://bucket/key [--version-id ID] [--mode M --until T | --clear]",
		Short: "Show, set or clear object retention",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, key, err := versionsTarget(args[0])
			if err != nil {
				return err
			}
			if clear {
				if mode != "" || until != "" {
					return usageErr("--clear cannot be combined with --mode/--until")
				}
				if err := adminops.DeleteObjectRetention(cmd.Context(), c.S3, bucket, key, versionID, bypass); err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(map[string]any{"retentionCleared": bucket + "/" + key})
				}
				col.ok.Printf("retention cleared on s3://%s/%s\n", bucket, key)
				return nil
			}
			if mode == "" && until == "" {
				lock, err := adminops.GetObjectLock(cmd.Context(), c.S3, bucket, key, versionID)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(lock)
				}
				if lock.Mode == "" {
					col.dim.Println("no retention configured")
					return nil
				}
				fmt.Printf("mode:         %s\n", lock.Mode)
				if lock.RetainUntil != nil {
					fmt.Printf("retain until: %s\n", lock.RetainUntil.Local().Format(time.RFC3339))
				}
				return nil
			}
			if mode != "GOVERNANCE" && mode != "COMPLIANCE" {
				return usageErr("--mode must be GOVERNANCE or COMPLIANCE")
			}
			when, err := parseRetentionUntil(until)
			if err != nil {
				return usageErr("%v", err)
			}
			if err := adminops.PutObjectRetention(cmd.Context(), c.S3, bucket, key, versionID, mode, when, bypass); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"retention": mode, "until": when.Format(time.RFC3339)})
			}
			col.ok.Printf("retention %s until %s set on s3://%s/%s\n",
				mode, when.Local().Format(time.RFC3339), bucket, key)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&versionID, "version-id", "", "target a specific version (default: current)")
	f.StringVar(&mode, "mode", "", "GOVERNANCE or COMPLIANCE")
	f.StringVar(&until, "until", "", "RFC3339 timestamp or +Nd/+Nh relative to now")
	f.BoolVar(&clear, "clear", false, "remove GOVERNANCE retention")
	f.BoolVar(&bypass, "bypass-governance", false, "send x-amz-bypass-governance-retention (required to shorten/clear GOVERNANCE; the server still checks permissions)")
	return cmd
}

func lockLegalHoldCmd() *cobra.Command {
	var versionID string
	var on, off bool
	cmd := &cobra.Command{
		Use:   "legalhold s3://bucket/key [--version-id ID] [--on|--off]",
		Short: "Show or toggle the legal hold of an object version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, key, err := versionsTarget(args[0])
			if err != nil {
				return err
			}
			if on == off {
				lock, err := adminops.GetObjectLock(cmd.Context(), c.S3, bucket, key, versionID)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(map[string]any{"legalHold": lock.LegalHold})
				}
				state := lock.LegalHold
				if state == "" {
					state = "off (never configured)"
				}
				fmt.Printf("legal hold: %s\n", state)
				return nil
			}
			if err := adminops.PutObjectLegalHold(cmd.Context(), c.S3, bucket, key, versionID, on); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"legalHold": map[bool]string{true: "ON", false: "OFF"}[on]})
			}
			col.ok.Printf("legal hold %s on s3://%s/%s\n", map[bool]string{true: "ON", false: "OFF"}[on], bucket, key)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&versionID, "version-id", "", "target a specific version (default: current)")
	f.BoolVar(&on, "on", false, "enable the hold")
	f.BoolVar(&off, "off", false, "disable the hold")
	return cmd
}

// parseRetentionUntil accepts RFC3339 or +Nd/+Nh relative to now.
func parseRetentionUntil(s string) (time.Time, error) {
	now := time.Now()
	if len(s) > 1 && s[0] == '+' {
		if d, err := parseIntDuration(s[1:]); err == nil {
			return now.Add(d), nil
		}
		return time.Time{}, fmt.Errorf("invalid --until %q (use RFC3339 or +Nd/+Nh)", s)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --until %q (use RFC3339 or +Nd/+Nh)", s)
	}
	return t, nil
}

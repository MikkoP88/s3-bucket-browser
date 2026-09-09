// versions_cmd.go implements `s3b versions …` (M4): object version
// timelines, restore-as-latest, undo-delete, permanent version destruction
// and bulk purges. Deleting a specific version is safety ladder L3 —
// callers arrive here only through explicit flags, never by accident.
package cli

import (
	"fmt"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/spf13/cobra"
)

func versionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions",
		Short: "Object version management (versioned buckets)",
		Long: "Work with object versions: list a timeline, restore an old version as latest,\n" +
			"undo an accidental delete (remove a delete marker), destroy versions permanently\n" +
			"and purge noncurrent versions / delete markers in bulk.",
	}
	cmd.AddCommand(
		versionsLsCmd(),
		versionsRestoreCmd(),
		versionsUndoCmd(),
		versionsRmCmd(),
		versionsPurgeCmd(),
		versionsStatCmd(),
	)
	return cmd
}

// versionsTarget parses an object-key argument shared by the subcommands.
func versionsTarget(arg string) (bucket, key string, err error) {
	u, err := parseS3URI(arg)
	if err != nil {
		return "", "", err
	}
	if !u.HasPrefix || u.IsPrefix {
		return u.Bucket, "", usageErr("this command takes an object key, not a bucket or folder: s3://%s/%s", u.Bucket, u.Key)
	}
	return u.Bucket, u.Key, nil
}

func versionsLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls s3://bucket/key",
		Short: "List the version timeline of an object, newest first",
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
			vers, err := versioning.ListForObject(cmd.Context(), c.S3, bucket, key)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(vers)
			}
			if len(vers) == 0 {
				col.dim.Println("(no versions — versioning is off or the object never existed)")
				return nil
			}
			for _, v := range vers {
				mark := " "
				if v.IsLatest {
					mark = "*"
				}
				when := ""
				if v.LastModified != nil {
					when = v.LastModified.Local().Format(time.DateTime)
				}
				if v.IsDeleteMarker {
					col.warn.Printf("%s delete-marker  %s  %s\n", mark, when, v.VersionID)
					continue
				}
				fmt.Printf("%s %10s  %s  %-9s %s\n",
					mark, humanSize(v.Size), when, v.StorageClass, v.VersionID)
			}
			col.dim.Println("(* = current version)")
			return nil
		},
	}
}

func versionsRestoreCmd() *cobra.Command {
	var versionID string
	cmd := &cobra.Command{
		Use:   "restore s3://bucket/key --version-id ID",
		Short: "Restore an old version as the current one (server-side copy)",
		Long:  "The previous current version stays in the timeline — nothing is lost.",
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
			if versionID == "" {
				return usageErr("restore needs --version-id (see `s3b versions ls`)")
			}
			if err := versioning.RestoreVersion(cmd.Context(), c.S3, bucket, key, versionID); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]string{"restored": bucket + "/" + key, "versionId": versionID})
			}
			col.ok.Printf("restored %s as current version of s3://%s/%s\n", versionID, bucket, key)
			return nil
		},
	}
	cmd.Flags().StringVar(&versionID, "version-id", "", "version to restore (required)")
	return cmd
}

func versionsUndoCmd() *cobra.Command {
	var versionID string
	cmd := &cobra.Command{
		Use:   "undo s3://bucket/key --version-id ID",
		Short: "Undo a delete: remove a delete marker so the object reappears",
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
			if versionID == "" {
				return usageErr("undo needs --version-id of the delete marker (see `s3b versions ls`)")
			}
			if err := versioning.RemoveDeleteMarker(cmd.Context(), c.S3, bucket, key, versionID); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]string{"undeleted": bucket + "/" + key, "marker": versionID})
			}
			col.ok.Printf("removed delete marker — s3://%s/%s is back\n", bucket, key)
			return nil
		},
	}
	cmd.Flags().StringVar(&versionID, "version-id", "", "delete marker to remove (required)")
	return cmd
}

func versionsRmCmd() *cobra.Command {
	var versionID string
	var all bool
	cmd := &cobra.Command{
		Use:   "rm s3://bucket/key (--version-id ID | --all)",
		Short: "Destroy version(s) permanently — unrecoverable, even from history (L3)",
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
			if versionID == "" && !all {
				return usageErr("versions rm needs --version-id or --all")
			}
			if versionID != "" && all {
				return usageErr("pass either --version-id or --all, not both")
			}
			if versionID != "" {
				if err := versioning.DeleteVersion(cmd.Context(), c.S3, bucket, key, versionID); err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(map[string]any{"destroyed": 1})
				}
				col.ok.Printf("destroyed version %s of s3://%s/%s\n", versionID, bucket, key)
				return nil
			}
			res, err := versioning.DeleteAllVersions(cmd.Context(), c.S3, bucket, key)
			if err != nil {
				return opErr(err)
			}
			reportDelete(res)
			if flagJSON {
				return printJSON(res)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&versionID, "version-id", "", "destroy exactly this version")
	f.BoolVar(&all, "all", false, "destroy every version and delete marker of the key")
	return cmd
}

func versionsPurgeCmd() *cobra.Command {
	var mode string
	var dryRun, force bool
	cmd := &cobra.Command{
		Use:   "purge s3://bucket[/prefix]",
		Short: "Bulk-purge versions under a prefix (noncurrent versions or delete markers)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			var m versioning.PurgeMode
			switch mode {
			case "noncurrent":
				m = versioning.PurgeNoncurrent
			case "markers":
				m = versioning.PurgeMarkers
			case "all":
				m = versioning.PurgeAll
			default:
				return usageErr("invalid --mode %q (use noncurrent, markers or all)", mode)
			}
			prefix := ""
			if u.HasPrefix {
				prefix = dirPrefix(u)
			}
			n, err := versioning.CountPurge(cmd.Context(), c.S3, u.Bucket, prefix, m)
			if err != nil {
				return opErr(err)
			}
			if dryRun {
				if flagJSON {
					return printJSON(map[string]any{"wouldPurge": n, "mode": m})
				}
				fmt.Printf("would purge %d %s version(s) under s3://%s/%s\n", n, m, u.Bucket, prefix)
				if n > rmForceThreshold && !force {
					col.dim.Printf("note: running it requires --force (> %d)\n", rmForceThreshold)
				}
				return nil
			}
			if n > rmForceThreshold && !force {
				return opErr(fmt.Errorf(
					"would purge %d version(s) — pass --force to proceed", n))
			}
			res, err := versioning.Purge(cmd.Context(), c.S3, u.Bucket, prefix, m)
			if err != nil {
				return opErr(err)
			}
			reportDelete(res)
			if flagJSON {
				return printJSON(res)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&mode, "mode", "noncurrent", "what to purge: noncurrent | markers | all")
	f.BoolVar(&dryRun, "dry-run", false, "count only, purge nothing")
	f.BoolVar(&force, "force", false,
		fmt.Sprintf("allow purging more than %d versions (L1 safety gate)", rmForceThreshold))
	return cmd
}

func versionsStatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stat s3://bucket[/prefix]",
		Short: "Version statistics: current, noncurrent, delete markers, noncurrent bytes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			prefix := ""
			if u.HasPrefix {
				prefix = dirPrefix(u)
			}
			st, err := versioning.CollectStats(cmd.Context(), c.S3, u.Bucket, prefix)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(st)
			}
			fmt.Printf("current objects:    %d\n", st.CurrentObjects)
			fmt.Printf("total versions:     %d\n", st.Versions)
			fmt.Printf("noncurrent:         %d (%s)\n", st.Noncurrent, humanSize(st.NoncurrentBytes))
			fmt.Printf("delete markers:     %d\n", st.DeleteMarkers)
			return nil
		},
	}
}

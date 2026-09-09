// find_cmd.go implements `s3b find` (M5): cancelable deep search across a
// bucket/prefix by name glob, size, age or storage class. Results stream
// as they are found; the summary line goes to stderr so stdout stays
// parseable.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/search"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/spf13/cobra"
)

func findCmd() *cobra.Command {
	var pattern, larger, smaller, older, newer, class string
	var limit int
	cmd := &cobra.Command{
		Use:   "find s3://bucket[/prefix]",
		Short: "Deep search objects by name, size, age or storage class",
		Long: "Streams every object under the prefix and prints the ones matching all filters.\n" +
			"--name is a substring, or a glob when it contains * or ? (matched against the full key,\n" +
			"so 'backup*' also matches nested paths). Sizes accept 10MB / 1.5GB forms; ages 30d / 24h.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			f := search.Filter{Pattern: pattern, Class: class, Limit: limit}
			if larger != "" {
				if f.LargerThan, err = search.ParseSize(larger); err != nil {
					return usageErr("--larger: %v", err)
				}
			}
			if smaller != "" {
				if f.SmallerThan, err = search.ParseSize(smaller); err != nil {
					return usageErr("--smaller: %v", err)
				}
			}
			if older != "" {
				if f.OlderThan, err = parseIntDuration(older); err != nil {
					return usageErr("--older: %v", err)
				}
			}
			if newer != "" {
				if f.NewerThan, err = parseIntDuration(newer); err != nil {
					return usageErr("--newer: %v", err)
				}
			}
			prefix := ""
			if u.HasPrefix {
				prefix = dirPrefix(u)
			}
			var matches []search.Result
			stats, err := search.Run(cmd.Context(), c.S3, u.Bucket, prefix, f, func(r search.Result) error {
				if flagJSON {
					matches = append(matches, r)
					return nil
				}
				printEntry(r.Entry)
				return nil
			})
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(matches)
			}
			fmt.Fprintf(os.Stderr, "%d match(es) among %d scanned under s3://%s/%s\n",
				stats.Matched, stats.Scanned, u.Bucket, prefix)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&pattern, "name", "n", "", "substring or glob to match against the key")
	f.StringVar(&larger, "larger", "", "match objects larger than this (e.g. 10MB)")
	f.StringVar(&smaller, "smaller", "", "match objects smaller than this (e.g. 500KB)")
	f.StringVar(&older, "older", "", "last modified longer ago than this (e.g. 30d)")
	f.StringVar(&newer, "newer", "", "last modified within this (e.g. 24h)")
	f.StringVar(&class, "class", "", "exact storage class (e.g. GLACIER)")
	f.IntVar(&limit, "limit", 0, "stop after N matches (0 = unlimited)")
	return cmd
}

// scCmd implements `s3b sc` (M5): server-side storage-class conversion.
func scCmd() *cobra.Command {
	var recursive, force, dryRun bool
	cmd := &cobra.Command{
		Use:   "sc s3://bucket[/key] CLASS",
		Short: "Convert objects to another storage class (server-side copy)",
		Long: "Rewrites the storage class via a self-copy. GLACIER/DEEP_ARCHIVE objects stay frozen:\n" +
			"reading them still needs an explicit restore. Converting more than " +
			fmt.Sprint(rmForceThreshold) + " objects in one go requires --force.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			class := args[1]
			if !transfer.ValidStorageClass(class) {
				return usageErr("unknown storage class %q (use one of: %s)",
					class, strings.Join(transfer.ValidStorageClasses, ", "))
			}
			if !u.HasPrefix {
				return usageErr("pass an object key or a folder prefix: s3://bucket/key or s3://bucket/prefix/")
			}
			if !u.IsPrefix {
				// single object
				if err := transfer.ConvertStorageClass(cmd.Context(), c.S3, u.Bucket, u.Key, "", class); err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(map[string]any{"converted": 1, "class": strings.ToUpper(class)})
				}
				col.ok.Printf("converted s3://%s/%s to %s\n", u.Bucket, u.Key, strings.ToUpper(class))
				return nil
			}
			if !recursive {
				return usageErr("converting a prefix needs --recursive")
			}
			keys, err := transfer.CollectPrefixKeys(cmd.Context(), c.S3, u.Bucket, dirPrefix(u))
			if err != nil {
				return opErr(err)
			}
			if len(keys) > rmForceThreshold && !force {
				return opErr(fmt.Errorf("would convert %d object(s) — pass --force to proceed", len(keys)))
			}
			if dryRun {
				if flagJSON {
					return printJSON(map[string]any{"wouldConvert": len(keys), "class": strings.ToUpper(class)})
				}
				fmt.Printf("would convert %d object(s) to %s\n", len(keys), strings.ToUpper(class))
				return nil
			}
			done := 0
			for _, k := range keys {
				if err := transfer.ConvertStorageClass(cmd.Context(), c.S3, u.Bucket, k, "", class); err != nil {
					if done > 0 {
						fmt.Fprintf(os.Stderr, "converted %d before failing\n", done)
					}
					return opErr(fmt.Errorf("key %s: %w", k, err))
				}
				done++
				if !flagJSON && flagVerbose {
					fmt.Printf("converted %s\n", k)
				}
			}
			if flagJSON {
				return printJSON(map[string]any{"converted": done, "class": strings.ToUpper(class)})
			}
			col.ok.Printf("converted %d object(s) to %s\n", done, strings.ToUpper(class))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&recursive, "recursive", "r", false, "convert every object under the prefix")
	f.BoolVar(&force, "force", false, fmt.Sprintf("allow converting more than %d objects", rmForceThreshold))
	f.BoolVar(&dryRun, "dry-run", false, "list what would be converted, change nothing")
	return cmd
}

// find_cmd.go implements `s3b find` (M5): cancelable search across a
// bucket/prefix by name glob, size, age, kind or storage class. Results stream
// as they are found; the summary line goes to stderr so stdout stays
// parseable. Source URIs (NAME://dir over any saved non-S3 source) ride the
// same filter pipeline over the source's engine — the GUI search's remote
// matrix, on the terminal's face.
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
	var pattern, larger, smaller, older, newer, class, kind, ext, path string
	var limit int
	cmd := &cobra.Command{
		Use:   "find s3://bucket[/prefix] | NAME://dir",
		Short: "Search objects by name, size, age, kind or storage class",
		Long: "Streams every object under the prefix and prints the ones matching all filters.\n" +
			"--name is a substring, or a glob when it contains * or ? (matched against the full key,\n" +
			"so 'backup*' also matches nested paths). --ext filters by name extension and --path by a\n" +
			"substring of the parent directory (both case-insensitive). Sizes accept 10MB / 1.5GB forms;\n" +
			"ages 30d / 24h; --kind file|dir keeps only files or folders.\n" +
			"Source URIs (NAME://dir over any saved non-S3 source) work the same way — every\n" +
			"entry under the path, depth-first, through the source's own engine; --class is\n" +
			"S3-only (remote trees carry no storage class).",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSourceURIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if kind != "" && !strings.EqualFold(kind, "file") && !strings.EqualFold(kind, "dir") {
				return usageErr("--kind must be file or dir")
			}
			f := search.Filter{Pattern: pattern, Kind: kind, Ext: ext, Path: path, Class: class, Limit: limit}
			if larger != "" {
				v, err := search.ParseSize(larger)
				if err != nil {
					return usageErr("--larger: %v", err)
				}
				f.LargerThan = v
			}
			if smaller != "" {
				v, err := search.ParseSize(smaller)
				if err != nil {
					return usageErr("--smaller: %v", err)
				}
				f.SmallerThan = v
			}
			if older != "" {
				v, err := parseIntDuration(older)
				if err != nil {
					return usageErr("--older: %v", err)
				}
				f.OlderThan = v
			}
			if newer != "" {
				v, err := parseIntDuration(newer)
				if err != nil {
					return usageErr("--newer: %v", err)
				}
				f.NewerThan = v
			}
			// Source URIs ride the same law as ls/tree/du: any saved
			// non-S3 source, deep-searched over its engine. A --class
			// filter over a source is a usage error up front — the GUI
			// search refuses the same mix with the same words.
			if r, err := dialSourceURI(cmd.Context(), args[0]); err != nil {
				return err
			} else if r != nil {
				defer r.Close()
				if class != "" {
					return usageErr("--class applies to S3 objects only — drop it when searching %s",
						uri(r.src.Name, r.path))
				}
				var matches []search.Result
				stats, rerr := search.RunRemote(cmd.Context(), r.fs, r.path, f, func(res search.Result) error {
					res.Source = r.src.Name
					// the hit speaks its path relative to the searched
					// root — the S3 leg's own grammar (FromObject trims
					// the searched prefix the same way)
					res.Name = strings.TrimPrefix(strings.TrimPrefix(res.Key, strings.TrimSuffix(r.path, "/")), "/")
					if flagJSON {
						matches = append(matches, res)
						return nil
					}
					printEntry(res.Entry)
					return nil
				})
				if rerr != nil {
					return opErr(rerr)
				}
				if flagJSON {
					return printJSON(matches)
				}
				fmt.Fprintf(os.Stderr, "%d match(es) among %d scanned under %s\n",
					stats.Matched, stats.Scanned, uri(r.src.Name, r.path))
				return nil
			}
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
	f.StringVar(&ext, "ext", "", "comma-separated name extensions (e.g. pdf,jpg; dot optional)")
	f.StringVar(&path, "path", "", "substring the parent directory must contain (e.g. docs)")
	f.StringVar(&larger, "larger", "", "match objects larger than this (e.g. 10MB)")
	f.StringVar(&smaller, "smaller", "", "match objects smaller than this (e.g. 500KB)")
	f.StringVar(&older, "older", "", "last modified longer ago than this (e.g. 30d)")
	f.StringVar(&newer, "newer", "", "last modified within this (e.g. 24h)")
	f.StringVar(&class, "class", "", "exact storage class (e.g. GLACIER; S3 runs only)")
	f.StringVar(&kind, "kind", "", "match only files or only folders (file|dir)")
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
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeScArgs,
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

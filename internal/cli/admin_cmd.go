// admin_cmd.go implements `s3b bucket …` (M3): the CLI face of the admin
// panel — versioning toggle, policy, CORS, lifecycle, encryption, public
// access block, website hosting and tags. get/put/delete triplets mirror
// the adminops engine 1:1; JSON payloads round-trip the editor structs.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/adminops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/spf13/cobra"
)

func bucketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bucket",
		Short: "Bucket administration (policy, CORS, lifecycle, encryption, PAB, website, tags, lock)",
	}
	cmd.AddCommand(
		bucketInfoCmd(),
		bucketVersioningCmd(),
		bucketPolicyCmd(),
		bucketCORSCmd(),
		bucketLifecycleCmd(),
		bucketEncryptionCmd(),
		bucketPABCmd(),
		bucketWebsiteCmd(),
		bucketTagsCmd(),
		bucketLockCmd(),
	)
	return cmd
}

// bucketLockCmd: `s3b bucket lock s3://b [--enable --mode M --days N]`.
// Object lock is a one-way door on AWS: once enabled it cannot be undone.
func bucketLockCmd() *cobra.Command {
	var enable bool
	var mode string
	var days int32
	cmd := &cobra.Command{
		Use:   "lock s3://bucket [--enable] [--mode GOVERNANCE|COMPLIANCE] [--days N]",
		Short: "Show (or enable) the object-lock configuration",
		Long: "Without flags prints the object-lock configuration.\n" +
			"Object lock can only be ENABLED at bucket creation (`s3b mb --object-lock`) — AWS/MinIO\n" +
			"refuse enabling it on existing buckets; it is permanent and cannot be disabled later.\n" +
			"--enable with --mode/--days sets/updates the default retention rule of a lock-enabled bucket.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			if !enable {
				cfg, err := adminops.GetLockConfig(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(cfg)
				}
				state := "disabled"
				if cfg.Enabled {
					state = "enabled"
				}
				fmt.Printf("object lock: %s\n", state)
				if cfg.Mode != "" || cfg.Days > 0 {
					fmt.Printf("default retention: %s, %d day(s)\n", orDash(cfg.Mode), cfg.Days)
				}
				return nil
			}
			if mode != "" && mode != "GOVERNANCE" && mode != "COMPLIANCE" {
				return usageErr("--mode must be GOVERNANCE or COMPLIANCE")
			}
			if days > 0 && mode == "" {
				return usageErr("--days needs --mode GOVERNANCE or COMPLIANCE")
			}
			if err := adminops.PutLockConfig(cmd.Context(), c.S3, bucket, adminops.LockConfig{
				Enabled: true, Mode: mode, Days: days,
			}); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"objectLock": "enabled", "mode": mode, "days": days})
			}
			col.ok.Printf("object lock enabled on s3://%s (permanent — cannot be disabled)\n", bucket)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&enable, "enable", false, "enable object lock (irreversible)")
	f.StringVar(&mode, "mode", "", "default retention mode (GOVERNANCE or COMPLIANCE)")
	f.Int32Var(&days, "days", 0, "default retention days")
	return cmd
}

// bucketArg parses the common s3://bucket argument (no key allowed).
func bucketArg(arg string) (string, error) {
	u, err := parseS3URI(arg)
	if err != nil {
		return "", err
	}
	if u.HasPrefix {
		return u.Bucket, usageErr("bucket admin commands take a bucket, not a key: s3://%s", u.Bucket)
	}
	return u.Bucket, nil
}

// readPayload reads a JSON payload from a file, or stdin when path is "-".
func readPayload(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	if path == "" {
		return nil, usageErr("missing payload file (or use - for stdin)")
	}
	return os.ReadFile(path)
}

func bucketInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info s3://bucket",
		Short: "Bucket overview: region, versioning, encryption, public access block",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			out := map[string]any{"bucket": bucket}
			var warns []string

			if h, err := bucketHead(ctx, c, bucket); err == nil {
				out["region"] = h
			} else {
				warns = append(warns, "region: "+err.Error())
			}
			if vs, err := versioning.Status(ctx, c.S3, bucket); err == nil {
				if vs == "" {
					vs = "off"
				}
				out["versioning"] = vs
			} else {
				warns = append(warns, "versioning: "+err.Error())
			}
			if e, err := adminops.GetEncryption(ctx, c.S3, bucket); err == nil {
				out["encryption"] = e.Algorithm
			} else {
				warns = append(warns, "encryption: "+err.Error())
			}
			if p, err := adminops.GetPAB(ctx, c.S3, bucket); err == nil {
				out["publicAccessBlock"] = p
			} else {
				warns = append(warns, "public access block: "+err.Error())
			}
			if flagJSON {
				out["warnings"] = warns
				return printJSON(out)
			}
			fmt.Println("bucket:      ", bucket)
			fmt.Println("region:      ", orDash(anyStr(out["region"])))
			fmt.Println("versioning:  ", orDash(anyStr(out["versioning"])))
			fmt.Println("encryption:  ", orDash(anyStr(out["encryption"])))
			if p, ok := out["publicAccessBlock"].(adminops.PABInfo); ok {
				fmt.Printf("public access block: %d of 4 on\n", countTrue(
					p.BlockPublicACLs, p.IgnorePublicACLs, p.BlockPublicPolicy, p.RestrictPublicBuckets))
			}
			for _, w := range warns {
				col.warn.Printf("warning: %s\n", w)
			}
			return nil
		},
	}
}

func bucketVersioningCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versioning s3://bucket on|off",
		Short: "Enable (on) or suspend (off) bucket versioning",
		Long: "Enabling versioning keeps every write as a new version; deletes create delete markers\n" +
			"instead of removing data. \"off\" suspends versioning — existing history is kept.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			status := "Enabled"
			switch args[1] {
			case "on", "enable", "enabled":
			case "off", "suspend", "suspended":
				status = "Suspended"
			default:
				return usageErr("versioning state must be on or off, got %q", args[1])
			}
			if err := versioning.SetStatus(cmd.Context(), c.S3, bucket, status); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]string{"bucket": bucket, "versioning": status})
			}
			col.ok.Printf("versioning %s for s3://%s\n", strings.ToLower(status), bucket)
			return nil
		},
	}
}

func bucketPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy get|put|delete s3://bucket [FILE]",
		Short: "Bucket policy (IAM JSON document)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print the bucket policy (raw JSON) and its safety summary",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				p, err := adminops.GetPolicy(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if p.Raw == "" {
					col.dim.Println("(no policy set)")
					return nil
				}
				if flagJSON {
					return printJSON(p)
				}
				fmt.Println(p.Raw)
				if p.Summary != nil {
					if p.Summary.HasPublicRead || p.Summary.HasPublicWrite {
						col.warn.Printf("warning: policy grants public %s%s\n",
							or("read", p.Summary.HasPublicRead), or(" write", p.Summary.HasPublicWrite))
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "put s3://bucket FILE",
			Short: "Set the bucket policy from a JSON file (\"-\" = stdin)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				raw, err := readPayload(args[1])
				if err != nil {
					return opErr(err)
				}
				if err := adminops.PutPolicy(cmd.Context(), c.S3, bucket, string(raw)); err != nil {
					return opErr(err)
				}
				col.ok.Printf("policy saved for s3://%s\n", bucket)
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Remove the bucket policy",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeletePolicy(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("policy removed from s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

func bucketCORSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cors get|put|delete s3://bucket [FILE]",
		Short: "CORS rules",
		Long: "put takes a JSON array of rules, e.g.\n" +
			`  [{"origins":["https://example.com"],"methods":["GET"],"maxAge":3600}]`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print CORS rules",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				rules, err := adminops.GetCORS(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(rules)
				}
				if len(rules) == 0 {
					col.dim.Println("(no CORS rules)")
					return nil
				}
				for i, r := range rules {
					fmt.Printf("%d. %s: %s", i+1, strings.Join(r.Origins, ", "), strings.Join(r.Methods, ", "))
					if r.MaxAge > 0 {
						fmt.Printf(" (max-age %ds)", r.MaxAge)
					}
					fmt.Println()
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "put s3://bucket FILE",
			Short: "Replace CORS rules from a JSON file (\"-\" = stdin)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				var rules []adminops.CORSRule
				if err := decodePayload(args[1], &rules); err != nil {
					return err
				}
				if err := adminops.PutCORS(cmd.Context(), c.S3, bucket, rules); err != nil {
					return opErr(err)
				}
				col.ok.Printf("CORS saved for s3://%s (%d rule(s))\n", bucket, len(rules))
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Remove all CORS rules",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeleteCORS(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("CORS removed from s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

func bucketLifecycleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lifecycle get|put|delete s3://bucket [FILE]",
		Short: "Lifecycle rules (transition, expiration, noncurrent cleanup)",
		Long: "put takes a JSON array of rules, e.g.\n" +
			`  [{"id":"archive","enabled":true,"prefix":"logs/","transitionDays":90,"transitionClass":"GLACIER"}]`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print lifecycle rules",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				rules, err := adminops.GetLifecycle(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(rules)
				}
				if len(rules) == 0 {
					col.dim.Println("(no lifecycle rules)")
					return nil
				}
				for _, r := range rules {
					state := "disabled"
					if r.Enabled {
						state = "enabled"
					}
					fmt.Printf("%s (%s, prefix %q):", r.ID, state, r.Prefix)
					if r.TransitionDays > 0 {
						fmt.Printf(" transition %dd -> %s", r.TransitionDays, r.TransitionClass)
					}
					if r.ExpirationDays > 0 {
						fmt.Printf(" expire after %dd", r.ExpirationDays)
					}
					if r.NoncurrentDays > 0 {
						fmt.Printf(" noncurrent after %dd", r.NoncurrentDays)
					}
					if r.AbortMPUDays > 0 {
						fmt.Printf(" abort MPU after %dd", r.AbortMPUDays)
					}
					fmt.Println()
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "put s3://bucket FILE",
			Short: "Replace lifecycle rules from a JSON file (\"-\" = stdin)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				var rules []adminops.LifecycleRule
				if err := decodePayload(args[1], &rules); err != nil {
					return err
				}
				if err := adminops.PutLifecycle(cmd.Context(), c.S3, bucket, rules); err != nil {
					return opErr(err)
				}
				col.ok.Printf("lifecycle saved for s3://%s (%d rule(s))\n", bucket, len(rules))
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Remove the lifecycle configuration",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeleteLifecycle(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("lifecycle removed from s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

func bucketEncryptionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encryption get|put|delete s3://bucket",
		Short: "Default server-side encryption",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print the default encryption setting",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				e, err := adminops.GetEncryption(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(e)
				}
				if e.Algorithm == "" {
					col.dim.Println("(no default encryption — objects are stored unencrypted)")
					return nil
				}
				fmt.Println(e.Algorithm)
				if e.KMSKeyID != "" {
					fmt.Println(e.KMSKeyID)
				}
				return nil
			},
		},
		encryptionPutCmd(),
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Remove default encryption",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeleteEncryption(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("default encryption removed from s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

func encryptionPutCmd() *cobra.Command {
	var algo, kmsKey string
	cmd := &cobra.Command{
		Use:   "put s3://bucket --algo AES256|aws:kms",
		Short: "Set default encryption",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			if algo != "AES256" && algo != "aws:kms" {
				return usageErr("--algo must be AES256 or aws:kms, got %q", algo)
			}
			if err := adminops.PutEncryption(cmd.Context(), c.S3, bucket, algo, kmsKey); err != nil {
				return opErr(err)
			}
			col.ok.Printf("default encryption %s saved for s3://%s\n", algo, bucket)
			return nil
		},
	}
	cmd.Flags().StringVar(&algo, "algo", "AES256", "AES256 or aws:kms")
	cmd.Flags().StringVar(&kmsKey, "kms-key", "", "KMS key ARN/ID (aws:kms only)")
	return cmd
}

func bucketPABCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pab get|put|delete s3://bucket",
		Short: "Public access block",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print the four public access block settings",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				p, err := adminops.GetPAB(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(p)
				}
				printPAB(p)
				return nil
			},
		},
		pabPutCmd(),
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Remove public access block settings",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeletePAB(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("public access block removed from s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

func pabPutCmd() *cobra.Command {
	var all, blockACLs, ignoreACLs, blockPolicy, restrictBuckets bool
	cmd := &cobra.Command{
		Use:   "put s3://bucket [--all | individual flags]",
		Short: "Set public access block (--all = all four on, recommended)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			p := adminops.PABInfo{
				BlockPublicACLs:       blockACLs,
				IgnorePublicACLs:      ignoreACLs,
				BlockPublicPolicy:     blockPolicy,
				RestrictPublicBuckets: restrictBuckets,
			}
			if all {
				p = p.All()
			}
			if err := adminops.PutPAB(cmd.Context(), c.S3, bucket, p); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(p)
			}
			printPAB(p)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&all, "all", false, "block everything (recommended)")
	f.BoolVar(&blockACLs, "block-public-acls", false, "block public ACLs")
	f.BoolVar(&ignoreACLs, "ignore-public-acls", false, "ignore public ACLs")
	f.BoolVar(&blockPolicy, "block-public-policy", false, "block public policies")
	f.BoolVar(&restrictBuckets, "restrict-public-buckets", false, "restrict public bucket policies")
	return cmd
}

func printPAB(p adminops.PABInfo) {
	printOnOff("block public ACLs:       ", p.BlockPublicACLs)
	printOnOff("ignore public ACLs:      ", p.IgnorePublicACLs)
	printOnOff("block public policies:   ", p.BlockPublicPolicy)
	printOnOff("restrict public buckets: ", p.RestrictPublicBuckets)
}

func printOnOff(label string, on bool) {
	if on {
		col.ok.Printf("%son\n", label)
		return
	}
	col.errf.Printf("%soff\n", label)
}

func bucketWebsiteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "website get|put|delete s3://bucket",
		Short: "Static website hosting",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print website hosting settings",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				w, err := adminops.GetWebsite(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(w)
				}
				if w.IndexSuffix == "" && w.RedirectHost == "" {
					col.dim.Println("(website hosting disabled)")
					return nil
				}
				if w.RedirectHost != "" {
					fmt.Printf("redirect all requests to %s (%s)\n", w.RedirectHost, orDash(w.RedirectProtocol))
					return nil
				}
				fmt.Printf("index document: %s\n", w.IndexSuffix)
				if w.ErrorKey != "" {
					fmt.Printf("error document: %s\n", w.ErrorKey)
				}
				return nil
			},
		},
		websitePutCmd(),
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Disable website hosting",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeleteWebsite(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("website hosting disabled for s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

func websitePutCmd() *cobra.Command {
	var index, errKey, redirectHost, redirectProto string
	cmd := &cobra.Command{
		Use:   "put s3://bucket",
		Short: "Configure website hosting",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket, err := bucketArg(args[0])
			if err != nil {
				return err
			}
			w := adminops.WebsiteInfo{
				IndexSuffix:      index,
				ErrorKey:         errKey,
				RedirectHost:     redirectHost,
				RedirectProtocol: redirectProto,
			}
			if err := adminops.PutWebsite(cmd.Context(), c.S3, bucket, w); err != nil {
				return opErr(err)
			}
			col.ok.Printf("website hosting saved for s3://%s\n", bucket)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&index, "index", "index.html", "index document")
	f.StringVar(&errKey, "error", "", "error document (e.g. 404.html)")
	f.StringVar(&redirectHost, "redirect-host", "", "redirect all requests to this host (overrides index/error)")
	f.StringVar(&redirectProto, "redirect-proto", "https", "protocol for --redirect-host (https|http)")
	return cmd
}

func bucketTagsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tags get|put|delete s3://bucket [k=v ...]",
		Short: "Cost-allocation tags",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get s3://bucket",
			Short: "Print bucket tags",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				tags, err := adminops.GetTags(cmd.Context(), c.S3, bucket)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(tags)
				}
				if len(tags) == 0 {
					col.dim.Println("(no tags)")
					return nil
				}
				for _, t := range tags {
					fmt.Printf("%s=%s\n", t.Key, t.Value)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "put s3://bucket k=v [k=v ...]",
			Short: "Replace the tag set",
			Args:  cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				var tags []adminops.Tag
				for _, a := range args[1:] {
					k, v, ok := strings.Cut(a, "=")
					if !ok || k == "" {
						return usageErr("tags must look like key=value, got %q", a)
					}
					tags = append(tags, adminops.Tag{Key: k, Value: v})
				}
				if err := adminops.PutTags(cmd.Context(), c.S3, bucket, tags); err != nil {
					return opErr(err)
				}
				col.ok.Printf("%d tag(s) saved for s3://%s\n", len(tags), bucket)
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete s3://bucket",
			Short: "Remove all bucket tags",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := resolveClient(cmd.Context())
				if err != nil {
					return err
				}
				bucket, err := bucketArg(args[0])
				if err != nil {
					return err
				}
				if err := adminops.DeleteTags(cmd.Context(), c.S3, bucket); err != nil {
					return opErr(err)
				}
				col.ok.Printf("tags removed from s3://%s\n", bucket)
				return nil
			},
		},
	)
	return cmd
}

// decodePayload reads a JSON payload from file/stdin into v.
func decodePayload(path string, v any) error {
	raw, err := readPayload(path)
	if err != nil {
		return opErr(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return opErr(fmt.Errorf("invalid JSON payload %s: %w", path, err))
	}
	return nil
}

// bucketHead returns the bucket region ("us-east-1" when unconstrained).
func bucketHead(ctx context.Context, c *s3client.Client, bucket string) (string, error) {
	out, err := bucketops.Head(ctx, c.S3, bucket)
	if err != nil {
		return "", err
	}
	region := string(out.LocationConstraint)
	if region == "" {
		region = "us-east-1"
	}
	return region, nil
}

// countTrue counts the set booleans.
func countTrue(vals ...bool) int {
	n := 0
	for _, v := range vals {
		if v {
			n++
		}
	}
	return n
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func anyStr(v any) string {
	s, _ := v.(string)
	return s
}

func or(s string, on bool) string {
	if on {
		return s
	}
	return ""
}

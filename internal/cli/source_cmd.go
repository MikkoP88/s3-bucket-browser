// source_cmd.go is the CLI surface for data sources: any-type connections
// (s3/sftp/scp/ftp/ftps/webdav/webdavs/local — remote engines live in
// pkg/core/remotefs) plus the password-encrypted Profile file container
// (export/import). The legacy `s3b profile` family remains as the S3
// specialization and stays in sync through the profile ↔ source mirroring
// in the store.
package cli

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/atomicfile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/errhelp"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func sourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Manage data sources (any connection type)",
		Long: "s3b source manages data sources of any type: s3, sftp, scp,\n" +
			"ftp, ftps, webdav, webdavs and local (remote commands address them\n" +
			"as NAME://path).\n" +
			"Sources of type s3 are mirrored as legacy profiles, so --profile\n" +
			"keeps resolving them by name.",
	}
	cmd.AddCommand(
		sourceAddCmd(),
		sourceListCmd(),
		sourceRemoveCmd(),
		sourceTestCmd(),
		sourceSplitCmd(),
		sourceExportCmd(),
		sourceImportCmd(),
	)
	return cmd
}

// stdinReader is shared across prompts: a per-call bufio.Reader would
// buffer-swallow the lines after the first when input is piped.
var stdinReader = bufio.NewReader(os.Stdin)

// readPasswordInput prompts on stderr and reads a password without echo
// when stdin is a terminal (plain line read otherwise, for pipes/scripts).
func readPasswordInput(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// resolvePassword picks a password: --password flag > $S3B_PASSWORD >
// interactive prompt with confirmation. Empty input at the prompt is an
// error — an encrypted container without a password is plaintext.
func resolvePassword(flagPw, label string) (string, error) {
	if pw := first(flagPw, os.Getenv("S3B_PASSWORD")); pw != "" {
		return pw, nil
	}
	pw, err := readPasswordInput(label)
	if err != nil {
		return "", opErr(err)
	}
	if pw == "" {
		return "", usageErr("a password is required")
	}
	pw2, err := readPasswordInput("Repeat password")
	if err != nil {
		return "", opErr(err)
	}
	if pw != pw2 {
		return "", usageErr("passwords do not match")
	}
	return pw, nil
}

// containerPassword reads a container password without confirmation (the
// file already exists; a typo just fails to decrypt).
func containerPassword(flagPw, fileLabel string) (string, error) {
	if pw := first(flagPw, os.Getenv("S3B_PASSWORD")); pw != "" {
		return pw, nil
	}
	pw, err := readPasswordInput("Password for " + fileLabel)
	if err != nil {
		return "", opErr(err)
	}
	if pw == "" {
		return "", usageErr("a password is required")
	}
	return pw, nil
}

func sourceAddCmd() *cobra.Command {
	var (
		typ                                                  string
		endpoint, region, accessKey, secretKey, sessionToken string
		bucket                                               string
		host, username, password, root                       string
		port                                                 int
		pathStyle, virtualHosted, insecure                   bool
	)
	cmd := &cobra.Command{
		Use:   "add NAME --type TYPE",
		Short: "Add or update a data source",
		Long: "Add or update a data source of any type:\n" +
			"  s3    --endpoint --region --access-key --secret-key --session-token\n" +
			"        --bucket (every S3 source is ONE bucket) --path-style/\n" +
			"        --virtual-hosted --insecure\n" +
			"        shorthand URL: add [NAME] s3://bucket (flags supply the rest;\n" +
			"        without NAME the bucket is the name)\n" +
			"  sftp/scp/ftp/ftps/webdav/webdavs\n" +
			"        --host --port --username --password --root\n" +
			"        shorthand URL: add [NAME] sftp://user:pass@host:port/root\n" +
			"        (port and root optional; without NAME the hostname is the name;\n" +
			"        webdavs:// is WebDAV over TLS)\n" +
			"  local --root PATH",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			name := args[0]
			var u profile.ConnURI
			// s3://BUCKET shorthand: sets the bucket (and the name when
			// no explicit NAME is given); credentials come from flags/env.
			if b, isS3 := profile.ParseS3BucketURI(args[len(args)-1]); isS3 {
				if b == "" {
					return usageErr("s3:// shorthand must name a bucket: s3://my-bucket")
				}
				if cmd.Flags().Changed("bucket") && bucket != b {
					return usageErr("--bucket %s conflicts with the URL (%s)", bucket, b)
				}
				bucket = b
				if len(args) == 1 {
					name = b
				}
			} else if len(args) == 2 {
				parsed, ok, perr := profile.ParseConnURI(args[1])
				if !ok {
					return usageErr("second argument must be a sftp:// scp:// ftp:// ftps:// webdav:// webdavs:// or s3:// URL")
				}
				if perr != nil {
					return usageErr("%v", perr)
				}
				u = parsed
			} else if parsed, ok, perr := profile.ParseConnURI(args[0]); ok {
				if perr != nil {
					return usageErr("%v", perr)
				}
				u, name = parsed, parsed.Host
			}
			bucket = strings.TrimSpace(bucket)
			if u.Type != "" {
				if cmd.Flags().Changed("type") && typ != u.Type {
					return usageErr("--type %s conflicts with the URL (its scheme implies --type %s)", typ, u.Type)
				}
				for _, f := range []string{"host", "port", "username", "password", "root"} {
					if cmd.Flags().Changed(f) {
						return usageErr("--%s cannot be combined with a URL argument (the URL sets it)", f)
					}
				}
				typ, host, port = u.Type, u.Host, u.Port
				username, password, root = u.Username, u.Password, u.Root
			}
			var src profile.Source
			switch typ {
			case profile.TypeS3:
				if bucket == "" {
					return usageErr("an S3 data source is one bucket — pass --bucket BUCKET or use the s3://BUCKET shorthand")
				}
				accessKey = first(accessKey, os.Getenv("S3B_ACCESS_KEY"))
				secretKey = first(secretKey, os.Getenv("S3B_SECRET_KEY"))
				p := profile.Profile{
					Name:         name,
					Endpoint:     provider.NormalizeEndpoint(endpoint, insecure),
					Region:       region,
					AccessKeyID:  accessKey,
					SecretKey:    secretKey,
					SessionToken: sessionToken,
					PathStyle:    pathStyle,
					Insecure:     insecure,
				}
				if virtualHosted {
					p.PathStyle = false
				}
				if !cmd.Flags().Changed("path-style") && !virtualHosted && p.Endpoint != "" {
					if host := provider.Hostname(p.Endpoint); host == "localhost" || net.ParseIP(host) != nil {
						p.PathStyle = true
					}
				}
				if err := s.UpsertS3Profile(p); err != nil {
					return usageErr("%v", err)
				}
				// bucket scoping is source-only (profiles cannot carry it)
				// — set it on the mirror the upsert just made
				m, err := s.GetSource(name)
				if err != nil {
					return opErr(err)
				}
				m.Bucket = bucket
				if err := s.UpsertSource(m); err != nil {
					return usageErr("%v", err)
				}
				if err := s.Save(); err != nil {
					return opErr(err)
				}
				mirror, _ := s.GetSource(name)
				if flagJSON {
					return printJSON(mirror.Public())
				}
				col.hi.Printf("source %q saved (s3)\n", name)
				fmt.Printf("  endpoint: %s\n", orDefault(p.Endpoint, "(AWS default)"))
				fmt.Printf("  bucket:   %s\n", bucket)
				fmt.Printf("  style:    %s\n", styleName(p.PathStyle))
				if p.Insecure {
					col.warn.Println("  warning:  TLS verification disabled (--insecure)")
				}
				return nil

			case profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP, profile.TypeFTPS, profile.TypeWebDAV, profile.TypeWebDAVS:
				if strings.TrimSpace(host) == "" {
					return usageErr("--host is required for %s sources", typ)
				}
				password = first(password, os.Getenv("S3B_PASSWORD"))
				src = profile.Source{
					Name:     name,
					Type:     typ,
					Host:     strings.TrimSpace(host),
					Port:     port,
					Username: username,
					Password: password,
					Root:     root,
				}
			case profile.TypeLocal:
				if strings.TrimSpace(root) == "" {
					return usageErr("--root is required for local sources")
				}
				if _, err := os.Stat(root); err != nil {
					return usageErr("local root: %v", err)
				}
				src = profile.Source{
					Name:      name,
					Type:      typ,
					LocalRoot: root,
				}
			default:
				return usageErr("unsupported source type %q (want s3, sftp, scp, ftp, ftps, webdav, webdavs or local)", typ)
			}

			if src.Type != profile.TypeS3 {
				if err := s.UpsertSource(src); err != nil {
					return usageErr("%v", err)
				}
				if err := s.Save(); err != nil {
					return opErr(err)
				}
			}
			if flagJSON {
				saved, _ := s.GetSource(name)
				return printJSON(saved.Public())
			}
			col.hi.Printf("source %q saved (%s)\n", name, typ)
			switch src.Type {
			case profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP, profile.TypeFTPS, profile.TypeWebDAV, profile.TypeWebDAVS:
				portStr := fmt.Sprintf("%d", src.Port)
				if src.Port == 0 {
					portStr = fmt.Sprintf("(default %d)", src.DefaultPort())
				}
				fmt.Printf("  host:     %s port: %s\n", src.Host, portStr)
				fmt.Printf("  user:     %s\n", orDefault(src.Username, "(none)"))
				if src.Password == "" {
					if src.Type == profile.TypeSFTP || src.Type == profile.TypeSCP {
						fmt.Println("  password: (none — key auth at dial time)")
					} else {
						fmt.Println("  password: (none — anonymous)")
					}
				}
			case profile.TypeLocal:
				fmt.Printf("  root:     %s\n", src.LocalRoot)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&typ, "type", "s3", "source type: s3, sftp, scp, ftp, ftps, webdav, webdavs, local")
	f.StringVar(&endpoint, "endpoint", "", "endpoint URL (empty = AWS)")
	f.StringVar(&region, "region", "", "region (default us-east-1)")
	f.StringVar(&accessKey, "access-key", "", "access key ID ($S3B_ACCESS_KEY)")
	f.StringVar(&secretKey, "secret-key", "", "secret access key ($S3B_SECRET_KEY)")
	f.StringVar(&sessionToken, "session-token", "", "STS session token")
	f.StringVar(&bucket, "bucket", "", "bucket this source is scoped to (s3 sources are per-bucket)")
	f.StringVar(&host, "host", "", "remote host (sftp/scp/ftp/ftps/webdav/webdavs)")
	f.IntVar(&port, "port", 0, "port (0 = per-type default at dial time)")
	f.StringVar(&username, "username", "", "remote username")
	f.StringVar(&password, "password", "", "remote password ($S3B_PASSWORD)")
	f.StringVar(&root, "root", "", "starting directory (remote) or directory root (local)")
	f.BoolVar(&pathStyle, "path-style", false, "path-style addressing (s3)")
	f.BoolVar(&virtualHosted, "virtual-hosted", false, "virtual-hosted addressing (s3)")
	f.BoolVar(&insecure, "insecure", false, "skip TLS verification (labs only)")
	return cmd
}

// sourceTypeLabels is the GUI badge's text twin (util.js SRC_TYPE_LABEL):
// the typed form of a source type, so plain-text surfaces speak the same
// "S3 · name" identity the tinted chip paints everywhere.
var sourceTypeLabels = map[string]string{
	profile.TypeS3:      "S3",
	profile.TypeSFTP:    "SFTP",
	profile.TypeSCP:     "SCP",
	profile.TypeFTP:     "FTP",
	profile.TypeFTPS:    "FTPS",
	profile.TypeWebDAV:  "WebDAV",
	profile.TypeWebDAVS: "WebDAVS",
	profile.TypeLocal:   "Local",
}

// sourceTypeLabel returns the typed form of a source type, "Other" for
// anything the GUI would not badge either.
func sourceTypeLabel(t string) string {
	if l, ok := sourceTypeLabels[t]; ok {
		return l
	}
	return "Other"
}

// sourceDetail is the one-line human summary per source type.
func sourceDetail(s profile.Source) string {
	switch s.Type {
	case profile.TypeS3:
		if s.S3 == nil {
			return ""
		}
		ep := orDefault(s.S3.Endpoint, "(AWS default)")
		if s.Bucket != "" {
			return fmt.Sprintf("%s @ %s", s.Bucket, ep)
		}
		return fmt.Sprintf("%s (account-wide — run 's3b source split %s')", ep, s.Name)
	case profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP, profile.TypeFTPS, profile.TypeWebDAV, profile.TypeWebDAVS:
		port := s.Port
		if port == 0 {
			port = s.DefaultPort()
		}
		detail := fmt.Sprintf("%s:%d", s.Host, port)
		if s.Username != "" {
			detail = s.Username + "@" + detail
		}
		if s.Root != "" {
			detail += " " + s.Root
		}
		return detail
	case profile.TypeLocal:
		return s.LocalRoot
	default:
		return s.Type
	}
}

func sourceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List data sources (secrets masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			if flagJSON {
				list := make([]profile.Source, 0, len(s.Sources))
				for _, src := range s.SortedSources() {
					list = append(list, src.Public())
				}
				return printJSON(list)
			}
			if len(s.Sources) == 0 {
				rprintf("no data sources — add one with: s3b source add <name> --type s3 --endpoint ...\n")
				return nil
			}
			for _, src := range s.SortedSources() {
				rprintf("  %-26s %s\n", sourceTypeLabel(src.Type)+" · "+src.Name, sourceDetail(src))
			}
			return nil
		},
	}
}

func sourceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove NAME|ID",
		Short: "Remove a data source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			src, err := s.GetSource(args[0])
			if err != nil {
				return usageErr("%v", err)
			}
			if src.Type == profile.TypeS3 {
				// The s3 source and its mirrored profile are the same
				// connection; remove both.
				if err := s.RemoveS3Profile(src.Name); err != nil {
					return usageErr("%v", err)
				}
			} else if err := s.RemoveSource(src.ID); err != nil {
				return usageErr("%v", err)
			}
			if err := s.Save(); err != nil {
				return opErr(err)
			}
			if !flagJSON {
				fmt.Printf("removed source: %s\n", src.Name)
			}
			return nil
		},
	}
}

func sourceTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [NAME|ID]",
		Short: "Test connectivity for a source",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			var src profile.Source
			if len(args) == 1 {
				if src, err = s.GetSource(args[0]); err != nil {
					return opErr(err)
				}
			} else {
				p, err := s.SoleProfile()
				if err != nil {
					return opErr(err)
				}
				if src, err = s.GetSource(p.Name); err != nil {
					src = profile.FromProfile(p)
				}
			}
			if src.Type != profile.TypeS3 || src.S3 == nil {
				// Real probe: dial the remotefs engine and list the root.
				fs, err := remotefs.Dial(cmd.Context(), src)
				if err == nil {
					defer fs.Close()
					_, err = fs.List(cmd.Context(), "/")
				}
				if err != nil {
					col.errf.Printf("FAIL %s: %v\n", src.Name, err)
					if flagJSON {
						return printJSON(map[string]any{"source": src.Name, "ok": false, "message": err.Error()})
					}
					return opErr(fmt.Errorf("connectivity test failed"))
				}
				if flagJSON {
					return printJSON(map[string]any{"source": src.Name, "ok": true})
				}
				col.ok.Printf("OK %s — connected over %s\n", src.Name, sourceTypeLabel(src.Type))
				return nil
			}
			p := *src.S3
			p.Name = src.Name
			c, err := newClient(cmd.Context(), p)
			if err != nil {
				return err
			}
			buckets, err := listingBuckets(cmd, c)
			if err != nil {
				col.errf.Printf("FAIL %s: %v\n", src.Name, err)
				if a := errAdvice(err); a != nil {
					fmt.Fprintln(os.Stderr, errhelp.Format(a))
				}
				return opErr(fmt.Errorf("connectivity test failed"))
			}
			if flagJSON {
				return printJSON(map[string]any{"source": src.Name, "ok": true, "buckets": len(buckets)})
			}
			col.ok.Printf("OK %s — %d bucket(s) visible\n", src.Name, len(buckets))
			return nil
		},
	}
}

// sourceSplitCmd splits a legacy account-wide s3 source into one
// bucket-scoped source per visible bucket — the CLI face of the one-bucket
// model's migration (the GUI runs the same split automatically).
func sourceSplitCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "split NAME",
		Short: "Split a legacy account-wide s3 source into one data source per bucket",
		Long: "Split a legacy account-wide s3 source into one bucket-scoped\ndata source per visible bucket, named after the bucket (every S3 data\nsource is one bucket).\n" +
			"Idempotent: buckets whose connection already has a source are\nrefreshed in place, and the account source (and its mirrored profile)\nis removed only after every bucket settled.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			src, err := s.GetSource(args[0])
			if err != nil {
				return usageErr("%v", err)
			}
			if src.Type != profile.TypeS3 || src.S3 == nil {
				return usageErr("source %q (%s) is not an s3 source", src.Name, src.Type)
			}
			if src.Bucket != "" {
				return usageErr("source %q is already scoped to bucket %q — nothing to split", src.Name, src.Bucket)
			}
			p := *src.S3
			p.Name = src.Name
			c, err := newClient(cmd.Context(), p)
			if err != nil {
				return err
			}
			buckets, err := listingBuckets(cmd, c)
			if err != nil {
				if a := errAdvice(err); a != nil {
					fmt.Fprintln(os.Stderr, errhelp.Format(a))
				}
				return opErr(fmt.Errorf("listing buckets of %q: %w", src.Name, err))
			}
			names := make([]string, 0, len(buckets))
			for _, b := range buckets {
				if n := strings.TrimSpace(aws.ToString(b.Name)); n != "" {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				return opErr(fmt.Errorf("source %q sees no buckets — nothing to split into", src.Name))
			}
			if dryRun {
				if flagJSON {
					return printJSON(map[string]any{"source": src.Name, "buckets": names})
				}
				rprintf("%q would split into %d bucket source(s):\n", src.Name, len(names))
				for _, n := range names {
					rprintf("  %s\n", n)
				}
				return nil
			}
			var created, matched []string
			for _, bucket := range names {
				if ex := splitExistingSource(s, src, bucket); ex != nil {
					// same connection already has a source for this bucket:
					// refresh its credentials in place, keep its name
					bp := *src.S3
					bp.Name = ex.Name
					if err := s.UpsertS3Profile(bp); err != nil {
						return usageErr("%v", err)
					}
					matched = append(matched, bucket)
					continue
				}
				bp := *src.S3
				bp.Name = bucket
				if err := s.UpsertS3Profile(bp); err != nil {
					return usageErr("%v", err)
				}
				// bucket scoping is source-only (profiles cannot carry it)
				// — set it on the mirror the upsert just made
				m, err := s.GetSource(bucket)
				if err != nil {
					return opErr(err)
				}
				m.Bucket = bucket
				if err := s.UpsertSource(m); err != nil {
					return usageErr("%v", err)
				}
				created = append(created, bucket)
			}
			// the account source and its mirrored profile are one legacy
			// connection — both go when every bucket settled
			if err := s.RemoveS3Profile(src.Name); err != nil {
				return opErr(err)
			}
			if err := s.Save(); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"source": src.Name, "created": created, "matched": matched})
			}
			rprintf("split %q into %d bucket source(s) (%d already present)\n", src.Name, len(created), len(matched))
			for _, n := range created {
				rprintf("  + %s\n", n)
			}
			for _, n := range matched {
				rprintf("  = %s (refreshed)\n", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what the split would create, change nothing")
	return cmd
}

// splitExistingSource finds the stored source the split would refresh: one
// scoped to the bucket on the account's connection (endpoint + access key).
func splitExistingSource(s *profile.Store, acct profile.Source, bucket string) *profile.Source {
	for i := range s.Sources {
		c := &s.Sources[i]
		if c.Type != profile.TypeS3 || c.S3 == nil || c.Bucket != bucket {
			continue
		}
		if strings.EqualFold(strings.TrimSuffix(c.S3.Endpoint, "/"), strings.TrimSuffix(acct.S3.Endpoint, "/")) &&
			c.S3.AccessKeyID == acct.S3.AccessKeyID {
			return c
		}
	}
	return nil
}

func sourceExportCmd() *cobra.Command {
	var name, password string
	cmd := &cobra.Command{
		Use:   "export FILE",
		Short: "Export all sources into an encrypted profile file (*.s3bprofile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			if len(s.Sources) == 0 {
				return opErr(fmt.Errorf("no data sources to export"))
			}
			path := args[0]
			if name == "" {
				base := filepath.Base(path)
				name = strings.TrimSuffix(base, filepath.Ext(base))
			}
			pw, err := resolvePassword(password, "Password (AES-256-GCM encrypts the file)")
			if err != nil {
				return err
			}
			data, err := profile.EncryptContainer(name, s.Sources, pw)
			if err != nil {
				return opErr(err)
			}
			if err := atomicfile.Write(path, data, 0o600); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"file": path, "sources": len(s.Sources)})
			}
			col.ok.Printf("exported %d source(s) → %s\n", len(s.Sources), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "container display name (default: file base name)")
	cmd.Flags().StringVar(&password, "password", "", "encryption password ($S3B_PASSWORD, else prompted)")
	return cmd
}

// importResult mirrors api.ImportResult for the CLI report.
type importResult struct {
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
}

func sourceImportCmd() *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:   "import FILE",
		Short: "Import sources from an encrypted profile file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			path := args[0]
			data, err := os.ReadFile(path)
			if err != nil {
				return opErr(err)
			}
			pw, err := containerPassword(password, filepath.Base(path))
			if err != nil {
				return err
			}
			_, srcs, err := profile.DecryptContainer(data, pw)
			if err != nil {
				return opErr(fmt.Errorf("%s: %w", filepath.Base(path), err))
			}
			var res importResult
			for _, src := range srcs {
				if _, err := s.GetSource(src.Name); err == nil {
					res.Skipped = append(res.Skipped, src.Name+" (already exists)")
					continue
				}
				if src.Type == profile.TypeS3 && src.S3 != nil {
					p := *src.S3
					p.Name = src.Name
					if err := s.UpsertS3Profile(p); err != nil {
						return opErr(err)
					}
				} else if err := s.UpsertSource(src); err != nil {
					return opErr(err)
				}
				res.Imported = append(res.Imported, src.Name)
			}
			if len(res.Imported) > 0 {
				if err := s.Save(); err != nil {
					return opErr(err)
				}
			}
			if flagJSON {
				return printJSON(res)
			}
			col.ok.Printf("imported %d source(s) from %s\n", len(res.Imported), filepath.Base(path))
			for _, skip := range res.Skipped {
				col.warn.Printf("  skipped %s\n", skip)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "container password ($S3B_PASSWORD, else prompted)")
	return cmd
}

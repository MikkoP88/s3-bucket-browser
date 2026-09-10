// source_cmd.go is the M8 CLI surface for data sources: any-type
// connections (s3 today; sftp/scp/ftp/ftps/local schemas fixed) plus the
// password-encrypted Profile file container (export/import). The legacy
// `s3b profile` family remains as the S3 specialization and stays in sync
// through the profile ↔ source mirroring in the store.
package cli

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/errhelp"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func sourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Manage data sources (any connection type)",
		Long: "s3b source manages data sources: S3 endpoints today, with\n" +
			"sftp/scp/ftp/ftps/local schemas already fixed for the upcoming\n" +
			"remote-filesystem engines. Sources of type s3 are mirrored as\n" +
			"legacy profiles, so --profile keeps resolving them by name.",
	}
	cmd.AddCommand(
		sourceAddCmd(),
		sourceListCmd(),
		sourceUseCmd(),
		sourceRemoveCmd(),
		sourceTestCmd(),
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
		host, username, password, root                       string
		port                                                 int
		pathStyle, virtualHosted, insecure, makeDefault      bool
	)
	cmd := &cobra.Command{
		Use:   "add NAME --type TYPE",
		Short: "Add or update a data source",
		Long: "Add or update a data source of any type:\n" +
			"  s3    --endpoint --region --access-key --secret-key --session-token\n" +
			"        --path-style/--virtual-hosted --insecure\n" +
			"  sftp/scp/ftp/ftps  --host --port --username --password --root\n" +
			"        (engines ship next; the connection is saved as configured)\n" +
			"  local --root PATH",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			name := args[0]
			var src profile.Source
			switch typ {
			case profile.TypeS3:
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
				if makeDefault || len(s.Profiles) == 1 {
					if err := s.SetDefaultS3(name); err != nil {
						return usageErr("%v", err)
					}
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
				fmt.Printf("  style:    %s\n", styleName(p.PathStyle))
				if p.Insecure {
					col.warn.Println("  warning:  TLS verification disabled (--insecure)")
				}
				return nil

			case profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP, profile.TypeFTPS:
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
				return usageErr("unsupported source type %q (want s3, sftp, scp, ftp, ftps or local)", typ)
			}

			if src.Type != profile.TypeS3 {
				if makeDefault {
					return usageErr("only S3 sources can be the default connection")
				}
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
			case profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP, profile.TypeFTPS:
				portStr := fmt.Sprintf("%d", src.Port)
				if src.Port == 0 {
					portStr = fmt.Sprintf("(default %d)", src.DefaultPort())
				}
				fmt.Printf("  host:     %s port: %s\n", src.Host, portStr)
				fmt.Printf("  user:     %s\n", orDefault(src.Username, "(none)"))
				if src.Password == "" {
					fmt.Println("  password: (none — key auth at dial time for sftp/scp)")
				}
			case profile.TypeLocal:
				fmt.Printf("  root:     %s\n", src.LocalRoot)
			}
			if src.Type != profile.TypeS3 && src.Type != profile.TypeLocal {
				col.dim.Println("  note:     remote-filesystem engines ship next; the connection is saved as configured")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&typ, "type", "s3", "source type: s3, sftp, scp, ftp, ftps, local")
	f.StringVar(&endpoint, "endpoint", "", "endpoint URL (empty = AWS)")
	f.StringVar(&region, "region", "", "region (default us-east-1)")
	f.StringVar(&accessKey, "access-key", "", "access key ID ($S3B_ACCESS_KEY)")
	f.StringVar(&secretKey, "secret-key", "", "secret access key ($S3B_SECRET_KEY)")
	f.StringVar(&sessionToken, "session-token", "", "STS session token")
	f.StringVar(&host, "host", "", "remote host (sftp/scp/ftp/ftps)")
	f.IntVar(&port, "port", 0, "port (0 = per-type default at dial time)")
	f.StringVar(&username, "username", "", "remote username")
	f.StringVar(&password, "password", "", "remote password ($S3B_PASSWORD)")
	f.StringVar(&root, "root", "", "starting directory (remote) or directory root (local)")
	f.BoolVar(&pathStyle, "path-style", false, "path-style addressing (s3)")
	f.BoolVar(&virtualHosted, "virtual-hosted", false, "virtual-hosted addressing (s3)")
	f.BoolVar(&insecure, "insecure", false, "skip TLS verification (labs only)")
	f.BoolVar(&makeDefault, "default", false, "make this the default source (s3)")
	return cmd
}

// sourceDetail is the one-line human summary per source type.
func sourceDetail(s profile.Source) string {
	switch s.Type {
	case profile.TypeS3:
		if s.S3 == nil {
			return ""
		}
		return orDefault(s.S3.Endpoint, "(AWS default)")
	case profile.TypeSFTP, profile.TypeSCP, profile.TypeFTP, profile.TypeFTPS:
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
				fmt.Println("no data sources — add one with: s3b source add <name> --type s3 --endpoint ...")
				return nil
			}
			for _, src := range s.SortedSources() {
				mark := " "
				if src.Default {
					mark = "*"
				}
				fmt.Printf("%s %-20s %-6s %s\n", mark, src.Name, src.Type, sourceDetail(src))
			}
			return nil
		},
	}
}

func sourceUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use NAME",
		Short: "Set the default source",
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
			if src.Type != profile.TypeS3 {
				return usageErr("only S3 sources can be the default connection (remote engines ship next)")
			}
			if err := s.SetDefaultS3(src.Name); err != nil {
				return usageErr("%v", err)
			}
			if err := s.Save(); err != nil {
				return opErr(err)
			}
			if !flagJSON {
				fmt.Printf("default source: %s\n", src.Name)
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
				p, err := s.DefaultProfile()
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
				col.ok.Printf("OK %s — connected over %s\n", src.Name, src.Type)
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
			if err := os.WriteFile(path, data, 0o600); err != nil {
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

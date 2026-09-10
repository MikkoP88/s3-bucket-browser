// Package cli implements the s3b command tree (cobra). One binary, two
// faces: no arguments launches the GUI (M2), arguments run the CLI.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/errhelp"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// Exit-code contract (inherited from s3-bucket-tester, PLAN.md §10).
const (
	exitOK         = 0
	exitOpFail     = 1
	exitUsage      = 2
	exitUnexpected = 3
)

// exitError carries a specific process exit code through cobra.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func usageErr(format string, a ...any) error {
	return exitError{exitUsage, fmt.Errorf(format, a...)}
}

func opErr(err error) error {
	return exitError{exitOpFail, err}
}

// Global flags (bound to the root command).
var (
	flagProfileName  string
	flagEndpointURL  string
	flagRegion       string
	flagPathStyle    bool
	flagVirtualHost  bool
	flagAccessKey    string
	flagSecretKey    string
	flagSessionToken string
	flagJSON         bool
	flagNoColor      bool
	flagVerbose      bool
	flagTimeout      time.Duration
)

// Version is injected at build time.
var Version = "dev"

var (
	out io.Writer = os.Stdout
	col           = newPalette()
)

type palette struct{ ok, warn, errf, dim, bold, hi *color.Color }

func newPalette() palette {
	return palette{
		ok:   color.New(color.FgGreen),
		warn: color.New(color.FgYellow),
		errf: color.New(color.FgRed),
		dim:  color.New(color.Faint),
		bold: color.New(color.Bold),
		hi:   color.New(color.FgCyan),
	}
}

func colorEnabled() bool {
	return !flagNoColor && os.Getenv("NO_COLOR") == ""
}

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "s3b",
		Short: "S3 Bucket Browser — manage S3 buckets and objects (GUI + CLI in one)",
		Long: "s3b is the CLI face of S3 Bucket Browser.\n" +
			"Run without arguments to launch the desktop GUI (milestone M2).\n" +
			"Documentation: https://github.com/MikkoP88/s3-bucket-browser",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flagProfileName, "profile", "", "profile name (default: $S3B_PROFILE, then the default profile)")
	pf.StringVar(&flagEndpointURL, "endpoint-url", "", "override the profile endpoint URL")
	pf.StringVar(&flagRegion, "region", "", "override the region")
	pf.BoolVar(&flagPathStyle, "path-style", false, "force path-style addressing")
	pf.BoolVar(&flagVirtualHost, "virtual-hosted", false, "force virtual-hosted addressing")
	pf.StringVar(&flagAccessKey, "access-key", "", "access key override ($S3B_ACCESS_KEY)")
	pf.StringVar(&flagSecretKey, "secret-key", "", "secret key override ($S3B_SECRET_KEY)")
	pf.StringVar(&flagSessionToken, "session-token", "", "session token override")
	pf.BoolVar(&flagJSON, "json", false, "machine-readable JSON output")
	pf.BoolVar(&flagNoColor, "no-color", false, "disable colors (also honors $NO_COLOR)")
	pf.BoolVar(&flagVerbose, "verbose", false, "verbose output")
	pf.DurationVar(&flagTimeout, "timeout", 5*time.Minute, "per-request timeout")

	root.AddCommand(
		sourceCmd(),
		profileCmd(),
		lsCmd(),
		treeCmd(),
		duCmd(),
		statCmd(),
		mbCmd(),
		rbCmd(),
		mkdirCmd(),
		cpCmd(),
		mvCmd(),
		rmCmd(),
		syncCmd(),
		presignCmd(),
		doctorCmd(),
		versionsCmd(),
		bucketCmd(),
		findCmd(),
		scCmd(),
		lockCmd(),
		versionCmd(),
	)
	root.CompletionOptions.HiddenDefaultCmd = false
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string) int {
	if !colorEnabled() {
		color.NoColor = true
	}
	root := NewRoot()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, "error:", ee.err.Error())
			if a := errhelp.ForError(ee.err); a != nil && !flagJSON {
				fmt.Fprintln(os.Stderr, errhelp.Format(a))
			}
			return ee.code
		}
		fmt.Fprintln(os.Stderr, "unexpected:", err)
		return exitUnexpected
	}
	return exitOK
}

// store loads the profile store, running the one-way M8 migration (legacy
// profiles become s3 data sources) so the CLI and GUI see the same set.
func store() (*profile.Store, error) {
	s, err := profile.Load()
	if err != nil {
		return nil, opErr(err)
	}
	changed, err := s.SeedFromProfiles()
	if err != nil {
		return nil, opErr(err)
	}
	if changed {
		if err := s.Save(); err != nil {
			return nil, opErr(err)
		}
	}
	return s, nil
}

// resolveClient loads the profile store, picks the active profile and
// builds a configured client from profile + flag overrides.
func resolveClient(ctx context.Context) (*s3client.Client, error) {
	s, err := store()
	if err != nil {
		return nil, err
	}
	name := flagProfileName
	if name == "" {
		name = os.Getenv("S3B_PROFILE")
	}
	var p profile.Profile
	if name != "" {
		p, err = s.Get(name)
	} else {
		p, err = s.DefaultProfile()
	}
	if err != nil {
		return nil, opErr(err)
	}
	return newClient(ctx, p)
}

// newClient builds a client from a profile, applying flag overrides.
func newClient(ctx context.Context, p profile.Profile) (*s3client.Client, error) {
	opts := s3client.Options{
		EndpointURL:  flagEndpointURL,
		Region:       flagRegion,
		AccessKey:    first(flagAccessKey, os.Getenv("S3B_ACCESS_KEY")),
		SecretKey:    first(flagSecretKey, os.Getenv("S3B_SECRET_KEY")),
		SessionToken: flagSessionToken,
		Timeout:      flagTimeout,
	}
	if flagPathStyle {
		opts.PathStyle = &flagPathStyle
	}
	if flagVirtualHost {
		v := false
		opts.PathStyle = &v
	}
	c, err := s3client.New(ctx, p, opts)
	if err != nil {
		return nil, opErr(err)
	}
	return c, nil
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// s3URI is a parsed s3:// argument.
type s3URI struct {
	Bucket    string
	Key       string
	IsPrefix  bool // key ends with "/" (explicit directory)
	HasPrefix bool // any key component present
}

// parseS3URI parses s3://bucket[/key[/]].
func parseS3URI(s string) (s3URI, error) {
	if !strings.HasPrefix(s, "s3://") {
		return s3URI{}, usageErr("not an S3 URI: %q (expected s3://bucket[/prefix/])", s)
	}
	rest := strings.TrimPrefix(s, "s3://")
	bucket, key, _ := strings.Cut(rest, "/")
	if bucket == "" {
		return s3URI{}, usageErr("missing bucket name in %q", s)
	}
	u := s3URI{Bucket: bucket, Key: strings.TrimPrefix(key, "/")}
	if u.Key != "" {
		u.HasPrefix = true
		u.IsPrefix = strings.HasSuffix(u.Key, "/")
	}
	return u, nil
}

// printJSON emits v as indented JSON (compact would also do; indented aids
// humans using it in jq pipelines either way).
func printJSON(v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// humanSize formats byte counts.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// parseIntDuration parses "90s", "12h", "7d" style durations.
func parseIntDuration(s string) (time.Duration, error) {
	if n := len(s); n > 1 {
		if unit := s[n-1]; unit == 'd' {
			days, err := strconv.Atoi(s[:n-1])
			if err == nil {
				return time.Duration(days) * 24 * time.Hour, nil
			}
		}
	}
	return time.ParseDuration(s)
}

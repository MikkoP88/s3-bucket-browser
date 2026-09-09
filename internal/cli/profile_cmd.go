package cli

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/errhelp"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
	"github.com/spf13/cobra"
)

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage connection profiles",
	}
	cmd.AddCommand(profileAddCmd(), profileListCmd(), profileUseCmd(), profileRemoveCmd(), profileTestCmd())
	return cmd
}

func profileAddCmd() *cobra.Command {
	var (
		endpoint, region, accessKey, secretKey, sessionToken string
		pathStyle, virtualHosted, insecure, makeDefault      bool
	)
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Add or update a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			name := args[0]
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
			// Smart default: local/IP endpoints need path-style.
			if !cmd.Flags().Changed("path-style") && !virtualHosted && p.Endpoint != "" {
				host := provider.Hostname(p.Endpoint)
				if host == "localhost" || net.ParseIP(host) != nil {
					p.PathStyle = true
				}
			}
			if err := s.Upsert(p); err != nil {
				return usageErr("%v", err)
			}
			if makeDefault {
				if err := s.SetDefault(name); err != nil {
					return usageErr("%v", err)
				}
			} else if len(s.Profiles) == 1 {
				s.Profiles[0].Default = true
			}
			if err := s.Save(); err != nil {
				return opErr(err)
			}

			key := p.Provider()
			if flagJSON {
				pub := p.Public()
				pub.Default = makeDefault || len(s.Profiles) == 1
				return printJSON(pub)
			}
			col.hi.Printf("profile %q saved", name)
			fmt.Println()
			fmt.Printf("  endpoint: %s\n", orDefault(p.Endpoint, "(AWS default)"))
			fmt.Printf("  provider: %s (policy: %s, ACL: %s)\n",
				provider.Get(key).Name, provider.Get(key).PolicySupport, provider.Get(key).ACLSupport)
			fmt.Printf("  style:    %s\n", styleName(p.PathStyle))
			if p.Insecure {
				col.warn.Println("  warning:  TLS verification disabled (--insecure)")
			}
			for _, w := range provider.Warnings(key, p.Endpoint, p.PathStyle) {
				col.warn.Println("  warning: ", w)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&endpoint, "endpoint", "", "endpoint URL (empty = AWS)")
	f.StringVar(&region, "region", "", "region (default us-east-1)")
	f.StringVar(&accessKey, "access-key", "", "access key ID ($S3B_ACCESS_KEY)")
	f.StringVar(&secretKey, "secret-key", "", "secret access key ($S3B_SECRET_KEY)")
	f.StringVar(&sessionToken, "session-token", "", "STS session token")
	f.BoolVar(&pathStyle, "path-style", false, "path-style addressing")
	f.BoolVar(&virtualHosted, "virtual-hosted", false, "virtual-hosted addressing")
	f.BoolVar(&insecure, "insecure", false, "skip TLS verification (labs only)")
	f.BoolVar(&makeDefault, "default", false, "make this the default profile")
	return cmd
}

func profileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List profiles (secrets masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			if flagJSON {
				list := make([]profile.Profile, 0, len(s.Profiles))
				for _, p := range s.Sorted() {
					list = append(list, p.Public())
				}
				return printJSON(list)
			}
			if len(s.Profiles) == 0 {
				fmt.Println("no profiles — add one with: s3b profile add <name> --endpoint ... --access-key ... --secret-key ...")
				return nil
			}
			for _, p := range s.Sorted() {
				mark := " "
				if p.Default {
					mark = "*"
				}
				prov := provider.Get(p.Provider()).Name
				fmt.Printf("%s %-16s %-40s %-12s %-10s %s\n",
					mark, p.Name, orDefault(p.Endpoint, "(AWS default)"), orDefault(p.Region, "us-east-1"), styleName(p.PathStyle), prov)
			}
			return nil
		},
	}
}

func profileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use NAME",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			if err := s.SetDefault(args[0]); err != nil {
				return usageErr("%v", err)
			}
			if err := s.Save(); err != nil {
				return opErr(err)
			}
			if !flagJSON {
				fmt.Printf("default profile: %s\n", args[0])
			}
			return nil
		},
	}
}

func profileRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			if err := s.Remove(args[0]); err != nil {
				return usageErr("%v", err)
			}
			if err := s.Save(); err != nil {
				return opErr(err)
			}
			if !flagJSON {
				fmt.Printf("removed profile: %s\n", args[0])
			}
			return nil
		},
	}
}

func profileTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [NAME]",
		Short: "Test connectivity for a profile (lightweight doctor)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store()
			if err != nil {
				return err
			}
			var p profile.Profile
			if len(args) == 1 {
				p, err = s.Get(args[0])
			} else {
				p, err = s.DefaultProfile()
			}
			if err != nil {
				return opErr(err)
			}
			c, err := newClient(cmd.Context(), p)
			if err != nil {
				return err
			}
			buckets, err := listingBuckets(cmd, c)
			if err != nil {
				col.errf.Printf("FAIL %s: %v\n", p.Name, err)
				if a := errAdvice(err); a != nil {
					fmt.Fprintln(os.Stderr, errhelp.Format(a))
				}
				return opErr(fmt.Errorf("connectivity test failed"))
			}
			if flagJSON {
				return printJSON(map[string]any{"profile": p.Name, "ok": true, "buckets": len(buckets)})
			}
			col.ok.Printf("OK %s — %d bucket(s) visible\n", p.Name, len(buckets))
			return nil
		},
	}
}

func styleName(pathStyle bool) string {
	if pathStyle {
		return "path-style"
	}
	return "virtual-host"
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// license_cmd.go: `s3b license` — the accept-license phase on the CLI
// face. The GUI's setup gate and these commands write the same record
// (license.json in the config store), so accepting from either side
// unlocks the install. The CLI does not hard-gate its commands: it is
// the scripting face, and a blocking prompt would break every pipeline;
// acceptance here is explicit (`license accept`) and inspectable
// (`license status`).
package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/internal/provenance"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
	"github.com/spf13/cobra"
)

func licenseCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "license",
		Short: "Show the software license and manage its acceptance",
		Long: "Every fresh install walks an accept-license phase before the application unlocks;\n" +
			"the GUI shows it as a gate on first launch. These commands read and write the same\n" +
			"acceptance record from the terminal, keyed by license name and version — a license\n" +
			"bump makes the phase run again on the next launch.",
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Print the license acceptance record",
		RunE: func(cmd *cobra.Command, args []string) error {
			rec := appsettings.LoadLicense()
			stale := !rec.Covers(provenance.LicenseName, provenance.LicenseVersion)
			if flagJSON {
				return printJSON(map[string]string{
					"product":    provenance.Product,
					"holder":     provenance.HolderFull,
					"license":    provenance.LicenseName,
					"version":    provenance.LicenseVersion,
					"url":        provenance.LicenseURL,
					"repo":       "https://" + provenance.Repo,
					"accepted":   fmt.Sprintf("%t", rec.Accepted),
					"acceptedAt": rec.AcceptedAt,
					"acceptedBy": rec.AcceptedBy,
					"face":       rec.Face,
					"current":    fmt.Sprintf("%t", !stale && rec.Accepted),
					"binary":     Version,
				})
			}
			fmt.Println(provenance.Product)
			fmt.Printf("License:  %s %s\n", provenance.LicenseName, provenance.LicenseVersion)
			if rec.Accepted && !stale {
				fmt.Printf("Status:   accepted %s by %s (face: %s)\n", rec.AcceptedAt, rec.AcceptedBy, rec.Face)
			} else if rec.Accepted {
				fmt.Printf("Status:   accepted %s by %s — but this build carries %s %s; accept again\n",
					rec.AcceptedAt, rec.AcceptedBy, provenance.LicenseName, provenance.LicenseVersion)
			} else {
				fmt.Println("Status:   not accepted — run `s3b license accept`")
			}
			return nil
		},
	}

	var yes bool
	accept := &cobra.Command{
		Use:   "accept",
		Short: "Record acceptance of the license",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(provenance.Notice())
			if !yes {
				fmt.Printf("\nAccept the %s %s? [y/N] ", provenance.LicenseName, provenance.LicenseVersion)
				line, err := bufio.NewReader(os.Stdin).ReadString('\n')
				if err != nil && line == "" {
					return fmt.Errorf("no answer (non-interactive input?) — pass --yes to accept")
				}
				ans := strings.ToLower(strings.TrimSpace(line))
				if ans != "y" && ans != "yes" {
					return fmt.Errorf("not accepted")
				}
			}
			by, err := cliWhoami()
			if err != nil {
				by = ""
			}
			rec := appsettings.LicenseAcceptance{
				Accepted:   true,
				License:    provenance.LicenseName,
				Version:    provenance.LicenseVersion,
				AcceptedAt: time.Now().UTC().Format(time.RFC3339),
				AcceptedBy: by,
				Face:       "cli",
			}
			if err := appsettings.SaveLicense(rec); err != nil {
				return err
			}
			fmt.Println("Accepted. The license gate will not show on the next launch.")
			return nil
		},
	}
	accept.Flags().BoolVarP(&yes, "yes", "y", false, "accept without the interactive prompt")

	decline := &cobra.Command{
		Use:   "decline",
		Short: "Revoke the recorded acceptance",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := appsettings.SaveLicense(appsettings.LicenseAcceptance{}); err != nil {
				return err
			}
			fmt.Println("Acceptance revoked — the license gate shows again on the next launch.")
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show",
		Short: "Print the license identity block",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(provenance.Notice())
			return nil
		},
	}

	root.AddCommand(status, accept, decline, show)
	return root
}

// cliWhoami names the account that accepted — best effort, never fatal.
func cliWhoami() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	if v := os.Getenv("USERNAME"); v != "" {
		return v, nil
	}
	return os.Getenv("USER"), nil
}

// provenance_cmd.go: `s3b provenance` — the hidden provenance readout.
// Deliberately not listed in --help (cobra Hidden): anyone holding a
// binary can ask it whose software it is and get the creator/license
// identity compiled into the build, next to the build stamp.
package cli

import (
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/internal/provenance"
	"github.com/spf13/cobra"
)

func provenanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "provenance",
		Short:  "Print the software's creator and license identity",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				return printJSON(map[string]string{
					"product":    provenance.Product,
					"holder":     provenance.HolderFull,
					"year":       provenance.Year,
					"license":    provenance.LicenseName,
					"licensever": provenance.LicenseVersion,
					"repo":       provenance.Repo,
					"payload":    provenance.Payload,
					"version":    Version,
				})
			}
			fmt.Println(provenance.Notice())
			fmt.Println("Build:", Version)
			fmt.Println("Marker:", provenance.Encode(provenance.Payload))
			return nil
		},
	}
}

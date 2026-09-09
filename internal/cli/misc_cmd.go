package cli

import (
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/errhelp"
	"github.com/MikkoP88/s3-bucket-browser/pkg/doctor"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

// errAdvice looks up plain-language remediation for an error.
func errAdvice(err error) *errhelp.Advice {
	return errhelp.ForError(err)
}

func presignCmd() *cobra.Command {
	var expires string
	cmd := &cobra.Command{
		Use:   "presign s3://bucket/key",
		Short: "Generate a pre-signed GET URL for an object",
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
			if !u.HasPrefix || u.IsPrefix {
				return usageErr("presign needs an object key, not a bucket or folder")
			}
			ttl, err := parseIntDuration(expires)
			if err != nil {
				return usageErr("invalid --expires %q (use e.g. 15m, 12h, 7d)", expires)
			}
			presigner := s3.NewPresignClient(c.S3)
			req, err := presigner.PresignGetObject(cmd.Context(), &s3.GetObjectInput{
				Bucket: aws.String(u.Bucket), Key: aws.String(u.Key),
			}, s3.WithPresignExpires(ttl))
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{
					"url": req.URL, "expiresIn": ttl.String(), "method": req.Method,
				})
			}
			fmt.Println(req.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&expires, "expires", "15m", "URL validity (15m, 12h, 7d, ...)")
	return cmd
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [s3://bucket]",
		Short: "Diagnose connectivity: DNS, TCP, TLS, auth, bucket policy, ACL",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			bucket := ""
			if len(args) == 1 {
				u, err := parseS3URI(args[0])
				if err != nil {
					return err
				}
				if u.HasPrefix {
					return usageErr("doctor takes a bucket: s3://%s", u.Bucket)
				}
				bucket = u.Bucket
			}
			rep := doctor.Run(cmd.Context(), c, bucket, c.Profile.Insecure)
			if flagJSON {
				return printJSON(rep)
			}
			printDoctorReport(rep)
			if rep.ExitWorthy() {
				return opErr(fmt.Errorf("doctor found %d failing check(s)", rep.Summary.Fail))
			}
			return nil
		},
	}
}

func printDoctorReport(rep *doctor.Report) {
	col.bold.Printf("endpoint: %s\n", rep.Endpoint)
	fmt.Printf("provider: %s\n", rep.Provider)
	if rep.Bucket != "" {
		fmt.Printf("bucket:   %s\n", rep.Bucket)
	}
	fmt.Println()
	for _, chk := range rep.Checks {
		switch chk.Status {
		case doctor.StatusPass:
			col.ok.Printf("PASS  %s\n", chk.Check)
		case doctor.StatusWarn:
			col.warn.Printf("WARN  %s\n", chk.Check)
		case doctor.StatusFail:
			col.errf.Printf("FAIL  %s\n", chk.Check)
		default:
			col.dim.Printf("SKIP  %s\n", chk.Check)
		}
		detail := chk.Detail
		if chk.Error != "" {
			detail = chk.Error
		}
		if detail != "" {
			fmt.Printf("      %s\n", detail)
		}
		if chk.Advice != nil {
			col.dim.Printf("      hint: %s\n", chk.Advice.Suggestion)
			for _, c := range chk.Advice.Commands {
				col.dim.Printf("        - %s\n", c)
			}
		}
	}
	if len(rep.Warnings) > 0 {
		fmt.Println()
		for _, w := range rep.Warnings {
			col.warn.Printf("warning: %s\n", w)
		}
	}
	fmt.Println()
	fmt.Printf("summary: %d pass, %d warn, %d fail, %d skip\n",
		rep.Summary.Pass, rep.Summary.Warn, rep.Summary.Fail, rep.Summary.Skip)
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the s3b version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				return printJSON(map[string]string{"version": Version})
			}
			fmt.Println(Version)
			return nil
		},
	}
}

func guiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gui",
		Short: "Launch the desktop GUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("The desktop GUI ships in milestone M2.")
			fmt.Println("Until then the CLI face is fully usable — start with `s3b --help`.")
			return nil
		},
	}
}

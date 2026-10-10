// cat_cmd.go implements `s3b cat`: print one file's bytes to stdout —
// the read-without-download glance on the terminal's face. The operand
// routes the way edit's does (s3:// to the view profile, NAME:// into
// any saved source — S3-type sources dial their own client — and a
// plain path is a workstation file); folders refuse up front because a
// stream names one file.
package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func catCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cat s3://bucket/key | NAME://path | PATH",
		Short: "Print one file to stdout",
		Long: "Streams one file's bytes to stdout — nothing is written to disk, so\n" +
			"it pipes cleanly (s3b cat s3://logs/app.log | grep ERROR). Operands\n" +
			"route the way edit's do: s3://bucket/key reads the view profile,\n" +
			"NAME://path reads any saved data source (S3-type sources dial their\n" +
			"own client), and a plain path reads a workstation file. Folders\n" +
			"refuse: a stream names one file. Bytes cross unmodified — pair it\n" +
			"with your own tooling for anything fancier.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTransferURIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				return usageErr("--json does not apply to cat — it prints raw bytes")
			}
			return runCat(cmd.Context(), args[0])
		},
	}
	return cmd
}

// runCat routes the operand the way runEdit does, then streams: S3 legs
// ride the copy engine's checksum-validated open, source legs hold the
// engine's Stat-first folder refusal, local legs are plain files.
func runCat(ctx context.Context, arg string) error {
	if strings.HasPrefix(arg, "s3://") {
		u, err := parseS3URI(arg)
		if err != nil {
			return err
		}
		if u.IsPrefix || u.Key == "" {
			return usageErr("%s is a bucket or folder — cat names one file", arg)
		}
		c, err := resolveClient(ctx)
		if err != nil {
			return err
		}
		rc, _, err := s3OpenCLI(ctx, c, u.Bucket, u.Key)
		if err != nil {
			return opErr(err)
		}
		defer rc.Close()
		if _, err := io.Copy(out, rc); err != nil {
			return opErr(err)
		}
		return nil
	}
	if r, err := dialS3SourceURI(ctx, arg); err != nil {
		return err
	} else if r != nil {
		if r.folder || r.key == "" {
			return usageErr("%s is a bucket or folder — cat names one file", arg)
		}
		rc, _, err := s3OpenCLI(ctx, r.c, r.bucket, r.key)
		if err != nil {
			return opErr(err)
		}
		defer rc.Close()
		if _, err := io.Copy(out, rc); err != nil {
			return opErr(err)
		}
		return nil
	}
	if r, err := dialSourceURI(ctx, arg); err != nil {
		return err
	} else if r != nil {
		defer r.Close()
		st, err := r.fs.Stat(ctx, r.path)
		if err != nil {
			return opErr(err)
		}
		if st.IsDir {
			return usageErr("%s is a folder — cat names one file", arg)
		}
		rc, _, err := r.fs.Open(ctx, r.path)
		if err != nil {
			return opErr(err)
		}
		defer rc.Close()
		if _, err := io.Copy(out, rc); err != nil {
			return opErr(err)
		}
		return nil
	}
	// a plain path: a workstation file, streaming the way the remote
	// legs do (directories refuse with the same words)
	fi, err := os.Stat(arg)
	if err != nil {
		return opErr(err)
	}
	if fi.IsDir() {
		return usageErr("%s is a folder — cat names one file", arg)
	}
	f, err := os.Open(arg)
	if err != nil {
		return opErr(err)
	}
	defer f.Close()
	if _, err := io.Copy(out, f); err != nil {
		return opErr(err)
	}
	return nil
}

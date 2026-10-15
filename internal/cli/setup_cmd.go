// setup_cmd.go: `s3b setup` — the terminal face of installation. The
// Windows setup wizard (scripts/installer/s3b.nsi) is the same law with
// dialogs: one scope choice at install, the same choice honored at
// uninstall. These commands install the running binary for the current
// user or the whole system, uninstall by the same selection, and report
// where s3b currently lives — including installs the wizard made.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/MikkoP88/s3-bucket-browser/pkg/setup"
	"github.com/spf13/cobra"
)

func setupCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "setup",
		Short: "Install or remove s3b by scope: current user or all users",
		Long: "setup installs the running binary into a stable seat and removes it the same way.\n" +
			"  s3b setup install --user     current user only — no administrator needed (default)\n" +
			"  s3b setup install --system   all users — needs an elevated terminal (sudo on unix)\n" +
			"  s3b setup uninstall          removes the install (the same selection it was made with)\n" +
			"  s3b setup status             where s3b lives now, and which seat PATH resolves to\n\n" +
			"On Windows the seat registers with App Paths, so Win+R and ShellExecute find s3b\n" +
			"without touching PATH. S3B_SETUP_USER_DIR relocates user installs (portable use, CI).",
	}
	root.AddCommand(setupInstallCmd(), setupUninstallCmd(), setupStatusCmd())
	return root
}

// scopeFlags binds the --user/--system pair shared by install and
// uninstall: exactly one of them, or the caller's default.
func scopeFlags(cmd *cobra.Command, user, system *bool) {
	cmd.Flags().BoolVar(user, "user", false, "the current user's seat")
	cmd.Flags().BoolVar(system, "system", false, "all users (needs elevation)")
}

func pickScope(user, system bool, def setup.Scope) (setup.Scope, error) {
	switch {
	case user && system:
		return "", usageErr("pick one scope: --user or --system")
	case user:
		return setup.User, nil
	case system:
		return setup.System, nil
	default:
		return def, nil
	}
}

func setupOpts() *setup.Options {
	return &setup.Options{Redirect: os.Getenv("S3B_SETUP_USER_DIR")}
}

func setupInstallCmd() *cobra.Command {
	var user, system bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the running s3b binary (default: current user)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := pickScope(user, system, setup.User)
			if err != nil {
				return err
			}
			o := setupOpts()
			seat, err := setup.Install(o, sc)
			if errors.Is(err, setup.ErrSameBinary) {
				fmt.Printf("already installed at %s\n", seat.Exe)
				return nil
			}
			if err != nil {
				return opErr(err)
			}
			col.ok.Printf("installed: %s (%s scope)\n", seat.Exe, sc)
			if seat.Dir != "" {
				if o.Redirect != "" && sc == setup.User {
					col.dim.Printf("portable seat: shell registration skipped (S3B_SETUP_USER_DIR)\n")
				} else {
					col.dim.Printf("registered with the shell (App Paths)\n")
				}
			}
			// The honest follow-up: can a bare `s3b` actually reach this seat?
			if st, perr := setup.Probe(o); perr == nil && st.OnPath != sc {
				col.dim.Printf("note: %s\n", setup.Advice(o, seat.Dir))
			}
			return nil
		},
	}
	scopeFlags(cmd, &user, &system)
	return cmd
}

func setupUninstallCmd() *cobra.Command {
	var user, system bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the installed s3b (the scope it was installed with)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			o := setupOpts()
			def := setup.Scope("")
			if !user && !system {
				// The uninstall follows the installation selection: with
				// no flag given, whichever scope is installed is the one
				// to remove (user first when both are).
				if st, err := setup.Probe(o); err == nil {
					switch {
					case st.User.Installed:
						def = setup.User
					case st.System.Installed:
						def = setup.System
					}
				}
				if def == "" {
					return opErr(errors.New("no s3b install found for either scope — nothing to remove"))
				}
			}
			sc, err := pickScope(user, system, def)
			if err != nil {
				return err
			}
			rep, err := setup.Uninstall(o, sc)
			if err != nil {
				return opErr(err)
			}
			col.ok.Printf("removed:  %s\n", rep.RemovedExe)
			if rep.RemovedDir {
				col.dim.Printf("removed the empty install dir %s\n", rep.Dir)
			}
			if rep.RegisteredLeft != "" {
				col.warn.Printf("shell registration still points at %s (another install — the setup wizard's) and was left in place\n", rep.RegisteredLeft)
			}
			return nil
		},
	}
	scopeFlags(cmd, &user, &system)
	return cmd
}

func setupStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report where s3b is installed and which seat PATH resolves to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			o := setupOpts()
			st, err := setup.Probe(o)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(st)
			}
			col.bold.Println("s3b setup status")
			for _, seat := range []struct {
				label string
				s     setup.Seat
			}{{"user", st.User}, {"system", st.System}} {
				if seat.s.Installed {
					marker := ""
					if st.OnPath == seat.s.Scope {
						marker = " — first on PATH"
					}
					fmt.Printf("  %-7s %s%s\n", seat.label+":", seat.s.Exe, marker)
				} else {
					fmt.Printf("  %-7s not installed\n", seat.label+":")
				}
				if seat.s.Registered != "" && seat.s.Registered != seat.s.Exe {
					col.dim.Printf("          shell registration points at %s (another install)\n", seat.s.Registered)
				}
			}
			if o.Redirect != "" && st.User.Redirected {
				col.dim.Printf("  portable: user seat redirected to %s (S3B_SETUP_USER_DIR)\n", st.User.Dir)
			}
			if st.OnPath == "" {
				col.dim.Printf("  PATH does not resolve to either seat — call s3b by full path, or %s\n", setup.Advice(o, st.User.Dir))
			}
			return nil
		},
	}
}

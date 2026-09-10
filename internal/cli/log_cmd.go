// log_cmd.go: `s3b log` — the M10.5 CLI face of the activity log. The GUI
// appends every drawer line to events.jsonl under the config dir; this
// command tails it with level/scope filters and an optional follow mode.
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
	"github.com/spf13/cobra"
)

func logCmd() *cobra.Command {
	var (
		lines    int
		level    string
		scope    string
		follow   bool
		interval time.Duration
	)
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show the activity log (GUI events: transfers, deletes, doctor runs)",
		Long: "Prints the persisted activity log (events.jsonl in the config dir) —\n" +
			"the same lines the GUI log drawer shows, kept across sessions.\n" +
			"--follow keeps watching for new lines.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if level != "" && level != "info" && level != "warn" && level != "error" {
				return usageErr("invalid --level %q (info, warn or error)", level)
			}
			if follow && flagJSON {
				return usageErr("--follow is interactive; drop it (or --json)")
			}
			entries, err := eventlog.Tail(lines, level, scope)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(entries)
			}
			for _, l := range entries {
				printLogLine(l)
			}
			if !follow {
				if len(entries) == 0 {
					col.dim.Println("(no events yet — GUI activity lands here)")
				}
				return nil
			}
			return followLog(level, scope, interval)
		},
	}
	f := cmd.Flags()
	f.IntVarP(&lines, "lines", "n", 50, "show the last N lines (0 = all)")
	f.StringVar(&level, "level", "", "filter by level: info, warn, error")
	f.StringVar(&scope, "scope", "", "filter by scope prefix (transfer, doctor, ...)")
	f.BoolVarP(&follow, "follow", "f", false, "keep watching for new lines")
	f.DurationVar(&interval, "interval", time.Second, "poll interval in --follow")
	return cmd
}

func printLogLine(l eventlog.Line) {
	ts := l.Time.Local().Format("15:04:05")
	switch l.Level {
	case "error":
		col.errf.Printf("%s ERR  %-9s %s\n", ts, l.Scope, l.Message)
	case "warn":
		col.warn.Printf("%s WARN %-9s %s\n", ts, l.Scope, l.Message)
	default:
		fmt.Printf("%s info %-9s %s\n", ts, l.Scope, l.Message)
	}
}

// followLog polls the log and prints lines appended after start, until
// Ctrl+C. Resilient to rotation (the file is re-opened each poll).
func followLog(level, scope string, interval time.Duration) error {
	seen := time.Now()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	fmt.Fprintln(os.Stderr, "following — Ctrl+C to stop")
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			entries, err := eventlog.Tail(0, level, scope)
			if err != nil {
				continue // transient — keep watching
			}
			for _, l := range entries {
				if l.Time.After(seen) {
					printLogLine(l)
				}
			}
			if n := len(entries); n > 0 {
				seen = entries[n-1].Time
			}
		}
	}
}

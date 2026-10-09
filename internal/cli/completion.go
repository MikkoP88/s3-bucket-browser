// completion.go: typed TAB completion for the CLI's own vocabulary.
// Every completer is offline: it reads the saved stores and the verbs'
// static enums, never dials a wire (a completion that stalls the shell is
// worse than none), and stays silent on a store it cannot read — the
// command itself will speak when it runs. The loader scripts are cobra's
// own `s3b completion bash|zsh|fish|powershell`.
package cli

import (
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/spf13/cobra"
)

// loadForCompletion reads the store read-only for one completion pass —
// no seeding migration, no save, no error speech.
func loadForCompletion() *profile.Store {
	s, err := profile.Load()
	if err != nil {
		return nil
	}
	return s
}

func completeSourceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	s := loadForCompletion()
	if s == nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, src := range s.SortedSources() {
		if strings.HasPrefix(src.Name, toComplete) {
			names = append(names, src.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	s := loadForCompletion()
	if s == nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, p := range s.Sorted() {
		if strings.HasPrefix(p.Name, toComplete) {
			names = append(names, p.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeSourceURIs offers the browsing operand grammar: s3:// plus every
// saved non-S3 source as NAME:// (ls, tree, du, stat, find, mkdir, rm —
// the commands whose URI leg is source-only). Later positionals belong to
// the shell's own file completion.
func completeSourceURIs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	s := loadForCompletion()
	if s == nil {
		return nil, cobra.ShellCompDirectiveError
	}
	cands := []string{"s3://"}
	for _, src := range s.SortedSources() {
		if src.Type != profile.TypeS3 {
			cands = append(cands, src.Name+"://")
		}
	}
	return filterComps(cands, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeTransferURIs offers the wider operand grammar cp/mv/sync/edit
// speak: s3:// plus EVERY saved source as NAME:// (S3 sources included —
// a per-bucket source scopes the path, an account-wide one reads the
// bucket from the first segment), both positions (SRC and DST alike).
// Local file operands stay shell's own: Default lets the shell keep
// completing filenames beside the candidates.
func completeTransferURIs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	s := loadForCompletion()
	if s == nil {
		return nil, cobra.ShellCompDirectiveError
	}
	cands := []string{"s3://"}
	for _, src := range s.SortedSources() {
		cands = append(cands, src.Name+"://")
	}
	return filterComps(cands, toComplete), cobra.ShellCompDirectiveDefault
}

// completeS3URIs offers the s3:// prefix for the S3-only verbs (sc,
// presign, doctor, versions, the admin and lock targets). Later
// positionals (the put verbs' FILE operand) go back to the shell.
func completeS3URIs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return filterComps([]string{"s3://"}, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeScArgs rides sc's two-position shape: the s3:// target first,
// the storage class second.
func completeScArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeS3URIs(cmd, args, toComplete)
	}
	return completeStorageClasses(cmd, args, toComplete)
}

// completeStorageClasses offers sc's CLASS operand.
func completeStorageClasses(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterComps(transfer.ValidStorageClasses, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeRetentionModes offers lock retention's --mode vocabulary.
func completeRetentionModes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterComps([]string{"GOVERNANCE", "COMPLIANCE"}, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeSourceTypes offers `source add --type`'s vocabulary — the same
// slice the validation accepts, one law.
func completeSourceTypes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterComps(sourceTypes, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filterComps(cands []string, toComplete string) []string {
	var out []string
	for _, c := range cands {
		if strings.HasPrefix(c, toComplete) {
			out = append(out, c)
		}
	}
	return out
}

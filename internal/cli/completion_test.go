// completion_test.go pins the typed TAB completions through the very
// protocol cobra's loader scripts speak (__complete): source and profile
// names, the operand URI grammars, and the static enums — all offline,
// all silent on a store they cannot read.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completeLines runs one __complete query and splits the protocol's
// answer: the candidate lines, then the ":<directive>" footer.
func completeLines(t *testing.T, args ...string) ([]string, int) {
	t.Helper()
	out, code := captureOut(t, append([]string{"__complete"}, args...)...)
	if code != 0 {
		t.Fatalf("__complete %v: exit %d (%s)", args, code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	last := lines[len(lines)-1]
	d := -1
	if _, err := fmt.Sscanf(last, ":%d", &d); err != nil {
		t.Fatalf("__complete %v: no directive footer in %q", args, out)
	}
	if len(lines) == 1 {
		return nil, d
	}
	return lines[:len(lines)-1], d
}

func TestCompletionSourceAndProfileNames(t *testing.T) {
	srcEnv(t)

	names, d := completeLines(t, "source", "test", "")
	if d != 4 { // ShellCompDirectiveNoFileComp
		t.Fatalf("source test directive = %d, want 4", d)
	}
	if len(names) != 1 || names[0] != "lab" {
		t.Fatalf("source test candidates = %v, want [lab]", names)
	}
	names, _ = completeLines(t, "source", "remove", "")
	if len(names) != 1 || names[0] != "lab" {
		t.Fatalf("source remove candidates = %v, want [lab]", names)
	}

	// An empty profile store completes nothing, without a sound.
	names, d = completeLines(t, "profile", "test", "")
	if d != 4 || len(names) != 0 {
		t.Fatalf("profile test on an empty store: %v (directive %d), want silence", names, d)
	}

	// The persistent --profile flag rides the same vocabulary.
	names, d = completeLines(t, "--profile", "")
	if d != 4 || len(names) != 0 {
		t.Fatalf("--profile on an empty store: %v (directive %d), want silence", names, d)
	}
}

func TestCompletionURIGrammars(t *testing.T) {
	srcEnv(t)

	// The browsing grammar: s3:// plus every non-S3 source.
	cands, d := completeLines(t, "ls", "")
	if d != 4 {
		t.Fatalf("ls directive = %d, want 4", d)
	}
	joined := strings.Join(cands, " ")
	if !strings.Contains(joined, "s3://") || !strings.Contains(joined, "lab://") {
		t.Fatalf("ls candidates = %v, want s3:// and lab://", cands)
	}

	// Prefix typing narrows to the one source.
	cands, _ = completeLines(t, "ls", "la")
	if len(cands) != 1 || cands[0] != "lab://" {
		t.Fatalf("ls la candidates = %v, want [lab://]", cands)
	}
	cands, _ = completeLines(t, "ls", "s3")
	if len(cands) != 1 || cands[0] != "s3://" {
		t.Fatalf("ls s3 candidates = %v, want [s3://]", cands)
	}

	// find rides the same operand grammar.
	cands, _ = completeLines(t, "find", "")
	joined = strings.Join(cands, " ")
	if !strings.Contains(joined, "s3://") || !strings.Contains(joined, "lab://") {
		t.Fatalf("find candidates = %v, want s3:// and lab://", cands)
	}

	// The S3-only verbs offer the s3:// prefix alone.
	cands, d = completeLines(t, "presign", "")
	if d != 4 || len(cands) != 1 || cands[0] != "s3://" {
		t.Fatalf("presign candidates = %v (directive %d), want [s3://] no-file", cands, d)
	}
	cands, _ = completeLines(t, "versions", "ls", "")
	if len(cands) != 1 || cands[0] != "s3://" {
		t.Fatalf("versions ls candidates = %v, want [s3://]", cands)
	}

	// The admin leaves inherit the s3:// grammar from the one walk.
	cands, _ = completeLines(t, "bucket", "info", "")
	if len(cands) != 1 || cands[0] != "s3://" {
		t.Fatalf("bucket info candidates = %v, want [s3://]", cands)
	}
	cands, _ = completeLines(t, "bucket", "policy", "put", "")
	if len(cands) != 1 || cands[0] != "s3://" {
		t.Fatalf("bucket policy put candidates = %v, want [s3://]", cands)
	}

	// The transfer grammar keeps the shell's file completion alive
	// beside the URI candidates (Default directive).
	_, d = completeLines(t, "cp", "")
	if d != 0 {
		t.Fatalf("cp directive = %d, want 0 (Default — local files stay completable)", d)
	}
	_, d = completeLines(t, "sync", "")
	if d != 0 {
		t.Fatalf("sync directive = %d, want 0", d)
	}

	// Later positionals go back to the shell (the put verbs' FILE arm).
	_, d = completeLines(t, "bucket", "policy", "put", "s3://b", "")
	if d != 0 {
		t.Fatalf("policy put FILE directive = %d, want 0 (shell files)", d)
	}
}

func TestCompletionStaticEnums(t *testing.T) {
	cliEnv(t)

	// sc's second position is the storage-class vocabulary, prefix-typed.
	cands, d := completeLines(t, "sc", "s3://b/docs", "GLA")
	if d != 4 {
		t.Fatalf("sc class directive = %d, want 4", d)
	}
	if strings.Join(cands, ",") != "GLACIER,GLACIER_IR" {
		t.Fatalf("sc GLA candidates = %v, want [GLACIER GLACIER_IR]", cands)
	}

	// lock retention's --mode vocabulary.
	cands, d = completeLines(t, "lock", "retention", "s3://b/k", "--mode", "")
	if d != 4 || strings.Join(cands, ",") != "GOVERNANCE,COMPLIANCE" {
		t.Fatalf("--mode candidates = %v (directive %d)", cands, d)
	}

	// source add --type offers exactly the list the validation accepts.
	cands, d = completeLines(t, "source", "add", "--type", "")
	if d != 4 || strings.Join(cands, ",") != strings.Join(sourceTypes, ",") {
		t.Fatalf("--type candidates = %v (directive %d), want %v", cands, d, sourceTypes)
	}
}

func TestCompletionOfflineAndSilent(t *testing.T) {
	// A corrupt store: the completions stay silent and error-directived
	// — they never dial, never diagnose; the command itself will speak.
	cliEnv(t)
	if err := os.WriteFile(filepath.Join(os.Getenv("S3B_CONFIG"), "profiles.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, d := completeLines(t, "source", "test", "")
	if d != 1 || len(names) != 0 { // ShellCompDirectiveError
		t.Fatalf("broken-store candidates = %v (directive %d), want silence + error directive", names, d)
	}
	// A readable store keeps completing.
	srcEnv(t)
	names, d = completeLines(t, "source", "test", "")
	if d != 4 || len(names) != 1 || names[0] != "lab" {
		t.Fatalf("readable-store candidates = %v (directive %d)", names, d)
	}
}

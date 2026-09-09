package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// helpText returns what an operator reads for the given command path.
func helpText(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errOut := executeCmdOutput(t, append(args, "--help")...)
	if code != cli.ExitOK {
		t.Fatalf("--help exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	return out
}

// TestRootHelp_NamesEveryCommandTakingTheDataFlags pins B35. The root listed
// "check, load, export" as the commands accepting --from while `snapshot save`
// accepts it too, and never named --type or --type-column at all — so the two
// flags a CSV input requires appeared in no overview.
func TestRootHelp_NamesEveryCommandTakingTheDataFlags(t *testing.T) {
	t.Parallel()

	out := helpText(t)

	// The commands that register --from, read from the tree rather than listed
	// here: check, load, export and snapshot save.
	if !strings.Contains(out, "snapshot save") {
		t.Errorf("the data-flag list omits `snapshot save`, which registers --from:\n%s", out)
	}
	for _, flag := range []string{"--type", "--type-column"} {
		if !strings.Contains(out, flag) {
			t.Errorf("the root help never names %s, which a CSV input requires:\n%s", flag, out)
		}
	}
}

// TestSnapshotSaveHelp_OutputIsNotUnconditionallyRequired pins B36. --output
// read "(required)" while `--into` alone satisfies the destination check, so
// the help refused a working invocation the command accepts.
func TestSnapshotSaveHelp_OutputIsNotUnconditionallyRequired(t *testing.T) {
	t.Parallel()

	out := helpText(t, "snapshot", "save")

	line := lineContaining(t, out, "--output")
	if strings.Contains(line, "(required)") {
		t.Errorf("--output is still called unconditionally required: %q", strings.TrimSpace(line))
	}
	if !strings.Contains(line, "--into") {
		t.Errorf("--output's help does not name the flag that makes it optional: %q", strings.TrimSpace(line))
	}
}

// TestSnapshotSaveHelp_MatchesTheCommandsOwnCheck is the second implementation
// of the assertion above: the help is measured against what runSnapshotSave
// actually accepts, not against a remembered string.
func TestSnapshotSaveHelp_MatchesTheCommandsOwnCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base, more := saveInto(t)

	// --into alone, no --output: the invocation the old help called invalid.
	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("--into alone exit code = %d, want %d — the help's claim is the one under test\nstderr:\n%s",
			code, cli.ExitOK, errOut)
	}

	// Neither flag: still refused, so "required" is not simply dropped.
	code, _, _ = executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", filepath.Join(dir, "unused.json"))
	if code != cli.ExitUsage {
		t.Errorf("with neither --output nor --into the exit code = %d, want %d", code, cli.ExitUsage)
	}
}

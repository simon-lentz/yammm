package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestRun_LabelFlagsAreRefusedBeforeWork: a --prefix or --separator whose
// composed label is not a Neo4j identifier is a usage error every command taking the flags reports before it loads a schema or
// opens a connection. The schema carries a load warning, so a load that ran
// before the refusal shows on stderr.
func TestRun_LabelFlagsAreRefusedBeforeWork(t *testing.T) {
	commands := map[string][]string{
		"neo4j constraints": {"neo4j", "constraints", shadowedSchema},
		"neo4j indexes":     {"neo4j", "indexes", shadowedSchema},
		"neo4j diff":        {"neo4j", "diff", "--uri", "bolt://127.0.0.1:1", shadowedSchema},
	}
	flags := map[string][]string{
		"a separator that is not identifier text": {"--separator", "::"},
		"a prefix that is not identifier text":    {"--prefix", "1bad "},
	}

	for cmdName, base := range commands {
		for flagName, flag := range flags {
			t.Run(cmdName+", "+flagName, func(t *testing.T) {
				code, stdout, stderr := runCLI(t, append(append([]string{}, base...), flag...)...)
				var failure string
				switch {
				case code != cli.ExitUsage:
					failure = fmt.Sprintf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
				case stdout != "" || strings.Contains(stderr, "W_ANNOTATION_SHADOWED"):
					failure = "work was done before the flags were refused; stdout:\n" + stdout + "\nstderr:\n" + stderr
				case !strings.Contains(stderr, "--"+flag[0][2:]):
					failure = "the refusal does not name the flag; stderr:\n" + stderr
				}
				checkRepairState(t, "label flags: "+cmdName+", "+flagName, failure)
			})
		}
	}
}

// An empty --separator composes each label from the schema and type names
// alone. Two types that then compose one label are the library's
// E_NEO4J_LABEL_COLLISION, raised at the types.
func TestRun_AnEmptySeparatorComposesLabelsWithNone(t *testing.T) {
	code, stdout, stderr := runCLI(t, "neo4j", "constraints", "--separator", "", "testdata/valid.yammm")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitOK, stderr)
	}
	if !strings.Contains(stdout, "FOR (n:testPerson)") {
		t.Errorf("stdout does not label Person as testPerson:\n%s", stdout)
	}
}

// TestRun_OnlyDiffTakesTheConnectionFlags: `neo4j constraints` and `neo4j
// indexes` open no connection, so each refuses a connection flag as unknown
// rather than accepting a value it would ignore.
func TestRun_OnlyDiffTakesTheConnectionFlags(t *testing.T) {
	for _, command := range []string{"constraints", "indexes"} {
		for _, flag := range []string{"--uri", "--username", "--password", "--database"} {
			t.Run(command+" "+flag, func(t *testing.T) {
				code, stdout, stderr := runCLI(t, "neo4j", command, flag, "x", "testdata/valid.yammm")
				if code != cli.ExitUsage {
					t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
				}
				if !strings.Contains(stderr, "unknown flag: "+flag) {
					t.Errorf("stderr does not refuse %s as unknown:\n%s", flag, stderr)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want nothing", stdout)
				}
			})
		}
	}
}

// TestRun_DiffRefusesAMissingURIBeforeLoading: without --uri or
// YAMMM_NEO4J_URI, `neo4j diff` says so as a usage error before it loads the
// schema, whose load warning would otherwise reach stderr.
func TestRun_DiffRefusesAMissingURIBeforeLoading(t *testing.T) {
	t.Setenv("YAMMM_NEO4J_URI", "")
	code, stdout, stderr := runCLI(t, "neo4j", "diff", shadowedSchema)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "--uri is required") || strings.Contains(stderr, "W_ANNOTATION_SHADOWED") {
		t.Errorf("want the --uri refusal before any load; stderr:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

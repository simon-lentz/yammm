package main

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestRun_LabelFlagsAreJudgedOverTheLoadedSchema: a --prefix or --separator
// whose labels break the label rule is refused by [cypher.Config.CheckLabels]
// over the loaded schema, with E_NEO4J_INVALID_IDENTIFIER, before gen writes a
// statement and before neo4j diff connects.
func TestRun_LabelFlagsAreJudgedOverTheLoadedSchema(t *testing.T) {
	commands := map[string][]string{
		"gen --to cypher": {"gen", "--to", "cypher", "testdata/valid.yammm"},
		"neo4j diff":      {"neo4j", "diff", "--uri", "bolt://127.0.0.1:1", "testdata/valid.yammm"},
	}
	flags := map[string][]string{
		"a separator that is not identifier text": {"--separator", "::"},
		"a prefix that is not identifier text":    {"--prefix", "1bad "},
	}

	for cmdName, base := range commands {
		for flagName, flag := range flags {
			t.Run(cmdName+", "+flagName, func(t *testing.T) {
				code, stdout, stderr := runCLI(t, append(append([]string{}, base...), flag...)...)
				if code != cli.ExitValidation {
					t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitValidation, stderr)
				}
				// Both generators judge the labels; the setting is reported once.
				if strings.Count(stderr, "E_NEO4J_INVALID_IDENTIFIER") != 1 || !strings.Contains(stderr, "compose no node label") {
					t.Errorf("stderr does not report the label setting once:\n%s", stderr)
				}
				if strings.Contains(stderr, "connect") {
					t.Errorf("neo4j diff connected before it judged the labels:\n%s", stderr)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want nothing", stdout)
				}
			})
		}
	}
}

// An empty --separator is the default separator, as an empty
// cypher.Config.LabelSeparator is.
func TestRun_AnEmptySeparatorIsTheDefault(t *testing.T) {
	code, stdout, stderr := runCLI(t, "gen", "--to", "cypher", "--separator", "", "testdata/valid.yammm")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitOK, stderr)
	}
	if !strings.Contains(stdout, "FOR (n:test__Person)") {
		t.Errorf("stdout does not label Person as test__Person:\n%s", stdout)
	}
}

// TestRun_OnlyDiffTakesTheConnectionFlags: gen opens no connection, so it
// refuses a connection flag as unknown rather than accepting a value it would
// ignore.
func TestRun_OnlyDiffTakesTheConnectionFlags(t *testing.T) {
	for _, flag := range []string{"--uri", "--username", "--password", "--database"} {
		t.Run(flag, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "gen", "--to", "cypher", flag, "x", "testdata/valid.yammm")
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

// TestRun_DiffJudgesTheURIBeforeAnyRead: a --uri that names no server on its
// text is a usage error judged right after the required check, so the exit is
// 2 whatever the schema holds, and nothing dials.
func TestRun_DiffJudgesTheURIBeforeAnyRead(t *testing.T) {
	uris := []string{"bogus://127.0.0.1", "bolt://", "bolt://127.0.0.1:", "bolt://127.0.0.1:99999", "bolt+unix://"}
	schemas := map[string]string{
		"a valid schema":             "testdata/valid.yammm",
		"a schema that is invalid":   "testdata/invalid.yammm",
		"a schema that is not there": filepath.Join(t.TempDir(), "missing.yammm"),
	}
	for _, uri := range uris {
		for schemaName, schemaPath := range schemas {
			t.Run(uri+", "+schemaName, func(t *testing.T) {
				code, stdout, stderr := runCLI(t, "neo4j", "diff", "--uri", uri, schemaPath)
				if code != cli.ExitUsage {
					t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
				}
				if want := "connect to neo4j at " + strconv.Quote(uri); !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
				if strings.Contains(stderr, "[E_") || strings.Contains(stderr, "verify neo4j connectivity") {
					t.Errorf("neo4j diff read the schema or dialed before it judged the URI:\n%s", stderr)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want nothing", stdout)
				}
			})
		}
	}
}

// TestRun_DiffVerifiesTheServerBeforeItQueries pins that `neo4j diff` asks
// the server whether it answers before its first query, so an unreachable one
// fails at once, naming the check, and not after the driver's retry window.
func TestRun_DiffVerifiesTheServerBeforeItQueries(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	uri := "bolt://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}

	code, stdout, stderr := runCLI(t, "neo4j", "diff", "--uri", uri, "testdata/valid.yammm")
	if code != cli.ExitRuntime {
		t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitRuntime, stderr)
	}
	if want := "verify neo4j connectivity at " + strconv.Quote(uri); !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

// TestRun_DiffJudgesTheIndexesBeforeConnecting: a schema whose constraints
// generate and whose indexes do not is refused before `neo4j diff` connects, so
// an unreachable server never decides the exit code.
func TestRun_DiffJudgesTheIndexesBeforeConnecting(t *testing.T) {
	code, stdout, stderr := runCLI(t, "neo4j", "diff", "--uri", "bolt://127.0.0.1:1", "--type-constraints", "none", "testdata/reserved_index.yammm")
	if code != cli.ExitValidation {
		t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitValidation, stderr)
	}
	if !strings.Contains(stderr, "E_NEO4J_INVALID_IDENTIFIER") {
		t.Errorf("stderr does not report the index refusal:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

// TestRun_TypeConstraintsIsReadInAnyCase: --type-constraints none emits no
// type constraint, in any case, and keeps every other constraint.
func TestRun_TypeConstraintsIsReadInAnyCase(t *testing.T) {
	for _, value := range []string{"none", "NONE"} {
		code, stdout, stderr := runCLI(t, "gen", "--to", "cypher", "--type-constraints", value, "testdata/valid.yammm")
		if code != cli.ExitOK {
			t.Fatalf("%s: exit code = %d, want %d; stderr:\n%s", value, code, cli.ExitOK, stderr)
		}
		if strings.Contains(stdout, "IS ::") {
			t.Errorf("%s: stdout holds a type constraint:\n%s", value, stdout)
		}
		if !strings.Contains(stdout, "CREATE CONSTRAINT") {
			t.Errorf("%s: stdout holds no constraint:\n%s", value, stdout)
		}
	}
}

// TestRun_EditionIsReadInAnyCase: --edition takes its value in any case.
func TestRun_EditionIsReadInAnyCase(t *testing.T) {
	lower, wantOut, stderr := runCLI(t, "gen", "--to", "cypher", "--edition", "community", "testdata/valid.yammm")
	if lower != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", lower, cli.ExitOK, stderr)
	}
	code, stdout, stderr := runCLI(t, "gen", "--to", "cypher", "--edition", "Community", "testdata/valid.yammm")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitOK, stderr)
	}
	if stdout != wantOut {
		t.Errorf("--edition Community emits otherwise than --edition community:\n%s\nwant:\n%s", stdout, wantOut)
	}
}

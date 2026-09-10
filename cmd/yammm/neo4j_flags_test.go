package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestRun_LabelFlagsAreRefusedBeforeWork: a --prefix or --separator whose
// composed label is not a Neo4j identifier, or an empty separator, is a usage
// error every command taking the flags reports before it loads a schema or
// opens a connection.
func TestRun_LabelFlagsAreRefusedBeforeWork(t *testing.T) {
	commands := map[string][]string{
		"neo4j constraints":  {"neo4j", "constraints", "testdata/valid.yammm"},
		"neo4j indexes":      {"neo4j", "indexes", "testdata/valid.yammm"},
		"neo4j diff":         {"neo4j", "diff", "--uri", "bolt://127.0.0.1:1", "testdata/valid.yammm"},
		"neo4j introspect":   {"neo4j", "introspect", "--uri", "bolt://127.0.0.1:1"},
		"export --to cypher": {"export", "--to", "cypher", "testdata/valid.yammm", "testdata/data.json"},
	}
	flags := map[string][]string{
		"an empty separator":                      {"--separator", ""},
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
				case stdout != "":
					failure = "work was done before the flags were refused; stdout:\n" + stdout
				case !strings.Contains(stderr, "--"+flag[0][2:]):
					failure = "the refusal does not name the flag; stderr:\n" + stderr
				}
				checkRepairState(t, "label flags: "+cmdName+", "+flagName, failure)
			})
		}
	}
}

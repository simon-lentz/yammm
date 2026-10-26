package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// docs/SPEC.md says `yammm neo4j constraints` rejects a list whose element is
// itself a collection with E_NEO4J_UNSUPPORTED_TYPE and emits nothing for the
// whole schema. No flag gives Neo4j a nested collection type, so the refusal
// holds under each one, and `neo4j diff`, whose desired side is the same
// emitter's output, refuses the schema before it reaches for a server: the
// address here answers nothing, and a command that dialled it would exit 3.
//
// Driven through runCLI because the exit code is what run() derives; it swaps
// process streams, so nothing here runs in parallel.
func TestNeo4jCommands_RefuseANestedCollectionUnderEveryFlag(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	nested := write("nested.yammm", "schema \"g\"\n\ntype P {\n\tid String primary\n\tm List<List<Integer>>\n\tv List<Vector[2]>\n}\n\ntype Q {\n\tid String primary\n}\n")
	unlabelled := write("unlabelled.yammm", "schema \"ga\"\n\nabstract type Q {\n\tid String primary\n\tm List<List<Integer>>\n}\n\ntype R {\n\tid String primary\n}\n")

	flagSets := [][]string{
		nil,
		{"--scalar-types=false"},
		{"--scalar-types=false", "--required-only-types"},
		{"--scalar-types=false", "--edition", "community"},
		{"--scalar-types=false", "--node-keys", "--named=false"},
	}
	for _, flags := range flagSets {
		for _, command := range [][]string{
			{"neo4j", "constraints"},
			{"neo4j", "diff", "--uri", "neo4j://127.0.0.1:1"},
			{"neo4j", "diff", "--uri", "neo4j://127.0.0.1:1", "--indexes=false"},
		} {
			args := append(append(append([]string{}, command...), flags...), nested)
			t.Run(strings.Join(args[:len(args)-1], " "), func(t *testing.T) {
				code, stdout, stderr := runCLI(t, args...)
				if code != cli.ExitValidation {
					t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitValidation, stderr)
				}
				if stdout != "" {
					t.Errorf("stdout holds %q, want nothing for the whole schema", stdout)
				}
				for _, property := range []string{`"m"`, `"v"`} {
					if !strings.Contains(stderr, "E_NEO4J_UNSUPPORTED_TYPE") || !strings.Contains(stderr, "property "+property) {
						t.Errorf("stderr does not refuse property %s with E_NEO4J_UNSUPPORTED_TYPE:\n%s", property, stderr)
					}
				}
			})
		}

		// A type with no label carries its properties to no node.
		args := append(append([]string{"neo4j", "constraints"}, flags...), unlabelled)
		t.Run("no label: "+strings.Join(flags, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(t, args...)
			if code != cli.ExitOK || !strings.Contains(stdout, "ga__R") || strings.Contains(stderr, "E_NEO4J_UNSUPPORTED_TYPE") {
				t.Errorf("exit code = %d, want %d and the labelled type's constraints; stdout:\n%s\nstderr:\n%s",
					code, cli.ExitOK, stdout, stderr)
			}
		})
	}
}

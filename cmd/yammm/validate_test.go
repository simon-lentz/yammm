package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func executeCmd(t *testing.T, args ...string) int {
	t.Helper()
	code, _ := executeCmdStderr(t, args...)
	return code
}

// executeCmdStderr runs the CLI in-process and returns the exit code plus
// captured stderr, for assertions on rendered diagnostics.
func executeCmdStderr(t *testing.T, args ...string) (int, string) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	cmd := newRootCmd("test")
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	t.Cleanup(func() {
		if t.Failed() {
			if outBuf.Len() > 0 {
				t.Logf("stdout:\n%s", outBuf.String())
			}
			if errBuf.Len() > 0 {
				t.Logf("stderr:\n%s", errBuf.String())
			}
		}
	})

	if err := cmd.Execute(); err != nil {
		if exitErr, ok := errors.AsType[*cli.ExitError](err); ok {
			return exitErr.Code, errBuf.String()
		}
		return cli.ExitUsage, errBuf.String()
	}

	return cli.ExitOK, errBuf.String()
}

// TestExitCodes pins the exact exit-code contract — usage(2) vs
// validation(1) vs runtime(3) — for every argument-only failure class.
// testscript can only distinguish success from failure, so this table is
// the in-process complement to testdata/script/*.txtar: the scripts own
// output and side-effect behavior, this table owns WHICH code.
func TestExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"validate invalid schema", []string{"validate", "testdata/invalid.yammm"}, cli.ExitValidation},
		// schema.Load wraps file-not-found into diagnostics → validation.
		{"validate missing file", []string{"validate", "testdata/nonexistent.yammm"}, cli.ExitValidation},
		{"check missing data file", []string{"check", "testdata/valid.yammm", "testdata/nonexistent.json"}, cli.ExitUsage},
		{"check csv without type", []string{"check", "testdata/valid.yammm", "testdata/data.csv"}, cli.ExitUsage},
		{"gen unsupported target", []string{"gen", "--to", "rust", "testdata/county.yammm"}, cli.ExitUsage},
		{"gen go-only flag with jsonschema target", []string{"gen", "--to", "jsonschema", "--package", "foo", "testdata/county.yammm"}, cli.ExitUsage},
		{"gen jsonschema-only flag with go target", []string{"gen", "--to", "go", "--schema-id", "https://example.com/x.json", "testdata/county.yammm"}, cli.ExitUsage},
		{"snapshot save without output or into", []string{"snapshot", "save", "testdata/valid.yammm", "testdata/data.json"}, cli.ExitUsage},
		{"snapshot info without arg or dir", []string{"snapshot", "info"}, cli.ExitUsage},
		{"snapshot info nonexistent dir", []string{"snapshot", "info", "--dir", "/does/not/exist/yammm-cli-test"}, cli.ExitRuntime},
		{"update-metadata missing file arg", []string{"snapshot", "update-metadata", "-s", "k=v"}, cli.ExitUsage},
		{"update-metadata nonexistent file", []string{"snapshot", "update-metadata", "-s", "k=v", "/nonexistent/path.ys"}, cli.ExitRuntime},
		{"neo4j diff requires uri", []string{"neo4j", "diff", "testdata/valid.yammm"}, cli.ExitUsage},
		{"neo4j introspect requires uri", []string{"neo4j", "introspect"}, cli.ExitUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code := executeCmd(t, tt.args...)
			assert.Equal(t, tt.want, code)
		})
	}
}

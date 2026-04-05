package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func executeCmd(t *testing.T, args ...string) int {
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
			return exitErr.Code
		}
		return cli.ExitUsage
	}

	return cli.ExitOK
}

func TestValidate_ValidSchema(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "validate", "testdata/valid.yammm")
	assert.Equal(t, 0, code)
}

func TestValidate_InvalidSchema(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "validate", "testdata/invalid.yammm")
	assert.Equal(t, cli.ExitValidation, code)
}

func TestValidate_MissingFile(t *testing.T) {
	t.Parallel()

	// schema.Load wraps file-not-found into diag.Result, so this is a validation error
	code := executeCmd(t, "validate", "testdata/nonexistent.yammm")
	assert.Equal(t, cli.ExitValidation, code)
}

func TestValidate_JSONFormat(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"validate", "--format", "json", "testdata/invalid.yammm"})

	err := cmd.Execute()
	require.Error(t, err)
}

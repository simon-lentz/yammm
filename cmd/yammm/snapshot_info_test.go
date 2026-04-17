package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// createYSFixture uses snapshot save to create a .ys file from testdata.
func createYSFixture(t *testing.T, tmpDir string) string {
	t.Helper()
	ysPath := filepath.Join(tmpDir, "test.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code, "failed to create .ys fixture")

	return ysPath
}

func TestSnapshotInfo_ValidFile(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixture(t, t.TempDir())

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", ysPath})

	err := cmd.Execute()
	require.NoError(t, err)

	out := outBuf.String()
	assert.Contains(t, out, "Snapshot:")
	assert.Contains(t, out, "Person")
	assert.Contains(t, out, "Total instances:")
	assert.Contains(t, out, "ok")
}

func TestSnapshotInfo_JSONFormat(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixture(t, t.TempDir())

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", "--format", "json", ysPath})

	err := cmd.Execute()
	require.NoError(t, err)

	var info map[string]any
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &info))
	assert.Contains(t, info, "SchemaName")
	assert.Contains(t, info, "TotalInstances")
	assert.Contains(t, info, "IntegrityStatus")
}

func TestSnapshotInfo_MalformedFile(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte(`not json at all`), 0o600))

	code := executeCmd(t, "snapshot", "info", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotInfo_HeaderOnlyFlag(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixture(t, t.TempDir())

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", "--header-only", ysPath})

	err := cmd.Execute()
	require.NoError(t, err)

	out := outBuf.String()
	// Header-only fields are present.
	assert.Contains(t, out, "Snapshot:")
	assert.Contains(t, out, "Person")
	assert.Contains(t, out, "Integrity:")
	// Instance-count and summary fields are NOT present — the whole
	// point is that HeaderOnlyRead doesn't compute them.
	assert.NotContains(t, out, "Total instances:",
		"header-only output should not include instance counts (they are not computed)")
	assert.NotContains(t, out, "Summary:",
		"header-only output should not include the summary section")
}

func TestSnapshotInfo_HeaderOnlyFlag_JSONFormat(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixture(t, t.TempDir())

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", "--header-only", "--format", "json", ysPath})

	err := cmd.Execute()
	require.NoError(t, err)

	var header map[string]any
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &header))
	assert.Contains(t, header, "SchemaName")
	assert.Contains(t, header, "IntegrityHash")
	assert.Contains(t, header, "Types")
	// SnapshotInfo-only fields are absent from the HeaderInfo JSON.
	assert.NotContains(t, header, "TotalInstances")
	assert.NotContains(t, header, "IntegrityStatus")
}

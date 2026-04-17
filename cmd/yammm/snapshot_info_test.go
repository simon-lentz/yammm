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

// --- --dir flag (PR-2) ---

// seedDirFixture creates n valid .ys files in a fresh dir named 0.ys, 1.ys, ...
func seedDirFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := range n {
		path := filepath.Join(dir, strconvItoa(i)+".ys")
		code := executeCmd(t, "snapshot", "save",
			"testdata/valid.yammm", "testdata/data.json", "-o", path)
		require.Equal(t, cli.ExitOK, code)
	}
	return dir
}

// strconvItoa avoids pulling strconv into the top of this file when a
// single tiny helper suffices.
func strconvItoa(i int) string {
	if i < 0 {
		return "-" + strconvItoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return strconvItoa(i/10) + string(rune('0'+i%10))
}

func TestSnapshotInfo_DirFlag_TextOutput(t *testing.T) {
	t.Parallel()

	dir := seedDirFixture(t, 2)

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", "--dir", dir})

	err := cmd.Execute()
	require.NoError(t, err)

	out := outBuf.String()
	assert.Contains(t, out, "Directory:")
	assert.Contains(t, out, "0.ys")
	assert.Contains(t, out, "1.ys")
	assert.Contains(t, out, "Summary: 2 ok")
}

func TestSnapshotInfo_DirFlag_JSONOutput(t *testing.T) {
	t.Parallel()

	dir := seedDirFixture(t, 2)

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", "--dir", dir, "--format", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &got))
	assert.Len(t, got, 2)

	// Each entry has name/path/header, no issues on the happy path.
	for _, entry := range got {
		assert.Contains(t, entry, "name")
		assert.Contains(t, entry, "path")
		assert.Contains(t, entry, "header")
		assert.NotNil(t, entry["header"], "header must not be null on happy path")
		_, hasIssues := entry["issues"]
		assert.False(t, hasIssues, "issues should be omitted when empty via omitempty")
	}
}

func TestSnapshotInfo_DirFlag_MutuallyExclusiveWithPositional(t *testing.T) {
	t.Parallel()

	dir := seedDirFixture(t, 1)
	file := filepath.Join(dir, "0.ys")

	code := executeCmd(t, "snapshot", "info", "--dir", dir, file)
	assert.Equal(t, cli.ExitUsage, code)
}

func TestSnapshotInfo_NoArgNoDirIsUsageError(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "snapshot", "info")
	assert.Equal(t, cli.ExitUsage, code)
}

func TestSnapshotInfo_DirFlag_NonexistentDirectory(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "snapshot", "info", "--dir", "/does/not/exist/yammm-cli-test")
	assert.Equal(t, cli.ExitRuntime, code)
}

func TestSnapshotInfo_DirFlag_WithCorruptFile(t *testing.T) {
	t.Parallel()

	dir := seedDirFixture(t, 1) // one valid file
	corrupt := filepath.Join(dir, "corrupt.ys")
	require.NoError(t, os.WriteFile(corrupt, []byte("not json"), 0o600))

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "info", "--dir", dir})

	err := cmd.Execute()
	require.NoError(t, err, "per-file corruption should NOT fail the overall command")

	out := outBuf.String()
	assert.Contains(t, out, "corrupt.ys")
	assert.Contains(t, out, "malformed")
	assert.Contains(t, out, "Summary: 1 ok, 1 malformed")
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestSnapshotInfo_MalformedFile(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte(`not json at all`), 0o600))

	code := executeCmd(t, "snapshot", "info", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotInfo_DirFlag_MutuallyExclusiveWithPositional(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	file := createYSFixture(t, tmpDir)

	code := executeCmd(t, "snapshot", "info", "--dir", tmpDir, file)
	assert.Equal(t, cli.ExitUsage, code)
}

// TestSnapshotInfo_ASummaryPrintsBesideAStructuralError pins that a document
// that decodes and fails its integrity check prints its summary beside the
// error, in text and as JSON, and exits by the result.
func TestSnapshotInfo_ASummaryPrintsBesideAStructuralError(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile("testdata/valid_snapshot.ys")
	require.NoError(t, err)
	tampered := regexp.MustCompile(`"integrity_hash":"sha256:[0-9a-f]{4}`).
		ReplaceAll(doc, []byte(`"integrity_hash":"sha256:0000`))
	require.NotEqual(t, doc, tampered)
	path := filepath.Join(t.TempDir(), "tampered.ys")
	require.NoError(t, os.WriteFile(path, tampered, 0o600))

	code, stdout, stderr := executeCmdOutput(t, "snapshot", "info", path)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, stderr, "E_SNAPSHOT_INTEGRITY_MISMATCH")
	assert.Contains(t, stdout, "Integrity:  mismatch")
	assert.Contains(t, stdout, "Total instances: 2")

	code, stdout, stderr = executeCmdOutput(t, "--json", "snapshot", "info", path)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, stderr, "E_SNAPSHOT_INTEGRITY_MISMATCH")
	var summary struct {
		Instances int `json:"total_instances"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &summary), "stdout:\n%s", stdout)
	assert.Equal(t, 2, summary.Instances)
}

// TestSnapshotInfo_AStructuralErrorPrecedesTheSummary pins that the errors of a
// document whose summary still prints reach a terminal above the summary.
func TestSnapshotInfo_AStructuralErrorPrecedesTheSummary(t *testing.T) {
	doc, err := os.ReadFile("testdata/valid_snapshot.ys")
	require.NoError(t, err)
	tampered := regexp.MustCompile(`"integrity_hash":"sha256:[0-9a-f]{4}`).
		ReplaceAll(doc, []byte(`"integrity_hash":"sha256:0000`))
	path := filepath.Join(t.TempDir(), "tampered.ys")
	require.NoError(t, os.WriteFile(path, tampered, 0o600))

	out := executeCmdCombined(t, "snapshot", "info", path)
	errAt := strings.Index(out, "E_SNAPSHOT_INTEGRITY_MISMATCH")
	summaryAt := strings.Index(out, "Total instances:")
	require.True(t, errAt >= 0 && summaryAt >= 0, "output:\n%s", out)
	assert.Less(t, errAt, summaryAt, "the error is written below the summary:\n%s", out)
}

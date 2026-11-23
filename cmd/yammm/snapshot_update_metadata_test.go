package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/snapshot"
)

// createYSFixtureWithMetadata builds a .ys file pre-populated with one
// metadata key so the exit-code tests have a valid file to operate on.
func createYSFixtureWithMetadata(t *testing.T, tmpDir string) string {
	t.Helper()
	ysPath := filepath.Join(tmpDir, "test.ys")
	code := executeCmd(
		t, "snapshot", "save",
		"testdata/valid.yammm", "testdata/data.json",
		"-o", ysPath,
		"-m", "phase=extract",
	)
	require.Equal(t, cli.ExitOK, code, "failed to create .ys fixture with metadata")
	return ysPath
}

func TestSnapshotUpdateMetadata_NoFlags(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", ysPath)
	assert.Equal(t, cli.ExitUsage, code, "neither --set nor --unset should surface usage error")
}

func TestSnapshotUpdateMetadata_BadSetFormat(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "no_equals_sign", ysPath)
	assert.Equal(t, cli.ExitUsage, code)
}

func TestSnapshotUpdateMetadata_MalformedInput(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte("not a yammm snapshot"), 0o600))

	code, _, errOut := executeCmdOutput(t, "snapshot", "update-metadata", "-s", "k=v", tmp)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, errOut, "error[E_SNAPSHOT_MALFORMED]: expected JSON object")
}

// TestSnapshotUpdateMetadata_SetAndUnsetLandInTheHeader pins what the header
// holds after a --set and after an --unset, and that the command prints
// nothing on success.
func TestSnapshotUpdateMetadata_SetAndUnsetLandInTheHeader(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixtureWithMetadata(t, t.TempDir())
	keys := func() []string {
		t.Helper()
		data, err := os.ReadFile(ysPath)
		require.NoError(t, err)
		header, readErr := snapshot.ReadHeader(t.Context(), bytes.NewReader(data))
		require.NoError(t, readErr, "header: %s", readErr)
		return slices.Sorted(maps.Keys(header.Metadata))
	}
	before := keys()

	code, out, errOut := executeCmdOutput(t, "snapshot", "update-metadata",
		"-s", "env=prod", "-s", "owner=simon", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Empty(t, out+errOut, "a command that succeeds prints nothing")
	assert.Equal(t, sortedWith(before, "env", "owner"), keys())

	code, out, errOut = executeCmdOutput(t, "snapshot", "update-metadata",
		"--unset", "env", "--unset", "owner", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Empty(t, out+errOut, "a command that succeeds prints nothing")
	assert.Equal(t, before, keys())
}

// sortedWith returns keys and more, sorted.
func sortedWith(keys []string, more ...string) []string {
	all := append(slices.Clone(keys), more...)
	slices.Sort(all)
	return all
}

// The command reads the metadata it edits with no size cap, so a header past
// snapshot.MaxHeaderSize, which snapshot.ReadHeader refuses, is updated.
func TestSnapshotUpdateMetadata_UpdatesAHeaderPastTheHeaderCap(t *testing.T) {
	t.Parallel()
	// The 16 MiB header is read on one goroutine, and the detector multiplies each byte.
	raceskip.Skip(t)
	ysPath := filepath.Join(t.TempDir(), "big.ys")
	big := strings.Repeat("x", snapshot.MaxHeaderSize)
	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", "testdata/data.json", "-o", ysPath, "-m", "big="+big)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)

	code, _, errOut = executeCmdOutput(t, "snapshot", "update-metadata", "-s", "k=v", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	info, readErr := snapshot.Stat(t.Context(), data)
	require.NoError(t, readErr, "the updated document verifies: %v", readErr)
	assert.Equal(t, map[string]string{"big": big, "k": "v"}, info.Metadata)
}

// A document whose header reads but whose outer shape breaks past it is
// refused as malformed, as every whole-document read refuses it.
func TestSnapshotUpdateMetadata_AShapeBreakPastTheHeaderIsMalformed(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	data = append(data, 'x')
	require.NoError(t, os.WriteFile(ysPath, data, 0o600))

	code, _, errOut := executeCmdOutput(t, "snapshot", "update-metadata", "-s", "k=v", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, errOut, "error[E_SNAPSHOT_MALFORMED]")
	after, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, after), "a refused document is not rewritten")
}

// The update seals the body under a fresh hash, so a document whose stored hash
// does not match its content is refused and left as it was.
func TestSnapshotUpdateMetadata_AnIntegrityMismatchIsRefused(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	tampered := bytes.Replace(data, []byte(`"name":"Bob"`), []byte(`"name":"Rob"`), 1)
	require.NotEqual(t, data, tampered, "fixture shape changed; Bob not found")
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	code, _, errOut := executeCmdOutput(t, "snapshot", "update-metadata", "-s", "k=v", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, errOut, "E_SNAPSHOT_INTEGRITY_MISMATCH")
	after, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	assert.Equal(t, tampered, after, "a refused document is not rewritten")
}

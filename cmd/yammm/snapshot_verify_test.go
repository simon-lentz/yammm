package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// The tampering tests need byte surgery on the .ys payload, which the
// testscript scripts cannot express — they stay in-process alongside their
// exact exit-code claims.

func TestSnapshotVerify_CorruptedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)

	// Replace a character in the payload to break the integrity hash.
	tampered := bytes.Replace(data, []byte(`"Alice"`), []byte(`"AlicX"`), 1)
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	code := executeCmd(t, "snapshot", "verify", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotVerify_SkipIntegrityCheck(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)

	tampered := bytes.Replace(data, []byte(`"Alice"`), []byte(`"AlicX"`), 1)
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	// With --skip-integrity-check, structural validation still runs but
	// the integrity hash mismatch is not an error.
	code := executeCmd(t, "snapshot", "verify", "--skip-integrity-check", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitOK, code)
}

func TestSnapshotVerify_EmptyIntegrityHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)

	unsigned := regexp.MustCompile(`"integrity_hash": ?"sha256:[0-9a-f]{64}"`).ReplaceAll(data, []byte(`"integrity_hash":""`))
	require.NotEqual(t, data, unsigned, "the fixture holds no signed integrity_hash")
	require.NoError(t, os.WriteFile(ysPath, unsigned, 0o600))

	code, stderr := executeCmdStderr(t, "snapshot", "verify", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, stderr, "E_SNAPSHOT_INTEGRITY_MISMATCH")

	code, stderr = executeCmdStderr(t, "snapshot", "info", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
	assert.Contains(t, stderr, "E_SNAPSHOT_INTEGRITY_MISMATCH")

	code = executeCmd(t, "snapshot", "verify", "--skip-integrity-check", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitOK, code)
}

func TestSnapshotVerify_MalformedFile(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte(`{"yammm_snapshot": "not an object"}`), 0o600))

	code := executeCmd(t, "snapshot", "verify", "testdata/valid.yammm", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}

// revalidationFixture saves n instances that meet the schema's invariant, then
// rewrites every saved age so each instance breaks it. The edited snapshot's
// integrity hash no longer verifies, so a read of it needs
// --skip-integrity-check.
func revalidationFixture(t *testing.T, n int) (schemaPath, ysPath string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath = filepath.Join(dir, "rv.yammm")
	require.NoError(t, os.WriteFile(schemaPath, []byte(`schema "rv"

type P {
    id String primary
    age Integer required
    ! "adult" age >= 18
}
`), 0o600))

	var b strings.Builder
	b.WriteString(`{"P": [`)
	for i := range n {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `{"id": "p%03d", "age": 30}`, i)
	}
	b.WriteString("]}")
	dataPath := filepath.Join(dir, "rv.json")
	require.NoError(t, os.WriteFile(dataPath, []byte(b.String()), 0o600))

	ysPath = filepath.Join(dir, "rv.ys")
	code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", ysPath, schemaPath, dataPath)
	require.Equal(t, cli.ExitOK, code, "save the fixture:\n%s", stderr)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	require.Equal(t, n, bytes.Count(data, []byte(`"age":30`)), "the saved document does not spell each age as the edit expects")
	require.NoError(t, os.WriteFile(ysPath, bytes.ReplaceAll(data, []byte(`"age":30`), []byte(`"age":5`)), 0o600))
	return schemaPath, ysPath
}

// TestSnapshotVerify_RevalidatePrintsEachFindingAsAWarning pins that
// --revalidate reports what the validator finds in the stored instances as
// warnings on stderr, one per instance, and that warnings alone exit 0.
func TestSnapshotVerify_RevalidatePrintsEachFindingAsAWarning(t *testing.T) {
	t.Parallel()

	schemaPath, ysPath := revalidationFixture(t, 3)
	code, stdout, stderr := executeCmdOutput(t, "snapshot", "verify", "--revalidate", "--skip-integrity-check", schemaPath, ysPath)
	assert.Equal(t, cli.ExitOK, code)
	assert.Empty(t, stdout)
	assert.Equal(t, 3, strings.Count(stderr, "warning[E_INVARIANT_FAIL]"), "stderr:\n%s", stderr)
	for _, id := range []string{"p000", "p001", "p002"} {
		assert.Contains(t, stderr, fmt.Sprintf(`revalidation of rv#P[[%q]]: adult`, id))
	}
}

// TestSnapshotVerify_RevalidateKeepsAHundredWarnings pins the one-call issue
// limit on a snapshot read: of 150 revalidation warnings, verify prints 100
// and a note that counts the 50 it dropped.
func TestSnapshotVerify_RevalidateKeepsAHundredWarnings(t *testing.T) {
	t.Parallel()

	schemaPath, ysPath := revalidationFixture(t, 150)
	code, _, stderr := executeCmdOutput(t, "snapshot", "verify", "--revalidate", "--skip-integrity-check", schemaPath, ysPath)
	assert.Equal(t, cli.ExitOK, code)
	assert.Equal(t, 100, strings.Count(stderr, "warning[E_INVARIANT_FAIL]"), "printed warnings")
	assert.Contains(t, stderr, "note: 50 more issue(s) dropped at an issue limit", "stderr carries no note counting the 50 dropped warnings")
}

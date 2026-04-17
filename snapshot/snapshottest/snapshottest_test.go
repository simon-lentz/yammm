package snapshottest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// minimalSchema is a tiny schema local to this test file — the wider
// testSchema helpers live in the snapshot_test package, which
// snapshottest can't import (would create a cycle).
func minimalSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("snapshottest-scan").
		WithSourceID(location.MustNewSourceID("test://snapshottest-scan.yammm")).
		AddType("Doc").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	require.False(t, result.HasErrors(), "minimalSchema: %s", result)
	return s
}

func seedValidYS(t *testing.T, path string) {
	t.Helper()
	s := minimalSchema(t)
	typ, ok := s.Type("Doc")
	require.True(t, ok)
	inst := instance.NewValidInstance(
		"Doc", typ.ID(),
		immutable.WrapKey([]any{"d1"}),
		immutable.WrapProperties(map[string]any{"id": "d1"}),
		nil, nil, nil,
	)
	g := graph.New(s)
	g.Add(context.Background(), inst)
	snap := g.Snapshot()

	data, result := snapshot.Marshal(context.Background(), snap)
	require.NoError(t, result.Err())
	require.NoError(t, snapshot.WriteFile(path, data))
}

func seedCorruptYS(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("garbage"), 0o600))
}

// recordingTB captures Errorf / Fatalf / Fail calls so tests can
// exercise failure branches of ExpectDirState without actually failing
// the enclosing test.
type recordingTB struct {
	testing.TB
	errors   []string
	fatalled bool
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatalled = true
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Fatal(args ...any) {
	r.fatalled = true
	r.errors = append(r.errors, fmt.Sprint(args...))
}

func (r *recordingTB) Helper() {}

func TestExpectDirState_HappyPathAllValid(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedValidYS(t, filepath.Join(dir, "b.ys"))

	rec := &recordingTB{TB: t}
	snapshottest.ExpectDirState(rec, dir, map[string]snapshottest.State{
		"a.ys": {Valid: true},
		"b.ys": {Valid: true},
	})

	assert.Empty(t, rec.errors, "happy path should produce no failures")
	assert.False(t, rec.fatalled)
}

func TestExpectDirState_ExpectedValidGotErrorFails(t *testing.T) {
	dir := t.TempDir()
	seedCorruptYS(t, filepath.Join(dir, "a.ys"))

	rec := &recordingTB{TB: t}
	snapshottest.ExpectDirState(rec, dir, map[string]snapshottest.State{
		"a.ys": {Valid: true},
	})

	require.NotEmpty(t, rec.errors, "expected failure but got clean result")
	assert.Contains(t, rec.errors[0], "a.ys",
		"failure message should name the offending file")
}

func TestExpectDirState_ErrorCodeMismatch(t *testing.T) {
	dir := t.TempDir()
	seedCorruptYS(t, filepath.Join(dir, "bad.ys"))

	rec := &recordingTB{TB: t}
	snapshottest.ExpectDirState(rec, dir, map[string]snapshottest.State{
		// Corrupt JSON -> E_SNAPSHOT_MALFORMED, but we claim E_SNAPSHOT_IO.
		"bad.ys": {Valid: false, ErrorCode: diag.E_SNAPSHOT_IO},
	})

	require.NotEmpty(t, rec.errors, "code mismatch should fail")
	assert.Contains(t, rec.errors[0], "ErrorCode")
}

func TestExpectDirState_ErrorCodeWildcardMatchesAny(t *testing.T) {
	dir := t.TempDir()
	seedCorruptYS(t, filepath.Join(dir, "bad.ys"))

	rec := &recordingTB{TB: t}
	snapshottest.ExpectDirState(rec, dir, map[string]snapshottest.State{
		"bad.ys": {Valid: false}, // ErrorCode = zero value = wildcard
	})

	assert.Empty(t, rec.errors, "zero-value ErrorCode should accept any error code")
}

func TestExpectDirState_MissingExpectedFile(t *testing.T) {
	dir := t.TempDir() // empty

	rec := &recordingTB{TB: t}
	snapshottest.ExpectDirState(rec, dir, map[string]snapshottest.State{
		"ghost.ys": {Valid: true},
	})

	require.NotEmpty(t, rec.errors)
	assert.Contains(t, rec.errors[0], "ghost.ys")
	assert.Contains(t, rec.errors[0], "not found")
}

func TestExpectDirState_UnexpectedFile(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "surprise.ys"))

	rec := &recordingTB{TB: t}
	snapshottest.ExpectDirState(rec, dir, map[string]snapshottest.State{})

	require.NotEmpty(t, rec.errors)
	assert.Contains(t, rec.errors[0], "surprise.ys")
	assert.Contains(t, rec.errors[0], "unexpected")
}

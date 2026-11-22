package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// moreData is a second data file disjoint from testdata/data.json, so a merge
// adds instances rather than replacing them.
const moreData = `{"Person": [{"id": "carol", "name": "Carol", "age": 41}]}`

// saveInto writes a base snapshot carrying metadata and a created_at, and
// returns its path beside a data file holding one further instance.
func saveInto(t *testing.T) (base, more string) {
	t.Helper()
	dir := t.TempDir()
	base = filepath.Join(dir, "base.ys")
	more = filepath.Join(dir, "more.json")
	if err := os.WriteFile(more, []byte(moreData), 0o600); err != nil {
		t.Fatalf("write data file: %v", err)
	}

	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--timestamp",
		"-m", "env=prod", "-m", "owner=simon",
		"testdata/valid.yammm", "testdata/data.json", "-o", base)
	if code != cli.ExitOK {
		t.Fatalf("base save exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	return base, more
}

// readHeader reads a .ys header without verifying the body, so a test can
// assert on what a merge wrote.
func readHeader(t *testing.T, path string) *snapshot.Header {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	header, result := snapshot.ReadHeader(context.Background(), bytes.NewReader(data))
	if err := result.Err(); err != nil {
		t.Fatalf("read header: %v", err)
	}
	return header
}

// The merged file's header is the one the load accepted, so a header past
// snapshot.MaxHeaderSize, which a header read refuses, still carries forward.
func TestSnapshotSave_IntoCarriesAHeaderPastTheHeaderCap(t *testing.T) {
	t.Parallel()
	// The 16 MiB header is read on one goroutine, and the detector multiplies each byte.
	raceskip.Skip(t)

	_, more := saveInto(t)
	base := filepath.Join(t.TempDir(), "big.ys")
	big := strings.Repeat("x", snapshot.MaxHeaderSize)
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--timestamp",
		"-m", "big="+big, "-m", "env=prod",
		"testdata/valid.yammm", "testdata/data.json", "-o", base)
	if code != cli.ExitOK {
		t.Fatalf("base save exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	before := statHeader(t, base)
	if before.CreatedAt.IsZero() {
		t.Fatal("the base snapshot has no created_at, so the assertion below is vacuous")
	}

	code, _, errOut = executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	after := statHeader(t, base)
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("created_at = %s, want %s", after.CreatedAt, before.CreatedAt)
	}
	if after.Metadata["env"] != "prod" || after.Metadata["big"] != big {
		t.Errorf("metadata keys after the merge = %v, want big and env carried forward", slices.Sorted(maps.Keys(after.Metadata)))
	}
}

// A document the load refuses comes back with no header, so a merge carries
// nothing forward from bytes it did not accept.
func TestLoadSnapshotFile_ARefusedDocumentHasNoHeader(t *testing.T) {
	t.Parallel()

	base, _ := saveInto(t)
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	tampered := bytes.Replace(data, []byte(`"name":"Bob"`), []byte(`"name":"Rob"`), 1)
	if bytes.Equal(tampered, data) {
		t.Fatal("fixture shape changed; Bob not found")
	}
	if err := os.WriteFile(base, tampered, 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	s, schemaResult := schema.Load(t.Context(), mustAbs(t, "testdata/valid.yammm"))
	if err := schemaResult.Err(); err != nil {
		t.Fatalf("load schema: %v", err)
	}
	_, header, result, err := cli.LoadSnapshotFile(t.Context(), base, s)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("the tampered document loaded; the test asserts nothing")
	}
	if header != nil {
		t.Errorf("header = %+v, want nil beside a refused load", header)
	}
}

// statHeader reads a .ys header with [snapshot.Stat], which reads a header of
// any size.
func statHeader(t *testing.T, path string) snapshot.Header {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	info, result := snapshot.Stat(t.Context(), data)
	if err := result.Err(); err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.Header
}

// TestSnapshotSave_IntoCarriesTheHeaderForward pins that a merge keeps the
// imported header's metadata and created_at, so `--into` with -o defaulted —
// an in-place edit — erases neither.
func TestSnapshotSave_IntoCarriesTheHeaderForward(t *testing.T) {
	t.Parallel()

	base, more := saveInto(t)
	before := readHeader(t, base)
	if before.CreatedAt.IsZero() {
		t.Fatal("the base snapshot has no created_at, so the assertion below is vacuous")
	}

	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	after := readHeader(t, base)
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("created_at = %s, want %s — an in-place merge preserves it", after.CreatedAt, before.CreatedAt)
	}
	for key, want := range map[string]string{"env": "prod", "owner": "simon"} {
		if got := after.Metadata[key]; got != want {
			t.Errorf("metadata[%q] = %q, want %q — a merge keeps the header's annotations", key, got, want)
		}
	}
}

// TestSnapshotSave_IntoLetsFlagsOverride keeps the carry-forward from becoming
// a rule an operator cannot escape: --metadata overlays the header's pairs and
// --timestamp replaces its created_at.
func TestSnapshotSave_IntoLetsFlagsOverride(t *testing.T) {
	t.Parallel()

	base, more := saveInto(t)
	// The base is seeded with a timestamp --timestamp would never write, so a
	// --timestamp that failed to override is visible. Comparing two
	// time.Now() values would not: both render at second precision, and two
	// saves inside one second are equal without the flag doing anything.
	seeded := writeForeignSnapshot(t, base, "2020-02-02T02:02:02.020202+02:00")

	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"--timestamp", "-m", "env=staging",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	after := readHeader(t, base)
	if after.CreatedAt.Equal(seeded) {
		t.Errorf("created_at = %s, unchanged — --timestamp must replace the carried value", after.CreatedAt)
	}
	if after.CreatedAt.IsZero() {
		t.Error("created_at is empty — --timestamp must set one")
	}
	if got := after.Metadata["env"]; got != "staging" {
		t.Errorf("metadata[\"env\"] = %q, want %q — --metadata overlays the header", got, "staging")
	}
	if got := after.Metadata["owner"]; got != "simon" {
		t.Errorf("metadata[\"owner\"] = %q, want %q — an overlay keeps the keys it does not name", got, "simon")
	}
}

// A merge carries the merged file's created_at as the time it states, written
// as Marshal writes a time: its fraction and its offset survive, where
// --timestamp writes whole seconds in UTC.
func TestSnapshotSave_IntoKeepsAForeignTimestamp(t *testing.T) {
	t.Parallel()

	const foreign = "2026-01-01T12:00:00.123456+02:00"

	base, more := saveInto(t)
	writeForeignSnapshot(t, base, foreign)

	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !bytes.Contains(data, []byte(`"created_at":"`+foreign+`"`)) {
		t.Errorf("created_at = %s, want %s with its fraction and its offset", readHeader(t, base).CreatedAt, foreign)
	}
}

// writeForeignSnapshot rewrites the snapshot at path so its created_at is
// createdAt, a form --timestamp does not write, changing nothing else, and
// returns that time. Marshal computes the integrity hash over the header it is
// given, so the document loads; patching the bytes would not.
func writeForeignSnapshot(t *testing.T, path, createdAt string) time.Time {
	t.Helper()
	ctx := context.Background()

	stamp, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t.Fatalf("parse %q: %v", createdAt, err)
	}
	s, schemaResult := schema.Load(ctx, mustAbs(t, "testdata/valid.yammm"))
	if err := schemaResult.Err(); err != nil {
		t.Fatalf("load schema: %v", err)
	}
	snap, header, loadResult, err := cli.LoadSnapshotFile(ctx, path, s)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if err := loadResult.Err(); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	// The metadata is carried too: a fixture that quietly dropped it would
	// make the overlay assertions pass or fail for the wrong reason.
	opts := []snapshot.Option{snapshot.WithCreatedAt(stamp)}
	if len(header.Metadata) > 0 {
		opts = append(opts, snapshot.WithMetadata(header.Metadata))
	}

	data, marshalResult := snapshot.Marshal(ctx, snap, opts...)
	if err := marshalResult.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	return stamp
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	return abs
}

// TestSnapshotSave_IntoMergesEveryType pins that a merge writes the merged
// file's instances and the new file's, of every type, and prints nothing: the
// reader of the file reports both counts.
func TestSnapshotSave_IntoMergesEveryType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "two.yammm")
	people := filepath.Join(dir, "people.json")
	companies := filepath.Join(dir, "companies.json")
	base := filepath.Join(dir, "base.ys")

	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(schemaPath, "schema \"two\"\n\ntype Person {\n\tid String primary\n}\n\ntype Company {\n\tid String primary\n}\n")
	write(people, `{"Person": [{"id": "alice"}, {"id": "bob"}]}`)
	write(companies, `{"Company": [{"id": "acme"}]}`)

	code, _, errOut := executeCmdOutput(t, "snapshot", "save", schemaPath, people, "-o", base)
	if code != cli.ExitOK {
		t.Fatalf("base save exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	code, out, errOut := executeCmdOutput(t, "snapshot", "save", schemaPath, companies, "--into", base)
	if code != cli.ExitOK || out != "" || errOut != "" {
		t.Fatalf("merge: exit %d with %q and %q, want %d and nothing printed", code, out, errOut, cli.ExitOK)
	}

	_, info, _ := executeCmdOutput(t, "snapshot", "info", base)
	if got := strings.Fields(lineContaining(t, info, "Total instances:"))[2]; got != "3" {
		t.Errorf("the merged file holds %s instances, want 3", got)
	}
	if got := strings.Fields(lineContaining(t, info, "Types:"))[1]; got != "2" {
		t.Errorf("the merged file holds %s types, want 2", got)
	}
}

// TestSnapshotSave_IntoRendersHeaderDiagnosticsOnce guards the mechanism this
// repair adds: reading the header a second time must not report the same
// document's diagnostics twice.
func TestSnapshotSave_IntoRendersHeaderDiagnosticsOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base := filepath.Join(dir, "base.ys")
	more := filepath.Join(dir, "more.json")
	if err := os.WriteFile(more, []byte(moreData), 0o600); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	stale, err := os.ReadFile("testdata/hash_algo_unsupported.ys")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(base, stale, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)

	// The two reads disagree on severity for this document by design: Load
	// calls the unreadable algorithm an error, the header-only surface a
	// warning. Merging both puts one fact on two lines.
	const code = "E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM"
	n := strings.Count(errOut, code)
	if n == 0 {
		t.Fatalf("the fixture reports no %s, so this test asserts nothing:\n%s", code, errOut)
	}
	if n > 1 {
		t.Errorf("%s is reported %d times, want once — the second read's diagnostics are dropped:\n%s", code, n, errOut)
	}
}

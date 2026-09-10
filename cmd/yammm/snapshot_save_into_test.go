package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
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
func readHeader(t *testing.T, path string) *snapshot.HeaderInfo {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	header, result := snapshot.HeaderOnly(context.Background(), data)
	if err := result.Err(); err != nil {
		t.Fatalf("read header: %v", err)
	}
	return header
}

// TestSnapshotSave_IntoCarriesTheHeaderForward pins B5. A merge dropped the
// imported header's metadata and created_at, so `--into` with -o defaulted —
// an in-place edit — silently erased both.
func TestSnapshotSave_IntoCarriesTheHeaderForward(t *testing.T) {
	t.Parallel()

	base, more := saveInto(t)
	before := readHeader(t, base)
	if before.CreatedAt == "" {
		t.Fatal("the base snapshot has no created_at, so the assertion below is vacuous")
	}

	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	after := readHeader(t, base)
	if after.CreatedAt != before.CreatedAt {
		t.Errorf("created_at = %q, want %q — an in-place merge preserves it", after.CreatedAt, before.CreatedAt)
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
	// The base is seeded with a timestamp yammm would never write, so a
	// --timestamp that failed to override is visible. Comparing two
	// time.Now() values would not: both render at second precision, and two
	// saves inside one second are equal without the flag doing anything.
	const seeded = "2020-02-02T02:02:02.020202+02:00"
	writeForeignSnapshot(t, base, seeded)

	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"--timestamp", "-m", "env=staging",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	after := readHeader(t, base)
	if after.CreatedAt == seeded {
		t.Errorf("created_at = %q, unchanged — --timestamp must replace the carried value", after.CreatedAt)
	}
	if after.CreatedAt == "" {
		t.Error("created_at is empty — --timestamp must set one")
	}
	if got := after.Metadata["env"]; got != "staging" {
		t.Errorf("metadata[\"env\"] = %q, want %q — --metadata overlays the header", got, "staging")
	}
	if got := after.Metadata["owner"]; got != "simon" {
		t.Errorf("metadata[\"owner\"] = %q, want %q — an overlay keeps the keys it does not name", got, "simon")
	}
}

// TestSnapshotSave_IntoKeepsAForeignTimestampByte pins why the repair carries
// bytes rather than a parsed time. yammm writes created_at at second precision,
// so its own documents round-trip through a parse; a foreign header carrying
// fractional seconds and an offset does not, and rewriting an operator's bytes
// is the defect class this unit exists to remove.
func TestSnapshotSave_IntoKeepsAForeignTimestampByte(t *testing.T) {
	t.Parallel()

	const foreign = "2026-01-01T12:00:00.123456+02:00"

	base, more := saveInto(t)
	writeForeignSnapshot(t, base, foreign)

	if got := readHeader(t, base).CreatedAt; got != foreign {
		t.Fatalf("fixture created_at = %q, want %q — the fixture itself is wrong", got, foreign)
	}

	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("merge exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}

	if got := readHeader(t, base).CreatedAt; got != foreign {
		t.Errorf("created_at = %q, want %q byte for byte — a parse-and-reformat loses the fraction and the offset",
			got, foreign)
	}
}

// writeForeignSnapshot rewrites the snapshot at path so its created_at is a
// form yammm does not write, changing nothing else. Marshal computes the
// integrity hash over the header it is given, so the document loads; patching
// the bytes would not.
func writeForeignSnapshot(t *testing.T, path, createdAt string) {
	t.Helper()
	ctx := context.Background()

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
	opts := []snapshot.Option{snapshot.WithCreatedAtFrom(&snapshot.HeaderInfo{CreatedAt: createdAt})}
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
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	return abs
}

// TestSnapshotSave_SummaryCountsTheArtefact pins B21. The summary counted the
// instances this invocation parsed while describing the document it had just
// written, so a merge reported a number no reader of the file could reproduce.
func TestSnapshotSave_SummaryCountsTheArtefact(t *testing.T) {
	t.Parallel()

	base, more := saveInto(t)

	_, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)

	// testdata/data.json holds two instances and more.json holds one.
	const want = "saved snapshot: 3 instances of 1 types"
	if !strings.Contains(errOut, want) {
		t.Errorf("stderr does not contain %q — the summary describes the artefact, not the input:\n%s", want, errOut)
	}
}

// TestSnapshotSave_SummaryAgreesWithSnapshotInfo is the second implementation
// of the count above: whatever route the summary takes, it must land on the
// number the reader of the same file reports.
func TestSnapshotSave_SummaryAgreesWithSnapshotInfo(t *testing.T) {
	t.Parallel()

	base, more := saveInto(t)
	_, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)

	_, out, _ := executeCmdOutput(t, "snapshot", "info", base)

	summary := strings.TrimSpace(lineContaining(t, errOut, "saved snapshot:"))
	reported := strings.TrimSpace(lineContaining(t, out, "Total instances:"))
	summaryCount := strings.Fields(summary)[2]
	reportedCount := strings.Fields(reported)[2]
	if summaryCount != reportedCount {
		t.Errorf("save reported %s instances and snapshot info reports %s — one file, two counts",
			summaryCount, reportedCount)
	}
}

// TestSnapshotSave_SummaryCountsEveryType covers the other half of the count.
// A single-type fixture cannot tell a real type count from a hard-coded 1, so
// this one merges a second type in and asserts both numbers against the reader
// of the same file.
func TestSnapshotSave_SummaryCountsEveryType(t *testing.T) {
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

	_, _, errOut = executeCmdOutput(t, "snapshot", "save", schemaPath, companies, "--into", base)
	summary := strings.TrimSpace(lineContaining(t, errOut, "saved snapshot:"))

	const want = "saved snapshot: 3 instances of 2 types"
	if !strings.Contains(summary, want) {
		t.Errorf("summary is %q, want %q", summary, want)
	}

	_, out, _ := executeCmdOutput(t, "snapshot", "info", base)
	reportedTypes := strings.Fields(strings.TrimSpace(lineContaining(t, out, "Types:")))[1]
	if got := strings.Fields(summary)[5]; got != reportedTypes {
		t.Errorf("save reported %s types and snapshot info reports %s — one file, two counts", got, reportedTypes)
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

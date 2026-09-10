package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// snakeCase matches a wire key this CLI is willing to emit.
var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// dataMapKeys names the wire fields whose object keys are user or document
// data rather than field names: metadata keys are whatever an operator set,
// and instance_counts is keyed by "schema#type". Their contents are values,
// so the convention does not reach them.
var dataMapKeys = map[string]bool{"metadata": true, "instance_counts": true}

// collectWireKeys walks decoded JSON and returns every object key that names a
// field, skipping the contents of the data maps above.
func collectWireKeys(t *testing.T, v any, path string, out *[]string) {
	t.Helper()
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			*out = append(*out, path+"/"+k)
			if dataMapKeys[k] {
				continue
			}
			collectWireKeys(t, child, path+"/"+k, out)
		}
	case []any:
		for _, child := range node {
			collectWireKeys(t, child, path+"[]", out)
		}
	}
}

// decodeStdout runs the CLI and decodes its stdout as one JSON document.
func decodeStdout(t *testing.T, args ...string) any {
	t.Helper()
	code, out, errOut := executeCmdOutput(t, args...)
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\nstdout:\n%s", err, out)
	}
	return doc
}

// saveFixture writes a .ys file with three metadata keys whose sorted order
// differs from any insertion order a map iteration could reproduce.
func saveFixture(t *testing.T, path string) {
	t.Helper()
	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", "testdata/data.json", "-o", path,
		"-m", "zebra=1", "-m", "alpha=2", "-m", "mid=3")
	if code != cli.ExitOK {
		t.Fatalf("snapshot save exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
}

// TestSnapshotInfo_JSONKeysAreSnakeCase pins B49 across all three JSON modes.
// Two of the three marshalled the library structs directly, so they emitted Go
// field names, and the directory mode wrapped a PascalCase header in
// snake_case entry keys — one command speaking two conventions in one
// document.
func TestSnapshotInfo_JSONKeysAreSnakeCase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "a.ys")
	saveFixture(t, file)

	tests := []struct {
		name string
		args []string
	}{
		{"single file", []string{"snapshot", "info", "--format", "json", file}},
		{"header only", []string{"snapshot", "info", "--header-only", "--format", "json", file}},
		{"directory", []string{"snapshot", "info", "--dir", dir, "--format", "json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var keys []string
			collectWireKeys(t, decodeStdout(t, tt.args...), "", &keys)
			if len(keys) == 0 {
				t.Fatal("no wire keys found — the walk asserts nothing")
			}
			for _, key := range keys {
				name := key[strings.LastIndex(key, "/")+1:]
				if !snakeCase.MatchString(name) {
					t.Errorf("wire key %q is not snake_case (at %s)", name, key)
				}
			}
		})
	}
}

// TestSnapshotInfo_HeaderOnlyReportsFileSize pins B48. HeaderOnlyRead reports
// zero by contract — a size is not knowable from an io.Reader — but the
// command opened the file and holds the handle, so it published a zero it
// could have stat'd.
func TestSnapshotInfo_HeaderOnlyReportsFileSize(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "a.ys")
	saveFixture(t, file)

	stat, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	doc, ok := decodeStdout(t, "snapshot", "info", "--header-only", "--format", "json", file).(map[string]any)
	if !ok {
		t.Fatal("header-only JSON is not an object")
	}
	size, ok := doc["file_size"].(float64)
	if !ok {
		t.Fatalf("file_size is absent or not a number: %#v", doc["file_size"])
	}
	if int64(size) != stat.Size() {
		t.Errorf("file_size = %d, want %d — the command holds the open handle", int64(size), stat.Size())
	}
}

// TestSnapshotInfo_DirDiagnosticsIsAlwaysTheWire pins B50. A clean entry omitted
// its issues entirely while its sibling header was an explicit null, so one DTO
// answered "nothing to report" two different ways; every entry now carries the
// diagnostic wire object, whose issues is an array even when empty.
func TestSnapshotInfo_DirDiagnosticsIsAlwaysTheWire(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	saveFixture(t, filepath.Join(dir, "clean.ys"))
	if err := os.WriteFile(filepath.Join(dir, "corrupt.ys"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	// A corrupt entry makes the scan exit non-zero, so this mode is driven
	// directly rather than through decodeStdout's exit-code check.
	code, out, errOut := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--format", "json")
	if code != cli.ExitValidation {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitValidation, errOut)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\nstdout:\n%s", err, out)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	for _, entry := range entries {
		name, _ := entry["name"].(string)
		wire, ok := entry["diagnostics"].(map[string]any)
		if !ok {
			t.Errorf("%s: diagnostics = %#v, want the diagnostic wire object", name, entry["diagnostics"])
			continue
		}
		if _, ok := wire["issues"].([]any); !ok {
			t.Errorf("%s: diagnostics.issues = %#v, want an array", name, wire["issues"])
		}
	}
}

// stripAttestation rewrites a .ys header into one a pre-v0.15.0 writer would
// have produced. The integrity hash covers the header, so only the
// header-only readers accept the result — which is where both nil arms run.
func stripAttestation(t *testing.T, path string) {
	t.Helper()
	const segment = `,"attestation":{"values":true,"associations":true}`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !strings.Contains(string(data), segment) {
		t.Fatalf("fixture carries no attestation segment to strip:\n%s", data)
	}
	stripped := strings.Replace(string(data), segment, "", 1)
	if err := os.WriteFile(path, []byte(stripped), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// TestSnapshotInfo_AbsentAttestationRenders covers the pre-v0.15.0 branch of
// both renderers. Neither arm was asserted, so a JSON reader could not tell an
// unstated claim from a false one, and a mutation that rendered the absence as
// values=false associations=false survived.
func TestSnapshotInfo_AbsentAttestationRenders(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "a.ys")
	saveFixture(t, file)
	stripAttestation(t, file)

	t.Run("text names the absence", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := executeCmdOutput(t, "snapshot", "info", "--header-only", file)
		if code != cli.ExitOK {
			t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
		}
		if !strings.Contains(out, "not stated") {
			t.Errorf("stdout does not name the absent claim:\n%s", out)
		}
	})

	t.Run("json renders null, not a claim", func(t *testing.T) {
		t.Parallel()

		doc, ok := decodeStdout(t, "snapshot", "info", "--header-only", "--format", "json", file).(map[string]any)
		if !ok {
			t.Fatal("header-only JSON is not an object")
		}
		att, present := doc["attestation"]
		if !present {
			t.Fatal("attestation is absent; the key states the claim or its absence")
		}
		if att != nil {
			t.Errorf("attestation = %#v, want null — a writer that stated nothing must not read as a claim", att)
		}
	})
}

// TestSnapshotInfo_DirExitCodeIsFormatIndependent pins the half of B17 that
// survived its own repair: dirEntriesExit was wired to the text return only,
// so the same malformed directory exited 1 as text and 0 as JSON — and a
// machine consumer, the one --format json exists for, read the silent code.
func TestSnapshotInfo_DirExitCodeIsFormatIndependent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	saveFixture(t, filepath.Join(dir, "clean.ys"))
	if err := os.WriteFile(filepath.Join(dir, "corrupt.ys"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	text, _, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir)
	jsonCode, _, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--format", "json")

	if text != cli.ExitValidation {
		t.Errorf("text exit code = %d, want %d", text, cli.ExitValidation)
	}
	if jsonCode != text {
		t.Errorf("json exit code = %d, text exit code = %d — the output format must not change what a scan reports", jsonCode, text)
	}
}

// TestSnapshotInfo_DirHeaderStatesPresenceAndSize covers the two things the
// entry's header answers and nothing asserted: it is null exactly when the
// file could not be parsed, and it carries the size the scan already stat'd.
func TestSnapshotInfo_DirHeaderStatesPresenceAndSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.ys")
	saveFixture(t, clean)
	if err := os.WriteFile(filepath.Join(dir, "corrupt.ys"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	stat, err := os.Stat(clean)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	var entries []map[string]any
	_, out, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--format", "json")
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\nstdout:\n%s", err, out)
	}

	byName := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		name, _ := entry["name"].(string)
		byName[name] = entry
	}

	if header := byName["corrupt.ys"]["header"]; header != nil {
		t.Errorf("corrupt entry header = %#v, want null — a file that did not parse states no header", header)
	}

	header, ok := byName["clean.ys"]["header"].(map[string]any)
	if !ok {
		t.Fatalf("clean entry header = %#v, want an object", byName["clean.ys"]["header"])
	}
	size, ok := header["file_size"].(float64)
	if !ok {
		t.Fatalf("header.file_size is absent or not a number: %#v", header["file_size"])
	}
	if int64(size) != stat.Size() {
		t.Errorf("header.file_size = %d, want %d — the scan stat'd the handle it opened", int64(size), stat.Size())
	}
}

// TestSnapshotInfo_DirModTimeSortsAsText pins B47. RFC 3339 Nano trims
// trailing zeros, so a whole-second time renders with no fraction at all and
// sorts ABOVE one 10 µs later — "Z" is above "." in every byte ordering a
// consumer would use.
func TestSnapshotInfo_DirModTimeSortsAsText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// earlier lands on a whole second, which is what makes the trimmed
	// rendering shorter than its successor's.
	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// The odd nanosecond count is load-bearing: a fixed-width layout that
	// truncates below microseconds still sorts, and only the round-trip sees it.
	later := earlier.Add(10*time.Microsecond + 123*time.Nanosecond)

	for name, stamp := range map[string]time.Time{"first.ys": earlier, "second.ys": later} {
		path := filepath.Join(dir, name)
		saveFixture(t, path)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
		// A filesystem that cannot hold the delta would make the ordering
		// vacuous rather than wrong.
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if !stat.ModTime().UTC().Equal(stamp) {
			t.Skipf("filesystem truncated the modification time to %s; the 10 µs delta this pins is not representable here",
				stat.ModTime().UTC().Format(time.RFC3339Nano))
		}
	}

	var entries []struct {
		Name    string `json:"name"`
		ModTime string `json:"mod_time"`
	}
	code, out, errOut := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--format", "json")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\nstdout:\n%s", err, out)
	}

	rendered := make(map[string]string, len(entries))
	for _, entry := range entries {
		rendered[entry.Name] = entry.ModTime
	}
	first, second := rendered["first.ys"], rendered["second.ys"]
	if first == "" || second == "" {
		t.Fatalf("both entries need a mod_time; got %q and %q", first, second)
	}
	if first >= second {
		t.Errorf("mod_time %q does not sort before %q, but the file is older — a text ordering built on this key is wrong",
			first, second)
	}
	if len(first) != len(second) {
		t.Errorf("mod_time widths differ (%d and %d): %q and %q — a variable-width fraction cannot sort as text",
			len(first), len(second), first, second)
	}

	// A layout without an offset does not parse as RFC 3339, and one that
	// truncates the fraction parses to a different instant.
	parsed, err := time.Parse(time.RFC3339, second)
	if err != nil {
		t.Fatalf("mod_time %q does not parse as RFC 3339: %v", second, err)
	}
	if !parsed.Equal(later) {
		t.Errorf("mod_time %q parses to %s, want %s — the layout dropped resolution the filesystem recorded",
			second, parsed.UTC().Format(time.RFC3339Nano), later.Format(time.RFC3339Nano))
	}
}

// TestSnapshotInfo_MetadataRendersSorted pins B46. Both text renderers walked
// the metadata map directly, so one file printed its annotations in a
// different order on consecutive runs and no diff of two reports was
// trustworthy.
func TestSnapshotInfo_MetadataRendersSorted(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "a.ys")
	saveFixture(t, file)

	const want = "Metadata:   alpha=2, mid=3, zebra=1"

	for _, mode := range []struct {
		name string
		args []string
	}{
		{"full", []string{"snapshot", "info", file}},
		{"header only", []string{"snapshot", "info", "--header-only", file}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()

			// Map iteration is randomized per range, so one run can be
			// sorted by chance; the assertion is on every run.
			for range 16 {
				code, out, errOut := executeCmdOutput(t, mode.args...)
				if code != cli.ExitOK {
					t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
				}
				if !strings.Contains(out, want) {
					t.Fatalf("stdout does not contain %q — metadata renders unsorted:\n%s", want, out)
				}
			}
		})
	}
}

// TestSnapshotInfo_MetadataSortIsTheKeyOrder guards the test above against
// passing on a fixture whose insertion order already matches sorted order.
func TestSnapshotInfo_MetadataSortIsTheKeyOrder(t *testing.T) {
	t.Parallel()

	keys := []string{"zebra", "alpha", "mid"}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	if strings.Join(keys, ",") == strings.Join(sorted, ",") {
		t.Fatal("the fixture's keys are already in sorted order, so the sort assertion cannot fail")
	}
}

// TestRoot_FormatFlagDocumentsPayloadShaping pins B34's confirmed half. The
// root called --format the "diagnostic output format" and said it applies to
// commands that produce diagnostics, while snapshot info shapes its whole
// stdout payload with it.
func TestRoot_FormatFlagDocumentsPayloadShaping(t *testing.T) {
	t.Parallel()

	code, out, errOut := executeCmdOutput(t, "--help")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	if !strings.Contains(out, "snapshot info") {
		t.Errorf("--format's help does not name the command whose stdout payload it shapes:\n%s", out)
	}
	if strings.Contains(out, "Applies to all commands that produce diagnostics.") {
		t.Errorf("--format is still described as reaching diagnostics alone:\n%s", out)
	}
}

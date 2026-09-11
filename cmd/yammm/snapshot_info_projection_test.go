package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// overflowFixture carries more header issues than the reader's limit stores,
// so its result is truncated and its diagnostics carry the truncation state.
const overflowFixture = "testdata/header_issues_overflow.ys"

// TestSnapshotInfo_TextAndJSONRenderOneProjection: the two modes render one
// structure, so a field one carries the other carries too.
func TestSnapshotInfo_TextAndJSONRenderOneProjection(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "a.ys")
	saveFixture(t, file)

	t.Run("header-only text reports the file size the JSON reports", func(t *testing.T) {
		t.Parallel()
		doc, _ := decodeStdout(t, "snapshot", "info", "--header-only", "--format", "json", file).(map[string]any)
		size, _ := doc["file_size"].(float64)
		_, text, _ := executeCmdOutput(t, "snapshot", "info", "--header-only", file)
		var failure string
		if !strings.Contains(text, strconv.FormatInt(int64(size), 10)) {
			failure = fmt.Sprintf("the text mode does not report the file size %d the JSON mode reports:\n%s", int64(size), text)
		}
		checkRepairState(t, "snapshot info: header-only text reports the file size", failure)
	})

	t.Run("an absent map or list is empty, never null", func(t *testing.T) {
		t.Parallel()
		plain := filepath.Join(t.TempDir(), "plain.ys")
		code, _, stderr := executeCmdOutput(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", plain)
		if code != cli.ExitOK {
			t.Fatalf("save: exit %d\n%s", code, stderr)
		}
		for _, mode := range [][]string{
			{"snapshot", "info", "--format", "json", plain},
			{"snapshot", "info", "--header-only", "--format", "json", plain},
		} {
			doc, _ := decodeStdout(t, mode...).(map[string]any)
			var failure string
			if _, ok := doc["metadata"].(map[string]any); !ok {
				failure = fmt.Sprintf("metadata = %#v, want an object; types renders [] for the same absence", doc["metadata"])
			}
			if _, ok := doc["features"].([]any); !ok {
				failure += fmt.Sprintf(" features = %#v, want an array", doc["features"])
			}
			checkRepairState(t, "snapshot info: absent metadata renders as an object, "+strings.Join(mode[2:len(mode)-1], " "), failure)
		}
	})
}

// TestSnapshotInfo_DirEntriesCarryTheDiagnosticWire: a directory entry's
// result is the same wire object stderr carries — every detail and the
// truncation state — and a warned row in text names its warning.
func TestSnapshotInfo_DirEntriesCarryTheDiagnosticWire(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	copyFile(t, overflowFixture, filepath.Join(dir, "overflow.ys"))
	copyFile(t, hashAlgoFixture, filepath.Join(dir, "unverifiable.ys"))

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		_, out, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--format", "json")
		var entries []map[string]any
		if err := json.Unmarshal([]byte(out), &entries); err != nil {
			t.Fatalf("stdout is not a JSON array: %v\n%s", err, out)
		}
		var failure string
		for _, entry := range entries {
			if entry["name"] != "overflow.ys" {
				continue
			}
			wire, ok := entry["diagnostics"].(map[string]any)
			if !ok {
				failure = fmt.Sprintf("the entry carries %#v under diagnostics, want the diagnostic wire object", entry["diagnostics"])
				break
			}
			var missing []string
			for _, key := range []string{"issues", "limitReached", "droppedCount"} {
				if _, present := wire[key]; !present {
					missing = append(missing, key)
				}
			}
			if len(missing) > 0 {
				failure = "the wire lacks " + strings.Join(missing, ", ")
			}
			if _, present := wire["limit"]; present {
				failure = "the wire carries limit, a collector's setting and not a fact about the entry"
			}
		}
		checkRepairState(t, "snapshot info: a directory entry carries the diagnostic wire", failure)
	})

	t.Run("text names a warned entry's warning", func(t *testing.T) {
		t.Parallel()
		_, out, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir)
		line := lineContaining(t, out, "unverifiable.ys")
		var failure string
		if !strings.Contains(line, "E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM") {
			failure = "the warned row names no warning: " + line
		}
		checkRepairState(t, "snapshot info: a warned directory row names its warning", failure)
	})
}

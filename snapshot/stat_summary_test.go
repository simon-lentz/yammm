package snapshot_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/snapshot"
)

// Stat's counts are the duplicates and the unresolved records Unmarshal reads,
// each from its own section, and its header states the schema's name.
func TestStat_CountsEachSectionAndStatesTheSchemaName(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{
			"$type": "Person", "id": "p1", "boss": map[string]any{"_target_id": "p1"},
			"friends": []any{map[string]any{"_target_id": "p8"}, map[string]any{"_target_id": "p9"}},
		},
		map[string]any{"$type": "Person", "id": "p1", "boss": map[string]any{"_target_id": "p1"}, "friends": []any{map[string]any{"_target_id": "p1"}}},
	)
	snap, readErr := snapshot.Unmarshal(t.Context(), []byte(doc), s)
	if readErr != nil {
		t.Fatalf("unmarshal: %s", readErr)
	}
	dups, unresolved := len(slices.Collect(snap.Duplicates())), len(slices.Collect(snap.Unresolved()))
	if dups != 1 || unresolved != 2 {
		t.Fatalf("fixture holds %d duplicates and %d unresolved records, want 1 and 2", dups, unresolved)
	}

	info, readErr := snapshot.Stat(t.Context(), []byte(doc))
	if readErr != nil {
		t.Fatalf("stat: %s", readErr)
	}
	if info.DuplicateCount != dups || info.UnresolvedCount != unresolved {
		t.Errorf("Stat counts %d duplicates and %d unresolved, want %d and %d", info.DuplicateCount, info.UnresolvedCount, dups, unresolved)
	}
	if info.IntegrityStatus != snapshot.IntegrityOK {
		t.Errorf("IntegrityStatus = %v, want ok", info.IntegrityStatus)
	}

	var wire struct {
		Header struct {
			SchemaName string `json:"schema_name"`
		} `json:"yammm_snapshot"`
	}
	if err := json.Unmarshal([]byte(doc), &wire); err != nil || wire.Header.SchemaName == "" {
		t.Fatalf("fixture states no schema_name: %v", err)
	}
	hdr, readErr2 := snapshot.ReadHeader(t.Context(), strings.NewReader(doc))
	if readErr2 != nil {
		t.Fatalf("read header: %s", readErr2)
	}
	if info.SchemaName != wire.Header.SchemaName || hdr.SchemaName != wire.Header.SchemaName {
		t.Errorf("SchemaName: Stat %q, ReadHeader %q, want %q", info.SchemaName, hdr.SchemaName, wire.Header.SchemaName)
	}
}

// The zero IntegrityStatus names no status, so its text is neither "ok" nor
// "mismatch".
func TestIntegrityStatus_TheZeroNamesNoStatus(t *testing.T) {
	t.Parallel()
	var zero snapshot.IntegrityStatus
	if zero == snapshot.IntegrityOK || zero == snapshot.IntegrityMismatch {
		t.Fatal("the zero IntegrityStatus is a named status")
	}
	if got := zero.String(); got == "ok" || got == "mismatch" {
		t.Errorf("IntegrityStatus(0).String() = %q, a status's text", got)
	}
}

// Stat stores at most 100 issues, the cap its doc states, and counts the rest.
func TestStat_StoresAtMostOneHundredIssues(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	friends := make([]any, 150)
	for i := range friends {
		friends[i] = map[string]any{"_target_id": "missing" + strings.Repeat("x", i)}
	}
	doc := readerDoc(t, s,
		map[string]any{"$type": "Person", "id": "p1", "boss": map[string]any{"_target_id": "p1"}, "friends": friends})
	if n := strings.Count(doc, `"reason":"target_missing"`); n != 150 {
		t.Fatalf("fixture holds %d target_missing records, want 150", n)
	}
	bogus := rehashDocument(t, []byte(strings.ReplaceAll(doc, `"reason":"target_missing"`, `"reason":"bogus"`)))

	_, res := diagtest.Issues(t, func() (*snapshot.Info, error) {
		return snapshot.Stat(t.Context(), bogus)
	})
	if res.Len() != 100 || res.DroppedCount() != 50 {
		t.Errorf("Stat stored %d issues and dropped %d, want 100 and 50: %s", res.Len(), res.DroppedCount(), res)
	}
}

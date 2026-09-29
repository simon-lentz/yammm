package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The reader half of the root type the entry schema cannot name.
//
// A root is keyed by name in every output document, so a type reached only
// through an intermediate import cannot hold one. This library cannot write
// such a document — graph.RebuildSnapshot refuses it — but a foreign writer or
// a document written before the rule can state one, and a reader that admitted
// it would hand the caller a snapshot no adapter can key.

const unreachableRootEntry = `schema "entry"

import "base.yammm" as base
`

const unreachableRootBase = `schema "base"

import "deep.yammm" as deep

type Basin {
	id String primary
}
`

const unreachableRootDeep = `schema "deep"

type Beacon {
	id String primary
	power Float
}
`

// unnameableDocument marshals a legal Basin-only snapshot, then ADDS a types
// table row for the transitively imported Beacon and points the populated group
// at it. Both constructors now refuse such a type in SnapshotParts.Types, so the
// document can only be built by editing the wire — which is exactly the shape a
// foreign writer, or a document written before the rule, can carry.
func unnameableDocument(t *testing.T, items string) ([]byte, *schema.Schema) {
	t.Helper()
	ctx := context.Background()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(unreachableRootEntry),
		"base.yammm":  []byte(unreachableRootBase),
		"deep.yammm":  []byte(unreachableRootDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load fixture: %s", res)
	}

	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema() == nil {
		t.Fatal("import alias base did not resolve")
	}
	basinType, ok := base.Schema().Type("Basin")
	if !ok {
		t.Fatal("base.Basin not found")
	}
	deepImp, ok := base.Schema().ImportByAlias("deep")
	if !ok || deepImp.Schema() == nil {
		t.Fatal("import alias deep did not resolve")
	}
	beaconType, ok := deepImp.Schema().Type("Beacon")
	if !ok {
		t.Fatal("deep.Beacon not found")
	}
	if _, addressable := schema.AddressableTag(s, beaconType.ID()); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Beacon")
	}
	basin := basinType.ID()

	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{basin},
		Instances: []graph.InstanceParts{
			{
				TypeID:     basin,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1"}),
			},
		},
	})
	if res.HasErrors() {
		t.Fatalf("assembling the legal base document: %s", res)
	}
	data, mres := snapshot.Marshal(ctx, built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	return addUnnameableGroup(t, data, items), s
}

// addUnnameableGroup appends a types-table row for deep#Beacon and replaces the
// instances section with a single group pointing at it. items is the group's
// "items" array, so a caller chooses between a populated group and an empty one.
func addUnnameableGroup(t *testing.T, data []byte, items string) []byte {
	t.Helper()
	var doc struct {
		Types []json.RawMessage `json:"types"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	rows := make([]json.RawMessage, len(doc.Types))
	copy(rows, doc.Types)
	rows = append(rows, json.RawMessage(`{"schema":"deep","name":"Beacon"}`))
	added := len(rows) - 1

	types, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal the types table: %v", err)
	}
	out := spliceJSONValue(t, data, "types", types)
	group := fmt.Sprintf(`[{"type":%d,"items":%s}]`, added, items)
	return spliceJSONValue(t, out, "instances", []byte(group))
}

// spliceJSONValue replaces the value of a top-level key with replacement,
// keeping every other byte of data as it stands. It matches brackets rather
// than searching for a terminator so a nested array cannot end the scan early.
func spliceJSONValue(t *testing.T, data []byte, key string, replacement []byte) []byte {
	t.Helper()
	marker := []byte(`"` + key + `":`)
	at := bytes.Index(data, marker)
	if at < 0 {
		t.Fatalf("document has no %q key: %s", key, data)
	}
	valueStart := at + len(marker)
	if valueStart >= len(data) || data[valueStart] != '[' {
		t.Fatalf("the %q value is not an array: %s", key, data)
	}

	depth, inString, escaped, end := 0, false, false, -1
	for i := valueStart; i < len(data); i++ {
		c := data[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatalf("the %q array is unterminated: %s", key, data)
	}

	out := make([]byte, 0, len(data)-(end-valueStart)+len(replacement))
	out = append(out, data[:valueStart]...)
	out = append(out, replacement...)
	out = append(out, data[end:]...)
	return out
}

// TestLoad_UnnameableDenotedTypeRefused pins the reader's member of the denoted
// type rule, over a group that HOLDS instances.
//
// Mutation: dropping the schema.Addressable arm from checkRootTypeEligible turns
// this red.
func TestLoad_UnnameableDenotedTypeRefused(t *testing.T) {
	t.Parallel()
	data, s := unnameableDocument(t, `[{"key":["d1"],"properties":{"id":"d1"},"provenance":null}]`)

	_, res := snapshot.Load(context.Background(), data, s, snapshot.WithIntegrityCheck(false))
	if !res.HasCode(diag.E_SNAPSHOT_UNNAMEABLE_TYPE) {
		t.Fatalf("Load accepted a document denoting a type the entry schema cannot name: %s", res)
	}
	var hinted, schemaNamed bool
	for issue := range res.Issues() {
		if issue.Code() != diag.E_SNAPSHOT_UNNAMEABLE_TYPE {
			continue
		}
		if issue.Hint() != "" {
			hinted = true
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyTypeSchema && d.Value != "" {
				schemaNamed = true
			}
		}
		if issue.Severity() == diag.Fatal {
			t.Errorf("a foreign document drew a Fatal issue, not an Error: %s", issue.Message())
		}
	}
	if !hinted {
		t.Error("the refusal carries no hint telling the caller to import the schema directly")
	}
	// The row renders as a bare name, so the declaring schema is the only thing
	// that tells the caller WHICH type to import.
	if !schemaNamed {
		t.Errorf("the refusal carries no %s detail", diag.DetailKeyTypeSchema)
	}
}

// TestLoad_UnnameableEmptyGroupRefused is the half the empty-group exemption used
// to swallow. A group with no items still DENOTES its type, and every writer keys
// its output by a denoted type's name, so nameability binds here where the three
// root members do not.
//
// Mutation: restoring the `items == 0` early return above the nameability check
// turns this red and leaves the test above green.
func TestLoad_UnnameableEmptyGroupRefused(t *testing.T) {
	t.Parallel()
	data, s := unnameableDocument(t, `[]`)

	_, res := snapshot.Load(context.Background(), data, s, snapshot.WithIntegrityCheck(false))
	if !res.HasCode(diag.E_SNAPSHOT_UNNAMEABLE_TYPE) {
		t.Fatalf("Load accepted an empty group denoting an unnameable type: %s", res)
	}
}

// TestLoad_UnnameableDenotedTypeIsRefusedWhateverTheOptions pins that no load
// option excuses the member. Revalidation checks an instance's PROPERTIES, which
// says nothing about whether the schema can name its type.
func TestLoad_UnnameableDenotedTypeIsRefusedWhateverTheOptions(t *testing.T) {
	t.Parallel()
	data, s := unnameableDocument(t, `[{"key":["d1"],"properties":{"id":"d1"},"provenance":null}]`)
	ctx := context.Background()

	for name, opts := range map[string][]snapshot.LoadOption{
		"integrity off": {snapshot.WithIntegrityCheck(false)},
		"revalidation":  {snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error)},
	} {
		if _, res := snapshot.Load(ctx, data, s, opts...); !res.HasCode(diag.E_SNAPSHOT_UNNAMEABLE_TYPE) {
			t.Errorf("%s: Load accepted the document: %s", name, res)
		}
	}
}

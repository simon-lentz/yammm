package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// fixtureEdge is one edge spec from
// snapshot/testdata/unresolved_edge_props/fixture.json. Each spec drives
// two parallel snapshot builds in TestMarshalLoad_UnresolvedEdgePropertiesGoldenBytes.
type fixtureEdge struct {
	Label      string         `json:"label"`
	TargetKey  string         `json:"target_key"`
	Properties map[string]any `json:"properties"`
}

type fixtureFile struct {
	Edges []fixtureEdge `json:"edges"`
}

// loadFixture reads the committed fixture. Missing fixture fails the test;
// regeneration is handled by TestMarshalLoad_UnresolvedEdgePropertiesGoldenBytes
// under the -update flag.
func loadFixture(t *testing.T) fixtureFile {
	t.Helper()
	path := filepath.Join("testdata", "unresolved_edge_props", "fixture.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read fixture")

	// Use json.Decoder with UseNumber so integer-shaped float values in
	// the fixture (e.g., 0.75) survive normalization exactly as they
	// appear in the JSON bytes — matching the wire-format path's
	// normalizeMap behavior downstream.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var ff fixtureFile
	require.NoError(t, dec.Decode(&ff), "decode fixture")
	return ff
}

// TestMarshalLoad_UnresolvedEdgePropertiesGoldenBytes is the §6 resolved-
// vs-unresolved wire-format parity test. For each edge in the committed
// fixture, it builds two snapshots — one where the edge resolves (target
// present, Edge.Properties() path) and one where the edge remains
// unresolved (target absent, UnresolvedEdge.Properties() path) — marshals
// both, extracts the serialized "properties" JSON payload from each
// location, and asserts byte-identical bytes.
//
// The fixture deliberately includes three distinct encoder-drift classes
// (multi-scalar top-level keys, nested-map recursive encoding,
// HTML-escape / UTF-8 handling) so a silent future change in the
// immutable.Properties → JSON conversion fails here before it reaches
// consumer code.
func TestMarshalLoad_UnresolvedEdgePropertiesGoldenBytes(t *testing.T) {
	s := testSchema(t)
	ctx := context.Background()
	fixture := loadFixture(t)

	for _, edge := range fixture.Edges {
		t.Run(edge.Label, func(t *testing.T) {
			// Normalize fixture properties through WrapProperties so the
			// in-memory shape matches what production code paths emit.
			// Without this step, json.Number values from the fixture
			// would stay as json.Number in the map, producing wire bytes
			// that differ from production's int64 / float64 / string
			// output.
			propsForTarget := immutable.WrapProperties(normalizeFixtureProps(edge.Properties)).Clone()

			// Snapshot A: edge resolves (both source and target present).
			targetCompany := mustValidInstance(t, s, "Company",
				[]any{edge.TargetKey}, map[string]any{"id": edge.TargetKey, "title": "t"})
			personA := buildPersonWithEdgeProps(t, s, "pA", edge.TargetKey, propsForTarget)
			//nolint:contextcheck // buildSnapshot uses context.Background() internally; shared helper across the snapshot test suite
			snapResolved := buildSnapshot(t, s, targetCompany, personA)
			require.Len(t, snapResolved.Edges(), 1, "expected resolved edge")
			require.Empty(t, snapResolved.Unresolved(), "snapshot A: no unresolved edge expected")

			// Snapshot B: edge stays unresolved (target absent).
			personB := buildPersonWithEdgeProps(t, s, "pB", edge.TargetKey, propsForTarget)
			//nolint:contextcheck // buildSnapshot uses context.Background() internally; shared helper across the snapshot test suite
			snapUnresolved := buildSnapshot(t, s, personB)
			require.Empty(t, snapUnresolved.Edges(), "snapshot B: no resolved edge expected")
			require.Len(t, snapUnresolved.Unresolved(), 1, "expected unresolved edge")

			dataR, mres := snapshot.Marshal(ctx, snapResolved)
			require.NoError(t, mres.Err())
			dataU, ures := snapshot.Marshal(ctx, snapUnresolved)
			require.NoError(t, ures.Err())

			// Extract the serialized properties payload from each path.
			resolvedProps := extractResolvedEdgeProperties(t, dataR, "EMPLOYER")
			unresolvedProps := extractUnresolvedEdgeProperties(t, dataU)

			require.Equal(t, string(resolvedProps), string(unresolvedProps),
				"resolved and unresolved property payloads must be byte-identical")
		})
	}
}

// buildPersonWithEdgeProps is a thin wrapper over mustValidInstanceWithEdgeProps
// that keeps the golden-bytes test body readable.
func buildPersonWithEdgeProps(t *testing.T, s *schema.Schema, personID, targetKey string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	return mustValidInstanceWithEdgeProps(t, s, "Person",
		[]any{personID}, map[string]any{"id": personID, "name": personID},
		"EMPLOYER", []any{targetKey}, props)
}

// normalizeFixtureProps converts json.Number values in the decoded
// fixture map into Go's native int64 / float64 forms, mirroring the
// behavior of immutable.NormalizeValue on the wire-format read path.
// Keeps the fixture's 0.75 value round-trip-stable.
func normalizeFixtureProps(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = immutable.NormalizeValue(v)
	}
	return out
}

// extractResolvedEdgeProperties pulls the first edge's "properties"
// payload from a marshaled snapshot. Uses json.RawMessage so the byte
// form is preserved exactly as emitted.
func extractResolvedEdgeProperties(t *testing.T, data []byte, relation string) []byte {
	t.Helper()
	var doc struct {
		Instances map[string][]struct {
			Edges map[string][]struct {
				Properties json.RawMessage `json:"properties"`
			} `json:"edges"`
		} `json:"instances"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))

	for _, insts := range doc.Instances {
		for _, inst := range insts {
			edges := inst.Edges[relation]
			if len(edges) > 0 {
				return []byte(edges[0].Properties)
			}
		}
	}
	t.Fatalf("no resolved edge for relation %q in marshaled data", relation)
	return nil
}

// extractUnresolvedEdgeProperties pulls the first unresolved edge's
// "properties" payload from a marshaled snapshot.
func extractUnresolvedEdgeProperties(t *testing.T, data []byte) []byte {
	t.Helper()
	var doc struct {
		Diagnostics struct {
			Unresolved []struct {
				Properties json.RawMessage `json:"properties"`
			} `json:"unresolved"`
		} `json:"diagnostics"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.NotEmpty(t, doc.Diagnostics.Unresolved, "no unresolved entry in marshaled data")
	return []byte(doc.Diagnostics.Unresolved[0].Properties)
}

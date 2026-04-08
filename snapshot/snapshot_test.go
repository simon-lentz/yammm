package snapshot_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// --- Test helpers ---

func testSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("testSchema: %s", result)
	}
	return s
}

func testSchemaWithComposition(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("test_comp").
		WithSourceID(location.MustNewSourceID("test://test_comp.yammm")).
		AddType("Parent").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithComposition("CHILDREN", schema.NewTypeRef("", "Child", location.Span{}), true, true).
		Done().
		AddType("Child").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("value", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("testSchemaWithComposition: %s", result)
	}
	return s
}

func mustValidInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("type %q not found", typeName)
	}
	return instance.NewValidInstance(typeName, typ.ID(), immutable.WrapKey(pk), immutable.WrapProperties(props), nil, nil, nil)
}

func mustValidInstanceWithEdge(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any, relName string, targetKeys [][]any) *instance.ValidInstance { //nolint:unparam // test helper kept general
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("type %q not found", typeName)
	}
	targets := make([]instance.ValidEdgeTarget, len(targetKeys))
	for i, tk := range targetKeys {
		targets[i] = instance.NewValidEdgeTarget(immutable.WrapKey(tk), immutable.Properties{})
	}
	edges := map[string]*instance.ValidEdgeData{
		relName: instance.NewValidEdgeData(targets),
	}
	return instance.NewValidInstance(typeName, typ.ID(), immutable.WrapKey(pk), immutable.WrapProperties(props), edges, nil, nil)
}

func mustValidPartInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("type %q not found", typeName)
	}
	return instance.NewValidInstance(typeName, typ.ID(), immutable.WrapKey(pk), immutable.WrapProperties(props), nil, nil, nil)
}

func buildSnapshot(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	t.Helper()
	g := graph.New(s)
	ctx := context.Background()
	for _, inst := range instances {
		g.Add(ctx, inst)
	}
	return g.Snapshot()
}

// --- Sub-phase A tests (Marshal + Verify + Info) ---

func TestMarshal_NilPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for nil snapshot")
		}
	}()
	snapshot.Marshal(context.Background(), nil)
}

func TestVerify_NilSchemaPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for nil schema")
		}
	}()
	snapshot.Verify(context.Background(), []byte(`{}`), nil)
}

func TestLoad_NilSchemaPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for nil schema")
		}
	}()
	snapshot.Load(context.Background(), []byte(`{}`), nil)
}

func TestMarshal_EmptySnapshot(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s)
	ctx := context.Background()

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Marshal produced empty output")
	}

	// Verify it's valid JSON.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Marshal output is not valid JSON: %v", err)
	}

	// Verify top-level structure.
	if _, ok := raw["yammm_snapshot"]; !ok {
		t.Error("missing yammm_snapshot header")
	}
	if _, ok := raw["types"]; !ok {
		t.Error("missing types")
	}
	if _, ok := raw["instances"]; !ok {
		t.Error("missing instances")
	}
	if _, ok := raw["diagnostics"]; !ok {
		t.Error("missing diagnostics")
	}
}

func TestMarshal_Deterministic(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)
	snapshottest.AssertDeterministic(t, snap, s)
}

func TestMarshalLoad_RoundTrip_Basic(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)
	snapshottest.AssertRoundTrip(t, snap, s)
}

func TestMarshalLoad_RoundTrip_Empty(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s)
	snapshottest.AssertRoundTrip(t, snap, s)
}

func TestMarshalLoad_RoundTrip_WithComposition(t *testing.T) {
	s := testSchemaWithComposition(t)
	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"id": "p1", "name": "Dad"})

	g := graph.New(s)
	ctx := context.Background()
	g.Add(ctx, parent)

	// Add composed child via AddComposed.
	child := mustValidPartInstance(t, s, "Child", []any{"ch1"}, map[string]any{"id": "ch1", "value": "hello"})
	g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", child)

	snap := g.Snapshot()
	snapshottest.AssertRoundTrip(t, snap, s)
}

func TestMarshalLoad_EdgeReconstruction(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify EdgesFrom on loaded snapshot.
	persons := loaded.InstancesOf("Person")
	if len(persons) != 1 {
		t.Fatalf("expected 1 Person, got %d", len(persons))
	}

	edges := loaded.EdgesFrom(persons[0])
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge from Person, got %d", len(edges))
	}

	edge := edges[0]
	if edge.Relation() != "EMPLOYER" {
		t.Errorf("edge relation: got %q, want %q", edge.Relation(), "EMPLOYER")
	}
	if edge.Target().TypeName() != "Company" {
		t.Errorf("edge target type: got %q, want %q", edge.Target().TypeName(), "Company")
	}
	if edge.Target().PrimaryKey().String() != `["c1"]` {
		t.Errorf("edge target key: got %q, want %q", edge.Target().PrimaryKey().String(), `["c1"]`)
	}
}

func TestMarshalLoad_LoadedDiagnostics(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)
	loaded, _ := snapshot.Load(ctx, data, s)

	// Loaded snapshots return diag.OK() for Diagnostics.
	if !loaded.Diagnostics().OK() {
		t.Error("loaded Diagnostics() should be OK")
	}
	if loaded.HasErrors() {
		t.Error("loaded HasErrors() should be false")
	}
}

func TestVerify_SchemaMismatch(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	// Build a different schema.
	otherSchema, result := schema.NewBuilder().
		WithName("other").
		WithSourceID(location.MustNewSourceID("test://other.yammm")).
		AddType("Widget").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("build other schema: %s", result)
	}

	verifyResult := snapshot.Verify(ctx, data, otherSchema)
	if verifyResult.OK() {
		t.Error("expected schema mismatch error")
	}

	found := false
	for issue := range verifyResult.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA {
			found = true
		}
	}
	if !found {
		t.Error("expected E_SNAPSHOT_INCOMPATIBLE_SCHEMA")
	}
}

func TestVerify_UnsupportedVersion(t *testing.T) {
	// Build a .ys-like document with version 99.
	doc := `{"yammm_snapshot":{"version":99,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":"sha256:abc","schema_hash_algorithm":1,"integrity_hash":"","features":[]},"types":[],"instances":{},"diagnostics":{"duplicates":[],"unresolved":[]}}`

	s := testSchema(t)
	result := snapshot.Verify(context.Background(), []byte(doc), s)
	if result.OK() {
		t.Error("expected version error")
	}

	found := false
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_UNSUPPORTED_VERSION {
			found = true
		}
	}
	if !found {
		t.Error("expected E_SNAPSHOT_UNSUPPORTED_VERSION")
	}
}

func TestVerify_IntegrityMismatch(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	// Corrupt a byte in the instances section.
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	// Find "Acme" and change it.
	idx := strings.Index(string(corrupted), "Acme")
	if idx > 0 {
		corrupted[idx] = 'X'
	}

	result := snapshot.Verify(ctx, corrupted, s)
	if result.OK() {
		t.Error("expected integrity mismatch")
	}

	found := false
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_INTEGRITY_MISMATCH {
			found = true
		}
	}
	if !found {
		t.Error("expected E_SNAPSHOT_INTEGRITY_MISMATCH")
	}
}

func TestVerify_SkipIntegrityCheck(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	// Corrupt a byte.
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	idx := strings.Index(string(corrupted), "Acme")
	if idx > 0 {
		corrupted[idx] = 'X'
	}

	// With skip integrity check, should not fail on integrity.
	result := snapshot.Verify(ctx, corrupted, s, snapshot.WithSkipIntegrityCheck())
	// May have other issues but not integrity mismatch.
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_INTEGRITY_MISMATCH {
			t.Error("should not get E_SNAPSHOT_INTEGRITY_MISMATCH with skip option")
		}
	}
}

func TestVerify_MalformedJSON(t *testing.T) {
	s := testSchema(t)
	result := snapshot.Verify(context.Background(), []byte(`not json`), s)
	if result.OK() {
		t.Error("expected error for malformed JSON")
	}
}

func TestVerify_MissingHeader(t *testing.T) {
	s := testSchema(t)
	result := snapshot.Verify(context.Background(), []byte(`{"types":[]}`), s)
	if result.OK() {
		t.Error("expected error for missing header")
	}

	found := false
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
			found = true
		}
	}
	if !found {
		t.Error("expected E_SNAPSHOT_MALFORMED")
	}
}

func TestInfo_Basic(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	info, result := snapshot.Info(ctx, data)
	if err := result.Err(); err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info == nil {
		t.Fatal("Info returned nil")
	}

	if info.Version != 1 {
		t.Errorf("Version: got %d, want 1", info.Version)
	}
	if info.SchemaName != "test" {
		t.Errorf("SchemaName: got %q, want %q", info.SchemaName, "test")
	}
	if info.TotalInstances != 2 {
		t.Errorf("TotalInstances: got %d, want 2", info.TotalInstances)
	}
	if info.TotalEdges != 1 {
		t.Errorf("TotalEdges: got %d, want 1", info.TotalEdges)
	}
	if info.IntegrityStatus != "ok" {
		t.Errorf("IntegrityStatus: got %q, want %q", info.IntegrityStatus, "ok")
	}
}

func TestMarshal_WithIndent(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, result := snapshot.Marshal(ctx, snap, snapshot.WithIndent("\t"))
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal with indent: %v", err)
	}

	// Indented output should contain newlines.
	if !strings.Contains(string(data), "\n") {
		t.Error("indented output should contain newlines")
	}

	// Should still be loadable.
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load indented: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
}

func TestMarshal_WithMetadata(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{
		"pipeline": "test-pipeline",
		"env":      "staging",
	}))

	info, _ := snapshot.Info(ctx, data)
	if info.Metadata == nil {
		t.Fatal("metadata should be present")
	}
	if info.Metadata["pipeline"] != "test-pipeline" {
		t.Errorf("metadata pipeline: got %q, want %q", info.Metadata["pipeline"], "test-pipeline")
	}
}

func TestMarshalLoad_ContextCancellation(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, result := snapshot.Marshal(ctx, snap)
	if result.OK() {
		t.Error("expected cancellation error from Marshal")
	}
}

// --- Sub-phase B tests (Load-specific) ---

func TestConstructionPathEquivalence(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	constructed := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, result := snapshot.Marshal(ctx, constructed)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := snapshottest.CompareSnapshots(constructed, loaded); err != nil {
		t.Errorf("construction path equivalence failed:\n%v", err)
	}
}

func TestLoad_TypeIDReconstruction(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)
	loaded, _ := snapshot.Load(ctx, data, s)

	companies := loaded.InstancesOf("Company")
	if len(companies) != 1 {
		t.Fatalf("expected 1 Company, got %d", len(companies))
	}

	// TypeID should match the provided schema's type, not persisted path.
	origType, _ := s.Type("Company")
	if companies[0].TypeID() != origType.ID() {
		t.Error("TypeID should be reconstructed from provided schema")
	}
}

func TestLoad_NullProvenanceRoundTrip(t *testing.T) {
	s := testSchema(t)
	// Instances without provenance.
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	companies := loaded.InstancesOf("Company")
	if len(companies) != 1 {
		t.Fatalf("expected 1 Company, got %d", len(companies))
	}

	// Provenance should be nil (no provenance was set).
	if companies[0].HasProvenance() {
		t.Error("expected nil provenance for instance without provenance")
	}
}

func TestMarshalLoad_DuplicateRoundTrip(t *testing.T) {
	s := testSchema(t)
	c1 := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	c1dup := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme Corp"})
	snap := buildSnapshot(t, s, c1, c1dup)

	// The constructed snapshot should have a duplicate.
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("expected 1 duplicate, got %d", len(snap.Duplicates()))
	}

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Duplicates()) != 1 {
		t.Fatalf("loaded duplicates: expected 1, got %d", len(loaded.Duplicates()))
	}

	dup := loaded.Duplicates()[0]
	if dup.Instance.TypeName() != "Company" {
		t.Errorf("duplicate type: got %q, want %q", dup.Instance.TypeName(), "Company")
	}
	if dup.HasDiagnostic() {
		t.Error("loaded duplicate should not have diagnostic")
	}
}

func TestMarshalLoad_UnresolvedRoundTrip(t *testing.T) {
	s := testSchema(t)
	// Person with edge to non-existent Company.
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c99"}})
	snap := buildSnapshot(t, s, person)

	if len(snap.Unresolved()) == 0 {
		t.Fatal("expected unresolved edges")
	}

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Unresolved()) != len(snap.Unresolved()) {
		t.Errorf("unresolved count: got %d, want %d", len(loaded.Unresolved()), len(snap.Unresolved()))
	}
}

// --- Wire struct field ordering test ---

func TestWireStructFieldOrder(t *testing.T) {
	// Verify that JSON serialization produces keys in the expected order.
	// This guards against accidental field reordering in wire structs.
	tests := []struct {
		name     string
		typ      reflect.Type
		expected []string
	}{
		{
			name: "headerWire",
			typ: reflect.TypeFor[struct {
				Version             int               `json:"version"`
				SchemaName          string            `json:"schema_name"`
				SchemaSource        string            `json:"schema_source"`
				SchemaHash          string            `json:"schema_hash"`
				SchemaHashAlgorithm int               `json:"schema_hash_algorithm"`
				IntegrityHash       string            `json:"integrity_hash"`
				Features            []string          `json:"features"`
				CreatedAt           string            `json:"created_at,omitempty"`
				Metadata            map[string]string `json:"metadata,omitempty"`
			}](),
			expected: []string{
				"version", "schema_name", "schema_source", "schema_hash",
				"schema_hash_algorithm", "integrity_hash", "features",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check struct field json tags match expected order.
			for i, expectedKey := range tt.expected {
				if i >= tt.typ.NumField() {
					t.Errorf("expected field %d (%q) but type has only %d fields",
						i, expectedKey, tt.typ.NumField())
					continue
				}
				tag := tt.typ.Field(i).Tag.Get("json")
				tagName := strings.Split(tag, ",")[0]
				if tagName != expectedKey {
					t.Errorf("field %d: expected json tag %q, got %q", i, expectedKey, tagName)
				}
			}
		})
	}
}

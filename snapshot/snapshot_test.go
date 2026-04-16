package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestHeaderOnly_Basic(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	header, result := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, result.Err(), "HeaderOnly should succeed")
	require.NotNil(t, header, "HeaderOnly should return a non-nil HeaderInfo")

	assert.Equal(t, 1, header.Version, "Version")
	assert.Equal(t, "test", header.SchemaName, "SchemaName")
	assert.NotEmpty(t, header.SchemaHash, "SchemaHash should be populated")
	assert.NotEmpty(t, header.IntegrityHash, "IntegrityHash should be populated (value, not verified)")
	assert.Equal(t, int64(len(data)), header.FileSize, "FileSize")
	assert.ElementsMatch(t, []string{"Company", "Person"}, header.Types, "Types should include both types")
}

func TestHeaderOnly_WithMetadata(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{
		"pipeline":            "msrb.emma_issues",
		"extraction_complete": "true",
	}))

	header, result := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, result.Err())
	require.NotNil(t, header.Metadata, "Metadata should be present")
	assert.Equal(t, "msrb.emma_issues", header.Metadata["pipeline"])
	assert.Equal(t, "true", header.Metadata["extraction_complete"])
}

func TestHeaderOnly_WithCreatedAt(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	// Fixed time so we can assert on the serialized form.
	created, err := time.Parse(time.RFC3339, "2026-04-16T14:30:00Z")
	require.NoError(t, err)
	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithCreatedAt(created))

	header, result := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, result.Err())
	assert.Equal(t, "2026-04-16T14:30:00Z", header.CreatedAt, "CreatedAt should round-trip as RFC 3339")
}

func TestHeaderOnly_CreatedAtOmitted(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap) // No WithCreatedAt — default is empty.

	header, result := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, result.Err())
	assert.Empty(t, header.CreatedAt, "CreatedAt should be empty when not set during marshal")
}

func TestHeaderOnly_ConsistentWithInfo(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{"key": "val"}))

	info, infoRes := snapshot.Info(ctx, data)
	require.NoError(t, infoRes.Err())
	header, headerRes := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, headerRes.Err())

	// Every field common to both APIs must match byte-for-byte.
	assert.Equal(t, info.Version, header.Version)
	assert.Equal(t, info.Features, header.Features)
	assert.Equal(t, info.SchemaName, header.SchemaName)
	assert.Equal(t, info.SchemaSource, header.SchemaSource)
	assert.Equal(t, info.SchemaHash, header.SchemaHash)
	assert.Equal(t, info.SchemaHashAlgorithm, header.SchemaHashAlgorithm)
	assert.Equal(t, info.IntegrityHash, header.IntegrityHash)
	assert.Equal(t, info.CreatedAt, header.CreatedAt)
	assert.Equal(t, info.Metadata, header.Metadata)
	assert.Equal(t, info.Types, header.Types)
	assert.Equal(t, info.FileSize, header.FileSize)
}

func TestHeaderOnly_IntegrityNotVerified(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	// Sanity-check: Info on clean data succeeds with IntegrityStatus == "ok".
	info, _ := snapshot.Info(ctx, data)
	require.Equal(t, "ok", info.IntegrityStatus, "clean data should verify integrity")

	// Corrupt a byte inside the instance body (Acme → Acmf). The header is
	// unchanged; the integrity hash in the header covers the whole document
	// so Info must report a mismatch.
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	corruptIdx := bytes.Index(corrupted, []byte("Acme"))
	require.Positive(t, corruptIdx, "should find Acme string to corrupt")
	corrupted[corruptIdx+3] = 'f'

	// Info reports a mismatch — it checks the full document.
	infoCorrupt, _ := snapshot.Info(ctx, corrupted)
	assert.Equal(t, "mismatch", infoCorrupt.IntegrityStatus, "Info should detect body corruption")

	// HeaderOnly succeeds — it does not verify integrity. The stored hash is
	// returned as-is, callers who want verification use Verify.
	header, headerRes := snapshot.HeaderOnly(ctx, corrupted)
	require.NoError(t, headerRes.Err(), "HeaderOnly should not error on body corruption")
	assert.NotEmpty(t, header.IntegrityHash, "HeaderOnly returns the stored hash value")
	assert.Equal(t, info.IntegrityHash, header.IntegrityHash,
		"the stored integrity hash is unchanged by body corruption")
}

func TestHeaderOnly_MalformedJSON(t *testing.T) {
	_, result := snapshot.HeaderOnly(context.Background(), []byte(`not json`))
	assert.False(t, result.OK(), "HeaderOnly should error on malformed JSON")
}

func TestHeaderOnly_EmptyInput(t *testing.T) {
	_, result := snapshot.HeaderOnly(context.Background(), nil)
	assert.False(t, result.OK(), "HeaderOnly should error on nil input")
}

func TestHeaderOnly_MissingHeader(t *testing.T) {
	_, result := snapshot.HeaderOnly(context.Background(), []byte(`{"types":[]}`))
	assert.False(t, result.OK(), "HeaderOnly should error when yammm_snapshot header is absent")

	var found bool
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
			found = true
		}
	}
	assert.True(t, found, "missing header should produce E_SNAPSHOT_MALFORMED")
}

func TestHeaderOnly_ContextCancellation(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	data, _ := snapshot.Marshal(context.Background(), snap)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, result := snapshot.HeaderOnly(ctx, data)
	assert.False(t, result.OK(), "cancelled context should produce a diagnostic")

	var found bool
	for issue := range result.BySeverity(diag.Fatal) {
		if issue.Code() == diag.E_CONTEXT_CANCELLED {
			found = true
		}
	}
	assert.True(t, found, "cancelled context should produce E_CONTEXT_CANCELLED at Fatal severity")
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

// These tests verify that all property types survive the
// marshal → load round-trip with correct values. The existing
// round-trip tests above only exercise String properties.

// fidelitySchema loads an inline schema with a single property of the given
// type declaration and returns both the schema and a validator.
func fidelitySchema(t *testing.T, propDecl string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	src := `schema "PropTest"
type Item {
    id String primary
    val ` + propDecl + `
}`
	s, result := schema.LoadString(t.Context(), src, "proptest")
	require.False(t, result.HasErrors(), "fidelitySchema: %s", result)
	return s, instance.NewValidator(s)
}

// fidelityRoundTrip validates an instance, builds a graph+snapshot,
// marshals, loads, and returns the loaded property map.
func fidelityRoundTrip(t *testing.T, s *schema.Schema, v *instance.Validator, val any) map[string]any {
	t.Helper()
	raw := instance.RawInstance{Properties: map[string]any{"id": "x", "val": val}}
	valid, result := v.ValidateOne(t.Context(), "Item", raw)
	require.True(t, result.OK(), "validate: %s", result)
	require.NotNil(t, valid)

	g := graph.New(s)
	g.Add(t.Context(), valid)
	snap := g.Snapshot()

	data, mr := snapshot.Marshal(t.Context(), snap)
	require.True(t, mr.OK(), "marshal: %s", mr)

	loaded, lr := snapshot.Load(t.Context(), data, s)
	require.True(t, lr.OK(), "load: %s", lr)

	items := loaded.InstancesOf("Item")
	require.Len(t, items, 1)
	return items[0].Properties().Clone()
}

func TestMarshalLoad_PropertyFidelity(t *testing.T) {
	t.Parallel()

	t.Run("int64_large_value", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Integer")
		props := fidelityRoundTrip(t, s, v, int64(9007199254740993))
		assert.Equal(t, int64(9007199254740993), props["val"])
	})

	t.Run("int64_boundary", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Integer")
		props := fidelityRoundTrip(t, s, v, int64(math.MaxInt64))
		assert.Equal(t, int64(math.MaxInt64), props["val"])
	})

	t.Run("float_scientific_notation", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Float")
		props := fidelityRoundTrip(t, s, v, 1.5e10)
		assert.InDelta(t, 1.5e10, props["val"], 1.0)
	})

	t.Run("float_type_narrowing", func(t *testing.T) {
		t.Parallel()
		// float64(1.0) marshals as JSON "1", normalizes to int64(1) on load.
		// The typed accessor should still return 1.0 via Float().
		s, v := fidelitySchema(t, "Float")
		raw := instance.RawInstance{Properties: map[string]any{"id": "x", "val": float64(1.0)}}
		valid, result := v.ValidateOne(t.Context(), "Item", raw)
		require.True(t, result.OK())

		g := graph.New(s)
		g.Add(t.Context(), valid)
		snap := g.Snapshot()

		data, mr := snapshot.Marshal(t.Context(), snap)
		require.True(t, mr.OK())
		loaded, lr := snapshot.Load(t.Context(), data, s)
		require.True(t, lr.OK())

		items := loaded.InstancesOf("Item")
		require.Len(t, items, 1)
		val, ok := items[0].Properties().Get("val")
		require.True(t, ok)
		f, fok := val.Float()
		require.True(t, fok)
		assert.Equal(t, float64(1.0), f)
	})

	t.Run("boolean_true_false", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Boolean")
		props := fidelityRoundTrip(t, s, v, true)
		assert.Equal(t, true, props["val"])

		props = fidelityRoundTrip(t, s, v, false)
		assert.Equal(t, false, props["val"])
	})

	t.Run("string_unicode", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "String")
		props := fidelityRoundTrip(t, s, v, "café ☕ 你好")
		assert.Equal(t, "café ☕ 你好", props["val"])
	})

	t.Run("list_string", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "List<String>")
		props := fidelityRoundTrip(t, s, v, []any{"a", "b", "c"})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any")
		assert.Equal(t, []any{"a", "b", "c"}, list)
	})

	t.Run("list_empty", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "List<String>")
		props := fidelityRoundTrip(t, s, v, []any{})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any, not nil")
		assert.Empty(t, list)
	})

	t.Run("vector", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Vector[3]")
		props := fidelityRoundTrip(t, s, v, []any{0.1, 0.2, 0.3})
		list, ok := props["val"].([]any)
		require.True(t, ok, "val should be []any")
		assert.Len(t, list, 3)
	})

	t.Run("enum", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, `Enum["active", "inactive"]`)
		props := fidelityRoundTrip(t, s, v, "active")
		assert.Equal(t, "active", props["val"])
	})

	t.Run("date", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Date")
		props := fidelityRoundTrip(t, s, v, "2026-03-15")
		assert.Equal(t, "2026-03-15", props["val"])
	})

	t.Run("timestamp", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, "Timestamp")
		props := fidelityRoundTrip(t, s, v, "2026-01-01T00:00:00Z")
		assert.Equal(t, "2026-01-01T00:00:00Z", props["val"])
	})

	t.Run("pattern", func(t *testing.T) {
		t.Parallel()
		s, v := fidelitySchema(t, `Pattern["^[^@]+@[^@]+$"]`)
		props := fidelityRoundTrip(t, s, v, "test@example.com")
		assert.Equal(t, "test@example.com", props["val"])
	})
}

package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
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

// mustTypeID resolves a schema type's ID, failing the test if absent.
func mustTypeID(t *testing.T, s *schema.Schema, typeName string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("type %q not found", typeName)
	}
	return typ.ID()
}

func mustValidInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
	)
}

func mustValidInstanceWithEdge(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any, relName string, targetKeys [][]any) *instance.ValidInstance { //nolint:unparam // test helper kept general
	t.Helper()
	targets := make([]instance.ValidEdgeTarget, len(targetKeys))
	for i, tk := range targetKeys {
		targets[i] = instance.NewValidEdgeTarget(immutable.WrapKey(tk), immutable.WrapProperties(nil))
	}
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
		instancetest.Edges(map[string]*instance.ValidEdgeData{relName: instance.NewValidEdgeData(targets)}),
	)
}

func mustValidPartInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
	)
}

// mustValidInstanceWithEdgeProps builds a ValidInstance with a single edge
// that carries schema-declared edge properties.
func mustValidInstanceWithEdgeProps(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any, relName string, targetKey []any, edgeProps map[string]any) *instance.ValidInstance { //nolint:unparam // test helper kept general
	t.Helper()
	targets := []instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(immutable.WrapKey(targetKey), immutable.WrapProperties(edgeProps)),
	}
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
		instancetest.Edges(map[string]*instance.ValidEdgeData{relName: instance.NewValidEdgeData(targets)}),
	)
}

func buildSnapshot(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	t.Helper()
	return snapshottest.BuildSnapshot(t, s, instances...)
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
	snapshottest.AssertDeterministic(t, snap)
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

	if info.Version != 2 {
		t.Errorf("Version: got %d, want 2", info.Version)
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

// sharedFields projects two structs' exported fields onto name-keyed maps
// restricted to the field names they share, so cross-type parity tests
// compare every common field without a hand-maintained list.
func sharedFields(t *testing.T, a, b any) (av, bv map[string]any) {
	t.Helper()
	ra, rb := reflect.ValueOf(a), reflect.ValueOf(b)
	names := make(map[string]bool)
	for i := range ra.NumField() {
		names[ra.Type().Field(i).Name] = true
	}
	av, bv = make(map[string]any), make(map[string]any)
	for i := range rb.NumField() {
		name := rb.Type().Field(i).Name
		if names[name] {
			av[name] = ra.FieldByName(name).Interface()
			bv[name] = rb.Field(i).Interface()
		}
	}
	require.NotEmpty(t, av, "structs share no fields — parity test is vacuous")
	return av, bv
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

	assert.Equal(t, 2, header.Version, "Version")
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
		"pipeline":            "catalog.book_ingest",
		"extraction_complete": "true",
	}))

	header, result := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, result.Err())
	require.NotNil(t, header.Metadata, "Metadata should be present")
	assert.Equal(t, "catalog.book_ingest", header.Metadata["pipeline"])
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

	// Every field common to both APIs must match byte-for-byte. The
	// reflected projection keys on shared field names, so fields added to
	// both structs later are parity-checked automatically.
	infoFields, headerFields := sharedFields(t, *info, *header)
	assert.Equal(t, infoFields, headerFields)
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

// HeaderOnlyRead streams header metadata from an io.Reader without
// materializing the full document. The tests pin each Godoc-advertised
// contract: reader-vs-bytes parity, the MaxHeaderSize cap and its
// distinguished error message, slow-reader tolerance via io.Pipe,
// cancellation at entry, reader-error uniform surfacing, and the
// baseline malformed-input paths.

func TestHeaderOnlyRead_HappyPath(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	header, result := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	require.NoError(t, result.Err(), "HeaderOnlyRead should succeed on well-formed input")
	require.NotNil(t, header, "HeaderOnlyRead should return a non-nil HeaderInfo")

	assert.Equal(t, 2, header.Version)
	assert.Equal(t, "test", header.SchemaName)
	assert.NotEmpty(t, header.SchemaHash)
	assert.NotEmpty(t, header.IntegrityHash, "IntegrityHash is the stored value, not a verification result")
	assert.ElementsMatch(t, []string{"Company", "Person"}, header.Types)
	assert.Equal(t, int64(0), header.FileSize, "FileSize is not populated in the reader variant")
}

func TestHeaderOnlyRead_ParityWithHeaderOnly(t *testing.T) {
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	snap := buildSnapshot(t, s, company, person)

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{"key": "val"}))

	fromBytes, bytesRes := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, bytesRes.Err())
	fromReader, readerRes := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	require.NoError(t, readerRes.Err())

	// Every HeaderInfo field equal EXCEPT FileSize — the single
	// documented structural delta between the two entry points. Pin the
	// delta explicitly, then zero it and compare whole structs so fields
	// added to HeaderInfo later are parity-checked automatically.
	assert.Equal(t, int64(len(data)), fromBytes.FileSize)
	assert.Equal(t, int64(0), fromReader.FileSize)

	fb, fr := *fromBytes, *fromReader
	fb.FileSize, fr.FileSize = 0, 0
	assert.Equal(t, fb, fr)
}

func TestHeaderOnlyRead_ExceedsMaxHeaderSize(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	// Metadata value sized to overflow MaxHeaderSize with slack so the
	// test tracks the constant as it evolves.
	huge := strings.Repeat("x", snapshot.MaxHeaderSize+10*1024)
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{"huge": huge}))

	_, result := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	require.True(t, result.HasErrors(), "header exceeding MaxHeaderSize should error")

	var found bool
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), "exceeded MaxHeaderSize") {
			found = true
		}
	}
	assert.True(t, found, "over-limit header should surface E_SNAPSHOT_MALFORMED with an 'exceeded MaxHeaderSize' message")
}

func TestHeaderOnlyRead_MaxHeaderSizeBoundaryMidToken(t *testing.T) {
	// Pins the distinguished error-message path when MaxHeaderSize
	// falls inside an unclosed string literal — the failure mode
	// operators most need help disambiguating from a generic
	// json-decoder parse error. Metadata value is sized past
	// MaxHeaderSize so the opening quote lies < 1 KiB into the header
	// and the closing quote lies past the cap, making the limit
	// unambiguously mid-token.
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	midTok := strings.Repeat("y", snapshot.MaxHeaderSize+10*1024)
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{"mid": midTok}))

	_, result := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	require.True(t, result.HasErrors(), "mid-token over-limit header should error")

	// The assertion targets the distinguished "exceeded MaxHeaderSize"
	// message, not the underlying json.Decoder error text, which is
	// version-sensitive.
	var found bool
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), "exceeded MaxHeaderSize") {
			found = true
			break
		}
	}
	assert.True(t, found, "mid-token cap should produce the 'exceeded MaxHeaderSize' message, not a generic JSON error")
}

// assertLoadTruncationClean builds a snapshot whose marshaled bytes strictly
// exceed offset, truncates to offset, calls snapshot.Load, and asserts that
// Load returns a nil snapshot plus a diag.Result carrying
// diag.E_SNAPSHOT_MALFORMED — no panic, no partial struct, no silent success.
func assertLoadTruncationClean(t *testing.T, offset int) {
	t.Helper()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	// Size metadata so the marshaled output strictly exceeds offset. 10 KiB
	// of slack absorbs the surrounding header / schema-hash / type-list
	// bytes without tracking their exact size.
	huge := strings.Repeat("x", offset+10*1024)
	data, _ := snapshot.Marshal(ctx, snap, snapshot.WithMetadata(map[string]string{"huge": huge}))
	require.Greater(t, len(data), offset, "fixture must exceed truncation offset")

	truncated := data[:offset]
	loaded, result := snapshot.Load(ctx, truncated, s)

	assert.Nil(t, loaded, "truncated Load must not return a partial struct")
	require.True(t, result.HasErrors(), "truncated Load must surface errors")

	var found bool
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
			found = true
			break
		}
	}
	assert.True(t, found, "truncated Load must surface E_SNAPSHOT_MALFORMED")
}

func TestLoad_TruncatedHeader_32KiB(t *testing.T) {
	// 32 KiB falls below the pre-bump 64 KiB MaxHeaderSize cap. A header
	// truncated here must surface E_SNAPSHOT_MALFORMED cleanly rather
	// than tripping the cap's own error path, which is a distinct branch.
	assertLoadTruncationClean(t, 32*1024)
}

func TestLoad_TruncatedHeader_1MiB(t *testing.T) {
	// Mid-range of the 16 MiB cap. Exercises the truncation path on a
	// realistic state-batched header size.
	assertLoadTruncationClean(t, 1*1024*1024)
}

func TestLoad_TruncatedHeader_15MiB(t *testing.T) {
	// Near-ceiling of the 16 MiB cap. Confirms the expanded header
	// budget still produces a structured error on truncation rather
	// than a silent partial decode.
	if testing.Short() {
		t.Skip("skipping 15 MiB fixture in -short mode")
	}
	assertLoadTruncationClean(t, 15*1024*1024)
}

func TestHeaderOnlyRead_SlowReader(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))

	ctx := context.Background()
	data, _ := snapshot.Marshal(ctx, snap)

	// Feed the bytes through io.Pipe in 64-byte chunks with a small
	// delay between writes. Exercises json.Decoder's partial-read
	// handling and the limitReader's byte accounting under timing
	// variance.
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		const chunkSize = 64
		for off := 0; off < len(data); off += chunkSize {
			end := min(off+chunkSize, len(data))
			if _, err := pw.Write(data[off:end]); err != nil {
				return
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	header, result := snapshot.HeaderOnlyRead(ctx, pr)
	require.NoError(t, result.Err(), "slow reader should not surface an error on well-formed input")
	require.NotNil(t, header)
	assert.Equal(t, "test", header.SchemaName)
	assert.ElementsMatch(t, []string{"Company"}, header.Types)
}

func TestHeaderOnlyRead_ContextCancellation(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))
	data, _ := snapshot.Marshal(context.Background(), snap)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, result := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	require.True(t, result.HasErrors(), "cancelled context should produce a diagnostic")

	var found bool
	for issue := range result.BySeverity(diag.Fatal) {
		if issue.Code() == diag.E_CONTEXT_CANCELLED {
			found = true
		}
	}
	assert.True(t, found, "cancelled context should produce Fatal E_CONTEXT_CANCELLED")
}

// errReader always returns the configured error on Read; used to exercise
// HeaderOnlyRead's reader-error handling paths.
type errReader struct{ err error }

func (er *errReader) Read(_ []byte) (int, error) { return 0, er.err }

func TestHeaderOnlyRead_ReaderErrors(t *testing.T) {
	// Every reader-error class should surface as E_SNAPSHOT_MALFORMED
	// on the returned diag.Result, not as a bare error return. The
	// library's uniform diagnostic surface is load-bearing for
	// dispatch callers that want one error shape across on-disk and
	// in-transit truncation.
	cases := []struct {
		name string
		err  error
	}{
		{"io.EOF", io.EOF},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF},
		{"arbitrary I/O error", errors.New("synthetic disk failure")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, result := snapshot.HeaderOnlyRead(context.Background(), &errReader{err: tc.err})
			require.True(t, result.HasErrors(), "reader error should error")
			var found bool
			for issue := range result.Errors() {
				if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
					found = true
				}
			}
			assert.True(t, found, "%s should surface as E_SNAPSHOT_MALFORMED", tc.name)
		})
	}
}

func TestHeaderOnlyRead_MalformedJSON(t *testing.T) {
	_, result := snapshot.HeaderOnlyRead(context.Background(), strings.NewReader("not a json snapshot"))
	assert.True(t, result.HasErrors(), "HeaderOnlyRead should error on malformed JSON")
}

func TestHeaderOnlyRead_MissingYammmSnapshotKey(t *testing.T) {
	_, result := snapshot.HeaderOnlyRead(context.Background(), strings.NewReader(`{"types":[]}`))
	require.True(t, result.HasErrors(), "valid JSON with wrong first key should error")

	var found bool
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
			found = true
		}
	}
	assert.True(t, found, "missing yammm_snapshot first key should produce E_SNAPSHOT_MALFORMED")
}

func TestHeaderInfo_SchemaHashMatches(t *testing.T) {
	s := testSchema(t)
	realHash := schema.StructuralHash(s)
	require.NotEmpty(t, realHash, "precondition: schema.StructuralHash should return non-empty")

	// A second schema with a different type set hashes differently.
	s2, res := schema.NewBuilder().
		WithName("other").
		WithSourceID(location.MustNewSourceID("test://other.yammm")).
		AddType("OnlyType").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	require.NoError(t, res.Err())
	otherHash := schema.StructuralHash(s2)
	require.NotEqual(t, realHash, otherHash, "precondition: different schemas should hash differently")

	t.Run("matching_hash_returns_true", func(t *testing.T) {
		h := &snapshot.HeaderInfo{
			SchemaHash:          realHash,
			SchemaHashAlgorithm: schema.StructuralHashVersion,
		}
		assert.True(t, h.SchemaHashMatches(s))
	})
	t.Run("mismatched_hash_returns_false", func(t *testing.T) {
		h := &snapshot.HeaderInfo{
			SchemaHash:          otherHash,
			SchemaHashAlgorithm: schema.StructuralHashVersion,
		}
		assert.False(t, h.SchemaHashMatches(s))
	})
	t.Run("nil_receiver_returns_false_without_panic", func(t *testing.T) {
		var h *snapshot.HeaderInfo
		assert.False(t, h.SchemaHashMatches(s))
	})
	t.Run("nil_schema_returns_false_without_panic", func(t *testing.T) {
		h := &snapshot.HeaderInfo{SchemaHash: realHash, SchemaHashAlgorithm: schema.StructuralHashVersion}
		assert.False(t, h.SchemaHashMatches(nil))
	})
	t.Run("empty_schema_hash_returns_false", func(t *testing.T) {
		h := &snapshot.HeaderInfo{SchemaHash: "", SchemaHashAlgorithm: schema.StructuralHashVersion}
		assert.False(t, h.SchemaHashMatches(s))
	})
	t.Run("algorithm_version_mismatch_returns_false", func(t *testing.T) {
		// Even if the hash string happens to match, a different
		// algorithm version means the values are not comparable.
		h := &snapshot.HeaderInfo{
			SchemaHash:          realHash,
			SchemaHashAlgorithm: 99, // fictional future algorithm version
		}
		assert.False(t, h.SchemaHashMatches(s))
	})
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

	snapshottest.DiffSnapshots(t, constructed, loaded)
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

// TestMarshalLoad_UnresolvedEdgePropertiesRoundTrip pins unresolved-edge
// property fidelity: edge properties declared on a forward reference to an
// absent target must survive Marshal → Load intact. A prior implementation
// silently dropped these both in the graph's unresolved-edge bookkeeping and
// at the unresolvedWire layer; this test regresses on both failure modes.
func TestMarshalLoad_UnresolvedEdgePropertiesRoundTrip(t *testing.T) {
	s := testSchema(t)
	// Person with EMPLOYER edge to non-existent Company c99, carrying
	// property values. Although the DSL builder used by testSchema does
	// not declare edge-property types on EMPLOYER, the in-memory
	// ValidEdgeTarget construction accepts arbitrary property maps —
	// mirroring graph/testhelpers_test.go's existing pattern at
	// TestGraph_Edge_Properties. The round-trip path is what's under
	// test, not schema-level type validation.
	person := mustValidInstanceWithEdgeProps(t, s, "Person",
		[]any{"p1"}, map[string]any{"id": "p1", "name": "Alice"},
		"EMPLOYER", []any{"c99"},
		map[string]any{"role": "Engineer", "since": int64(2020)})

	snap := buildSnapshot(t, s, person)
	if len(snap.Unresolved()) != 1 {
		t.Fatalf("expected 1 unresolved edge, got %d", len(snap.Unresolved()))
	}

	ctx := context.Background()
	data, result := snapshot.Marshal(ctx, snap)
	require.NoError(t, result.Err(), "Marshal")
	loaded, result := snapshot.Load(ctx, data, s)
	require.NoError(t, result.Err(), "Load")

	require.Len(t, loaded.Unresolved(), 1)
	u := loaded.Unresolved()[0]
	require.Equal(t, "target_missing", u.Reason)

	role, ok := u.Property("role")
	require.True(t, ok, "Property(role) should round-trip")
	roleStr, _ := role.String()
	require.Equal(t, "Engineer", roleStr)

	since, ok := u.Property("since")
	require.True(t, ok, "Property(since) should round-trip")
	sinceInt, _ := since.Int()
	require.Equal(t, int64(2020), sinceInt)

	// snapshottest.CompareSnapshots asserts the full per-field diff,
	// including the Properties field PR-6 added. Independent gate.
	snapshottest.AssertRoundTrip(t, snap, s)
}

// TestMarshalLoad_MixedResolvedAndUnresolvedEdgeProperties pins wire-format
// parity: an edge with identical property shape survives round-trip
// regardless of whether it resolves (target present) or stays unresolved
// (target absent). This is the high-level invariant that
// [TestMarshalLoad_UnresolvedEdgePropertiesGoldenBytes] pins at the
// serialization-bytes layer.
func TestMarshalLoad_MixedResolvedAndUnresolvedEdgeProperties(t *testing.T) {
	s := testSchema(t)

	resolvedCompany := mustValidInstance(t, s, "Company",
		[]any{"c1"}, map[string]any{"id": "c1", "title": "Acme"})

	edgeProps := map[string]any{"role": "Engineer", "since": int64(2020)}

	// personA's EMPLOYER target exists (resolves); personB's does not.
	personA := mustValidInstanceWithEdgeProps(t, s, "Person",
		[]any{"pA"}, map[string]any{"id": "pA", "name": "Alice"},
		"EMPLOYER", []any{"c1"}, edgeProps)
	personB := mustValidInstanceWithEdgeProps(t, s, "Person",
		[]any{"pB"}, map[string]any{"id": "pB", "name": "Bob"},
		"EMPLOYER", []any{"missing"}, edgeProps)

	snap := buildSnapshot(t, s, resolvedCompany, personA, personB)
	require.Len(t, snap.Edges(), 1, "expected one resolved edge")
	require.Len(t, snap.Unresolved(), 1, "expected one unresolved edge")

	ctx := context.Background()
	data, mres := snapshot.Marshal(ctx, snap)
	require.NoError(t, mres.Err())
	loaded, lres := snapshot.Load(ctx, data, s)
	require.NoError(t, lres.Err())

	// Both the resolved and unresolved entries survive with identical
	// property content (per-key comparison using the .Clone() map form).
	resolvedProps := loaded.Edges()[0].Properties().Clone()
	unresolvedProps := loaded.Unresolved()[0].Properties().Clone()
	require.Equal(t, edgeProps, resolvedProps,
		"resolved edge properties must survive round-trip")
	require.Equal(t, edgeProps, unresolvedProps,
		"unresolved edge properties must survive round-trip and match the resolved shape")
}

// TestLoad_V1Compatibility pins the "v2 reader accepts v1 files losslessly"
// half of the asymmetric-bump contract. A hand-crafted v1 document (no
// "properties" field on unresolvedWire entries) loads cleanly through the
// v0.3.0 decoder; the resulting UnresolvedEdge has empty Properties —
// equivalent to the v1 in-memory semantics.
//
// Integrity is not the point of this test (we are constructing a v1 doc by
// hand rather than recomputing its SHA-256), so WithSkipIntegrityCheck
// keeps the test focused on version-acceptance semantics.
func TestLoad_V1Compatibility(t *testing.T) {
	s := testSchema(t)
	// Minimal v1-style document. The unresolved-edge entry has NO
	// "properties" field (the v1 shape). The schema_hash matches
	// testSchema's StructuralHash so schema-compatibility passes; the
	// integrity_hash is a placeholder under WithSkipIntegrityCheck.
	// types[] lists only Person because no Company instances are present
	// (matches Marshal's type-emission behavior).
	schemaHash := schema.StructuralHash(s)
	doc := fmt.Sprintf(`{"yammm_snapshot":{"version":1,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":%q,"schema_hash_algorithm":1,"integrity_hash":"","features":[]},"types":["Person"],"instances":{"Person":[{"key":["p1"],"properties":{"id":"p1","name":"Alice"},"provenance":null}]},"diagnostics":{"duplicates":[],"unresolved":[{"source_type":"Person","source_key":["p1"],"relation":"EMPLOYER","target_type":"Company","target_key":["c99"],"required":true,"reason":"target_missing"}]}}`, schemaHash)

	ctx := context.Background()
	loaded, result := snapshot.Load(ctx, []byte(doc), s, snapshot.WithSkipIntegrityCheck())
	require.NoError(t, result.Err(), "v2 reader must accept v1 documents cleanly: %v", result)

	require.Len(t, loaded.Unresolved(), 1)
	u := loaded.Unresolved()[0]
	require.Equal(t, 0, u.Properties().Len(),
		"v1 document has no properties field on unresolved wire; v2 reader loads as empty Properties")
}

// TestLoad_UnsupportedVersion pins the rejection path for versions outside
// the accept range on either side (v0, v3, v99). Each surfaces the
// E_SNAPSHOT_UNSUPPORTED_VERSION code with the observed version and the
// supported range named in the message.
func TestLoad_UnsupportedVersion(t *testing.T) {
	s := testSchema(t)
	schemaHash := schema.StructuralHash(s)
	for _, v := range []int{0, 3, 99} {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			doc := fmt.Sprintf(`{"yammm_snapshot":{"version":%d,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":%q,"schema_hash_algorithm":1,"integrity_hash":"","features":[]},"types":[],"instances":{},"diagnostics":{"duplicates":[],"unresolved":[]}}`, v, schemaHash)

			_, result := snapshot.Load(context.Background(), []byte(doc), s,
				snapshot.WithSkipIntegrityCheck())
			require.True(t, result.HasErrors(), "version %d must be rejected", v)

			found := false
			for issue := range result.Errors() {
				if issue.Code() == diag.E_SNAPSHOT_UNSUPPORTED_VERSION {
					found = true
					require.Contains(t, issue.Message(), "[1, 2]",
						"reject message names the accept range")
				}
			}
			require.True(t, found, "expected E_SNAPSHOT_UNSUPPORTED_VERSION")
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

// TestWriteFile_TmpSuffixConstant pins the convention other snapshot
// primitives (notably ScanDir in PR-2) key off. Any accidental rename of
// the constant's value surfaces here before downstream consumers import
// the change.
func TestWriteFile_TmpSuffixConstant(t *testing.T) {
	assert.Equal(t, ".tmp", snapshot.TmpSuffix)
}

func TestWriteFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.ys")
	data := []byte("hello-world-payload")

	require.NoError(t, snapshot.WriteFile(path, data))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, got)

	// The staging file must be renamed away — no tmp left on success.
	_, statErr := os.Stat(path + snapshot.TmpSuffix)
	assert.True(t, os.IsNotExist(statErr), "successful WriteFile should leave no tmp file behind")
}

func TestWriteFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.ys")

	first := []byte("first-content")
	require.NoError(t, snapshot.WriteFile(path, first))

	second := []byte("second-content-different-length")
	require.NoError(t, snapshot.WriteFile(path, second))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, second, got, "second WriteFile should fully overwrite the first content via rename semantics")

	_, statErr := os.Stat(path + snapshot.TmpSuffix)
	assert.True(t, os.IsNotExist(statErr))
}

func TestWriteFile_CreateFailsOnNonexistentDirectory(t *testing.T) {
	dir := t.TempDir()
	// Parent directory does not exist — os.Create on the tmp sibling fails.
	path := filepath.Join(dir, "no-such-dir", "snap.ys")

	err := snapshot.WriteFile(path, []byte("payload"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temp", "error should be tagged with the failing step")
	require.ErrorIs(t, err, fs.ErrNotExist, "wrapped cause should be fs.ErrNotExist")

	// No partial tmp should exist anywhere reachable under dir.
	_, statErr := os.Stat(path + snapshot.TmpSuffix)
	assert.True(t, os.IsNotExist(statErr))
}

func TestWriteFile_RenameFailsCleansUpTmp(t *testing.T) {
	dir := t.TempDir()
	// Pre-create path as a directory so os.Rename(tmp, path) fails:
	// the tmp is a regular file, and renaming a file over a non-empty
	// directory (or a directory at all, on some platforms) surfaces
	// as an error.
	path := filepath.Join(dir, "occupied")
	require.NoError(t, os.Mkdir(path, 0o750))
	// Place a file inside so the directory is non-empty; rename over a
	// non-empty directory is universally an error across darwin/linux.
	require.NoError(t, os.WriteFile(filepath.Join(path, "sentinel"), []byte("x"), 0o600))

	err := snapshot.WriteFile(path, []byte("payload"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename temp to final", "error should surface the rename step")

	// The rename-failure branch must have removed the staging file —
	// this is the cleanup path consumers rely on when a destination
	// becomes unwritable between runs.
	_, statErr := os.Stat(path + snapshot.TmpSuffix)
	assert.True(t, os.IsNotExist(statErr), "rename-failure branch should have removed the staging file")
}

func TestWriteFile_EmptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.ys")

	require.NoError(t, snapshot.WriteFile(path, []byte{}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size(), "zero-length data should produce a zero-byte file, not a special case")

	_, statErr := os.Stat(path + snapshot.TmpSuffix)
	assert.True(t, os.IsNotExist(statErr))
}

func TestWriteFile_ConcurrentDistinctPaths(t *testing.T) {
	// The realistic consumer shape: N goroutines write to N distinct
	// paths concurrently. This is the common consumer case — one .ys
	// per batch, one goroutine per batch, no shared path. Pins that
	// the primitive is safe for concurrent use when callers coordinate
	// destinations (via RunID-suffixed paths, per-batch keys, etc.)
	// and catches any accidental shared state introduced by a future
	// refactor.
	dir := t.TempDir()

	letters := []byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H'}
	const n = 8
	var wg sync.WaitGroup
	writeErrs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			path := filepath.Join(dir, fmt.Sprintf("snap-%d.ys", i))
			payload := bytes.Repeat([]byte{letters[i]}, 32)
			writeErrs[i] = snapshot.WriteFile(path, payload)
		})
	}
	wg.Wait()
	for i, err := range writeErrs {
		require.NoError(t, err, "goroutine %d write", i)
	}

	for i := range n {
		path := filepath.Join(dir, fmt.Sprintf("snap-%d.ys", i))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		expected := bytes.Repeat([]byte{letters[i]}, 32)
		assert.Equal(t, expected, got, "goroutine %d's write should not be corrupted by a sibling", i)

		_, statErr := os.Stat(path + snapshot.TmpSuffix)
		assert.True(t, os.IsNotExist(statErr), "goroutine %d should have renamed its staging file away", i)
	}
}

func TestWriteFile_ConcurrentSamePath(t *testing.T) {
	// N goroutines race to WriteFile to the SAME path. This primitive
	// is not designed for multi-writer coordination — a consumer's state
	// machine ensures one writer per path via its lockfile protocol —
	// but the filesystem-level race is well-defined: exactly one
	// rename wins, the rest see ENOENT because the tmp was renamed
	// away first.
	//
	// Invariants pinned:
	//   1. At least one call returned nil (otherwise no file would
	//      exist at path).
	//   2. The final file contents match EXACTLY one contender's
	//      bytes. Contenders are same-length so a truncate-then-write
	//      interleave cannot produce a mix (the second write
	//      overwrites the first in full).
	//   3. Any call that returned an error returned a rename-step
	//      error — no corruption-at-create, no partial-write surface.
	//   4. No staging file survives once all goroutines finish: the
	//      winning rename removes the tmp; losing renames' cleanup
	//      os.Remove is a no-op but leaves nothing behind.
	//
	// Runs under -race via the default matrix to catch any accidental
	// in-process shared state.
	dir := t.TempDir()
	path := filepath.Join(dir, "racy.ys")

	letters := []byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H'}
	const n = 8
	contenders := make([][]byte, n)
	for i := range contenders {
		contenders[i] = bytes.Repeat([]byte{letters[i]}, 32)
	}

	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = snapshot.WriteFile(path, contenders[idx])
		}(i)
	}
	wg.Wait()

	var successes int
	for i, err := range errs {
		if err == nil {
			successes++
			continue
		}
		assert.Contains(t, err.Error(), "rename temp to final", "goroutine %d's error should be at the rename step (the expected race outcome)", i)
	}
	assert.GreaterOrEqual(t, successes, 1, "at least one concurrent writer must succeed")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	var matched bool
	for _, c := range contenders {
		if bytes.Equal(got, c) {
			matched = true
			break
		}
	}
	assert.True(t, matched, "final file contents must match exactly one contender's bytes (no interleave)")

	_, statErr := os.Stat(path + snapshot.TmpSuffix)
	assert.True(t, os.IsNotExist(statErr), "no staging file should remain after all goroutines complete")
}

func TestWriteFile_LeavesNoTmpOnSuccess(t *testing.T) {
	// Pre-seed a stale tmp file at the staging path (simulating a prior
	// crashed write that didn't get to its own cleanup). A successful
	// WriteFile must overwrite it via os.Create and ultimately rename
	// it away, leaving no tmp behind.
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.ys")
	stale := path + snapshot.TmpSuffix
	require.NoError(t, os.WriteFile(stale, []byte("stale-garbage-from-prior-crash"), 0o600))

	fresh := []byte("fresh-content")
	require.NoError(t, snapshot.WriteFile(path, fresh))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, fresh, got)

	_, statErr := os.Stat(stale)
	assert.True(t, os.IsNotExist(statErr), "successful WriteFile must rename the tmp away, even when a stale tmp pre-existed")
}

// TestMarshal_GoldenBytes pins the exact serialized .ys wire bytes for a
// representative snapshot — header shape, key order, instance encoding,
// resolved edge, and unresolved-edge entry — so any wire-format change is a
// reviewed golden diff, not an incidental one.
func TestMarshal_GoldenBytes(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(
		t, s,
		mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}),
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"title": "Acme"}),
		mustValidInstanceWithEdge(t, s, "Person", []any{"p2"}, map[string]any{"name": "Bob"},
			"EMPLOYER", [][]any{{"c1"}}),
		mustValidInstanceWithEdge(t, s, "Person", []any{"p3"}, map[string]any{"name": "Cara"},
			"EMPLOYER", [][]any{{"c-missing"}}),
	)

	data, res := snapshot.Marshal(t.Context(), snap, snapshot.WithIndent("\t"))
	require.NoError(t, res.Err())
	yammmtest.Golden(t, "marshal_representative.ys", data)
}

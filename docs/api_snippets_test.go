package docs_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	csvadapter "github.com/simon-lentz/yammm/adapter/csv"
	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The Go snippets in API.md are prose: nothing compiles them, so a signature
// change leaves them stating a call that no longer exists, and the next reader
// copies it. TestSpecExamples does not cover them — it reads SPEC.md only, it
// extracts ```yammm fences only, and it checks with the DSL parser, which has
// nothing to say about Go.
//
// These tests close that gap for the neo4j diff and introspection surface,
// whose call shapes appear in three places that must agree: docs/API.md
// ("Constraint Diffing", "Index Diffing", "Introspection Queries"), the package
// documentation in adapter/neo4j/doc.go ("Diff Scope and Name Blocking"), and
// claude-plugin/skills/yammm/references/adapters.md ("Diffing Against a Live
// Database"). A change here that breaks compilation is the signal to update all
// three.

// probeSchema is the smallest schema exercising both diff halves: a primary key
// (which emits constraints) and an annotated property (which emits an index).
const probeSchema = `schema "Probe"

type Document {
  content_hash String primary
  state        String @index
}
`

func loadProbeSchema(t *testing.T, ctx context.Context) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(ctx, probeSchema, "probe.yammm")
	if err := result.Err(); err != nil {
		t.Fatalf("loading probe schema: %v", err)
	}
	return s
}

// TestDocumentedDiffCallShapes pins the diff call shapes the documentation
// shows: ownership computed once via OwnedLabels and passed to both halves,
// each half taking the other's remote objects as the variadic blocker list, and
// six result buckets on each. It is a compile-time assertion first — the
// runtime assertions only guard against the entry points silently returning
// nothing to classify.
func TestDocumentedDiffCallShapes(t *testing.T) {
	ctx := context.Background()
	s := loadProbeSchema(t, ctx)
	adapter := neo4j.New()

	desiredConstraints, cResult := adapter.ConstraintsStructured(ctx, s)
	if err := cResult.Err(); err != nil {
		t.Fatalf("generating constraints: %v", err)
	}
	desiredIndexes, iResult := adapter.IndexesStructured(ctx, s)
	if err := iResult.Err(); err != nil {
		t.Fatalf("generating indexes: %v", err)
	}

	// An empty database: every declaration is a create, which is enough to
	// exercise the signatures and the bucket names.
	var actualConstraints []neo4j.RemoteConstraint
	var actualIndexes []neo4j.RemoteIndex

	owned := adapter.OwnedLabels(ctx, s)

	cDiff := adapter.DiffConstraints(desiredConstraints, actualConstraints, owned, actualIndexes...)
	iDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, owned, actualConstraints...)

	// Every bucket the documentation names, on both results.
	cBuckets := len(cDiff.Match) + len(cDiff.Drift) + len(cDiff.Create) + len(cDiff.Drop) +
		len(cDiff.Unverified) + cDiff.Excluded
	iBuckets := len(iDiff.Match) + len(iDiff.Drift) + len(iDiff.Create) + len(iDiff.Drop) +
		len(iDiff.Unverified) + iDiff.Excluded

	if len(desiredConstraints) == 0 || cBuckets == 0 {
		t.Errorf("probe schema classified no constraints: %d desired, %d bucketed",
			len(desiredConstraints), cBuckets)
	}
	if len(desiredIndexes) == 0 || iBuckets == 0 {
		t.Errorf("probe schema classified no indexes: %d desired, %d bucketed",
			len(desiredIndexes), iBuckets)
	}

	// The in-sync predicate API.md and adapters.md both show. Against an empty
	// database it must be false — a gate that reads "in sync" here would report
	// an unprovisioned database as provisioned.
	inSync := len(iDiff.Drift) == 0 && len(iDiff.Create) == 0 && len(iDiff.Drop) == 0 &&
		len(iDiff.Unverified) == 0
	if inSync {
		t.Error("in-sync predicate reported an empty database as in sync")
	}

	// OwnedLabels accessors, which a caller reporting on a drop or drift needs
	// to resolve a remote object's labels the way the diff did.
	if _, ok := owned.LabelOf([]string{"Probe__Document"}); !ok {
		t.Error("OwnedLabels does not own the label the adapter emits for Document")
	}
	if !owned.Contains("Probe__Document") {
		t.Error("OwnedLabels.Contains disagrees with LabelOf")
	}
}

// TestDocumentedRemoteIndexSurface pins the RemoteIndex fields and accessors
// API.md's "Introspection Queries" section lists.
func TestDocumentedRemoteIndexSurface(t *testing.T) {
	ri := neo4j.RemoteIndex{
		Name:             "Probe__Document_state_idx",
		Type:             "RANGE",
		EntityType:       "NODE",
		LabelsOrTypes:    []string{"Probe__Document"},
		Properties:       []string{"state"},
		Options:          nil,
		State:            "",
		OwningConstraint: "",
	}

	// An unreported state counts as online, so older servers and hand-built
	// records do not read as drift.
	if !ri.IsOnline() {
		t.Error("IsOnline is false for an unreported state")
	}
	if _, ok := ri.VectorDimensions(); ok {
		t.Error("VectorDimensions reported a dimension for a range index")
	}
	if _, ok := ri.VectorSimilarity(); ok {
		t.Error("VectorSimilarity reported a function for a range index")
	}
}

// TestDocumentedIntrospectionQueries pins the introspection queries against the
// descriptions in API.md's "Introspection Queries" table. The index query's
// exact projection is documented because a caller running it against a server
// that does not support a column fails, so the column list is a contract.
func TestDocumentedIntrospectionQueries(t *testing.T) {
	if got, want := neo4j.IntrospectConstraintsQuery(), "SHOW CONSTRAINTS YIELD *"; got != want {
		t.Errorf("IntrospectConstraintsQuery() = %q, documented as %q", got, want)
	}

	indexQuery := neo4j.IntrospectIndexesQuery()
	for _, column := range []string{
		"name", "type", "entityType", "labelsOrTypes",
		"properties", "options", "state", "owningConstraint",
	} {
		if !strings.Contains(indexQuery, column) {
			t.Errorf("IntrospectIndexesQuery() omits the documented column %q: %s", column, indexQuery)
		}
	}
	// Constraint-backing indexes are deliberately returned; only LOOKUP indexes
	// are filtered out. A reinstated owningConstraint filter would silently
	// remove the rows the diff needs to detect a blocked CREATE INDEX.
	if !strings.Contains(indexQuery, "type <> 'LOOKUP'") {
		t.Errorf("IntrospectIndexesQuery() does not filter LOOKUP indexes: %s", indexQuery)
	}
	if strings.Contains(indexQuery, "owningConstraint IS NULL") {
		t.Errorf("IntrospectIndexesQuery() filters out constraint-backing indexes, "+
			"which the documented diff behavior depends on receiving: %s", indexQuery)
	}
}

// TestDocumentedScanFilterShape compiles API.md's "Pre-open filtering" example.
// The snippet is prose that no other gate reads, so a signature change to
// WithScanFilter, ScanCandidate or ScanDirWith would leave it stating a call
// that no longer exists.
func TestDocumentedScanFilterShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.ys"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	recent := snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
		info, err := c.Info()
		return err == nil && !info.ModTime().Before(cutoff)
	})

	var seen int
	for entry, err := range snapshot.ScanDirWith(t.Context(), dir, recent) {
		if err != nil {
			t.Fatalf("ScanDirWith: %v", err)
		}
		seen++
		if entry.ModTime.IsZero() {
			t.Error("the documented entry carries no ModTime")
		}
	}
	if seen != 1 {
		t.Errorf("the documented filter admitted %d entries, want 1", seen)
	}

	entries, result := snapshot.ScanDirSliceWith(t.Context(), dir, recent)
	if result.HasErrors() || len(entries) != 1 {
		t.Errorf("ScanDirSliceWith: %d entries, result %v", len(entries), result.Err())
	}
}

// The pins below were chosen by the 2026-08-25 documentation audit: each one
// compiles a call shape API.md or SPEC.md states, so a signature change that
// would leave the prose silently wrong breaks the build instead.

// TestDocumentedSchemaIdentity pins the two Schema Identity facts a reader acts
// on: the algorithm version, and the digest's shape. The version went from 1 to
// 2 in v0.15.0 and API.md still said 1 until this audit.
func TestDocumentedSchemaIdentity(t *testing.T) {
	t.Parallel()
	if schema.StructuralHashVersion != 4 {
		t.Errorf("StructuralHashVersion is %d; the documented value is 4", schema.StructuralHashVersion)
	}
	h := schema.StructuralHash(loadProbeSchema(t, t.Context()))
	hex, ok := strings.CutPrefix(h, "sha256:")
	if !ok || len(hex) != 64 {
		t.Errorf("StructuralHash returned %q; the documented shape is sha256:<64 hex>", h)
	}
}

// TestDocumentedBuilderExample runs API.md's headline builder chain verbatim.
// The documented example omitted a primary key and could never have built; the
// assertion is that the corrected one does.
func TestDocumentedBuilderExample(t *testing.T) {
	t.Parallel()
	s, result := schema.NewBuilder().
		WithName("MySchema").
		WithSourceID(location.MustNewSourceID("test://my-schema.yammm")).
		AddType("Person").
		WithPrimaryKey("name", schema.NewStringConstraint()).
		WithOptionalProperty("age", schema.IntegerBetween(0, 150)).
		Done().
		AddType("Car").
		WithPrimaryKey("vin", schema.NewStringConstraint()).
		WithRelation("OWNER", schema.NewTypeRef("", "Person", location.Span{}), false, false).
		Done().
		Build()
	if err := result.Err(); err != nil || s == nil {
		t.Fatalf("the documented builder example does not build: %v", err)
	}
}

// TestDocumentedRendererCallShapes pins the whole diag rendering surface.
// API.md documented FormatIssue and FormatIssues, removed in v0.12.0, and never
// mentioned FormatResultJSON, which is the only structured renderer.
func TestDocumentedRendererCallShapes(t *testing.T) {
	t.Parallel()
	r := diag.NewRenderer(
		diag.WithExcerpts(true),
		diag.WithColors(false),
		diag.WithDistinguishFatal(true),
		diag.WithModuleRoot("/project"),
	)
	res := diag.OK()
	_ = r.FormatResult(res)
	_ = r.FormatResultJSON(res)

	// The four Result methods whose truncation semantics API.md now states.
	_, _, _ = res.Len(), res.DroppedCount(), res.TruncationNote()
	_ = res.HasCode(diag.E_SYNTAX)

	// ContextualError's field types, as declared inside package diag.
	_ = diag.ContextualError{Result: diag.OK(), Tag: "t"}
}

// TestDocumentedKeyFunctions pins the graph key surface API.md documents, in
// both directions: FormatKey's canonical rendering and the round trip its
// table promises.
func TestDocumentedKeyFunctions(t *testing.T) {
	t.Parallel()
	if got := graph.FormatKey("vin-123"); got != `["vin-123"]` {
		t.Errorf("FormatKey returned %q; the documented canonical form is [\"vin-123\"]", got)
	}
	if got := graph.FormatKey("us", 12345); got != `["us",12345]` {
		t.Errorf("FormatKey returned %q; the documented form is [\"us\",12345]", got)
	}
	parts, err := graph.ParseKeyStrings(`["us","ca"]`)
	if err != nil {
		t.Fatalf("ParseKeyStrings: %v", err)
	}
	if len(parts) != 2 || parts[0] != "us" || parts[1] != "ca" {
		t.Errorf("ParseKeyStrings returned %v; the documented round trip is [us ca]", parts)
	}
}

// TestDocumentedAdapterParseSurface pins the parse and write shapes of the two
// adapters whose documented surface had drifted: adapter/json documented three
// removed methods, adapter/csv one.
func TestDocumentedAdapterParseSurface(t *testing.T) {
	t.Parallel()
	_ = (*jsonadapter.Adapter).ParseObject
	_ = (*jsonadapter.Adapter).MarshalObject
	_ = (*jsonadapter.Adapter).WriteObject
	_ = (*csvadapter.Adapter).ParseTyped
	_ = (*csvadapter.Adapter).ParseWithTypeColumn

	// Pinned as typed values, not bare method values. A bare `_ = (*T).M`
	// compiles under ANY signature, so it could not see adapter/csv's write
	// surface lose its vestigial variadic — the Serialization fence shows these
	// calls, and nothing compiled it. The json and neo4j writers keep their
	// write options, which is why only csv is pinned this way.
	var (
		marshalSnapshot func(*csvadapter.Adapter, context.Context, *graph.Snapshot) (map[string][]byte, error)
		writeSnapshot   func(*csvadapter.Adapter, context.Context, func(string) (io.Writer, error), *graph.Snapshot) error
	)
	marshalSnapshot = (*csvadapter.Adapter).MarshalSnapshot
	writeSnapshot = (*csvadapter.Adapter).WriteSnapshot
	_, _ = marshalSnapshot, writeSnapshot
}

// TestDocumentedFormatSurface pins the format package facts the Formatting
// section states: the entry point's arity and the wrap threshold's value.
func TestDocumentedFormatSurface(t *testing.T) {
	t.Parallel()
	if format.LineWidthThreshold != 100 {
		t.Errorf("LineWidthThreshold is %d; the documented value is 100", format.LineWidthThreshold)
	}
	formatted, err := format.TokenStream("schema \"S\"\n")
	if err != nil || formatted == "" {
		t.Fatalf("the documented TokenStream shape: %v", err)
	}
	// The four documented signatures, held as typed values so a shape change
	// fails here rather than only in the prose.
	var (
		wrap, align, indent func(string) string
		width               func(string) int
	)
	wrap, align, indent, width = format.WrapLongLines, format.AlignColumns, format.NormalizeIndentation, format.DisplayWidth
	_, _, _, _ = wrap, align, indent, width
}

// TestDocumentedModuleRootDiscovery pins the module-root discovery surface the
// "Module root discovery" section documents: the finder's three results, the
// typed error matched with errors.As and the fields the prose names, the issue
// builder, the marker constant, and the family predicate. A signature change
// fails here rather than only leaving the prose stating a call that no longer
// exists.
func TestDocumentedModuleRootDiscovery(t *testing.T) {
	t.Parallel()

	// The three documented signatures, held as typed values so a shape change
	// fails here rather than only in the prose.
	var (
		find         func(string) (string, bool, error)
		buildIssue   func(error) diag.Issue
		isResolution func(string) bool
	)
	find, buildIssue, isResolution = schema.FindModuleRoot, schema.ModuleRootIssue, diag.IsImportResolutionCode

	dir := t.TempDir()
	root, found, err := find(dir)
	if err != nil {
		t.Fatalf("FindModuleRoot on a marker-free temp dir: %v", err)
	}
	if found || root != "" {
		t.Fatalf("FindModuleRoot = (%q, %v); a marker sits above this test's temp directory", root, found)
	}

	// The documented errors.As shape and the three fields the section names.
	marker := filepath.Join(dir, schema.ModuleRootMarker)
	if err := os.WriteFile(marker, []byte("module example.com/thing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = find(dir)
	malformed, ok := errors.AsType[*schema.MalformedModuleRootError](err)
	if !ok {
		t.Fatalf("errors.AsType against *MalformedModuleRootError failed for %v", err)
	}
	if malformed.Path == "" || malformed.Line == 0 || malformed.Reason == "" {
		t.Errorf("the documented fields are not all populated: %+v", malformed)
	}

	if code := buildIssue(err).Code(); code != diag.E_LOAD_MODULE_ROOT_MALFORMED {
		t.Errorf("ModuleRootIssue produced %v, want E_LOAD_MODULE_ROOT_MALFORMED", code)
	}

	// The family predicate and the origin vocabulary the section enumerates.
	for _, code := range []diag.Code{diag.E_IMPORT_RESOLVE, diag.E_PATH_ESCAPE, diag.E_IMPORT_CYCLE} {
		if !isResolution(code.String()) {
			t.Errorf("%s is documented as a member of the import-resolution family", code)
		}
	}
	for _, origin := range []string{
		diag.ModuleRootExplicit, diag.ModuleRootDiscovered,
		diag.ModuleRootSynthetic, diag.ModuleRootDefault, diag.ModuleRootNone,
	} {
		if origin == "" {
			t.Error("an origin value the section enumerates is empty")
		}
	}
	_ = diag.DetailKeyModuleRoot
	_ = diag.DetailKeyModuleRootOrigin
}

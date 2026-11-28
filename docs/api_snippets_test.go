package docs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/moduleroot"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/neo4j"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
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
// documentation in neo4j/doc.go ("Diff Scope"), and
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

func loadProbeSchema(t *testing.T, ctx context.Context) schema.Schema {
	t.Helper()
	s, loadErr := schema.LoadString(ctx, probeSchema, "probe.yammm")
	if err := loadErr; err != nil {
		t.Fatalf("loading probe schema: %v", err)
	}
	return s
}

// TestDocumentedDiffCallShapes pins the diff call shapes the documentation
// shows: one Diff over the mapping, the schema and both kinds of remote rows,
// and six result buckets on each half. It is a compile-time assertion first —
// the runtime assertions only guard against the entry points silently returning
// nothing to classify.
func TestDocumentedDiffCallShapes(t *testing.T) {
	ctx := context.Background()
	s := loadProbeSchema(t, ctx)
	c := cypher.Config{}

	desiredConstraints, genErr := c.Constraints(s)
	if err := genErr; err != nil {
		t.Fatalf("generating constraints: %v", err)
	}
	desiredIndexes, genErr2 := c.Indexes(s)
	if err := genErr2; err != nil {
		t.Fatalf("generating indexes: %v", err)
	}

	// An empty database: every declaration is a create, which is enough to
	// exercise the signatures and the bucket names.
	var actualConstraints []neo4j.RemoteConstraint
	var actualIndexes []neo4j.RemoteIndex

	plan, diffErr := neo4j.Diff(c, s, actualConstraints, actualIndexes)
	if err := diffErr; err != nil {
		t.Fatalf("planning: %v", err)
	}
	cDiff, iDiff := plan.Constraints, plan.Indexes
	owned := c.OwnedLabels(s)

	// Every bucket the documentation names, on both results.
	cBuckets := len(cDiff.Match) + len(cDiff.Drift) + len(cDiff.Create) + len(cDiff.Drop) + cDiff.Excluded
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
		t.Error("OwnedLabels does not own the label the mapping gives Document")
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

	// An unreported state counts as online, so a hand-built record does not
	// read as drift.
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
	if got, want := neo4j.ShowConstraints, "SHOW CONSTRAINTS YIELD name, type, entityType, labelsOrTypes, properties, propertyType, createStatement"; got != want {
		t.Errorf("ShowConstraints = %q, documented as %q", got, want)
	}

	indexQuery := neo4j.ShowIndexes
	for _, column := range []string{
		"name", "type", "entityType", "labelsOrTypes",
		"properties", "options", "state", "owningConstraint",
	} {
		if !strings.Contains(indexQuery, column) {
			t.Errorf("ShowIndexes omits the documented column %q: %s", column, indexQuery)
		}
	}
	// Constraint-backing and LOOKUP indexes are deliberately returned: a filter
	// on either would silently remove rows the diff needs to detect a blocked
	// create.
	if strings.Contains(indexQuery, "WHERE") {
		t.Errorf("ShowIndexes filters the rows API.md documents as every index: %s", indexQuery)
	}
	if strings.Contains(indexQuery, "owningConstraint IS NULL") {
		t.Errorf("ShowIndexes filters out constraint-backing indexes, "+
			"which the documented diff behavior depends on receiving: %s", indexQuery)
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
	var res diag.Result
	_ = r.FormatResult(res)
	_ = r.FormatResultJSON(res)

	// The four Result methods whose truncation semantics API.md now states.
	_, _, _ = res.Len(), res.DroppedCount(), res.TruncationNote()
	_ = res.HasCode(diag.E_SYNTAX)

	// ResultError's field type, as declared inside package diag.
	_ = diag.ResultError{Result: res}
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
	parts, err := graph.ParseKey(`["us","ca"]`)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	if len(parts) != 2 || parts[0] != "us" || parts[1] != "ca" {
		t.Errorf("ParseKey returned %v; the documented round trip is [us ca]", parts)
	}
}

// TestDocumentedModuleRootDiscovery pins what the "Module root discovery"
// section states through Load, its one Go door: a malformed marker above the
// entry fails the load with E_LOAD_MODULE_ROOT_MALFORMED, whose module_root
// detail names the marker's directory and whose origin is "discovered", and the
// section's five origin values exist.
func TestDocumentedModuleRootDiscovery(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, moduleroot.Find)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yammm.mod"), []byte("module example.com/thing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "main.yammm")
	if err := os.WriteFile(entry, []byte("schema \"s\"\n\ntype A {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := schema.Load(t.Context(), entry)
	if !errors.Is(err, diag.E_LOAD_MODULE_ROOT_MALFORMED) {
		t.Fatalf("Load under a malformed marker = %v, want E_LOAD_MODULE_ROOT_MALFORMED", err)
	}
	re, _ := errors.AsType[*diag.ResultError](err)
	root, err := location.SourceIDFromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if re.Result.Len() == 0 {
		t.Fatal("the load's ResultError holds no issue")
	}
	for issue := range re.Result.Issues() {
		details := map[string]string{}
		for _, d := range issue.Details() {
			details[d.Key] = d.Value
		}
		if got := details[diag.DetailKeyModuleRoot]; got != root.String() {
			t.Errorf("module_root = %q, want the marker's directory %q", got, root)
		}
		if got := details[diag.DetailKeyModuleRootOrigin]; got != diag.ModuleRootDiscovered {
			t.Errorf("module_root_origin = %q, want %q", got, diag.ModuleRootDiscovered)
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
}

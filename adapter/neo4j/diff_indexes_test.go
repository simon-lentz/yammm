package neo4j

import (
	"slices"
	"strings"
	"testing"
)

// vectorOptions builds a driver-shaped options map for a VECTOR index.
func vectorOptions(dim int, sim string) map[string]any {
	return map[string]any{
		"indexConfig": map[string]any{
			"vector.dimensions":          int64(dim),
			"vector.similarity_function": sim,
		},
	}
}

// wantCounts asserts the classification sizes, INCLUDING Unverified, so every
// caller states the expected Unverified count rather than leaving the bucket
// unread.
//
// A change that wrongly appends to Unverified — dropping a continue, or adding a
// second unverifiability probe alongside the drift check — is invisible to a
// helper that reads only four buckets, and the CLI then downgrades confirmed
// drift to an "unverified" exit code while a deploy gate stops failing on it.
func wantCounts(t *testing.T, r *IndexDiffResult, match, drift, create, drop, unverified int) {
	t.Helper()
	assertCounts(t, "index",
		len(r.Match), len(r.Drift), len(r.Create), len(r.Drop), len(r.Unverified),
		match, drift, create, drop, unverified)
}

// assertCounts is the one five-bucket comparison both diffs use. A sixth
// classification added later has to be threaded through one helper, not two, so
// neither side can silently stop checking it.
func assertCounts(t *testing.T, noun string, gotMatch, gotDrift, gotCreate, gotDrop, gotUnverified,
	match, drift, create, drop, unverified int,
) {
	t.Helper()
	if gotMatch != match || gotDrift != drift || gotCreate != create ||
		gotDrop != drop || gotUnverified != unverified {
		t.Errorf("%s match/drift/create/drop/unverified = %d/%d/%d/%d/%d; want %d/%d/%d/%d/%d",
			noun, gotMatch, gotDrift, gotCreate, gotDrop, gotUnverified,
			match, drift, create, drop, unverified)
	}
}

func TestDiffIndexes_AllMatch(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "i2", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "cosine")},
	}

	wantCounts(t, a.DiffIndexes(desired, actual, testOwned()), 2, 0, 0, 0, 0)
}

func TestDiffIndexes_CreateNew(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"published_on"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
	}

	result := a.DiffIndexes(desired, actual, testOwned())
	wantCounts(t, result, 1, 0, 1, 0, 0)
	if !slices.Equal(result.Create[0].Properties, []string{"published_on"}) {
		t.Errorf("Create[0].Properties = %v; want [published_on]", result.Create[0].Properties)
	}
}

func TestDiffIndexes_DropOrphaned(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "i2", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"old_field"}},
	}

	result := a.DiffIndexes(desired, actual, testOwned())
	wantCounts(t, result, 1, 0, 0, 1, 0)
	if result.Drop[0].Properties[0] != "old_field" {
		t.Errorf("Drop[0] property = %q; want old_field", result.Drop[0].Properties[0])
	}
}

// TestDiffIndexes_OrderSensitive pins the deliberate divergence from the
// constraint diff: a composite index whose remote property order differs is a
// distinct index — Create + Drop, not Match.
func TestDiffIndexes_OrderSensitive(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"a", "b"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"b", "a"}},
	}

	wantCounts(t, a.DiffIndexes(desired, actual, testOwned()), 0, 0, 1, 1, 0)
}

func TestDiffIndexes_VectorDimensionDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(512, "cosine")},
	}

	result := a.DiffIndexes(desired, actual, testOwned())
	wantCounts(t, result, 0, 1, 0, 0, 0)
	if !strings.Contains(result.Drift[0].Reason, "dimension") {
		t.Errorf("Drift reason = %q; want a dimension mismatch", result.Drift[0].Reason)
	}
}

func TestDiffIndexes_VectorSimilarityDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "euclidean")},
	}

	result := a.DiffIndexes(desired, actual, testOwned())
	wantCounts(t, result, 0, 1, 0, 0, 0)
	if !strings.Contains(result.Drift[0].Reason, "similarity") {
		t.Errorf("Drift reason = %q; want a similarity mismatch", result.Drift[0].Reason)
	}
}

// TestDiffIndexes_VectorSimilarityCaseInsensitive pins that the similarity
// comparison is case-insensitive: Neo4j reports the similarity function
// uppercased ("COSINE"), while the schema side is lowercase ("cosine"), so a
// case-sensitive compare would misclassify every in-sync vector index as drift.
func TestDiffIndexes_VectorSimilarityCaseInsensitive(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "COSINE")},
	}

	wantCounts(t, a.DiffIndexes(desired, actual, testOwned()), 1, 0, 0, 0, 0)
}

func TestDiffIndexes_FiltersNonOwned(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "other", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"other_schema__Foo"}, Properties: []string{"x"}},
	}

	// The other-schema index is neither matched nor reported as a drop.
	wantCounts(t, a.DiffIndexes(desired, actual, testOwned()), 1, 0, 0, 0, 0)
}

// TestDiffIndexes_EmptyDesired_AllDrop pins the always-on drift behavior: a
// schema-owned remote index with no declaration is reported as a drop.
func TestDiffIndexes_EmptyDesired_AllDrop(t *testing.T) {
	t.Parallel()
	a := New()

	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "i2", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "cosine")},
	}

	wantCounts(t, a.DiffIndexes(nil, actual, testOwned()), 0, 0, 0, 2, 0)
}

func TestDiffIndexes_Empty(t *testing.T) {
	t.Parallel()
	a := New()
	wantCounts(t, a.DiffIndexes(nil, nil, testOwned()), 0, 0, 0, 0, 0)
}

// SHOW INDEXES reports every non-LOOKUP, non-constraint-backed index, including
// kinds the DSL cannot express (TEXT, POINT, and 4.x BTREE), multi-label
// FULLTEXT indexes (a declaration always targets one label), and relationship
// indexes the adapter never emits. Classifying one as an undeclared drop
// reports drift no schema edit can resolve. Single-label FULLTEXT is
// deliberately absent here: it IS declarable — see
// [TestDiffIndexes_UndeclaredFulltextDrops].
func TestDiffIndexes_IgnoresUndeclarableKinds(t *testing.T) {
	t.Parallel()
	a := New()

	actual := []RemoteIndex{
		{Name: "ft", Type: "FULLTEXT", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity", "other__Thing"}, Properties: []string{"body"}},
		{Name: "tx", Type: "TEXT", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "pt", Type: "POINT", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"loc"}},
		{Name: "bt", Type: "BTREE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"legacy"}},
		{Name: "rel", Type: "RANGE", EntityType: "RELATIONSHIP", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"since"}},
	}

	wantCounts(t, a.DiffIndexes(nil, actual, testOwned()), 0, 0, 0, 0, 0)
}

// A remote RANGE index on a schema-owned label with no declaration IS drift the
// schema can resolve, so it stays a drop — the undeclarable-kind exemption must
// not swallow the case the feature exists for.
func TestDiffIndexes_UndeclaredRangeStillDrops(t *testing.T) {
	t.Parallel()
	a := New()

	actual := []RemoteIndex{
		{Name: "r", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
	}

	wantCounts(t, a.DiffIndexes(nil, actual, testOwned()), 0, 0, 0, 1, 0)
}

// allIndexKinds must contain every kind indexKindToRemoteType recognises.
//
// The sweep is the whole point. Asking allIndexKinds what to check makes the
// list agree with itself and proves nothing: the failure that matters is a kind
// present in the mapper and ABSENT from the list, which RemoteIndex.Declarable
// then stops recognising, silently dropping every remote index of that kind out
// of the diff. So this asks the mapper instead, over a numeric range far wider
// than the enum, and catches a new kind wherever it is placed — below
// IndexRange, or at a non-contiguous explicit value. Asking allIndexKinds what
// to check would make the list agree with itself and prove nothing.
func TestIndexKind_EnumerationIsComplete(t *testing.T) {
	t.Parallel()

	// The sweep spans negatives as well as positives. IndexRange is 0, so "a
	// kind added below IndexRange" means a negative constant — which a
	// zero-based sweep can never find, and which is exactly the placement the
	// enumeration is most likely to acquire.
	const sweep = 64
	var mapped []IndexKind
	for i := -sweep; i < sweep; i++ {
		k := IndexKind(i)
		if indexKindToRemoteType(k) != unknownRemoteIndexType {
			mapped = append(mapped, k)
		}
	}

	if len(mapped) == 0 {
		t.Fatal("the sweep found no mapped kinds; indexKindToRemoteType or unknownRemoteIndexType changed shape")
	}
	for _, k := range mapped {
		if !slices.Contains(allIndexKinds, k) {
			t.Errorf("IndexKind %d maps to %q but is missing from allIndexKinds; "+
				"RemoteIndex.Declarable would silently drop its remote indexes from the diff",
				k, indexKindToRemoteType(k))
		}
	}
	for _, k := range allIndexKinds {
		if indexKindToRemoteType(k) == unknownRemoteIndexType {
			t.Errorf("allIndexKinds contains IndexKind %d, which indexKindToRemoteType does not map", k)
		}
	}
}

// Every mapped kind must have a distinct remote type string: RemoteIndex.Declarable
// and the identity keys both compare on it, so two kinds sharing one string
// would make a remote index of one match a desired index of the other.
func TestIndexKind_RemoteTypesAreDistinct(t *testing.T) {
	t.Parallel()
	seen := make(map[string]IndexKind, len(allIndexKinds))
	for _, k := range allIndexKinds {
		rt := indexKindToRemoteType(k)
		if prev, dup := seen[rt]; dup {
			t.Errorf("IndexKind %d and %d both map to %q", prev, k, rt)
		}
		seen[rt] = k
	}
}

// A schema-owned remote index that duplicates a matched one is the redundant,
// undeclared definition the diff exists to surface: one realises the
// declaration and the other is a drop. Both must be reported, so pairing has to
// be one-to-one rather than a lookup by identity.
func TestDiffIndexes_DuplicateOfMatchedIndexStillDrops(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"code"}},
	}
	actual := []RemoteIndex{
		{Name: "test__Entity_code_idx", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"code"}},
		{Name: "hand_made_code_lookup", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"code"}},
	}

	got := a.DiffIndexes(desired, actual, testOwned())
	wantCounts(t, got, 1, 0, 0, 1, 0)
	if len(got.Drop) == 1 && got.Drop[0].Name != "hand_made_code_lookup" {
		t.Errorf("Drop[0].Name = %q; the SECOND member of the bucket is the undeclared duplicate", got.Drop[0].Name)
	}
}

// An index whose vector configuration the server does not report is classified
// as unverified, not as a match: remoteIndexKey matches on label/properties/type
// only, so a wrongly-dimensioned remote index would otherwise be reported as in
// sync.
func TestDiffIndexes_VectorMissingConfig_Unverified(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}},
	}

	got := a.DiffIndexes(desired, actual, testOwned())
	wantCounts(t, got, 0, 0, 0, 0, 1)
	if got.Unverified[0].Reason == "" {
		t.Error("an unverified entry should carry a reason")
	}
}

// When only one vector setting is unreadable, the reason must name which — a
// single fixed "no vector configuration" message sent an operator looking for a
// server that reports no options at all, when one setting had in fact been read.
func TestDiffIndexes_VectorPartialConfig_ReasonNamesTheGap(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"},
		VectorDimensions: 768, VectorSimilarity: "cosine",
	}}

	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"dimension only", map[string]any{"vector.dimensions": int64(768)}, "similarity"},
		{"similarity only", map[string]any{"vector.similarity_function": "cosine"}, "dimension"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := New().DiffIndexes(desired, []RemoteIndex{{
				Name: "i1", Type: "VECTOR", EntityType: "NODE",
				LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"},
				Options: map[string]any{"indexConfig": tc.cfg},
			}}, testOwned())
			wantCounts(t, got, 0, 0, 0, 0, 1)
			if !strings.Contains(got.Unverified[0].Reason, tc.want) {
				t.Errorf("reason = %q; should name the %s as the unread setting",
					got.Unverified[0].Reason, tc.want)
			}
		})
	}
}

// A setting the database DID disclose and that disagrees is definite and
// actionable, so it outranks a second setting being unreadable: each setting is
// compared independently, and a demonstrably wrong dimension is drift whether or
// not the similarity function beside it could be read.
func TestDiffIndexes_ReadableDriftOutranksUnreadable(t *testing.T) {
	t.Parallel()
	got := New().DiffIndexes(
		[]Index{{
			Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"},
			VectorDimensions: 768, VectorSimilarity: "cosine",
		}},
		[]RemoteIndex{{
			Name: "i1", Type: "VECTOR", EntityType: "NODE",
			LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"},
			// Dimension readable and wrong; similarity absent.
			Options: map[string]any{"indexConfig": map[string]any{"vector.dimensions": int64(512)}},
		}}, ownedSet("test__Entity"),
	)

	wantCounts(t, got, 0, 1, 0, 0, 0)
	if !strings.Contains(got.Drift[0].Reason, "dimension") {
		t.Errorf("Drift reason = %q; want the readable dimension mismatch", got.Drift[0].Reason)
	}
}

// An index the server reports as FAILED matches its declaration in every other
// column while serving no queries, so a definition-only comparison calls it in
// sync. POPULATING is transient and resolves on its own, so it is unverified
// rather than a change to apply.
func TestDiffIndexes_IndexState(t *testing.T) {
	t.Parallel()
	desired := []Index{{Name: "i", Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}}}

	for _, tc := range []struct {
		state                    string
		match, drift, unverified int
	}{
		{"ONLINE", 1, 0, 0},
		{"", 1, 0, 0}, // server did not report it: tolerate, do not invent drift
		{"FAILED", 0, 1, 0},
		{"POPULATING", 0, 0, 1},
	} {
		t.Run("state="+tc.state, func(t *testing.T) {
			t.Parallel()
			got := New().DiffIndexes(desired, []RemoteIndex{{
				Name: "i", Type: "RANGE", EntityType: "NODE",
				LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"},
				State: tc.state,
			}}, testOwned())
			if len(got.Match) != tc.match || len(got.Drift) != tc.drift || len(got.Unverified) != tc.unverified {
				t.Errorf("match/drift/unverified = %d/%d/%d; want %d/%d/%d",
					len(got.Match), len(got.Drift), len(got.Unverified),
					tc.match, tc.drift, tc.unverified)
			}
		})
	}
}

// A constraint's backing index is created and dropped with its constraint, so
// the diff must not report one as an undeclared orphan — even when the caller
// introspected with `SHOW INDEXES YIELD *` instead of IntrospectIndexesQuery(),
// which filters them server-side.
func TestDiffIndexes_IgnoresConstraintBackedIndexes(t *testing.T) {
	t.Parallel()
	got := New().DiffIndexes(nil, []RemoteIndex{{
		Name: "test__Entity_id_unique", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
		OwningConstraint: "test__Entity_id_unique",
	}}, testOwned())
	if len(got.Drop) != 0 {
		t.Errorf("Drop = %d; a constraint-backing index is not independently declarable", len(got.Drop))
	}
}

// The declarability gate admits a remote Type case-insensitively, so the
// identity key must canonicalise it too. When the two disagree, an index that
// exactly realises a declaration cannot pair: it becomes an orphan to drop while
// the declaration becomes a create, so the plan tells the operator to drop the
// very index satisfying the schema.
func TestDiffIndexes_LowercaseRemoteTypeStillPairs(t *testing.T) {
	t.Parallel()
	got := New().DiffIndexes(
		[]Index{{Name: "app__T_x_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"x"}}},
		[]RemoteIndex{{
			// A consumer that lower-cases the SHOW INDEXES type column.
			Name: "app__T_x_idx", Type: "range", EntityType: "NODE",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}, State: "ONLINE",
		}}, appOwned(),
	)
	wantCounts(t, got, 1, 0, 0, 0, 0)
}

// Pairing by identity alone must survive the same case difference: with the
// names disagreeing, only the identity key can match the pair, so a
// case-sensitive key reports Create + Drop for one already-correct index.
func TestDiffIndexes_LowercaseRemoteTypePairsByIdentity(t *testing.T) {
	t.Parallel()
	got := New().DiffIndexes(
		[]Index{{Name: "schema_name", Kind: IndexRange, Label: "app__T", Properties: []string{"x"}}},
		[]RemoteIndex{{
			Name: "database_name", Type: "range", EntityType: "NODE",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}, State: "ONLINE",
		}}, appOwned(),
	)
	wantCounts(t, got, 1, 0, 0, 0, 0)
}

// Index names are global, but definitions are not: node and relationship indexes
// occupy separate namespaces. A relationship index whose type is spelled like an
// owned node label must not absorb a node index's definition, or the CREATE is
// never emitted and the operator is told to drop an unrelated relationship
// index.
func TestDiffIndexes_RelationshipIndexDoesNotBlockNodeCreate(t *testing.T) {
	t.Parallel()
	got := New().DiffIndexes(
		[]Index{{Name: "app__T_since_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"since"}}},
		[]RemoteIndex{{
			Name: "rel_idx", Type: "RANGE", EntityType: "RELATIONSHIP",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"since"}, State: "ONLINE",
		}}, appOwned(),
	)
	wantCounts(t, got, 0, 0, 1, 0, 0)
	if len(got.Drop) != 0 {
		t.Error("a relationship index was claimed as a droppable schema object")
	}

	// The same record as a NODE index is owned, declarable, and realises the
	// declaration — so the outcome above turns on the entity type alone and not
	// on the record being unrecognisable for some other reason.
	asNode := New().DiffIndexes(
		[]Index{{Name: "app__T_since_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"since"}}},
		[]RemoteIndex{{
			Name: "rel_idx", Type: "RANGE", EntityType: "NODE",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"since"}, State: "ONLINE",
		}}, appOwned(),
	)
	wantCounts(t, asNode, 1, 0, 0, 0, 0)
}

// Declaring @@index over a primary key is legal in the DSL, and the server
// serves that definition from the constraint's backing index rather than
// creating a second one. The declaration is therefore satisfied, not blocked:
// reporting drift would be permanent, since dropping the backing index means
// dropping the primary key, and any deploy gate keyed on a clean diff could
// never pass on a valid, fully-applied schema.
func TestDiffIndexes_ConstraintBackedDefinitionSatisfiesDeclaration(t *testing.T) {
	t.Parallel()
	backing := RemoteIndex{
		Name: "pk__Doc_doc_id_unique", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"doc_id"},
		OwningConstraint: "pk__Doc_doc_id_unique", State: "ONLINE",
	}
	got := New().DiffIndexes(
		[]Index{{Name: "app__T_doc_id_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"doc_id"}}},
		[]RemoteIndex{backing}, appOwned(),
	)
	wantCounts(t, got, 1, 0, 0, 0, 0)
	if got.Match[0].Actual.Name != backing.Name {
		t.Errorf("Match Actual = %q; want the backing index that serves the definition", got.Match[0].Actual.Name)
	}

	// A backing index over a DIFFERENT property serves nothing the schema
	// declared, so the declaration is still a Create — the branch above turns on
	// the definitions agreeing, not on the index merely backing a constraint.
	elsewhere := backing
	elsewhere.Properties = []string{"other_id"}
	got = New().DiffIndexes(
		[]Index{{Name: "app__T_doc_id_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"doc_id"}}},
		[]RemoteIndex{elsewhere}, appOwned(),
	)
	wantCounts(t, got, 0, 0, 1, 0, 0)
}

// A definition served by a constraint's backing index satisfies the declaration
// — but only while that index actually serves queries. The state check every
// paired index goes through applies here too: skipping it reports a POPULATING
// or FAILED backing index as in sync and a deploy gate exits 0 while nothing
// serves the declared lookup.
func TestDiffIndexes_ConstraintBackedDefinitionHonoursState(t *testing.T) {
	t.Parallel()
	a := New()
	desired := []Index{{
		Name: "app__Doc_doc_id_idx", Kind: IndexRange,
		Label: "app__Doc", Properties: []string{"doc_id"},
	}}
	backing := func(state string) []RemoteIndex {
		return []RemoteIndex{{
			Name: "pk__Doc_doc_id_unique", Type: "RANGE", EntityType: "NODE",
			LabelsOrTypes: []string{"app__Doc"}, Properties: []string{"doc_id"},
			OwningConstraint: "pk__Doc_doc_id_unique", State: state,
		}}
	}

	// Control: ONLINE is the satisfied case, so the assertions below turn on the
	// state and not on the branch being unreachable.
	wantCounts(t, a.DiffIndexes(desired, backing("ONLINE"), ownedSet("app__Doc")), 1, 0, 0, 0, 0)
	wantCounts(t, a.DiffIndexes(desired, backing("POPULATING"), ownedSet("app__Doc")), 0, 0, 0, 0, 1)
	wantCounts(t, a.DiffIndexes(desired, backing("FAILED"), ownedSet("app__Doc")), 0, 1, 0, 0, 0)
}

// A NOT NULL or TYPE constraint has no backing index, so it appears in no SHOW
// INDEXES row — but its name still occupies the one namespace indexes share with
// constraints. Reaching the index diff only through alsoBlocking, it must turn a
// Create the server would ignore into drift naming the holder.
func TestDiffIndexes_ConstraintNameBlocksCreate(t *testing.T) {
	t.Parallel()
	a := New()
	desired := []Index{{
		Name: "app__T_x_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"x"},
	}}
	blocker := RemoteConstraint{
		Name: "app__T_x_idx", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"y"},
	}

	got := a.DiffIndexes(desired, nil, ownedSet("app__T"), blocker)
	wantCounts(t, got, 0, 1, 0, 0, 0)
	if len(got.Drift) == 1 && !strings.Contains(got.Drift[0].Reason, "constraint") {
		t.Errorf("Reason = %q; want it to name the constraint holding the name", got.Drift[0].Reason)
	}

	// Control: not passing the constraint is the degraded contract — the same
	// declaration is a Create, because nothing told the diff the name was taken.
	wantCounts(t, a.DiffIndexes(desired, nil, ownedSet("app__T")), 0, 0, 1, 0, 0)

	// Control: a constraint whose name collides with nothing leaves the create
	// alone, so the branch turns on the name and not on any constraint existing.
	free := blocker
	free.Name = "app__T_y_not_null"
	wantCounts(t, a.DiffIndexes(desired, nil, ownedSet("app__T"), free), 0, 0, 1, 0, 0)
}

// A blocker pairing already handed to another declaration still blocks — the
// server no-ops the CREATE either way — but the report otherwise shows one
// object as both matched and to be dropped with nothing connecting the lines.
func TestDiffIndexes_BlockerThatRealisesAnotherDeclarationSaysSo(t *testing.T) {
	t.Parallel()
	a := New()
	desired := []Index{
		{Name: "app__T_code_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"code"}},
		{Name: "app__T_sku_idx", Kind: IndexRange, Label: "app__T", Properties: []string{"sku"}},
	}
	// An operator's index carrying the name generated for the `code` declaration
	// while actually serving the `sku` one.
	actual := []RemoteIndex{{
		Name: "app__T_code_idx", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"sku"},
	}}

	got := a.DiffIndexes(desired, actual, ownedSet("app__T"))
	wantCounts(t, got, 1, 1, 0, 0, 0)
	if len(got.Drift) == 1 && !strings.Contains(got.Drift[0].Reason, "realises another declaration") {
		t.Errorf("Reason = %q; want it to say the holder realises another declaration", got.Drift[0].Reason)
	}

	// Control: a blocker that realises nothing this schema declares gets the
	// plain message, so the branch turns on pairing and not on the name alone.
	foreign := []RemoteIndex{{
		Name: "app__T_code_idx", Type: "TEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"other__Thing"}, Properties: []string{"z"},
	}}
	plain := a.DiffIndexes(desired[:1], foreign, ownedSet("app__T"))
	if len(plain.Drift) != 1 {
		t.Fatalf("got %d drift entries; want 1", len(plain.Drift))
	}
	if strings.Contains(plain.Drift[0].Reason, "realises another declaration") {
		t.Errorf("Reason = %q; a foreign blocker realises nothing here", plain.Drift[0].Reason)
	}
}

// A FULLTEXT node index really does report several labels, so a message telling
// an operator which object to drop must name all of them — naming the first
// sends them looking under a label the object does not exclusively belong to.
func TestDiffIndexes_BlockerMessageNamesEveryLabel(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "app__Doc_body_idx", Kind: IndexRange, Label: "app__Doc", Properties: []string{"body"},
	}}
	actual := []RemoteIndex{{
		Name: "app__Doc_body_idx", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"other__Thing", "app__Doc"}, Properties: []string{"body"},
	}}

	got := New().DiffIndexes(desired, actual, ownedSet("app__Doc"))
	if len(got.Drift) != 1 {
		t.Fatalf("got %d drift entries; want 1", len(got.Drift))
	}
	if !strings.Contains(got.Drift[0].Reason, "other__Thing|app__Doc") {
		t.Errorf("Reason = %q; want every label the blocker carries", got.Drift[0].Reason)
	}
}

// DiffIndexes is public API and a caller may build Index values with no name —
// every fixture in this file does. A nameless desired index blocked by an
// equally nameless remote is a DEFINITION block, and describing it as a name
// collision tells the operator to drop an index called "".
func TestDiffIndexes_NamelessBlockerIsDescribedAsADefinition(t *testing.T) {
	t.Parallel()
	desired := []Index{{Kind: IndexRange, Label: "app__T", Properties: []string{"x"}}}
	actual := []RemoteIndex{{
		Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"},
	}}

	// The remote is declarable and on an owned label, so pairing claims it by
	// identity; a second nameless declaration of the same definition is the one
	// that reaches the blocker path.
	got := New().DiffIndexes(append(desired, desired[0]), actual, ownedSet("app__T"))
	if len(got.Drift) != 1 {
		t.Fatalf("got %d drift entries; want 1: %+v", len(got.Drift), got)
	}
	if strings.Contains(got.Drift[0].Reason, `index name ""`) {
		t.Errorf("Reason = %q; a nameless pair is a definition block, not a name collision", got.Drift[0].Reason)
	}
	if !strings.Contains(got.Drift[0].Reason, "definition is already served") {
		t.Errorf("Reason = %q; want the definition-block message", got.Drift[0].Reason)
	}
}

// Objects the diff could not compare must be counted, or an empty Drop reads as
// "the database is accounted for".
func TestDiffIndexes_ExcludedCountsWhatWasNotCompared(t *testing.T) {
	t.Parallel()
	actual := []RemoteIndex{
		{Name: "mine", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"}},
		// A shape the DSL cannot declare: a multi-label FULLTEXT on an owned
		// label. (A single-label FULLTEXT would be a Drop, not excluded.)
		{Name: "ft", Type: "FULLTEXT", EntityType: "NODE", LabelsOrTypes: []string{"app__T", "other__Thing"}, Properties: []string{"body"}},
		// A label no current type declares.
		{Name: "stale", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"app__Publisher"}, Properties: []string{"id"}},
	}
	got := New().DiffIndexes(
		[]Index{{Name: "mine", Kind: IndexRange, Label: "app__T", Properties: []string{"x"}}},
		actual, ownedSet("app__T"),
	)
	wantCounts(t, got, 1, 0, 0, 0, 0)
	if got.Excluded != 2 {
		t.Errorf("Excluded = %d; want the multi-label FULLTEXT and stale-label indexes counted", got.Excluded)
	}
}

// The fulltext acceptance criterion for a schema-first consumer migrating a
// hand-named registry: a remote FULLTEXT row whose (label, kind, properties)
// equal a declaration's pairs by identity in phase two regardless of its name —
// Match under the legacy name, no rename, no DDL.
func TestDiffIndexes_FulltextIdentityPairingUnderForeignName(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "app__Doc_body_fulltext_idx", Kind: IndexFulltext,
		Label: "app__Doc", Properties: []string{"body"},
	}}
	actual := []RemoteIndex{{
		Name: "legacy_body_fulltext", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"app__Doc"}, Properties: []string{"body"},
	}}
	got := New().DiffIndexes(desired, actual, ownedSet("app__Doc"))
	wantCounts(t, got, 1, 0, 0, 0, 0)
	if len(got.Match) == 1 && got.Match[0].Actual.Name != "legacy_body_fulltext" {
		t.Errorf("Match.Actual.Name = %q; want the legacy name claimed as-is", got.Match[0].Actual.Name)
	}
}

// The declarability flip: an owned, undeclared, single-label FULLTEXT row is a
// Drop — the drift authority the fulltext annotations exist to provide. Before
// FULLTEXT became declarable such a row was merely Excluded.
func TestDiffIndexes_UndeclaredFulltextDrops(t *testing.T) {
	t.Parallel()
	actual := []RemoteIndex{{
		Name: "orphan_ft", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"body"},
	}}
	wantCounts(t, New().DiffIndexes(nil, actual, testOwned()), 0, 0, 0, 1, 0)
}

// A fulltext name held under a different definition is drift, exactly as for
// range: the CREATE carries IF NOT EXISTS and would silently no-op.
func TestDiffIndexes_FulltextNameOnlyPairingIsDrift(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "app__Doc_body_fulltext_idx", Kind: IndexFulltext,
		Label: "app__Doc", Properties: []string{"body"},
	}}
	actual := []RemoteIndex{{
		Name: "app__Doc_body_fulltext_idx", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"app__Doc"}, Properties: []string{"title"},
	}}
	got := New().DiffIndexes(desired, actual, ownedSet("app__Doc"))
	wantCounts(t, got, 0, 1, 0, 0, 0)
	if len(got.Drift) == 1 && !strings.Contains(got.Drift[0].Reason, "FULLTEXT") {
		t.Errorf("Reason = %q; want the shapes rendered with their FULLTEXT kind", got.Drift[0].Reason)
	}
}

// A multi-label FULLTEXT row is a different object from any single-label
// declaration: it stays out of identity pairing and out of the definition
// map — the server creates the declaration beside it, so the declaration is a
// genuine Create — and it is counted in Excluded, not dropped.
func TestDiffIndexes_MultiLabelFulltextExcludedAndDoesNotBlockDefinition(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "app__Doc_body_fulltext_idx", Kind: IndexFulltext,
		Label: "app__Doc", Properties: []string{"body"},
	}}
	actual := []RemoteIndex{{
		Name: "combined_ft", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"app__Doc", "other__Thing"}, Properties: []string{"body"},
	}}
	got := New().DiffIndexes(desired, actual, ownedSet("app__Doc"))
	wantCounts(t, got, 0, 0, 1, 0, 0)
	if got.Excluded != 1 {
		t.Errorf("Excluded = %d; want the multi-label row counted", got.Excluded)
	}
}

// A multi-label FULLTEXT row's NAME still blocks every CREATE — blockers are
// collected before the declarability filter — and the blocker message names
// every label the object carries.
func TestDiffIndexes_MultiLabelFulltextNameStillBlocks(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "held_name", Kind: IndexFulltext, Label: "app__Doc", Properties: []string{"body"},
	}}
	actual := []RemoteIndex{{
		Name: "held_name", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"app__Doc", "other__Thing"}, Properties: []string{"other"},
	}}
	got := New().DiffIndexes(desired, actual, ownedSet("app__Doc"))
	wantCounts(t, got, 0, 1, 0, 0, 0)
	if got.Excluded != 1 {
		t.Errorf("Excluded = %d; want the multi-label row counted", got.Excluded)
	}
	if len(got.Drift) == 1 && !strings.Contains(got.Drift[0].Reason, "app__Doc|other__Thing") {
		t.Errorf("Reason = %q; want every label the blocker carries", got.Drift[0].Reason)
	}
}

// Index state applies to fulltext exactly as to range: FAILED is actionable
// drift, POPULATING is unverified.
func TestDiffIndexes_FulltextStateProblems(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "app__Doc_body_fulltext_idx", Kind: IndexFulltext,
		Label: "app__Doc", Properties: []string{"body"},
	}}
	remote := func(state string) []RemoteIndex {
		return []RemoteIndex{{
			Name: "app__Doc_body_fulltext_idx", Type: "FULLTEXT", EntityType: "NODE",
			LabelsOrTypes: []string{"app__Doc"}, Properties: []string{"body"}, State: state,
		}}
	}
	wantCounts(t, New().DiffIndexes(desired, remote("FAILED"), ownedSet("app__Doc")), 0, 1, 0, 0, 0)
	wantCounts(t, New().DiffIndexes(desired, remote("POPULATING"), ownedSet("app__Doc")), 0, 0, 0, 0, 1)
}

// Analyzer configuration is a documented known limit, not compared: the schema
// cannot declare one, so a remote fulltext row with a custom analyzer but
// matching (label, properties) reads as Match — the vector config probes are
// kind-gated and must not fire on a FULLTEXT pair.
func TestDiffIndexes_FulltextAnalyzerNotCompared(t *testing.T) {
	t.Parallel()
	desired := []Index{{
		Name: "app__Doc_body_fulltext_idx", Kind: IndexFulltext,
		Label: "app__Doc", Properties: []string{"body"},
	}}
	actual := []RemoteIndex{{
		Name: "app__Doc_body_fulltext_idx", Type: "FULLTEXT", EntityType: "NODE",
		LabelsOrTypes: []string{"app__Doc"}, Properties: []string{"body"},
		Options: map[string]any{"indexConfig": map[string]any{
			"fulltext.analyzer":              "swedish",
			"fulltext.eventually_consistent": false,
		}},
	}}
	wantCounts(t, New().DiffIndexes(desired, actual, ownedSet("app__Doc")), 1, 0, 0, 0, 0)
}

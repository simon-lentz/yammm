package graph_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// grownSchema has a root whose invariants read its associations, with a
// property of every stored kind, a composite-keyed target, edge properties and
// a composition, so a grown root's held form carries each shape the judge
// rebuilds.
const grownSchema = `schema "grown"

type Vendor {
	id UUID primary
}

type Site {
	code String primary
	day Date primary
}

type Account {
	id String primary
	opened Date
	seen Timestamp
	rate Float
	count Integer
	tags List<String>
	on Boolean
	vec Vector[2]
	tier Enum["gold", "silver"]
	--> VENDORS (many) Vendor {
		since Date
		weight Float
	}
	--> SITES (many) Site
	--> OWNER (_:one) Vendor
	--> COOWNER (_:one) Vendor
	*-> LINES (many) Line
	! "few_vendors" VENDORS -> Len <= 3
	! "one_holder" OWNER == nil || COOWNER == nil
}

part type Line {
	seq String primary
	at Timestamp
	n Integer
}
`

const (
	vendor1 = "9b2e1c70-6f0a-4f11-8a2d-0d6b4c1f7e01"
	vendor2 = "9b2e1c70-6f0a-4f11-8a2d-0d6b4c1f7e02"
	vendor3 = "9b2e1c70-6f0a-4f11-8a2d-0d6b4c1f7e03"
	vendor4 = "9b2e1c70-6f0a-4f11-8a2d-0d6b4c1f7e04"
)

func loadGrown(t *testing.T) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), grownSchema, "grown.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	return s
}

// account returns the Account a1 with every property set and the given
// association fields.
func account(t *testing.T, s schema.Schema, fields map[string]any) instance.ValidInstance {
	t.Helper()
	props := map[string]any{
		"id": "a1", "opened": "2026-01-02", "seen": "2026-01-02T03:04:05Z", "rate": 1.5,
		"count": 7, "tags": []any{"x", "y"}, "on": true, "vec": []any{1.0, 2.0}, "tier": "gold",
		"lines": []any{
			map[string]any{"seq": "l1", "at": "2026-03-04T05:06:07Z", "n": 1},
			map[string]any{"seq": "l2", "at": "2026-03-05T05:06:07Z", "n": 2},
		},
	}
	maps.Copy(props, fields)
	return instancetest.Valid(t, s, "Account", props)
}

// listing returns a1 as a source lists it under another parent: its key and
// the given association fields, with no composed children, which a merge
// refuses.
func listing(t *testing.T, s schema.Schema, fields map[string]any) instance.ValidInstance {
	t.Helper()
	props := map[string]any{"id": "a1"}
	maps.Copy(props, fields)
	return instancetest.Valid(t, s, "Account", props)
}

// vendors returns a VENDORS listing naming each id, with edge properties.
func vendors(ids ...string) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"_target_id": id, "since": "2026-02-03", "weight": 0.25})
	}
	return out
}

// grownGraph adds the vendors and sites the fixtures name, then a1 with
// first's associations, then merges each listing in more.
func grownGraph(t *testing.T, s schema.Schema, first map[string]any, more ...map[string]any) *graph.Graph {
	t.Helper()
	g := graph.New(s)
	for _, id := range []string{vendor1, vendor2, vendor3} {
		mustAdd(t, g, instancetest.Valid(t, s, "Vendor", map[string]any{"id": id}))
	}
	mustAdd(t, g, instancetest.Valid(t, s, "Site", map[string]any{"code": "s", "day": "2026-01-01"}))
	mustAdd(t, g, account(t, s, first))
	for _, fields := range more {
		merged, err := g.AddOrMerge(t.Context(), listing(t, s, fields))
		if err != nil || !merged {
			t.Fatalf("AddOrMerge = (%t, %v), want a merge", merged, err)
		}
	}
	return g
}

// invariantFailures returns the invariant each E_INVARIANT_FAIL in err names.
func invariantFailures(t *testing.T, err error) []string {
	t.Helper()
	var names []string
	for issue := range diagtest.Failures(t, err).Issues() {
		if issue.Code() != diag.E_INVARIANT_FAIL {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyInvariantName {
				names = append(names, d.Value)
			}
		}
	}
	slices.Sort(names)
	return names
}

func TestCheck_JudgesARootAMergeGrewPastAnUpperBound(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	g := grownGraph(t, s,
		map[string]any{"vendors": vendors(vendor1, vendor2)},
		map[string]any{"vendors": vendors(vendor3, vendor4)},
	)
	err := g.Check(t.Context())
	if got := invariantFailures(t, err); !slices.Equal(got, []string{"few_vendors"}) {
		t.Fatalf("Check's invariant failures = %v, want [few_vendors]; err: %v", got, err)
	}
	for issue := range diagtest.Failures(t, err).Issues() {
		if issue.Code() != diag.E_INVARIANT_FAIL {
			continue
		}
		details := map[string]string{}
		for _, d := range issue.Details() {
			if _, seen := details[d.Key]; seen {
				t.Errorf("detail %q appears twice", d.Key)
			}
			details[d.Key] = d.Value
		}
		if details[diag.DetailKeyTypeName] != "Account" || details[diag.DetailKeyPrimaryKey] != `["a1"]` {
			t.Errorf("issue details = %v, want type Account and key [\"a1\"]", details)
		}
		if want := "few_vendors"; issue.Message() != want {
			t.Errorf("message = %q, want %q", issue.Message(), want)
		}
	}
	// Check accumulates nothing, so a second call returns the same verdict.
	if got := invariantFailures(t, g.Check(t.Context())); !slices.Equal(got, []string{"few_vendors"}) {
		t.Errorf("a second Check's invariant failures = %v, want [few_vendors]", got)
	}
}

func TestCheck_JudgesTwoOneAssociationsMergesFilled(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	g := grownGraph(t, s,
		map[string]any{"owner": map[string]any{"_target_id": vendor1}},
		map[string]any{"coowner": map[string]any{"_target_id": vendor2}},
	)
	if got := invariantFailures(t, g.Check(t.Context())); !slices.Equal(got, []string{"one_holder"}) {
		t.Fatalf("Check's invariant failures = %v, want [one_holder]", got)
	}
}

// Every shape a held form rebuilds, a forward reference included, revalidates
// clean when the grown root keeps its invariants.
func TestCheck_ALawfulMergeIsClean(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	g := grownGraph(t, s,
		map[string]any{
			"vendors": vendors(vendor1),
			"sites":   []any{map[string]any{"_target_code": "s", "_target_day": "2026-01-01"}},
			"owner":   map[string]any{"_target_id": vendor1},
		},
		map[string]any{
			"vendors": vendors(vendor2, vendor4),
			"sites":   []any{map[string]any{"_target_code": "t", "_target_day": "2026-01-02"}},
		},
	)
	if err := g.Check(t.Context()); err != nil {
		t.Fatalf("Check over a lawful merge: %v", err)
	}
}

// The judge and the reader's revalidation rebuild one held form, so a graph
// and the document it writes draw one verdict.
func TestCheck_AndTheReadersRevalidationDrawOneVerdict(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	cases := []struct {
		name string
		more map[string]any
		want []string
	}{
		{"lawful", map[string]any{"vendors": vendors(vendor3)}, nil},
		{"past the bound", map[string]any{"vendors": vendors(vendor3, vendor4)}, []string{"few_vendors"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := grownGraph(t, s, map[string]any{"vendors": vendors(vendor1, vendor2)}, tc.more)
			checked := g.Check(t.Context())
			if got := invariantFailures(t, checked); !slices.Equal(got, tc.want) {
				t.Fatalf("Check's invariant failures = %v, want %v", got, tc.want)
			}
			data, err := snapshot.Marshal(t.Context(), g.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			_, read := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
			if got := invariantFailures(t, read); !slices.Equal(got, tc.want) {
				t.Errorf("the reader's invariant failures = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheck_JudgesARootGrownOnASeededGraph(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	seed := grownGraph(t, s, map[string]any{"vendors": vendors(vendor1, vendor2, vendor3)}).Snapshot()
	g := graph.NewFromSnapshot(seed)
	if err := g.Check(t.Context()); err != nil {
		t.Fatalf("Check before the merge: %v", err)
	}
	mustMerge(t, g, listing(t, s, map[string]any{"vendors": vendors(vendor4)}))
	if got := invariantFailures(t, g.Check(t.Context())); !slices.Equal(got, []string{"few_vendors"}) {
		t.Fatalf("Check's invariant failures = %v, want [few_vendors]", got)
	}
}

// A seeded root stands on the seed's claim, one an earlier graph's merge grew
// included: Check judges only what a merge in its own graph grew, and the
// reader's revalidation is what judges the document.
func TestCheck_LeavesASeededRootToTheSeedsClaim(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	grown := grownGraph(t, s,
		map[string]any{"vendors": vendors(vendor1, vendor2)},
		map[string]any{"vendors": vendors(vendor3, vendor4)},
	)
	data, err := snapshot.Marshal(t.Context(), grown.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.NewFromSnapshot(grown.Snapshot()).Check(t.Context()); err != nil {
		t.Errorf("Check over a graph seeded in memory: %v", err)
	}
	read, err := snapshot.Unmarshal(t.Context(), data, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.NewFromSnapshot(read).Check(t.Context()); err != nil {
		t.Errorf("Check over a graph seeded from the document: %v", err)
	}
	_, revalidated := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	if got := invariantFailures(t, revalidated); !slices.Equal(got, []string{"few_vendors"}) {
		t.Errorf("the reader's invariant failures = %v, want [few_vendors]", got)
	}
}

// A merge that names only targets the root already holds grows nothing.
func TestCheck_AMergeThatAddsNothingLeavesTheRootUnjudged(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	g := grownGraph(t, s,
		map[string]any{"vendors": vendors(vendor1, vendor2, vendor3)},
		map[string]any{"vendors": vendors(vendor1)},
	)
	if err := g.Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// A merge cannot carry a listing past a lower bound: the validator judges each
// listing alone and refuses one under the bound before any graph sees it.
func TestMerge_ALowerBoundRefusesAOneTargetListing(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), `schema "lower"

type Vendor {
	id String primary
}

type Account {
	id String primary
	--> VENDORS (many) Vendor
	! "two_vendors" VENDORS -> Len >= 2
}
`, "lower.yammm")
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.NewValidator(s).ValidateOne(t.Context(), "Account", instance.RawInstance{
		Properties: map[string]any{"id": "a1", "vendors": []any{map[string]any{"_target_id": "v1"}}},
	})
	if got := invariantFailures(t, err); !slices.Equal(got, []string{"two_vendors"}) {
		t.Fatalf("a one-target listing's invariant failures = %v, want [two_vendors]", got)
	}
}

func TestCheck_IssueCarriesTheHeldRootsSpan(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	span := location.Span{
		Start: location.Position{Line: 4, Column: 2},
		End:   location.Position{Line: 4, Column: 8},
	}
	g := graph.New(s)
	for _, id := range []string{vendor1, vendor2} {
		mustAdd(t, g, instancetest.Valid(t, s, "Vendor", map[string]any{"id": id}))
	}
	mustAdd(t, g, instancetest.ValidRaw(t, s, "Account", instance.RawInstance{
		Properties: map[string]any{"id": "a1", "owner": map[string]any{"_target_id": vendor1}},
		Provenance: instance.NewProvenance("accounts.json", path.Root.Index(0), span),
	}))
	mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "coowner": map[string]any{"_target_id": vendor2}}))
	err := g.Check(t.Context())
	for issue := range diagtest.Failures(t, err).Issues() {
		if issue.Code() == diag.E_INVARIANT_FAIL && issue.Span() == span &&
			issue.SourceName() == "accounts.json" && issue.Path() == path.Root.Index(0).String() {
			return
		}
	}
	t.Fatalf("no E_INVARIANT_FAIL at the held root's source, path and span in %v", err)
}

// A cancellation at any check the judgement makes ends Check with the
// cancellation and no verdict on the root it was judging.
func TestCheck_ACancelledJudgementReportsTheCancellation(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	g := grownGraph(t, s,
		map[string]any{"vendors": vendors(vendor1, vendor2)},
		map[string]any{"vendors": vendors(vendor3, vendor4)},
	)
	checks := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { _ = g.Check(ctx) })
	if checks < 2 {
		t.Fatalf("Check made %d context checks; the judgement made none", checks)
	}
	for at := int64(1); at <= checks; at++ {
		err := g.Check(yammmtest.NewCheckContext(t.Context(), at))
		if !errors.Is(err, diag.E_CONTEXT_CANCELLED) {
			t.Fatalf("cancelled at check %d: Check = %v, want E_CONTEXT_CANCELLED", at, err)
		}
		if got := invariantFailures(t, err); len(got) != 0 {
			t.Errorf("cancelled at check %d: Check reports invariant failures %v", at, got)
		}
	}
}

const grownEntry = `schema "entry"

import "base.yammm" as base

type Holder {
	id String primary
}
`

const grownBase = `schema "base"

import "deep.yammm" as deep

type Link {
	id String primary
}
`

const grownDeep = `schema "deep"

type Peer {
	id String primary
}

type Beacon {
	id String primary
	--> PEERS (many) Peer
	! "one_peer" PEERS -> Len <= 1
}
`

// A root the entry schema cannot name is judged under its declared name by a
// validator over its declaring schema, and its issues carry the graph's name.
func TestCheck_JudgesARootTheEntrySchemaCannotName(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(grownEntry),
		"base.yammm":  []byte(grownBase),
		"deep.yammm":  []byte(grownDeep),
	}, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	base, _ := s.ImportByAlias("base")
	deepImport, _ := base.Schema().ImportByAlias("deep")
	deep := deepImport.Schema()
	beacon, _ := deep.Type("Beacon")
	if _, addressable := schema.AddressableTag(s, beacon.ID()); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Beacon")
	}
	g := graph.New(s)
	mustAdd(t, g,
		instancetest.Valid(t, deep, "Peer", map[string]any{"id": "p1"}),
		instancetest.Valid(t, deep, "Peer", map[string]any{"id": "p2"}),
		instancetest.Valid(t, deep, "Beacon", map[string]any{"id": "b1", "peers": []any{map[string]any{"_target_id": "p1"}}}),
	)
	mustMerge(t, g, instancetest.Valid(t, deep, "Beacon", map[string]any{"id": "b1", "peers": []any{map[string]any{"_target_id": "p2"}}}))
	err = g.Check(t.Context())
	if got := invariantFailures(t, err); !slices.Equal(got, []string{"one_peer"}) {
		t.Fatalf("Check's invariant failures = %v, want [one_peer]; err: %v", got, err)
	}
	for issue := range diagtest.Failures(t, err).Issues() {
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyTypeName && d.Value != "deep#Beacon" {
				t.Errorf("type-name detail = %q, want deep#Beacon", d.Value)
			}
		}
	}
}

func loadSchema(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), src, "fixture.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	return s
}

// stringKeyed returns a VENDORS or SITES listing naming each String key.
func stringKeyed(ids ...string) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"_target_id": id})
	}
	return out
}

// judgedTwice returns Check's invariant failures and the reader's, over the
// document g writes.
func judgedTwice(t *testing.T, g *graph.Graph, s schema.Schema) (checked, read []string) {
	t.Helper()
	checked = invariantFailures(t, g.Check(t.Context()))
	data, err := snapshot.Marshal(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	_, rerr := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	return checked, invariantFailures(t, rerr)
}

const recordSetSchema = `schema "records"

type Vendor {
	id String primary
}

type Account {
	id String primary
	--> VENDORS (many) Vendor
	--> SITES (many) Vendor
	! "listed" VENDORS != nil
	! "few_sites" SITES -> Len <= 2
	! "v1_first" VENDORS -> Len < 2 || VENDORS[0] == "v1"
	! "v2_first" VENDORS -> Len < 2 || VENDORS[0] == "v2" || SITES -> Len == 0
}
`

// A merged root is judged as the records it holds, never as its listings were
// written: a many association written empty reads as the empty list it holds,
// and its targets read in key order whether or not each target is held.
func TestCheck_JudgesTheRecordsARootHolds(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, recordSetSchema)
	t.Run("a listing written empty", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": []any{}, "sites": stringKeyed("s1")}))
		mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": []any{}, "sites": stringKeyed("s2")}))
		if checked, read := judgedTwice(t, g, s); len(checked) != 0 || len(read) != 0 {
			t.Errorf("Check's failures = %v, the reader's = %v, want none", checked, read)
		}
	})
	t.Run("targets in key order", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		mustAdd(t, g, instancetest.Valid(t, s, "Vendor", map[string]any{"id": "v2"}))
		mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v2"), "sites": stringKeyed("s1")}))
		mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v1")}))
		want := []string{"v2_first"}
		if checked, read := judgedTwice(t, g, s); !slices.Equal(checked, want) || !slices.Equal(read, want) {
			t.Errorf("with v1 missing: Check's failures = %v, the reader's = %v, want %v", checked, read, want)
		}
		mustAdd(t, g, instancetest.Valid(t, s, "Vendor", map[string]any{"id": "v1"}))
		if checked, read := judgedTwice(t, g, s); !slices.Equal(checked, want) || !slices.Equal(read, want) {
			t.Errorf("with v1 held: Check's failures = %v, the reader's = %v, want %v", checked, read, want)
		}
	})
}

// A merge that adds nothing to a root already past its bound leaves it
// unjudged: only a merge that grows a root puts it before Check.
func TestCheck_AMergeThatAddsNothingToARootPastItsBoundLeavesItUnjudged(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	grown := grownGraph(t, s,
		map[string]any{"vendors": vendors(vendor1, vendor2)},
		map[string]any{"vendors": vendors(vendor3, vendor4)},
	)
	g := graph.NewFromSnapshot(grown.Snapshot())
	mustMerge(t, g, listing(t, s, map[string]any{"vendors": vendors(vendor1)}))
	if err := g.Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

const inheritedSchema = `schema "inherited"

type Vendor {
	id String primary
}

abstract type Base {
	--> VENDORS (many) Vendor
	! "one_vendor" VENDORS -> Len <= 1
}

type Account extends Base {
	id String primary
}
`

func TestCheck_JudgesAnInheritedInvariant(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, inheritedSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v1")}))
	mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v2")}))
	if got := invariantFailures(t, g.Check(t.Context())); !slices.Equal(got, []string{"one_vendor"}) {
		t.Fatalf("Check's invariant failures = %v, want [one_vendor]", got)
	}
}

const composedSchema = `schema "composed"

type Vendor {
	id String primary
}

type Account {
	id String primary
	--> VENDORS (many) Vendor
	*-> LINES (many) Line
	! "lined_if_shared" VENDORS -> Len <= 1 || LINES -> Len >= 1
}

part type Line {
	seq String primary
	*-> SUBS (many) Sub
	! "has_sub" SUBS -> Len >= 1
}

part type Sub {
	k String primary
}
`

// A grown root's held form carries its composed children, each with its own
// compositions, so an invariant over them reads what the root holds.
func TestCheck_ReadsAGrownRootsComposedChildren(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, composedSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{
		"id": "a1", "vendors": stringKeyed("v1"),
		"lines": []any{map[string]any{"seq": "l1", "subs": []any{map[string]any{"k": "s1"}}}},
	}))
	mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v2")}))
	if checked, read := judgedTwice(t, g, s); len(checked) != 0 || len(read) != 0 {
		t.Fatalf("Check's failures = %v, the reader's = %v, want none", checked, read)
	}
}

// failingRoots returns a graph holding three Accounts, a, m and z, each grown
// past few_vendors.
func failingRoots(t *testing.T, s schema.Schema) *graph.Graph {
	t.Helper()
	g := graph.New(s)
	for _, id := range []string{"z", "a", "m"} {
		mustAdd(t, g, account(t, s, map[string]any{"id": id, "vendors": vendors(vendor1, vendor2)}))
		mustMerge(t, g, listing(t, s, map[string]any{"id": id, "vendors": vendors(vendor3, vendor4)}))
	}
	return g
}

// failedKeys returns the key of each root an E_INVARIANT_FAIL in err names, in
// the order the issues stand.
func failedKeys(t *testing.T, err error) []string {
	t.Helper()
	var keys []string
	for issue := range diagtest.Failures(t, err).Issues() {
		if issue.Code() != diag.E_INVARIANT_FAIL {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPrimaryKey {
				keys = append(keys, d.Value)
			}
		}
	}
	slices.Sort(keys)
	return keys
}

func TestCheck_JudgesEveryGrownRoot(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	if got, want := failedKeys(t, failingRoots(t, s).Check(t.Context())), []string{`["a"]`, `["m"]`, `["z"]`}; !slices.Equal(got, want) {
		t.Fatalf("Check's failed roots = %v, want %v", got, want)
	}
}

// Check judges the grown roots in (type, key) order, so a cancellation keeps
// the verdicts of a prefix of that order, and only those.
func TestCheck_ACancellationKeepsTheVerdictsOfTheRootsJudgedBeforeIt(t *testing.T) {
	t.Parallel()
	s := loadGrown(t)
	g := failingRoots(t, s)
	order := []string{`["a"]`, `["m"]`, `["z"]`}
	checks := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { _ = g.Check(ctx) })
	seen := map[int]bool{}
	for at := int64(1); at <= checks; at++ {
		err := g.Check(yammmtest.NewCheckContext(t.Context(), at))
		if !errors.Is(err, diag.E_CONTEXT_CANCELLED) {
			t.Fatalf("cancelled at check %d: Check = %v, want E_CONTEXT_CANCELLED", at, err)
		}
		got := failedKeys(t, err)
		if !slices.Equal(got, order[:len(got)]) {
			t.Errorf("cancelled at check %d: kept verdicts on %v, want a prefix of %v", at, got, order)
		}
		seen[len(got)] = true
	}
	for n := range order {
		if !seen[n] {
			t.Errorf("no cancellation point kept exactly %d verdicts", n)
		}
	}
}

const requiredSchema = `schema "required"

type Vendor {
	id String primary
}

type Ticket {
	id String primary
	--> FOR (one) Vendor
}

type Account {
	id String primary
	--> VENDORS (many) Vendor
	! "one_vendor" VENDORS -> Len <= 1
}
`

// A cancelled judgement ends Check: it reports neither a verdict on the roots
// left unjudged nor the completeness check that follows.
func TestCheck_ACancelledJudgementEndsCheck(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, requiredSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Ticket", map[string]any{"id": "t1", "for": map[string]any{"_target_id": "gone"}}))
	mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v1")}))
	mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v2")}))
	if err := g.Check(t.Context()); !errors.Is(err, diag.E_UNRESOLVED_REQUIRED) || !errors.Is(err, diag.E_INVARIANT_FAIL) {
		t.Fatalf("Check = %v, want E_UNRESOLVED_REQUIRED and E_INVARIANT_FAIL", err)
	}
	checks := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { _ = g.Check(ctx) })
	for at := int64(1); at <= checks; at++ {
		err := g.Check(yammmtest.NewCheckContext(t.Context(), at))
		if errors.Is(err, diag.E_UNRESOLVED_REQUIRED) {
			t.Errorf("cancelled at check %d: Check ran the completeness check: %v", at, err)
		}
	}
}

const constrainedSchema = `schema "constrained"

type Vendor {
	id UUID primary
}

type Plain {
	id String primary
	s String[1, 5]
	--> VENDORS (many) Vendor
}

type Bounded {
	id String primary
	--> VENDORS (many) Vendor
	! "few_vendors" VENDORS -> Len <= 2
}
`

// seededFrom returns a graph seeded from the document g writes, with each old
// replaced by new and no integrity check, so the seed holds what no validator
// judged.
func seededFrom(t *testing.T, g *graph.Graph, s schema.Schema, old, new string) *graph.Graph {
	t.Helper()
	data, err := snapshot.Marshal(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	edited := []byte(strings.ReplaceAll(string(data), old, new))
	snap, err := snapshot.Unmarshal(t.Context(), edited, s, snapshot.WithIntegrityCheck(false))
	if err != nil {
		t.Fatal(err)
	}
	return graph.NewFromSnapshot(snap)
}

// Check judges only a root whose type declares an invariant: a grown root of
// any other type stands on the claim it entered under.
func TestCheck_JudgesNoRootOfATypeWithoutAnInvariant(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, constrainedSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Plain", map[string]any{"id": "p1", "s": "abc", "vendors": stringKeyed(vendor1)}))
	seeded := seededFrom(t, g, s, `"abc"`, `"abcdefghij"`)
	mustMerge(t, seeded, instancetest.Valid(t, s, "Plain", map[string]any{"id": "p1", "vendors": stringKeyed(vendor2)}))
	if err := seeded.Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// A target_missing record's key is judged as held, as the reader judges it, so
// a seed's unjudged key draws one verdict from Check and the reader.
func TestCheck_JudgesATargetMissingKeyAsHeld(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, constrainedSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Bounded", map[string]any{"id": "b1", "vendors": stringKeyed(vendor1)}))
	seeded := seededFrom(t, g, s, vendor1, "not-a-uuid")
	mustMerge(t, seeded, instancetest.Valid(t, s, "Bounded", map[string]any{"id": "b1", "vendors": stringKeyed(vendor2)}))
	checked := seeded.Check(t.Context())
	data, err := snapshot.Marshal(t.Context(), seeded.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	_, read := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	for _, err := range []error{checked, read} {
		if !errors.Is(err, diag.E_CONSTRAINT_FAIL) {
			t.Errorf("want E_CONSTRAINT_FAIL on the unjudged key, got %v", err)
		}
	}
}

// A held target's key is judged as the reader judges an edge's: taken as
// judged, since a root stands at it, so a seed's unjudged key at a held target
// draws no failure on the source from Check or the reader.
func TestCheck_TakesAHeldTargetsKeyAsJudged(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, constrainedSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Vendor", map[string]any{"id": vendor1}))
	mustAdd(t, g, instancetest.Valid(t, s, "Bounded", map[string]any{"id": "b1", "vendors": stringKeyed(vendor1)}))
	seeded := seededFrom(t, g, s, vendor1, "not-a-uuid")
	mustMerge(t, seeded, instancetest.Valid(t, s, "Bounded", map[string]any{"id": "b1", "vendors": stringKeyed(vendor2)}))
	if err := seeded.Check(t.Context()); err != nil {
		t.Errorf("Check: %v", err)
	}
	data, err := snapshot.Marshal(t.Context(), seeded.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	_, read := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	for issue := range diagtest.Failures(t, read).Issues() {
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyTypeName && d.Value == "Bounded" {
				t.Errorf("the reader judged the source's held key: %v", issue)
			}
		}
	}
}

const oneComposedSchema = `schema "one_composed"

type Vendor {
	id String primary
}

type Account {
	id String primary
	--> VENDORS (many) Vendor
	*-> NOTE (_:one) Note
	! "no_note_or_one_vendor" NOTE == nil || VENDORS -> Len <= 1
}

part type Note {
	text String primary
}
`

// A grown root with no child under a single composition reads that
// composition as nil, as the record that wrote it did.
func TestCheck_ReadsAnEmptySingleCompositionAsNil(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, oneComposedSchema)
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v1")}))
	mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v2")}))
	if checked, read := judgedTwice(t, g, s); len(checked) != 0 || len(read) != 0 {
		t.Fatalf("Check's failures = %v, the reader's = %v, want none", checked, read)
	}
}

// A grown root's failures are not capped: Check stores every one, as the
// reader's revalidation does under a read that caps none.
func TestCheck_StoresEveryFailureOfAGrownRoot(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("schema \"many_invariants\"\n\ntype Vendor {\n\tid String primary\n}\n\ntype Account {\n\tid String primary\n\t--> VENDORS (many) Vendor\n")
	const n = 101
	for i := range n {
		fmt.Fprintf(&b, "\t! \"bound_%03d\" VENDORS -> Len <= 1\n", i)
	}
	b.WriteString("}\n")
	s := loadSchema(t, b.String())
	g := graph.New(s)
	mustAdd(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v1")}))
	mustMerge(t, g, instancetest.Valid(t, s, "Account", map[string]any{"id": "a1", "vendors": stringKeyed("v2")}))
	checked := invariantFailures(t, g.Check(t.Context()))
	data, err := snapshot.Marshal(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	_, rerr := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Error), snapshot.WithIssueLimit(diag.NoLimit))
	read := invariantFailures(t, rerr)
	if len(checked) != n || len(read) != n {
		t.Fatalf("Check stored %d failures and the reader %d, want %d each", len(checked), len(read), n)
	}
}

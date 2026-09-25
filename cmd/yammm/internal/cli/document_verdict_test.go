package cli

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// person is one generated Person as a plain model: what the document states,
// read without the validator.
type person struct {
	id      any // a string, or an int no String key reads
	named   bool
	manager any // a string target, an int no key reads, or nil for an absent field
}

func (p person) raw() instance.RawInstance {
	props := map[string]any{"id": p.id}
	if p.named {
		props["name"] = "n"
	}
	if p.manager != nil {
		props["manager"] = map[string]any{"_target_id": p.manager}
	}
	return instance.RawInstance{Properties: props}
}

func (p person) key() (string, bool) {
	s, ok := p.id.(string)
	return s, ok
}

func (p person) valid() bool {
	_, keyed := p.key()
	_, targeted := p.manager.(string)
	return keyed && p.named && (p.manager == nil || targeted)
}

func randomPeople(r *rand.Rand, n int) []person {
	ids := []any{"a", "b", "c", "d", 7}
	targets := []any{"a", "b", "c", "d", "x", 7, nil}
	people := make([]person, n)
	for i := range people {
		people[i] = person{id: ids[r.IntN(len(ids))], named: r.IntN(5) < 3, manager: targets[r.IntN(len(targets))]}
	}
	return people
}

// expectedVerdict is the model: a key is held when any earlier instance of the
// merged file or the document states it, and a target is missing when nothing
// the merged file or the document states holds its key.
type expectedVerdict struct {
	duplicates int
	missing    []string // source keys of a target_missing E_UNRESOLVED_REQUIRED
	absent     []string // source keys of an absent E_UNRESOLVED_REQUIRED
}

func model(base, doc []person) expectedVerdict {
	installed := map[string]bool{}
	var sources []person
	for _, p := range base {
		if k, _ := p.key(); p.valid() && !installed[k] {
			installed[k] = true
			sources = append(sources, p)
		}
	}
	var want expectedVerdict
	held := maps.Clone(installed)
	for _, p := range doc {
		k, ok := p.key()
		if !ok {
			continue
		}
		if held[k] {
			want.duplicates++
		}
		held[k] = true
		if p.valid() && !installed[k] {
			installed[k] = true
			sources = append(sources, p)
		}
	}
	for _, p := range sources {
		k, _ := p.key()
		switch target := p.manager.(type) {
		case nil:
			want.absent = append(want.absent, k)
		case string:
			if !held[target] {
				want.missing = append(want.missing, k)
			}
		}
	}
	slices.Sort(want.missing)
	slices.Sort(want.absent)
	return want
}

func raws(people []person) map[string][]instance.RawInstance {
	out := map[string][]instance.RawInstance{}
	for _, p := range people {
		out["Person"] = append(out["Person"], p.raw())
	}
	return out
}

func observed(result diag.Result) expectedVerdict {
	var got expectedVerdict
	for issue := range result.Issues() {
		details := map[string]string{}
		for _, d := range issue.Details() {
			details[d.Key] = d.Value
		}
		switch issue.Code() {
		case diag.E_DUPLICATE_PK:
			got.duplicates++
		case diag.E_UNRESOLVED_REQUIRED:
			parts, err := graph.ParseKeyStrings(details[diag.DetailKeyPrimaryKey])
			if err != nil || len(parts) != 1 {
				panic(fmt.Sprintf("unreadable source key %q", details[diag.DetailKeyPrimaryKey]))
			}
			if details[diag.DetailKeyReason] == "absent" {
				got.absent = append(got.absent, parts[0])
			} else {
				got.missing = append(got.missing, parts[0])
			}
		}
	}
	slices.Sort(got.missing)
	slices.Sort(got.absent)
	return got
}

// TestBuildGraph_AgreesWithAModelOfTheDocument is the second implementation of
// the verdict: over generated documents, alone and merged into a graph built
// from other data, the duplicates and missing targets reported equal what a
// plain reading of the data says the document and the merged file hold.
func TestBuildGraph_AgreesWithAModelOfTheDocument(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), `schema "p12"

type Person {
    id String primary
    name String required
    --> MANAGER (one) Person
}
`, "p12.yammm")
	if res.HasErrors() {
		t.Fatal(res.String())
	}
	r := rand.New(rand.NewPCG(12, 0)) //nolint:gosec // a fixed seed makes the documents reproducible
	for i := range 600 {
		people := randomPeople(r, 1+r.IntN(8))
		split := 0
		if i%2 == 1 {
			split = r.IntN(len(people) + 1)
		}
		base, doc := people[:split], people[split:]

		var g *graph.Graph
		if split > 0 {
			validated, _ := ValidateInstances(t.Context(), s, raws(base))
			g, _ = BuildGraph(t.Context(), s, nil, validated)
		}
		validated, validateResult := ValidateInstances(t.Context(), s, raws(doc))
		_, graphResult := BuildGraph(t.Context(), s, g, validated)
		got := observed(MergeResults(validateResult, graphResult))
		want := model(base, doc)
		if got.duplicates != want.duplicates || !slices.Equal(got.missing, want.missing) || !slices.Equal(got.absent, want.absent) {
			t.Fatalf("base %+v, document %+v:\n got %+v\nwant %+v", base, doc, got, want)
		}
	}
}

const teamSchema = `schema "t"

type Team {
    id String primary
    name String required
}

type Person {
    id String primary
    name String required
    --> TEAM (one) Team
}
`

func teamRaws(team, people []map[string]any) map[string][]instance.RawInstance {
	out := map[string][]instance.RawInstance{}
	for _, p := range team {
		out["Team"] = append(out["Team"], instance.RawInstance{Properties: p})
	}
	for _, p := range people {
		out["Person"] = append(out["Person"], instance.RawInstance{Properties: p})
	}
	return out
}

func codeCount(result diag.Result, code diag.Code) int {
	n := 0
	for issue := range result.Issues() {
		if issue.Code() == code {
			n++
		}
	}
	return n
}

// TestBuildGraph_TypesAMissingTargetByItsRelation pins that a refused root
// explains a missing target only when it is of the relation's target type: a
// refused Team holding the key explains it, a refused Person holding it does not.
func TestBuildGraph_TypesAMissingTargetByItsRelation(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), teamSchema, "t.yammm")
	if res.HasErrors() {
		t.Fatal(res.String())
	}
	member := map[string]any{"id": "p", "name": "P", "team": map[string]any{"_target_id": "t"}}
	for _, tc := range []struct {
		name        string
		raws        map[string][]instance.RawInstance
		wantMissing int
	}{
		{"a refused Team", teamRaws([]map[string]any{{"id": "t"}}, []map[string]any{member}), 0},
		{"a refused Person", teamRaws(nil, []map[string]any{member, {"id": "t", "team": map[string]any{"_target_id": "t"}}}), 1},
	} {
		validated, _ := ValidateInstances(t.Context(), s, tc.raws)
		_, result := BuildGraph(t.Context(), s, nil, validated)
		if got := codeCount(result, diag.E_UNRESOLVED_REQUIRED); got != tc.wantMissing {
			t.Errorf("%s: %d E_UNRESOLVED_REQUIRED, want %d\n%s", tc.name, got, tc.wantMissing, result)
		}
	}
}

// TestBuildGraph_ReportsADuplicateAsTheGraphDoes pins that the duplicate the
// verdict reports is the issue graph.Graph.Add raises: over a refused Team and
// two valid ones of one key, the verdict's issue and the graph's differ in
// nothing but their span.
func TestBuildGraph_ReportsADuplicateAsTheGraphDoes(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), teamSchema, "t.yammm")
	if res.HasErrors() {
		t.Fatal(res.String())
	}
	validated, _ := ValidateInstances(t.Context(), s, teamRaws([]map[string]any{{"id": "t"}, {"id": "t", "name": "A"}, {"id": "t", "name": "B"}}, nil))
	_, result := BuildGraph(t.Context(), s, nil, validated)
	var issues []diag.Issue
	for issue := range result.Issues() {
		if issue.Code() == diag.E_DUPLICATE_PK {
			issues = append(issues, issue)
		}
	}
	if len(issues) != 2 {
		t.Fatalf("%d E_DUPLICATE_PK, want 2: the verdict's and the graph's\n%s", len(issues), result)
	}
	a, b := issues[0], issues[1]
	if a.Severity() != diag.Error || a.Severity() != b.Severity() || a.Message() != b.Message() || !slices.Equal(a.Details(), b.Details()) {
		t.Errorf("the two duplicates differ:\n%s %q %v\n%s %q %v", a.Severity(), a.Message(), a.Details(), b.Severity(), b.Message(), b.Details())
	}
	if want := `duplicate primary key ["t"] for type "Team"`; a.Message() != want {
		t.Errorf("message = %q, want %q", a.Message(), want)
	}

	// The refused holder is the duplicate when it comes second.
	validated, _ = ValidateInstances(t.Context(), s, teamRaws([]map[string]any{{"id": "t", "name": "A"}, {"id": "t"}}, nil))
	_, result = BuildGraph(t.Context(), s, nil, validated)
	var refused []diag.Issue
	for issue := range result.Issues() {
		if issue.Code() == diag.E_DUPLICATE_PK {
			refused = append(refused, issue)
		}
	}
	if len(refused) != 1 {
		t.Fatalf("%d E_DUPLICATE_PK for a refused second holder, want 1\n%s", len(refused), result)
	}
	if c := refused[0]; c.Severity() != a.Severity() || c.Message() != a.Message() || !slices.Equal(c.Details(), a.Details()) {
		t.Errorf("the refused holder's duplicate differs from the graph's:\n%s %q %v\n%s %q %v", c.Severity(), c.Message(), c.Details(), a.Severity(), a.Message(), a.Details())
	}
}

// TestBuildGraph_JudgesNothingOnACancelledContext pins that a cancelled build
// adds no verdict: a graph that never ran its checks has no duplicate to answer.
func TestBuildGraph_JudgesNothingOnACancelledContext(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), teamSchema, "t.yammm")
	if res.HasErrors() {
		t.Fatal(res.String())
	}
	validated, _ := ValidateInstances(t.Context(), s, teamRaws([]map[string]any{{"id": "t"}, {"id": "t", "name": "A"}}, nil))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, result := BuildGraph(ctx, s, nil, validated)
	if !result.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("a cancelled build reported no cancellation:\n%s", result)
	}
	if got := codeCount(result, diag.E_DUPLICATE_PK); got != 0 {
		t.Errorf("a cancelled build reported %d E_DUPLICATE_PK:\n%s", got, result)
	}
}

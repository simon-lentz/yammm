package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

const (
	rawInstant   = "2020-01-02T03:04:05+00:00"
	canonInstant = "2020-01-02T03:04:05Z"
)

// callerSpellingSchema declares a Timestamp-keyed root that composes a part and
// associates to a Timestamp-keyed peer, so every address the graph receives
// — an edge endpoint, an unresolved target — has a
// canonical spelling distinct from the one a caller may hold.
func callerSpellingSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "caller_spelling"

type Run {
	at Timestamp primary
	--> NEXT (_:many) Run
	*-> STEPS (_:many) Step
}

part type Step {
	n String primary
}

type Tag {
	name String primary
}
`
	s, res := schema.LoadString(t.Context(), src, "caller_spelling.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// runInstance validates a Run over s, its key and its NEXT target spelled as given.
func runInstance(t *testing.T, s schema.Schema, at, next string) instance.ValidInstance {
	t.Helper()
	data := map[string]any{"at": at}
	if next != "" {
		data["next"] = []any{map[string]any{"_target_at": next}}
	}
	return instancetest.Valid(t, s, "Run", data)
}

// TestAdd_AnUnresolvedTargetCarriesTheCanonicalKey pins that a forward
// reference whose target key the caller spelled non-canonically carries the
// text its kind stores, the one spelling a document may hold.
func TestAdd_AnUnresolvedTargetCarriesTheCanonicalKey(t *testing.T) {
	t.Parallel()
	s := callerSpellingSchema(t)
	want := graph.FormatKey(canonInstant)

	g := graph.New(s)
	if r := g.Add(t.Context(), runInstance(t, s, "2021-01-01T00:00:00Z", rawInstant)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	added := slices.Collect(g.Snapshot().Unresolved())
	if len(added) != 1 || added[0].TargetKey() != want {
		t.Errorf("unresolved target = %v, want one record at %s", added, want)
	}
}

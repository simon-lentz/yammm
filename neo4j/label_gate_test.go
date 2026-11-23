package neo4j

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

// [cypher.Config.CheckLabels] gates constraints, indexes and the write shape
// alike. The CLI exposes --separator, and a separator that is no Neo4j
// identifier text makes every label invalid; without the gate the emitters
// produce Cypher the server refuses at apply time, with no yammm diagnostic.
func TestCheckLabels_GatesEveryEmitter(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	// "-" is legal in the CLI flag but not in an unquoted Neo4j identifier.
	a := cypher.Config{LabelSeparator: "-"}

	t.Run("indexes", func(t *testing.T) {
		t.Parallel()
		got, err := a.Indexes(s)
		assertGated(t, got == nil, errors.Is(err, cypher.E_NEO4J_INVALID_IDENTIFIER), err)
	})
	t.Run("constraints", func(t *testing.T) {
		t.Parallel()
		got, err := a.Constraints(s)
		assertGated(t, got == nil, errors.Is(err, cypher.E_NEO4J_INVALID_IDENTIFIER), err)
	})
	t.Run("shape", func(t *testing.T) {
		t.Parallel()
		got, err := shapeFor(a, s)
		assertGated(t, got == nil, errors.Is(err, cypher.E_NEO4J_INVALID_IDENTIFIER), err)
	})
}

func assertGated(t *testing.T, gotNil, hasCode bool, err error) {
	t.Helper()
	if !hasCode {
		t.Errorf("want E_NEO4J_INVALID_IDENTIFIER for a label the separator made illegal, got: %v", err)
	}
	if !gotNil {
		t.Error("an invalid label must suppress emission, not be emitted into Cypher the server rejects")
	}
}

// A diff under a nil *OwnedLabels owns nothing. This is a documented,
// deliberate zero value, pinned so it cannot become incidental.
func TestOwnedLabels_NilOwnsNothing(t *testing.T) {
	t.Parallel()
	var owned *cypher.OwnedLabels
	got := diff(cypher.Config{}, nil, []cypher.Index{{Name: "i", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"x"}}}, nil, []RemoteIndex{{
		Name: "i", Type: "RANGE", EntityType: "NODE",
		LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"},
	}}, owned).Indexes
	// Nothing is owned, so nothing matches and nothing drops. Blocking is
	// collected before ownership, though, so the taken name still reports as
	// drift naming the holder — passing a nil by mistake surfaces the conflict
	// rather than yielding a confident plan of statements that would all no-op.
	wantCounts(t, got, 0, 1, 0, 0, 0)

	// With no remote holding the name there is nothing to block, and it is a Create.
	free := diff(cypher.Config{}, nil, []cypher.Index{{Name: "i", Kind: cypher.IndexRange, Label: "app__T", Properties: []string{"x"}}}, nil, nil, owned).Indexes
	wantCounts(t, free, 0, 0, 1, 0, 0)
}

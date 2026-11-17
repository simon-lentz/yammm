package neo4j

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

// Every remote index row lands in one place: it is the Actual a declaration was
// compared with (a Match or an Unverified), a row some drift names among its
// Blockers, a Drop, or one of the rows Excluded counts.
//
// The oracle is the count: each case states what each bucket holds, and the
// buckets plus the distinct rows the drifts name must add up to the rows.
func TestIndexDiff_EveryRemoteRowLandsInOneBucket(t *testing.T) {
	t.Parallel()

	const label = "app__T"
	backing := func(name, state string, props ...string) RemoteIndex {
		return RemoteIndex{
			Name: name, Type: "RANGE", EntityType: "NODE", State: state,
			LabelsOrTypes: []string{label}, Properties: props, OwningConstraint: "c_" + name,
		}
	}
	undeclarable := func(name, kind, prop string) RemoteIndex {
		return RemoteIndex{Name: name, Type: kind, EntityType: "NODE", LabelsOrTypes: []string{label}, Properties: []string{prop}}
	}
	plain := func(name, onLabel string, props ...string) RemoteIndex {
		return RemoteIndex{Name: name, Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{onLabel}, Properties: props}
	}
	declared := func(name string, props ...string) cypher.Index {
		return cypher.Index{Name: name, Kind: cypher.IndexRange, Label: label, Properties: props}
	}

	for _, c := range []struct {
		name    string
		desired []cypher.Index
		actual  []RemoteIndex
		owned   labelSet
		// match, drift, create, drop, unverified, excluded
		want [6]int
	}{
		{
			name:    "a backing index that serves a declaration and is online",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "ONLINE", "id")},
			want:    [6]int{1, 0, 0, 0, 0, 0},
		},
		{
			name:    "a backing index that serves a declaration and is FAILED",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "FAILED", "id")},
			want:    [6]int{0, 1, 0, 0, 0, 0},
		},
		{
			name:    "a backing index that serves a declaration and is POPULATING",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "POPULATING", "id")},
			want:    [6]int{0, 0, 0, 0, 1, 0},
		},
		{
			name:    "a FAILED backing index that serves, beside one that serves nothing",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "FAILED", "id"), backing("other", "FAILED", "other")},
			want:    [6]int{0, 1, 0, 0, 0, 1},
		},
		{
			name:   "a backing index that serves no declaration",
			actual: []RemoteIndex{backing("pk", "ONLINE", "id")},
			want:   [6]int{0, 0, 0, 0, 0, 1},
		},
		{
			name:    "two backing indexes of one definition: the first serves, the second is not compared",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "ONLINE", "id"), backing("pk2", "ONLINE", "id")},
			want:    [6]int{1, 0, 0, 0, 0, 1},
		},
		{
			name:    "a backing index on a label the set does not hold that serves a declaration on it",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "ONLINE", "id")},
			owned:   ownedSet("app__Other"),
			want:    [6]int{1, 0, 0, 0, 0, 0},
		},
		{
			name:    "an undeclarable row that holds a declaration's name blocks it and is not excluded",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{undeclarable("idx", "TEXT", "a")},
			want:    [6]int{0, 1, 0, 0, 0, 0},
		},
		{
			name:    "a row on another label that holds a declaration's name blocks it and is not excluded",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{plain("idx", "other__Thing", "x")},
			want:    [6]int{0, 1, 0, 0, 0, 0},
		},
		{
			name:    "a row on a label the set does not hold that holds a declaration's definition blocks it and is not excluded",
			desired: []cypher.Index{declared("idx", "id")},
			actual:  []RemoteIndex{plain("held", label, "id")},
			owned:   ownedSet("app__Other"),
			want:    [6]int{0, 1, 0, 0, 0, 0},
		},
		{
			name:    "one of each",
			desired: []cypher.Index{declared("idx", "id"), declared("mine", "x"), declared("new", "y")},
			actual: []RemoteIndex{
				backing("pk", "POPULATING", "id"),
				plain("mine", label, "x"),
				plain("orphan", label, "z"),
				plain("stale", "app__Gone", "id"),
				undeclarable("text", "TEXT", "a"),
				undeclarable("point", "POINT", "b"),
			},
			want: [6]int{1, 0, 1, 1, 1, 3},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			owned := c.owned
			if owned == nil {
				owned = ownedSet(label)
			}
			got := diff(cypher.Config{}, nil, c.desired, nil, c.actual, owned).Indexes
			counts := [6]int{len(got.Match), len(got.Drift), len(got.Create), len(got.Drop), len(got.Unverified), got.Excluded}
			if counts != c.want {
				t.Errorf("match, drift, create, drop, unverified, excluded = %v, want %v", counts, c.want)
			}
			named := make(map[string]struct{})
			for _, d := range got.Drift {
				for _, b := range d.Blockers.Indexes {
					named[indexTarget(b)] = struct{}{}
				}
			}
			if landed := counts[0] + counts[3] + counts[4] + counts[5] + len(named); landed != len(c.actual) {
				t.Errorf("%d remote rows landed %d times: %v with %d rows named by drifts", len(c.actual), landed, counts, len(named))
			}
		})
	}
}

// A definition is served by the first row that holds it in the diff's record
// order, whatever rows stand before that one in the input, and a row realising
// one declaration blocks another declaration of its definition.
func TestIndexDiff_ADefinitionBelongsToItsFirstRowInRecordOrder(t *testing.T) {
	t.Parallel()
	const label = "app__T"
	unrelated := RemoteIndex{Name: "unrelated", Type: "TEXT", EntityType: "NODE", LabelsOrTypes: []string{label}, Properties: []string{"a"}}
	backing := func(name string) RemoteIndex {
		return RemoteIndex{
			Name: name, Type: "RANGE", EntityType: "NODE", State: "ONLINE",
			LabelsOrTypes: []string{label}, Properties: []string{"id"}, OwningConstraint: name,
		}
	}

	served := diff(cypher.Config{}, nil, []cypher.Index{{Name: "idx", Kind: cypher.IndexRange, Label: label, Properties: []string{"id"}}}, nil, []RemoteIndex{unrelated, backing("first"), backing("second")}, ownedSet(label)).Indexes
	if len(served.Match) != 1 || served.Match[0].Actual.Name != "first" || served.Excluded != 2 {
		t.Errorf("match = %+v, excluded = %d; want the first backing index matched and two rows excluded", served.Match, served.Excluded)
	}

	// Two declarations of one definition: pairing gives the row to the one
	// whose name it holds, and the row then blocks the other's CREATE, so the
	// declaration it realises is created again too.
	blocked := diff(cypher.Config{}, nil, []cypher.Index{
		{Name: "mine", Kind: cypher.IndexRange, Label: label, Properties: []string{"x"}},
		{Name: "again", Kind: cypher.IndexRange, Label: label, Properties: []string{"x"}},
	}, nil, []RemoteIndex{unrelated, {Name: "mine", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{label}, Properties: []string{"x"}}}, ownedSet(label)).Indexes
	if len(blocked.Match) != 0 || len(blocked.Drift) != 2 || !slices.Equal(blockerNames(blocked.Drift[1].Blockers), []string{"i:mine"}) || blocked.Excluded != 1 {
		t.Errorf("match = %+v, drift = %+v, excluded = %d; want no match, both declarations blocked by index \"mine\", and one row excluded",
			blocked.Match, blocked.Drift, blocked.Excluded)
	}
}

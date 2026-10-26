package neo4j

import "testing"

// Every remote row DiffIndexes is given lands in one bucket: it is the Actual
// a declaration was compared with (a Match, an Unverified, or a Drift that is
// not a block), a Drop, or one of the rows Excluded counts. A block is the one
// entry outside that rule: its Actual is the row that holds a declaration's
// name or definition against a CREATE, which the comparison never took, so a
// blocker is still counted where it was.
//
// The oracle is the count: each case states its rows, how many of its Drift
// entries are blocks, and what each bucket holds, and the buckets less the
// blocks must add up to the rows.
func TestDiffIndexes_EveryRemoteRowLandsInOneBucket(t *testing.T) {
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
	declared := func(name string, props ...string) Index {
		return Index{Name: name, Kind: IndexRange, Label: label, Properties: props}
	}

	for _, c := range []struct {
		name    string
		desired []Index
		actual  []RemoteIndex
		owned   *OwnedLabels
		// match, drift, create, drop, unverified, excluded
		want   [6]int
		blocks int
	}{
		{
			name:    "a backing index that serves a declaration and is online",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "ONLINE", "id")},
			want:    [6]int{1, 0, 0, 0, 0, 0},
		},
		{
			name:    "a backing index that serves a declaration and is FAILED",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "FAILED", "id")},
			want:    [6]int{0, 1, 0, 0, 0, 0},
		},
		{
			name:    "a backing index that serves a declaration and is POPULATING",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "POPULATING", "id")},
			want:    [6]int{0, 0, 0, 0, 1, 0},
		},
		{
			name:    "a FAILED backing index that serves, beside one that serves nothing",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "FAILED", "id"), backing("other", "FAILED", "other")},
			want:    [6]int{0, 1, 0, 0, 0, 1},
		},
		{
			name:   "a backing index that serves no declaration",
			actual: []RemoteIndex{backing("pk", "ONLINE", "id")},
			want:   [6]int{0, 0, 0, 0, 0, 1},
		},
		{
			name:   "two undeclarable rows with no name",
			actual: []RemoteIndex{undeclarable("", "TEXT", "a"), undeclarable("", "POINT", "b")},
			want:   [6]int{0, 0, 0, 0, 0, 2},
		},
		{
			name:   "two undeclarable rows under one name",
			actual: []RemoteIndex{undeclarable("same", "TEXT", "a"), undeclarable("same", "POINT", "b")},
			want:   [6]int{0, 0, 0, 0, 0, 2},
		},
		{
			name:    "a backing index with no name that serves, beside an undeclarable row with no name",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("", "ONLINE", "id"), undeclarable("", "TEXT", "a")},
			want:    [6]int{1, 0, 0, 0, 0, 1},
		},
		{
			name:    "an undeclarable row with no name, ahead of the backing index with no name that serves",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{undeclarable("", "TEXT", "a"), backing("", "ONLINE", "id")},
			want:    [6]int{1, 0, 0, 0, 0, 1},
		},
		{
			name:    "two backing indexes of one definition: the first serves, the second is not compared",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "ONLINE", "id"), backing("pk2", "ONLINE", "id")},
			want:    [6]int{1, 0, 0, 0, 0, 1},
		},
		{
			name:    "a backing index on a label the set does not hold that serves a declaration on it",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{backing("pk", "ONLINE", "id")},
			owned:   ownedSet("app__Other"),
			want:    [6]int{1, 0, 0, 0, 0, 0},
		},
		{
			name:    "an undeclarable row that holds a declaration's name blocks it and stays counted",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{undeclarable("idx", "TEXT", "a")},
			want:    [6]int{0, 1, 0, 0, 0, 1},
			blocks:  1,
		},
		{
			name:    "a row on another label that holds a declaration's name blocks it and stays counted",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{plain("idx", "other__Thing", "x")},
			want:    [6]int{0, 1, 0, 0, 0, 1},
			blocks:  1,
		},
		{
			name:    "a row on a label the set does not hold that holds a declaration's definition blocks it and stays counted",
			desired: []Index{declared("idx", "id")},
			actual:  []RemoteIndex{plain("held", label, "id")},
			owned:   ownedSet("app__Other"),
			want:    [6]int{0, 1, 0, 0, 0, 1},
			blocks:  1,
		},
		{
			name:    "one of each",
			desired: []Index{declared("idx", "id"), declared("mine", "x"), declared("new", "y")},
			actual: []RemoteIndex{
				backing("pk", "POPULATING", "id"),
				plain("mine", label, "x"),
				plain("orphan", label, "z"),
				plain("stale", "app__Gone", "id"),
				undeclarable("", "TEXT", "a"),
				undeclarable("", "POINT", "b"),
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
			got := New().DiffIndexes(c.desired, c.actual, owned)
			counts := [6]int{len(got.Match), len(got.Drift), len(got.Create), len(got.Drop), len(got.Unverified), got.Excluded}
			if counts != c.want {
				t.Errorf("match, drift, create, drop, unverified, excluded = %v, want %v", counts, c.want)
			}
			if landed := counts[0] + counts[1] - c.blocks + counts[3] + counts[4] + counts[5]; landed != len(c.actual) {
				t.Errorf("%d remote rows landed %d times: %v with %d blocks", len(c.actual), landed, counts, c.blocks)
			}
		})
	}
}

// A definition is served, or blocked, by the first row that holds it, whatever
// rows stand before that one: the comparison finds the row by where it stands.
func TestDiffIndexes_ADefinitionBelongsToItsFirstRowWhereverItStands(t *testing.T) {
	t.Parallel()
	const label = "app__T"
	unrelated := RemoteIndex{Name: "unrelated", Type: "TEXT", EntityType: "NODE", LabelsOrTypes: []string{label}, Properties: []string{"a"}}
	backing := func(name string) RemoteIndex {
		return RemoteIndex{
			Name: name, Type: "RANGE", EntityType: "NODE", State: "ONLINE",
			LabelsOrTypes: []string{label}, Properties: []string{"id"}, OwningConstraint: name,
		}
	}

	served := New().DiffIndexes(
		[]Index{{Name: "idx", Kind: IndexRange, Label: label, Properties: []string{"id"}}},
		[]RemoteIndex{unrelated, backing("first"), backing("second")}, ownedSet(label))
	if len(served.Match) != 1 || served.Match[0].Actual.Name != "first" || served.Excluded != 2 {
		t.Errorf("match = %+v, excluded = %d; want the first backing index matched and two rows excluded", served.Match, served.Excluded)
	}

	// Two declarations of one definition: pairing gives the row to the one
	// whose name it holds, and the row then blocks the other's CREATE.
	blocked := New().DiffIndexes(
		[]Index{
			{Name: "mine", Kind: IndexRange, Label: label, Properties: []string{"x"}},
			{Name: "again", Kind: IndexRange, Label: label, Properties: []string{"x"}},
		},
		[]RemoteIndex{unrelated, {Name: "mine", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{label}, Properties: []string{"x"}}},
		ownedSet(label))
	if len(blocked.Match) != 1 || len(blocked.Drift) != 1 || blocked.Drift[0].Actual.Name != "mine" || blocked.Excluded != 1 {
		t.Errorf("match = %+v, drift = %+v, excluded = %d; want one match, the second declaration blocked by index \"mine\", and one row excluded",
			blocked.Match, blocked.Drift, blocked.Excluded)
	}
}

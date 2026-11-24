package cypher

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/internal/mapping"
)

// allConstraintKinds must contain every kind constraintKindToRemoteType
// recognises, or the neo4j diff silently stops recognising that
// kind's remote constraints. The sweep spans negatives so a kind placed below
// ConstraintUnique is found too.
func TestConstraintKind_EnumerationIsComplete(t *testing.T) {
	t.Parallel()
	const sweep = 64
	for i := -sweep; i < sweep; i++ {
		k := ConstraintKind(i)
		if constraintKindToRemoteType(k) == mapping.UnknownRemoteType {
			continue
		}
		if !slices.Contains(allConstraintKinds, k) {
			t.Errorf("ConstraintKind %d maps to %q but is missing from allConstraintKinds",
				k, constraintKindToRemoteType(k))
		}
	}
	for _, k := range allConstraintKinds {
		if constraintKindToRemoteType(k) == mapping.UnknownRemoteType {
			t.Errorf("allConstraintKinds contains ConstraintKind %d, which has no remote type", k)
		}
	}
}

// mappedIndexKinds sweeps a numeric range far wider than the enum, the zero
// kind and negatives included, and returns every kind indexKindToRemoteType
// maps, wherever a kind is placed.
func mappedIndexKinds(t *testing.T) []IndexKind {
	t.Helper()
	const sweep = 64
	var mapped []IndexKind
	for i := -sweep; i < sweep; i++ {
		if k := IndexKind(i); indexKindToRemoteType(k) != mapping.UnknownRemoteType {
			mapped = append(mapped, k)
		}
	}
	if len(mapped) == 0 {
		t.Fatal("the sweep found no mapped kinds; indexKindToRemoteType or mapping.UnknownRemoteType changed shape")
	}
	return mapped
}

// Every mapped kind must have a distinct remote type string: RemoteIndex.Declarable
// and the identity keys both compare on it, so two kinds sharing one string
// would make a remote index of one match a desired index of the other.
func TestIndexKind_RemoteTypesAreDistinct(t *testing.T) {
	t.Parallel()
	seen := make(map[string]IndexKind)
	for _, k := range mappedIndexKinds(t) {
		rt := indexKindToRemoteType(k)
		if prev, dup := seen[rt]; dup {
			t.Errorf("IndexKind %d and %d both map to %q", prev, k, rt)
		}
		seen[rt] = k
	}
}

// The index kinds' remote types are exactly [mapping.IndexTypes], which
// decides which remote index types the diff owns. A kind whose type the list
// omits is invisible to the diff, neither matched nor dropped; a listed type no
// kind emits makes the diff drop every owned remote index of that type.
func TestIndexKind_RemoteTypesAreDeclarable(t *testing.T) {
	t.Parallel()
	var emitted []string
	for _, k := range mappedIndexKinds(t) {
		emitted = append(emitted, indexKindToRemoteType(k))
	}
	slices.Sort(emitted)
	if declarable := slices.Sorted(slices.Values(mapping.IndexTypes())); !slices.Equal(emitted, declarable) {
		t.Errorf("the index kinds emit %q, and mapping.IndexTypes lists %q", emitted, declarable)
	}
}

// Every constraint kind's remote type must be one the Enterprise diff owns, or
// a constraint the generator emits is never paired with its database row.
//
// The other direction holds too: a listed type no kind emits makes the diff
// drop every owned remote constraint of that type. Community owns UNIQUENESS
// alone, the one kind it emits.
func TestConstraintKind_RemoteTypesAreDeclarable(t *testing.T) {
	t.Parallel()
	var emitted []string
	for _, k := range allConstraintKinds {
		emitted = append(emitted, constraintKindToRemoteType(k))
	}
	slices.Sort(emitted)
	if declarable := slices.Sorted(slices.Values(mapping.DeclarableConstraintTypes(false))); !slices.Equal(emitted, declarable) {
		t.Errorf("the constraint kinds emit %q, and the Enterprise declarable types are %q", emitted, declarable)
	}
	if got, want := mapping.DeclarableConstraintTypes(true), []string{ConstraintUnique.String()}; !slices.Equal(got, want) {
		t.Errorf("the Community declarable types are %q, want %q", got, want)
	}
}

// The zero kind names no kind: a Constraint or Index built without one renders
// as unknown rather than as the first real kind.
func TestKinds_TheZeroValueNamesNoKind(t *testing.T) {
	t.Parallel()
	if got := ConstraintKind(0).String(); got != "ConstraintKind(0)" {
		t.Errorf("ConstraintKind(0).String() = %q, want ConstraintKind(0)", got)
	}
	if got := IndexKind(0).String(); got != "IndexKind(0)" {
		t.Errorf("IndexKind(0).String() = %q, want IndexKind(0)", got)
	}
	if slices.Contains(allConstraintKinds, 0) || indexKindToRemoteType(0) != mapping.UnknownRemoteType {
		t.Error("the zero kind maps to a remote type")
	}
}

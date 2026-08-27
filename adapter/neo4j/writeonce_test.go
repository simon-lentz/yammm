package neo4j

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func TestImmutableKeysFor_OwnAndInherited(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "writeonce.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("type Entity not found")
	}
	got := ImmutableKeysFor(st)
	want := []string{"first_seen", "origin"} // inherited + own, sorted
	if !slices.Equal(got, want) {
		t.Errorf("ImmutableKeysFor(Entity) = %v; want %v", got, want)
	}
}

func TestImmutableKeysFor_NoAnnotations(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "writeonce.yammm")
	st, ok := s.Type("Plain")
	if !ok {
		t.Fatal("type Plain not found")
	}
	if got := ImmutableKeysFor(st); got != nil {
		t.Errorf("ImmutableKeysFor(Plain) = %v; want nil", got)
	}
}

// TestWriteOnce_RejectedOnPartType pins the schema-layer guard that keeps
// ImmutableKeysFor unreachable for parts: @writeOnce on a part-type property
// fails the load, because a part is replaced on every parent write.
func TestWriteOnce_RejectedOnPartType(t *testing.T) {
	t.Parallel()
	const src = `schema "wo_part"

type Root {
	id String primary
	*-> NOTES (_:many) Note
}

part type Note {
	body  String    required
	added Timestamp @writeOnce
}
`
	_, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(src),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if !res.HasErrors() {
		t.Fatal("@writeOnce on a part-type property must fail the load")
	}
	if !res.HasCode(diag.E_INVALID_ANNOTATION_TARGET) {
		t.Errorf("want E_INVALID_ANNOTATION_TARGET, got: %v", res)
	}
}

func TestImmutableKeysFor_NilType(t *testing.T) {
	t.Parallel()
	if got := ImmutableKeysFor(nil); got != nil {
		t.Errorf("ImmutableKeysFor(nil) = %v; want nil", got)
	}
}

// TestImmutableKeysFor_ShadowedDropsInherited pins the derivation-layer
// consequence of the shadowing rule: a subtype that re-declares the inherited
// @writeOnce property without re-stating the annotation drops it from its
// derived set (the load warns via W_ANNOTATION_SHADOWED).
func TestImmutableKeysFor_ShadowedDropsInherited(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "writeonce_shadow.yammm")
	st, ok := s.Type("Shadowed")
	if !ok {
		t.Fatal("type Shadowed not found")
	}
	if got := ImmutableKeysFor(st); got != nil {
		t.Errorf("ImmutableKeysFor(Shadowed) = %v; want nil (annotation dropped by re-declaration)", got)
	}
}

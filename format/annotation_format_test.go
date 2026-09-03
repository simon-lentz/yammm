package format

import (
	"strings"
	"testing"
)

func TestFormat_Annotation_Spacing(t *testing.T) {
	out, err := TokenStream(`schema "main"
type T {
	id String primary
	state String @index
	embedding Vector[768] @vector(cosine)
	@@index(state, id)
}
`)
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	for _, want := range []string{"@index", "@vector(cosine)", "@@index(state, id)"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted output is missing %q\n---\n%s", want, out)
		}
	}
	for _, bad := range []string{"@ index", "@ vector", "@@ index", "@vector (", "@@index ("} {
		if strings.Contains(out, bad) {
			t.Errorf("formatted output has bad spacing %q\n---\n%s", bad, out)
		}
	}
}

func TestFormat_Annotation_MultiplicitySpacingPreserved(t *testing.T) {
	// The annotation name→( spacing must collapse WITHOUT collapsing a relation
	// multiplicity's `name (one)` space — they are the same token pair, so the
	// annotation case is decided from loop state, not a pair rule.
	out, err := TokenStream(`schema "main"
type T {
	id String primary
	state String @index
	--> WORKS_AT (one) Company
}
type Company {
	id String primary
}
`)
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	if !strings.Contains(out, "@index") {
		t.Errorf("annotation spacing wrong (want @index)\n---\n%s", out)
	}
	if !strings.Contains(out, "WORKS_AT (one)") {
		t.Errorf("multiplicity spacing must be preserved (want `WORKS_AT (one)`)\n---\n%s", out)
	}
}

func TestFormat_Annotation_Idempotent(t *testing.T) {
	once, err := TokenStream(`schema "main"
type T {
	id String primary
	state String @index @writeOnce
	/* composite */
	@@index(state, id)
}
`)
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	twice, err := TokenStream(once)
	if err != nil {
		t.Fatalf("format error (2nd pass): %v", err)
	}
	if once != twice {
		t.Errorf("format is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

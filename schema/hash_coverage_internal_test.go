package schema

import (
	"reflect"
	"testing"
)

// TestStructuralHash_TypeFieldCoverage classifies every Type field as
// hashed or excluded with a stated reason. A new field must take a row
// here, or the hash silently under-covers the rules that decide validity —
// the algorithm-v1 defect this pin exists to stop.
func TestStructuralHash_TypeFieldCoverage(t *testing.T) {
	t.Parallel()

	classified := map[string]string{
		// Hashed.
		"name":            "hashed via ID().Name()",
		"isAbstract":      "hashed: decides E_ABSTRACT_TYPE",
		"isPart":          "hashed: decides part-at-root rejection",
		"allProperties":   "hashed via AllPropertiesSlice (linearized)",
		"primaryKeys":     "hashed via PrimaryKeysSlice",
		"allAssociations": "hashed via AllAssociationsSlice, edge properties included",
		"allCompositions": "hashed via AllCompositionsSlice",
		"allInvariants":   "hashed via AllInvariantsSlice, order-independent, name excluded",
		"superTypes":      "hashed via SuperTypesSlice (extends rows)",

		// Subsumed: the linearized all* slices cover the own members.
		"properties":   "subsumed by allProperties",
		"associations": "subsumed by allAssociations",
		"compositions": "subsumed by allCompositions",
		"invariants":   "subsumed by allInvariants",
		"inherits":     "subsumed by superTypes (linearized ancestors)",

		// Excluded: never rejects data.
		"annotations":    "excluded: annotations configure DDL, never reject data",
		"allAnnotations": "excluded: annotations configure DDL, never reject data",
		"suppressedAnns": "excluded: annotation bookkeeping",

		// Excluded: identity, display, or location only.
		"schemaName": "excluded: display form; the schema name hashes at the top level",
		"sourceID":   "excluded: a hashed path would split embedded:// and disk loads",
		"span":       "excluded: source location",
		"nameSpan":   "excluded: source location",
		"doc":        "excluded: documentation",

		// Excluded: derived state and caches.
		"subTypes":     "excluded: derived from the subtypes' own extends rows",
		"propByName":   "excluded: lookup index over allProperties",
		"relByName":    "excluded: lookup index over relations",
		"canonicalMap": "excluded: lookup cache",
		"sealed":       "excluded: lifecycle marker",
	}

	rt := reflect.TypeFor[Type]()
	for f := range rt.Fields() {
		if _, ok := classified[f.Name]; !ok {
			t.Errorf("Type field %q is not classified for StructuralHash — decide hashed or excluded, and record it here", f.Name)
		}
	}
	for name := range classified {
		if _, ok := rt.FieldByName(name); !ok {
			t.Errorf("classified field %q no longer exists on Type; delete its row", name)
		}
	}
}

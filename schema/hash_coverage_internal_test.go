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
		"schemaName": "excluded: display form; the schema name hashes in the closure member's frame",
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

// TestStructuralHash_SchemaFieldCoverage classifies every Schema field the
// same way, at the closure level: what a member frame carries, and what the
// walk deliberately leaves out. A new field must take a row here.
func TestStructuralHash_SchemaFieldCoverage(t *testing.T) {
	t.Parallel()

	classified := map[string]string{
		// Hashed, per closure member.
		"name":      "hashed: the member frame opens with the schema name",
		"types":     "hashed via Types(), each through hashType",
		"dataTypes": "hashed via DataTypes(), name and resolved constraint",

		// Hashed indirectly: the walk iterates the closure these produce.
		"imports":     "excluded as declarations (alias and path never reject data); the imported schemas hash as closure members",
		"closure":     "drives the walk: every member frames, entry first, the rest by name",
		"closureOnce": "excluded: lifecycle marker for closure",
		"typeByID":    "excluded: lookup index over the closure's types",

		// Excluded: identity, location, or load context only.
		"sourceID":   "excluded: a hashed path would split embedded:// and disk loads",
		"span":       "excluded: source location",
		"doc":        "excluded: documentation",
		"sources":    "excluded: the load's source registry, not a rule",
		"moduleRoot": "excluded: load context",

		// Excluded: derived state and caches.
		"typeByName":    "excluded: lookup index over types",
		"dataByName":    "excluded: lookup index over dataTypes",
		"importByAlias": "excluded: lookup index over imports",
		"sealed":        "excluded: lifecycle marker",
	}

	rt := reflect.TypeFor[Schema]()
	for f := range rt.Fields() {
		if _, ok := classified[f.Name]; !ok {
			t.Errorf("Schema field %q is not classified for StructuralHash — decide hashed or excluded, and record it here", f.Name)
		}
	}
	for name := range classified {
		if _, ok := rt.FieldByName(name); !ok {
			t.Errorf("classified field %q no longer exists on Schema; delete its row", name)
		}
	}
}

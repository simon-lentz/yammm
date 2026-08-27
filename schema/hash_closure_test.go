package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// loadClosure loads entry from an in-memory source set and fails the test on
// any load error.
func loadClosure(t *testing.T, sources map[string]string, entry string) *schema.Schema {
	t.Helper()
	bytesBy := make(map[string][]byte, len(sources))
	for k, v := range sources {
		bytesBy[k] = []byte(v)
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), bytesBy, entry, ".", schema.WithSourcesOnly(true))
	if result.HasErrors() {
		t.Fatalf("load %s: %v", entry, result.Err())
	}
	return s
}

const closureEntry = `schema "entry"

import "dep.yammm" as d

type Root {
	id String primary
	--> USES (one) d.Dep
}
`

// depWith returns the dep schema with the given member declarations.
func depWith(members string) string {
	return "schema \"dep\"\n\ntype Dep {\n\tid String primary\n" + members + "}\n"
}

// The defect's direct inverse: two closures that differ only in an imported
// type's constraint must hash differently, because the importing schema's
// validator enforces that constraint on every Dep instance it admits.
func TestStructuralHash_CoversImportedTypeConstraints(t *testing.T) {
	t.Parallel()
	loose := loadClosure(t, map[string]string{
		"entry.yammm": closureEntry,
		"dep.yammm":   depWith("\tcode String\n"),
	}, "entry.yammm")
	tight := loadClosure(t, map[string]string{
		"entry.yammm": closureEntry,
		"dep.yammm":   depWith("\tcode String [3, 3]\n"),
	}, "entry.yammm")
	if schema.StructuralHash(loose) == schema.StructuralHash(tight) {
		t.Fatal("an imported type's constraint must move the importing schema's hash")
	}
}

// A transitively imported schema decides validity too — relation targets
// resolve through the whole closure and cross-schema supertypes merge their
// members — so a change two imports away must move the entry's hash.
func TestStructuralHash_CoversTransitiveImports(t *testing.T) {
	t.Parallel()
	diamond := func(base string) map[string]string {
		return map[string]string{
			"entry.yammm": "schema \"entry\"\n\nimport \"a.yammm\" as a\n\ntype Root {\n\tid String primary\n\t--> IN_A (one) a.Alpha\n}\n",
			"a.yammm":     "schema \"a\"\n\nimport \"base.yammm\" as base\n\ntype Alpha {\n\tid String primary\n\t--> GROUNDED (one) base.Ground\n}\n",
			"base.yammm":  base,
		}
	}
	before := loadClosure(t, diamond("schema \"base\"\n\ntype Ground {\n\tid String primary\n}\n"), "entry.yammm")
	after := loadClosure(t, diamond("schema \"base\"\n\ntype Ground {\n\tid String primary\n\tlabel String required\n}\n"), "entry.yammm")
	if schema.StructuralHash(before) == schema.StructuralHash(after) {
		t.Fatal("a required property added two imports away must move the entry schema's hash")
	}
}

// TestStructuralHash_ClosureCoverage is the closure-level coverage statement:
// which facts about a schema's import closure enter the digest and which are
// deliberately excluded, each pinned by a pair of loads. A change to what the
// walk covers must take a row here.
func TestStructuralHash_ClosureCoverage(t *testing.T) {
	t.Parallel()
	entryVia := func(alias, path string) string {
		return "schema \"entry\"\n\nimport \"" + path + "\" as " + alias + "\n\ntype Root {\n\tid String primary\n\t--> USES (one) " + alias + ".Dep\n}\n"
	}
	dep := depWith("\tcode String\n")

	tests := []struct {
		name     string
		a, b     map[string]string
		entry    string
		wantSame bool
	}{
		{
			name:     "a member's schema name is hashed",
			a:        map[string]string{"entry.yammm": closureEntry, "dep.yammm": dep},
			b:        map[string]string{"entry.yammm": closureEntry, "dep.yammm": "schema \"dependency\"\n\ntype Dep {\n\tid String primary\n\tcode String\n}\n"},
			entry:    "entry.yammm",
			wantSame: false,
		},
		{
			name:     "a member's data types are hashed",
			a:        map[string]string{"entry.yammm": closureEntry, "dep.yammm": "schema \"dep\"\n\ntype Code = String [3, 3]\n\ntype Dep {\n\tid String primary\n\tcode Code\n}\n"},
			b:        map[string]string{"entry.yammm": closureEntry, "dep.yammm": "schema \"dep\"\n\ntype Code = String [2, 2]\n\ntype Dep {\n\tid String primary\n\tcode Code\n}\n"},
			entry:    "entry.yammm",
			wantSame: false,
		},
		{
			name:     "an import alias is excluded",
			a:        map[string]string{"entry.yammm": entryVia("d", "dep.yammm"), "dep.yammm": dep},
			b:        map[string]string{"entry.yammm": entryVia("x", "dep.yammm"), "dep.yammm": dep},
			entry:    "entry.yammm",
			wantSame: true,
		},
		{
			name:     "an import path is excluded",
			a:        map[string]string{"entry.yammm": entryVia("d", "dep.yammm"), "dep.yammm": dep},
			b:        map[string]string{"entry.yammm": entryVia("d", "lib/dep.yammm"), "lib/dep.yammm": dep},
			entry:    "entry.yammm",
			wantSame: true,
		},
		{
			name: "import declaration order is excluded",
			a: map[string]string{
				"entry.yammm": "schema \"entry\"\n\nimport \"a.yammm\" as a\nimport \"b.yammm\" as b\n\ntype Root {\n\tid String primary\n\t--> IN_A (one) a.Alpha\n\t--> IN_B (one) b.Beta\n}\n",
				"a.yammm":     "schema \"a\"\n\ntype Alpha {\n\tid String primary\n}\n",
				"b.yammm":     "schema \"b\"\n\ntype Beta {\n\tid String primary\n}\n",
			},
			b: map[string]string{
				"entry.yammm": "schema \"entry\"\n\nimport \"b.yammm\" as b\nimport \"a.yammm\" as a\n\ntype Root {\n\tid String primary\n\t--> IN_A (one) a.Alpha\n\t--> IN_B (one) b.Beta\n}\n",
				"a.yammm":     "schema \"a\"\n\ntype Alpha {\n\tid String primary\n}\n",
				"b.yammm":     "schema \"b\"\n\ntype Beta {\n\tid String primary\n}\n",
			},
			entry:    "entry.yammm",
			wantSame: true,
		},
		{
			name:     "closure membership is hashed even for a member with no types",
			a:        map[string]string{"entry.yammm": "schema \"entry\"\n\nimport \"empty.yammm\" as e\n\ntype Root {\n\tid String primary\n}\n", "empty.yammm": "schema \"empty\"\n"},
			b:        map[string]string{"entry.yammm": "schema \"entry\"\n\ntype Root {\n\tid String primary\n}\n"},
			entry:    "entry.yammm",
			wantSame: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ha := schema.StructuralHash(loadClosure(t, tt.a, tt.entry))
			hb := schema.StructuralHash(loadClosure(t, tt.b, tt.entry))
			if (ha == hb) != tt.wantSame {
				t.Errorf("hashes equal = %v, want %v", ha == hb, tt.wantSame)
			}
		})
	}
}

// Every schema's hash moved at algorithm 3, including one with no imports:
// the uniform closure walk frames the entry schema like any other member. The
// digest below is the algorithm-2 value of buildVectorSchemaMinimal, kept so
// nothing can quietly restore byte-equality with a v2 hash.
func TestStructuralHash_NoImportHashMovedFromAlgorithm2(t *testing.T) {
	t.Parallel()
	const v2Minimal = "sha256:e295ab45c0118bcfa0a910c2a071d83fff13fc1a60d0d863a444d15ecd87845e"
	if got := schema.StructuralHash(buildVectorSchemaMinimal()); got == v2Minimal {
		t.Fatal("a schema with no imports still hashes as algorithm 2 did")
	}
}

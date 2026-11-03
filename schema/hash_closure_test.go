package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// loadClosure loads entry from an in-memory source set and fails the test on
// any load error.
func loadClosure(t *testing.T, sources map[string]string, entry string) schema.Schema {
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

const (
	baseDecl = "type Base {\n\tid String primary\n\tx String\n}\n"
	codeDecl = "type Code = String[3, 3]\n"
)

// withEntry adds an entry schema importing a and b, with the given declarations.
func withEntry(members map[string]string, decls string) map[string]string {
	members["entry.yammm"] = "schema \"entry\"\n\nimport \"a.yammm\" as a\nimport \"b.yammm\" as b\n\n" + decls
	return members
}

// twoBases is a closure whose members a and b each declare Base alike.
func twoBases(decls string) map[string]string {
	return withEntry(map[string]string{"a.yammm": "schema \"a\"\n\n" + baseDecl, "b.yammm": "schema \"b\"\n\n" + baseDecl}, decls)
}

// twoCodes is a closure whose members a and b each declare Code alike.
func twoCodes(decls string) map[string]string {
	return withEntry(map[string]string{"a.yammm": "schema \"a\"\n\n" + codeDecl, "b.yammm": "schema \"b\"\n\n" + codeDecl}, decls)
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
			name: "the entry's direct import set is excluded",
			a: map[string]string{
				"entry.yammm": "schema \"entry\"\n\nimport \"la.yammm\" as la\nimport \"lb.yammm\" as lb\n\ntype Root {\n\tid String primary\n}\n",
				"la.yammm":    "schema \"la\"\n\ntype U {\n\tid String primary\n}\n",
				"lb.yammm":    "schema \"lb\"\n\ntype T {\n\tid String primary\n}\n",
			},
			b: map[string]string{
				"entry.yammm": "schema \"entry\"\n\nimport \"la.yammm\" as la\n\ntype Root {\n\tid String primary\n}\n",
				"la.yammm":    "schema \"la\"\n\nimport \"lb.yammm\" as lb\n\ntype U {\n\tid String primary\n}\n",
				"lb.yammm":    "schema \"lb\"\n\ntype T {\n\tid String primary\n}\n",
			},
			entry:    "entry.yammm",
			wantSame: true,
		},
		{
			name:     "the member that owns a supertype declared alike is excluded",
			a:        twoBases("type K extends a.Base {\n\tlabel String\n}\n"),
			b:        twoBases("type K extends b.Base {\n\tlabel String\n}\n"),
			entry:    "entry.yammm",
			wantSame: true,
		},
		{
			name:     "the member that owns a data type declared alike is excluded",
			a:        twoCodes("type K {\n\tid String primary\n\tc a.Code\n}\n"),
			b:        twoCodes("type K {\n\tid String primary\n\tc b.Code\n}\n"),
			entry:    "entry.yammm",
			wantSame: true,
		},
		{
			name:     "the data type a property names is excluded",
			a:        twoCodes("type K {\n\tid String primary\n\tc a.Code\n}\n"),
			b:        twoCodes("type K {\n\tid String primary\n\tc String[3, 3]\n}\n"),
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

// A type's rendered record holds its inherited invariants as well as its own. The
// parent renders the invariant too, so only the digest tells the two forms apart;
// it moves when the hash algorithm does.
func TestStructuralHash_ATypeHoldsItsInheritedInvariants(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"h\"\n\nabstract type P {\n\tid String primary\n\t! \"p\" id != \"\"\n}\n\ntype K extends P {\n\t! \"k\" id != \"x\"\n}\n", "h.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	const want = "sha256:b8d311ed99f90e73f7c1ac815bd42674f3cfd68391f050354d1d7757b49587af"
	if got := schema.StructuralHash(s); got != want {
		t.Errorf("StructuralHash = %s, want %s", got, want)
	}
}

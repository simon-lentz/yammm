package gogen

import (
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/simon-lentz/yammm/internal/ident"
	"github.com/simon-lentz/yammm/schema"
)

// goExportedIdent converts a yammm name to an exported Go identifier through
// ident.ToUpperCamelInitialisms. An all-separator name yields "" and a
// digit-leading one (a schema name such as "2020census") an unexported
// "_"-prefixed string, so both take an "X" prefix.
func goExportedIdent(name string, inits map[string]bool) string {
	out := ident.ToUpperCamelInitialisms(name, inits)
	if out == "" {
		return "X"
	}
	if r := []rune(out)[0]; !unicode.IsUpper(r) && !unicode.IsTitle(r) {
		return "X" + out
	}
	return out
}

// goPackageName sanitizes a schema name into a lowercase package identifier,
// falling back to "schema" when nothing usable remains. A keyword, "main" (a
// file of declarations cannot build as a program) and "init" (no file can
// import it without an alias) take a "_" suffix.
func goPackageName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" || unicode.IsDigit([]rune(out)[0]) {
		return "schema"
	}
	if token.IsKeyword(out) || out == "main" || out == "init" {
		return out + "_"
	}
	return out
}

// validPackageName reports whether name can head a package clause: a Go
// identifier that is not a keyword and not the blank identifier.
func validPackageName(name string) bool {
	return token.IsIdentifier(name) && name != "_"
}

// nameTable holds the Go names of every top-level declaration, all in the one
// package-block namespace that taken records. It keeps the initialism set its
// names were derived with, so every later derivation uses the same set.
type nameTable struct {
	inits      map[string]bool
	taken      map[string]bool
	types      map[schema.TypeID]string
	dataTypes  map[*schema.DataType]string
	inlineEnum map[string]string // memo key -> Go enum type name
}

// reservedNames are the package-level identifiers gogen emits or once emitted.
// A schema entity of one of these names takes a qualified name, and freeing
// one would move that entity's emitted identifier.
//
//nolint:gochecknoglobals // Intentional: static reserved-name list.
var reservedNames = []string{
	"Graph",
	"SerializedModel",
	"SerializedModelEntry",
	"SerializedSources",
	"SerializedEntry",
	"SchemaHash",
	dateGoName,
}

// buildNameTable assigns every type and data type in the closure its Go name;
// the package doc's Imports section states the rule. Every unique candidate is
// assigned before any shared one is qualified, so a qualified name never takes
// another entity's bare name.
func buildNameTable(s *schema.Schema, inits map[string]bool) *nameTable {
	nt := &nameTable{
		inits:      inits,
		taken:      map[string]bool{},
		types:      map[schema.TypeID]string{},
		dataTypes:  map[*schema.DataType]string{},
		inlineEnum: map[string]string{},
	}
	for _, r := range reservedNames {
		nt.taken[r] = true
	}

	// A type and a data type may share a name, and both become top-level
	// declarations, so the two kinds are grouped under one candidate.
	type origin struct {
		schemaName string
		id         schema.TypeID    // set for a type
		dt         *schema.DataType // set for a data type
	}
	byCandidate := map[string][]origin{}
	for _, sc := range s.Closure() {
		for _, t := range sc.TypesSlice() {
			cand := nt.ident(t.Name())
			byCandidate[cand] = append(byCandidate[cand], origin{schemaName: sc.Name(), id: t.ID()})
		}
		for _, dt := range sc.DataTypesSlice() {
			cand := nt.ident(dt.Name())
			byCandidate[cand] = append(byCandidate[cand], origin{schemaName: sc.Name(), dt: dt})
		}
	}
	assign := func(o origin, name string) {
		nt.taken[name] = true
		if o.dt == nil {
			nt.types[o.id] = name
		} else {
			nt.dataTypes[o.dt] = name
		}
	}

	var shared []string
	for _, cand := range slices.Sorted(maps.Keys(byCandidate)) {
		if origins := byCandidate[cand]; len(origins) == 1 && !nt.taken[cand] {
			assign(origins[0], cand)
			continue
		}
		shared = append(shared, cand)
	}
	// Two claimants in one schema qualify to one name, and reserve's numeric
	// suffix separates them in closure order, types before data types.
	for _, cand := range shared {
		for _, o := range byCandidate[cand] {
			assign(o, nt.reserve(nt.ident(o.schemaName)+cand))
		}
	}
	return nt
}

// ident is goExportedIdent under the table's initialism set.
func (nt *nameTable) ident(name string) string {
	return goExportedIdent(name, nt.inits)
}

// field returns name's Go field name, the first of "<base>", "<base>2", …
// that used does not hold, and records it in used, one struct's namespace.
// Names the identifier transform merges ("foo_1" and "foo1") reach the suffix,
// and so does an edge property that derives a key field's name (target_id).
func (nt *nameTable) field(name string, used map[string]bool) string {
	base := nt.ident(name)
	cand := base
	for i := 2; used[cand]; i++ {
		cand = base + strconv.Itoa(i)
	}
	used[cand] = true
	return cand
}

// goType returns the resolved Go type name for a type identity.
func (nt *nameTable) goType(id schema.TypeID) (string, bool) {
	n, ok := nt.types[id]
	return n, ok
}

// goDataType returns the resolved Go type name for a datatype (pointer-keyed).
func (nt *nameTable) goDataType(dt *schema.DataType) (string, bool) {
	n, ok := nt.dataTypes[dt]
	return n, ok
}

// reserve returns the first free name in "<base>", "<base>2", … and records it
// in the shared namespace.
func (nt *nameTable) reserve(base string) string {
	cand := base
	for i := 2; nt.taken[cand]; i++ {
		cand = base + strconv.Itoa(i)
	}
	nt.taken[cand] = true
	return cand
}

// goInlineEnum returns the memoized Go type name of an inline-enum field:
// "<ownerGoName><FieldGoName>", reserved in the shared namespace on first use.
func (nt *nameTable) goInlineEnum(owner enumOwner, p *schema.Property) string {
	key := owner.key + "\x00" + p.Name()
	if n, ok := nt.inlineEnum[key]; ok {
		return n
	}
	name := nt.reserve(owner.goName + nt.ident(p.Name()))
	nt.inlineEnum[key] = name
	return name
}

// enumOwner names the struct an inline-enum field belongs to: its Go name, and
// a key unique among every struct the file emits.
type enumOwner struct {
	goName string
	key    string
}

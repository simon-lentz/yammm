package gogen

import (
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/simon-lentz/yammm/internal/ident"
	"github.com/simon-lentz/yammm/schema"
)

// goExportedIdent converts a yammm identifier to an exported Go identifier via
// ident.ToUpperCamelInitialisms with the effective initialism set (golint-idiomatic
// id->ID, url->URL, json->JSON, plus any injected through WithInitialisms). It
// GUARANTEES an exported result. The transform upper-cases the first letter segment,
// so a letter-leading name is exported and never a keyword (every Go keyword is
// lower-case). But an all-separator input yields "" and a digit-leading input yields
// a "_"-prefixed (unexported) string — reachable for an arbitrary schema name (e.g.
// "2020census") used as a collision qualifier, since schema names are unconstrained
// STRING literals (type/datatype/property/relation names are UC_WORD/LC_WORD and
// cannot start with a digit). Prefix "X" in those cases so the result always begins
// with an upper-/title-case letter.
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

// goField returns a struct-field Go name, disambiguated within a single struct's
// field namespace. The loader already rejects case-insensitive field collisions
// (checkPropertyCaseCollisions / checkPropertyRelationCollisions /
// checkRelationCollisions in schema/collision.go), so the residuals are names that
// ident merges to one identifier despite differing in the source — separators
// ("foo_bar" and "foo__bar" both -> "FooBar") or letter/digit boundaries ("foo_1"
// and "foo1" both -> "Foo1"). goField returns the first free "<base>", "<base>2",
// … and REGISTERS the chosen name in used, so a later natural "<base>2" cannot
// silently re-collide with an earlier disambiguated one (a bug the naive
// increment-only form has). used is a per-struct set (value > 0 means taken);
// caller passes one used map per struct (a struct's properties and relations share
// one field namespace).
func goField(name string, used map[string]int, inits map[string]bool) string {
	base := goExportedIdent(name, inits)
	cand := base
	for i := 2; used[cand] > 0; i++ {
		cand = base + strconv.Itoa(i)
	}
	used[cand]++ // register the chosen name (base or disambiguated)
	return cand
}

// goPackageName sanitizes a schema name into a valid lowercase package identifier,
// falling back to "schema" when the input yields nothing usable. Unlike exported
// identifiers, a lower-case package name CAN collide with a Go keyword (e.g. a
// schema named "type"), so guard with go/token.
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
	if token.IsKeyword(out) {
		return out + "_"
	}
	return out
}

// nameTable holds the resolved, collision-free Go names for every entity that
// becomes a top-level declaration: types, datatypes, and inline enums. They share
// ONE Go package-block namespace (with the Graph aggregate and the
// SerializedModel/SchemaHash/SerializedModelEntry consts), so the table tracks a
// single `taken` set seeded with those reserved names. Types are keyed by their
// stable TypeID; datatypes by pointer (DataType has no exported identity, and the
// same *schema.DataType the closure walk sees is exactly what ResolveDataType
// returns from the loaded graph — verified: DataTypesSlice clones the same pointers
// DataType()/ResolveDataType() return). Inline-enum names are resolved on demand and
// memoized in inlineEnum.
//
// EDGE_ names are deliberately NOT in this set: they always contain underscores,
// while goExportedIdent only ever emits underscore-free CamelCase, so an EDGE_ name
// can never collide with a type/datatype/enum name. Each EDGE_ name is owner-qualified
// (EDGE_<OwnerGoName>_<edge>_<TargetGoName>, built from the table's collision-resolved
// Go names), so EDGE_ names are unique by construction — no emitter dedup is needed.
type nameTable struct {
	taken      map[string]bool             // every assigned top-level Go identifier
	types      map[schema.TypeID]string    // resolved type -> Go type name
	dataTypes  map[*schema.DataType]string // resolved datatype -> Go type name (pointer-keyed)
	inlineEnum map[string]string           // "<typeID>\x00<rawField>" -> Go enum-type name
}

// reservedNames are the package-level identifiers gogen always emits; no schema
// entity may take them (Go has one package block namespace shared by types, consts,
// and vars). SerializedModelEntry is reserved unconditionally — a stable reserved
// set is simpler than one that varies with source count, and a schema type named
// "SerializedModelEntry" is absurd. SerializedSources and SerializedEntry are
// emitted on both arms of that dispatch and reserved on the same footing: without
// them a schema type of either name takes the Go name, format.Source succeeds, and
// the collision surfaces as a type-check failure.
//
//nolint:gochecknoglobals // Intentional: static reserved-name list.
var reservedNames = []string{
	"Graph",
	"SerializedModel",
	"SerializedModelEntry",
	"SerializedSources",
	"SerializedEntry",
	"SchemaHash",
}

// buildNameTable walks the closure (entry + transitively imported schemas) and
// assigns each type and datatype a collision-free exported Go name in ONE shared
// namespace (seeded with the reserved structural names). Types and datatypes are
// assigned together because yammm permits a type and a datatype to share a name
// (separate loader indices — indexTypes/indexDataTypes in schema/complete.go), and
// both become top-level Go declarations. Unqualified where unique; schema-qualified
// on collision (or when the bare name is reserved). A clash that survives
// qualification is a hard error (mirrors DetectLabelCollisions in
// adapter/neo4j/labels.go).
func buildNameTable(s *schema.Schema, inits map[string]bool) (*nameTable, error) {
	schemas := s.Closure()

	nt := &nameTable{
		taken:      map[string]bool{},
		types:      map[schema.TypeID]string{},
		dataTypes:  map[*schema.DataType]string{},
		inlineEnum: map[string]string{},
	}
	for _, r := range reservedNames {
		nt.taken[r] = true
	}

	// Pass 1: candidate (unqualified) names for every declared entity — types AND
	// datatypes — grouped so cross-kind collisions are detected, not just same-kind.
	type origin struct {
		kind       string // "type" | "datatype"
		schemaName string
		id         schema.TypeID    // kind == "type"
		dt         *schema.DataType // kind == "datatype"
	}
	byCandidate := map[string][]origin{}
	for _, sc := range schemas {
		for _, t := range sc.TypesSlice() {
			cand := goExportedIdent(t.Name(), inits)
			byCandidate[cand] = append(byCandidate[cand], origin{kind: "type", schemaName: sc.Name(), id: t.ID()})
		}
		for _, dt := range sc.DataTypesSlice() {
			cand := goExportedIdent(dt.Name(), inits)
			byCandidate[cand] = append(byCandidate[cand], origin{kind: "datatype", schemaName: sc.Name(), dt: dt})
		}
	}

	assign := func(o origin, name string) {
		nt.taken[name] = true
		if o.kind == "type" {
			nt.types[o.id] = name
		} else {
			nt.dataTypes[o.dt] = name
		}
	}

	// Pass 2: assign in deterministic (sorted-candidate) order. Use the bare
	// candidate only when it is the sole claimant AND not reserved/taken; otherwise
	// schema-qualify every claimant. A qualified name that is STILL taken (two
	// entities in the same schema mapping to one Go name — e.g. a type and a
	// datatype both named Region) cannot be separated by qualification: hard error.
	for _, cand := range slices.Sorted(maps.Keys(byCandidate)) {
		origins := byCandidate[cand]
		if len(origins) == 1 && !nt.taken[cand] {
			assign(origins[0], cand)
			continue
		}
		for _, o := range origins {
			qualified := goExportedIdent(o.schemaName, inits) + cand
			if nt.taken[qualified] {
				return nil, fmt.Errorf(
					"gogen: Go name collision that schema-qualification cannot resolve: %q (%s %q in schema %q); rename one entity",
					qualified, o.kind, cand, o.schemaName,
				)
			}
			assign(o, qualified)
		}
	}

	return nt, nil
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

// goSnakeKey returns the lower_snake JSON key for a Graph field, derived from the
// (already collision-resolved, unique) Go type name via the canonical internal/ident
// transform — the same one Relation.FieldName uses to turn a schema identifier into a
// JSON field name. This keeps the Graph envelope's keys snake_case, consistent with
// property and relation-field keys.
func goSnakeKey(goName string) string {
	return ident.ToLowerSnake(goName)
}

// reserve returns the first free name in "<base>", "<base>2", … and records it in
// the shared namespace. Used for synthesized names (inline enums, enum value
// consts) that must not collide with an already-assigned type / datatype / enum /
// reserved name.
func (nt *nameTable) reserve(base string) string {
	cand := base
	for i := 2; nt.taken[cand]; i++ {
		cand = base + strconv.Itoa(i)
	}
	nt.taken[cand] = true
	return cand
}

// goInlineEnum returns the memoized Go type name for an inline-enum property —
// "<OwnerGoType><FieldGoName>", reserved in the shared namespace. The owner type is
// already named (types/datatypes are assigned before any emission).
func (nt *nameTable) goInlineEnum(owner *schema.Type, p *schema.Property, inits map[string]bool) string {
	key := owner.ID().String() + "\x00" + p.Name()
	if n, ok := nt.inlineEnum[key]; ok {
		return n
	}
	name := nt.reserve(nt.types[owner.ID()] + goExportedIdent(p.Name(), inits))
	nt.inlineEnum[key] = name
	return name
}

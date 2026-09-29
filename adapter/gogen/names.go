package gogen

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
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
// or "schema" when nothing usable remains. A keyword, "main", "init" and a
// predeclared identifier, which an importing file would lose, take "_".
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
	if token.IsKeyword(out) || types.Universe.Lookup(out) != nil || out == "main" || out == "init" {
		return out + "_"
	}
	return out
}

// The words that open each family's exact spelling. None ends in "X" and none
// is "EDGE", which keeps every exact spelling off every bare one.
const (
	wordType            = "Type"
	wordDataType        = "DataType"
	wordLayout          = "Timestamp"
	wordAssociation     = "Association"
	wordEnum            = "Enum"
	wordAssociationEnum = "AssociationEnum"
	wordElement         = "Element"
	wordConst           = "Const"
	wordField           = "Field"
)

// exactSpelling is word, "_", and each identity part written by [exactPart],
// joined by "__"; the package doc's Names section states why no bare spelling
// and no other identity can hold it.
func exactSpelling(word string, parts ...string) string {
	written := make([]string, len(parts))
	for i, p := range parts {
		written[i] = exactPart(p)
	}
	return word + "_" + strings.Join(written, "__")
}

// exactPart writes s with every letter and digit kept and every other rune as
// "_<hex>_", its code point in upper-case hexadecimal.
func exactPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		fmt.Fprintf(&b, "_%X_", r)
	}
	return b.String()
}

// constExact is a value constant's exact spelling: its enum's exact spelling,
// whose word fixes how many parts follow, and the value as one more part.
func constExact(enumExact, value string) string {
	return wordConst + "_" + enumExact + "__" + exactPart(value)
}

// reservedNames are the identifiers gogen emits or once emitted, each a
// permanent claimant of its spelling; freeing one would move the emitted name
// of an entity that claims it.
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

// enumKey names an inline enum by its owner's exact spelling and its
// property's name.
type enumKey struct {
	owner, property string
}

// constKey names an enum value constant by its enum's exact spelling and the
// value.
type constKey struct {
	enum, value string
}

// nameTable holds the Go name of every top-level declaration a schema entity
// gives, and the initialism set every name is derived with.
type nameTable struct {
	inits       map[string]bool
	types       map[schema.TypeID]string
	dataTypes   map[*schema.DataType]string
	dtKeys      map[*schema.DataType]string // each data type's exact spelling
	layouts     map[string]string
	edges       map[*schema.Relation]string
	inlineEnum  map[enumKey]string
	enumKeys    map[enumKey]string // each inline enum's exact spelling
	elements    map[*schema.DataType]string
	elementKeys map[*schema.DataType]string // each element enum's exact spelling
	consts      map[constKey]string
}

// claim is one entity's two spellings and where its assigned name is kept.
type claim struct {
	bare, exact string
	assign      func(name string)
}

// buildNameTable names every schema entity's declaration by the package doc's
// Names rule. Every bare spelling is counted before any name is assigned, so a name
// depends on the set of claims and never on their order.
func buildNameTable(s *schema.Schema, inits map[string]bool, edges []edgeRec, layouts []string) *nameTable {
	nt := &nameTable{
		inits:       inits,
		types:       map[schema.TypeID]string{},
		dataTypes:   map[*schema.DataType]string{},
		dtKeys:      map[*schema.DataType]string{},
		layouts:     map[string]string{},
		edges:       map[*schema.Relation]string{},
		inlineEnum:  map[enumKey]string{},
		enumKeys:    map[enumKey]string{},
		elements:    map[*schema.DataType]string{},
		elementKeys: map[*schema.DataType]string{},
		consts:      map[constKey]string{},
	}
	var claims []claim
	enumValues := func(bare, exact string, values []string) {
		for _, v := range values {
			key := constKey{enum: exact, value: v}
			claims = append(claims, claim{
				bare:   bare + nt.ident(v),
				exact:  constExact(exact, v),
				assign: func(name string) { nt.consts[key] = name },
			})
		}
	}
	inlineEnums := func(ownerBare, ownerWord string, ownerParts []string, props []*schema.Property) {
		ownerKey := exactSpelling(ownerWord, ownerParts...)
		enumWord := wordEnum
		if ownerWord == wordAssociation {
			enumWord = wordAssociationEnum
		}
		for _, p := range props {
			ec, ok := inlineEnum(p.Constraint())
			if !ok {
				continue
			}
			key := enumKey{owner: ownerKey, property: p.Name()}
			bare := ownerBare + nt.ident(p.Name())
			exact := exactSpelling(enumWord, append(slices.Clone(ownerParts), p.Name())...)
			nt.enumKeys[key] = exact
			claims = append(claims, claim{bare: bare, exact: exact, assign: func(name string) { nt.inlineEnum[key] = name }})
			enumValues(bare, exact, ec.Values())
		}
	}

	for _, sc := range s.Closure() {
		for _, t := range sc.TypesSlice() {
			id := t.ID()
			claims = append(claims, claim{bare: nt.ident(t.Name()), exact: typeKey(t), assign: func(name string) { nt.types[id] = name }})
		}
		for _, dt := range sc.DataTypesSlice() {
			bare := nt.ident(dt.Name())
			exact := exactSpelling(wordDataType, sc.Name(), dt.Name())
			nt.dtKeys[dt] = exact
			claims = append(claims, claim{bare: bare, exact: exact, assign: func(name string) { nt.dataTypes[dt] = name }})
			if lc, isList := dt.Constraint().(schema.ListConstraint); isList {
				if ec, ok := inlineEnum(lc); ok {
					elemBare := bare + "Element"
					elemExact := exactSpelling(wordElement, sc.Name(), dt.Name())
					nt.elementKeys[dt] = elemExact
					claims = append(claims, claim{bare: elemBare, exact: elemExact, assign: func(name string) { nt.elements[dt] = name }})
					enumValues(elemBare, elemExact, ec.Values())
				}
			}
			if temporalLayout(dt.Constraint()) != "" {
				continue
			}
			if ec, ok := schema.ResolveAlias(dt.Constraint()).(schema.EnumConstraint); ok {
				enumValues(bare, exact, ec.Values())
			}
		}
	}
	for _, sc := range s.Closure() {
		for _, t := range sc.TypesSlice() {
			inlineEnums(nt.ident(t.Name()), wordType, []string{t.SchemaName(), t.Name()}, t.AllPropertiesSlice())
		}
	}
	for _, e := range edges {
		rel := e.rel
		bare := "EDGE_" + nt.ident(e.owner.Name()) + "_" + rel.FieldName() + "_" + nt.ident(e.target.Name())
		parts := []string{e.owner.SchemaName(), e.owner.Name(), rel.Name()}
		claims = append(claims, claim{bare: bare, exact: edgeKey(e), assign: func(name string) { nt.edges[rel] = name }})
		inlineEnums(bare, wordAssociation, parts, rel.PropertiesSlice())
	}
	for _, layout := range layouts {
		claims = append(claims, claim{bare: layoutTypeBase(layout), exact: layoutTypeExact(layout), assign: func(name string) { nt.layouts[layout] = name }})
	}

	claimants := map[string]int{}
	for _, r := range reservedNames {
		claimants[r]++
	}
	for _, c := range claims {
		claimants[c.bare]++
	}
	for _, c := range claims {
		if claimants[c.bare] == 1 {
			c.assign(c.bare)
		} else {
			c.assign(c.exact)
		}
	}
	return nt
}

// typeKey is t's exact spelling, which also keys its struct as an enum owner.
func typeKey(t *schema.Type) string {
	return exactSpelling(wordType, t.SchemaName(), t.Name())
}

// edgeKey is an association's exact spelling, which also keys its EDGE_ struct
// as an enum owner.
func edgeKey(e edgeRec) string {
	return exactSpelling(wordAssociation, e.owner.SchemaName(), e.owner.Name(), e.rel.Name())
}

// ident is goExportedIdent under the table's initialism set.
func (nt *nameTable) ident(name string) string {
	return goExportedIdent(name, nt.inits)
}

// fieldWire is one struct field before it is named: its wire key, which is
// unique in its struct, and its bare Go spelling.
type fieldWire struct {
	wire, bare string
}

// fieldNames names every field of one struct, keyed by wire key: the bare
// spelling when no other field claims it, and otherwise "Field_" and the key.
func fieldNames(fields []fieldWire) map[string]string {
	claimants := map[string]int{}
	for _, f := range fields {
		claimants[f.bare]++
	}
	names := make(map[string]string, len(fields))
	for _, f := range fields {
		if claimants[f.bare] == 1 {
			names[f.wire] = f.bare
		} else {
			names[f.wire] = wordField + "_" + f.wire
		}
	}
	return names
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

// goInlineEnum returns the Go type name of the inline enum property p declares
// in owner's struct.
func (nt *nameTable) goInlineEnum(owner enumOwner, p *schema.Property) string {
	return nt.inlineEnum[enumKey{owner: owner.key, property: p.Name()}]
}

// goDataTypeElement returns the Go type name of the inline enum a List
// DataType holds as its innermost element. It is false for any other DataType.
func (nt *nameTable) goDataTypeElement(dt *schema.DataType) (string, bool) {
	n, ok := nt.elements[dt]
	return n, ok
}

// enumOwner names the struct an inline-enum field belongs to by its exact
// spelling, which no other struct shares.
type enumOwner struct {
	key string
}

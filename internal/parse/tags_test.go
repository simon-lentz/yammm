package parse

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The rule table holds no keywords, so every reserved-word decision lives in a
// negative lookahead group written out inside a struct tag. Struct tags are
// compile-time strings and cannot be composed from the maps, so the same list
// is repeated by hand across the grammar. These tests are what make the
// repetition structural instead of remembered.

// grammarRoots are the types the parser set is built from. Walking the field
// graph from here reaches every node the grammar can use, so a node added
// without being wired to a parser is correctly out of scope. The optional
// groups need no root of their own: the field that holds one is a pointer the
// walk follows.
func grammarRoots() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[schemaHeadNode](),
		reflect.TypeFor[importNode](),
		reflect.TypeFor[dtDeclNode](),
		reflect.TypeFor[typeHeadNode](),
		reflect.TypeFor[memberNode](),
	}
}

// nonLiteralLookaheads are the lookahead groups that name token classes rather
// than spellings. They carry no reserved-word list to keep in step.
var nonLiteralLookaheads = []string{"UC_WORD | LC_WORD '.'"}

// tokenClasses is the canonical-set name the nonLiteralLookaheads groups carry.
const tokenClasses = "token classes"

// lookaheadSites is the grammar's whole lookahead layout: every field holding
// a group, and the canonical set each group spells out, in tag order. A group
// that changes set, disappears, or appears somewhere new all fail here.
var lookaheadSites = map[string][]string{
	"anyNameNode.LC":       {"reserved lowercase"},
	"anyNameNode.UC":       {"datatype names"},
	"dtDeclNode.Name":      {"datatype names"},
	"typeHeadNode.Name":    {"datatype names"},
	"typeRefNode.Qual":     {"reserved lowercase", "datatype names"},
	"typeRefNode.Name":     {"datatype names"},
	"relPropNode.Name":     {"non-property names"},
	"relPropNode.Required": {tokenClasses},
	"typeAnnNode.Name":     {"non-property names"},
	"propNode.Name":        {"non-property names"},
	"propNode.Primary":     {tokenClasses},
	"propNode.Required":    {tokenClasses},
	"annNode.Name":         {"non-property names"},
	"argNode.Ident":        {"non-property names"},
	"argNode.UC":           {"datatype names"},
	"aliasC.Qual":          {"reserved lowercase", "datatype names"},
	"aliasC.Name":          {"datatype names"},
}

// TestTags_LookaheadGroupsAreAtTheirCanonicalSites is the guard. Struct tags
// are compile-time strings, so the same reserved-word list is written out by
// hand across the grammar; this holds every copy to its map and every site to
// its set.
func TestTags_LookaheadGroupsAreAtTheirCanonicalSites(t *testing.T) {
	canonical := map[string][]string{
		"reserved lowercase": sortedKeys(reservedLC),
		"datatype names":     sortedKeys(datatypeKeywords),
		"non-property names": sortedKeys(propertyNameExclusions),
	}

	groups := collectLookaheadGroups(t)
	if len(groups) == 0 {
		t.Fatal("no lookahead groups found — the extractor is broken, not the grammar")
	}

	got := map[string][]string{}
	for _, g := range groups {
		got[g.where] = append(got[g.where], classifyLookahead(t, g, canonical))
	}

	for where, want := range lookaheadSites {
		switch have, ok := got[where]; {
		case !ok:
			t.Errorf("%s: expected lookahead group(s) %v, found none", where, want)
		case !slices.Equal(have, want):
			t.Errorf("%s: lookahead sets = %v, want %v", where, have, want)
		}
	}
	for where, have := range got {
		if _, ok := lookaheadSites[where]; !ok {
			t.Errorf("%s: lookahead group(s) %v at a site nothing expects", where, have)
		}
	}
}

// classifyLookahead names the canonical set a group spells out, reporting the
// group and returning "" when it matches none.
func classifyLookahead(t *testing.T, g lookaheadGroup, canonical map[string][]string) string {
	t.Helper()
	if slices.Contains(nonLiteralLookaheads, strings.TrimSpace(g.text)) {
		return tokenClasses
	}
	literals := quotedLiterals(g.text)
	if len(literals) == 0 {
		t.Errorf("%s: lookahead %q names no spellings and is not a known token-class guard",
			g.where, g.text)
		return ""
	}
	for name, want := range canonical {
		if slices.Equal(literals, want) {
			return name
		}
	}
	t.Errorf("%s: lookahead group does not equal any canonical set\n got: %v\n%s",
		g.where, literals, renderCanonical(canonical))
	return ""
}

// TestTags_CanonicalSetsHaveTheExpectedSizes pins the three sets against the
// language's own counts, so a spelling cannot be added to a map and to every
// tag copy at once without a deliberate change here.
func TestTags_CanonicalSetsHaveTheExpectedSizes(t *testing.T) {
	if len(reservedLC) != 17 {
		t.Errorf("reservedLC has %d spellings, want 17", len(reservedLC))
	}
	if len(datatypeKeywords) != 11 {
		t.Errorf("datatypeKeywords has %d names, want 11", len(datatypeKeywords))
	}
	if len(propertyNameExclusions) != 6 {
		t.Errorf("propertyNameExclusions has %d spellings, want 6", len(propertyNameExclusions))
	}
	for name := range propertyNameExclusions {
		if !reservedLC[name] {
			t.Errorf("%q is excluded from property names but is not reserved", name)
		}
	}
}

type lookaheadGroup struct {
	where string
	text  string
}

func collectLookaheadGroups(t *testing.T) []lookaheadGroup {
	t.Helper()
	var out []lookaheadGroup
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for f := range rt.Fields() {
			if tag, ok := f.Tag.Lookup("parser"); ok {
				for _, g := range lookaheadsIn(tag) {
					out = append(out, lookaheadGroup{where: rt.Name() + "." + f.Name, text: g})
				}
			}
			walk(f.Type)
		}
	}
	for _, rt := range grammarRoots() {
		walk(rt)
	}
	return out
}

// lookaheadsIn returns the body of every "(?! ... )" group in a tag.
func lookaheadsIn(tag string) []string {
	var out []string
	for rest := tag; ; {
		i := strings.Index(rest, "(?!")
		if i < 0 {
			return out
		}
		rest = rest[i+len("(?!"):]
		j := strings.IndexByte(rest, ')')
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j+1:]
	}
}

// quotedLiterals returns the single-quoted spellings in a lookahead body,
// sorted so two copies written in different orders still compare equal.
func quotedLiterals(group string) []string {
	var out []string
	for rest := group; ; {
		i := strings.IndexByte(rest, '\'')
		if i < 0 {
			break
		}
		rest = rest[i+1:]
		j := strings.IndexByte(rest, '\'')
		if j < 0 {
			break
		}
		out = append(out, rest[:j])
		rest = rest[j+1:]
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func renderCanonical(sets map[string][]string) string {
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	slices.Sort(names)
	var sb strings.Builder
	for _, name := range names {
		sb.WriteString(" " + name + ": " + strings.Join(sets[name], " ") + "\n")
	}
	return sb.String()
}

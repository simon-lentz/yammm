package gogen

import (
	"context"
	"fmt"
	"go/token"
	"maps"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/schema"
)

func TestGoIdent(t *testing.T) {
	// goExportedIdent applies the merged initialism set; defaultInitialisms is
	// gogen's golint base (domain acronyms would arrive via WithInitialisms).
	cases := map[string]string{
		"fips":     "Fips",
		"in_state": "InState",
		"id":       "ID",
		"base_url": "BaseURL",
		"http_api": "HTTPAPI", // both in the full golint set
		// The transform gives a digit-leading value "_2020Census", which is
		// unexported, so goExportedIdent prefixes "X".
		"2020census": "X_2020Census",
		"":           "X",
	}
	for in, want := range cases {
		if got := goExportedIdent(in, defaultInitialisms); got != want {
			t.Errorf("goExportedIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGoPackageName(t *testing.T) {
	cases := map[string]string{
		"municipal": "municipal",
		"Geo Data":  "geodata",
		"123schema": "schema",
		"":          "schema",
		"type":      "type_",
		"Main":      "main_",
		"main":      "main_",
		"Init":      "init_",
		"in-it":     "init_",
		"nil":       "nil_",
		"String":    "string_",
		"error":     "error_",
		"any":       "any_",
		"len":       "len_",
		"true":      "true_",
		"iota":      "iota_",
	}
	for in, want := range cases {
		if got := goPackageName(in); got != want {
			t.Errorf("goPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

// nameTableOf builds s's name table as generation does.
func nameTableOf(t *testing.T, s *schema.Schema) *nameTable {
	t.Helper()
	g, err := newGenerator(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.registerDataTypeFields(); err != nil {
		t.Fatal(err)
	}
	if err := g.collectEdges(); err != nil {
		t.Fatal(err)
	}
	layouts, err := g.collectTemporalDemand()
	if err != nil {
		t.Fatal(err)
	}
	return buildNameTable(s, g.inits, g.edges, layouts)
}

// typeNames maps "<schema>.<type>" to its Go name over s's closure.
func typeNames(t *testing.T, s *schema.Schema, nt *nameTable) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, sc := range s.Closure() {
		for _, typ := range sc.TypesSlice() {
			name, ok := nt.goType(typ.ID())
			if !ok {
				t.Fatalf("%s.%s has no Go name", sc.Name(), typ.Name())
			}
			got[sc.Name()+"."+typ.Name()] = name
		}
	}
	return got
}

// TestBuildNameTable_OneSchemaSharingACandidateTakesExactSpellings pins that
// two entities of one schema mapping to one Go name both take their exact
// spellings: a type and a data type both named Region, which the kind
// separates, and two types Url and URL, which the initialisms map to one name.
func TestBuildNameTable_OneSchemaSharingACandidateTakesExactSpellings(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want map[string]string
	}{
		"a type and a data type": {
			src:  "schema \"geo\"\n\ntype Region = String\n\ntype Region {\n\tid String primary\n\tr Region\n}\n",
			want: map[string]string{"type Region": "Type_geo__Region", "datatype Region": "DataType_geo__Region"},
		},
		"two types under an initialism": {
			src:  "schema \"geo\"\n\ntype Url {\n\tid String primary\n}\n\ntype URL {\n\tid String primary\n}\n",
			want: map[string]string{"type Url": "Type_geo__Url", "type URL": "Type_geo__URL"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.LoadString(context.Background(), tc.src, "collide.yammm")
			if res.HasErrors() {
				t.Fatalf("load: %v", res.Err())
			}
			nt := nameTableOf(t, s)
			got := map[string]string{}
			for _, typ := range s.TypesSlice() {
				got["type "+typ.Name()], _ = nt.goType(typ.ID())
			}
			for _, dt := range s.DataTypesSlice() {
				got["datatype "+dt.Name()], _ = nt.goDataType(dt)
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("named %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildNameTable_SharedCandidatesTakeExactSpellings pins that every
// claimant of a shared candidate takes its exact spelling and every sole
// claimant its bare one, even where the bare spelling reads like another
// entity's qualified name: b's AFoo and MainBar stay bare beside a's Foo and
// the entry's Bar.
func TestBuildNameTable_SharedCandidatesTakeExactSpellings(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("testdata", "imports", "qualified_name_main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	s, res := schema.Load(context.Background(), abs)
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	want := map[string]string{
		"main.Bar":    "Type_main__Bar",
		"main.Holder": "Holder",
		"a.Foo":       "Type_a__Foo",
		"a.Bar":       "Type_a__Bar",
		"b.Foo":       "Type_b__Foo",
		"b.AFoo":      "AFoo",
		"b.MainBar":   "MainBar",
	}
	if got := typeNames(t, s, nameTableOf(t, s)); !maps.Equal(got, want) {
		t.Errorf("named %v, want %v", got, want)
	}
}

// TestBuildNameTable_AReservedNameIsAClaimant pins that a type named as an
// emitted declaration takes its exact spelling rather than shadowing it.
func TestBuildNameTable_AReservedNameIsAClaimant(t *testing.T) {
	for _, reserved := range reservedNames {
		if reserved == dateGoName {
			continue // a DSL keyword; TestBuildNameTable_ATypeWhoseBareSpellingIsDateTakesItsExactSpelling
		}
		s, res := schema.LoadString(context.Background(),
			"schema \"geo\"\n\ntype "+reserved+" {\n\tid String primary\n}", "g.yammm")
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		if got := typeNames(t, s, nameTableOf(t, s))["geo."+reserved]; got != "Type_geo__"+reserved {
			t.Errorf("type %s is named %q, want its exact spelling", reserved, got)
		}
	}
}

// TestLayoutTypeBase pins a per-layout type's bare spelling: every letter and
// digit of the layout, nothing else, behind a fixed prefix. Another claimant
// of that spelling moves the layout to its exact spelling
// (TestMarshal_LayoutNameNeverPassesToAnotherLayout).
func TestLayoutTypeBase(t *testing.T) {
	cases := map[string]string{
		"2006-01-02 15:04:05":        "Timestamp20060102150405",
		"2006-01-02T15:04:05Z07:00":  "Timestamp20060102T150405Z0700",
		"Jan _2, 2006":               "TimestampJan22006",
		"--":                         "Timestamp",
		"02 Jänner 2006":             "Timestamp02Jänner2006",
		"2006-01-02T15:04:05.000000": "Timestamp20060102T150405000000",
	}
	for in, want := range cases {
		if got := layoutTypeBase(in); got != want {
			t.Errorf("layoutTypeBase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildNameTable_ATypeWhoseBareSpellingIsDateTakesItsExactSpelling pins
// that the emitted Date type's name is a permanent claimant: Date is a DSL
// keyword, but a type Date_ has the bare spelling Date.
func TestBuildNameTable_ATypeWhoseBareSpellingIsDateTakesItsExactSpelling(t *testing.T) {
	if !slices.Contains(reservedNames, dateGoName) {
		t.Fatalf("%q is not a reserved name", dateGoName)
	}
	s, res := schema.LoadString(context.Background(),
		"schema \"g\"\n\ntype Date_ {\n\tid String primary\n\td Date required\n}\n", "d.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	if got := typeNames(t, s, nameTableOf(t, s))["g.Date_"]; got != "Type_g__Date_5F_" {
		t.Errorf("type Date_ is named %q, want Type_g__Date_5F_", got)
	}
}

// TestBuildNameTable_AnExactSpellingNeverMeetsAReservedName pins that schema
// "schema" and schema "b" both declaring Hash name it by kind, schema and
// name, so no claimant can reach the emitted SchemaHash constant's name.
func TestBuildNameTable_AnExactSpellingNeverMeetsAReservedName(t *testing.T) {
	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"main.yammm": []byte("schema \"schema\"\n\nimport \"b.yammm\" as b\n\ntype Hash {\n\tid String primary\n\t--> HAS_B (one) b.Hash\n}\n"),
		"b.yammm":    []byte("schema \"b\"\n\ntype Hash {\n\tid String primary\n}\n"),
	}, "main.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	want := map[string]string{"schema.Hash": "Type_schema__Hash", "b.Hash": "Type_b__Hash"}
	if got := typeNames(t, s, nameTableOf(t, s)); !maps.Equal(got, want) {
		t.Errorf("named %v, want %v", got, want)
	}
}

// TestBuildNameTable_NamesDoNotDependOnImportOrder pins that the closure's
// order, which the order of import declarations sets, decides no name: two
// schemas declare BFoo, and schemas "a_b" and "ab" declare Foo, which the
// escape keeps apart; four orders of the imports name them alike.
func TestBuildNameTable_NamesDoNotDependOnImportOrder(t *testing.T) {
	src := func(name, typ string) []byte {
		return []byte("schema \"" + name + "\"\n\ntype " + typ + " {\n\tid String primary\n}\n")
	}
	imports := []string{
		"import \"a.yammm\" as a\n",
		"import \"d.yammm\" as d\n",
		"import \"ab.yammm\" as ab\n",
		"import \"c.yammm\" as c\n",
	}
	want := map[string]string{
		"main.Holder": "Holder",
		"a.BFoo":      "Type_a__BFoo",
		"d.BFoo":      "Type_d__BFoo",
		"a_b.Foo":     "Type_a_5F_b__Foo",
		"ab.Foo":      "Type_ab__Foo",
	}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}, {1, 3, 0, 2}} {
		var head strings.Builder
		for _, i := range order {
			head.WriteString(imports[i])
		}
		s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
			"main.yammm": []byte("schema \"main\"\n\n" + head.String() + "\ntype Holder {\n\tid String primary\n}\n"),
			"a.yammm":    src("a", "BFoo"),
			"d.yammm":    src("d", "BFoo"),
			"ab.yammm":   src("a_b", "Foo"),
			"c.yammm":    src("ab", "Foo"),
		}, "main.yammm", ".", schema.WithSourcesOnly(true))
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		if got := typeNames(t, s, nameTableOf(t, s)); !maps.Equal(got, want) {
			t.Errorf("imports in order %v named %v, want %v", order, got, want)
		}
	}
}

// exactArity is how many identity parts each family's exact spelling holds.
var exactArity = map[string]int{
	wordType: 2, wordDataType: 2, wordLayout: 1, wordAssociation: 3,
	wordEnum: 3, wordAssociationEnum: 4, wordElement: 2,
}

// readExact reads an exact spelling back to its word and identity parts. A
// value constant's parts are its enum's word, the enum's parts, and the value.
func readExact(name string) (string, []string, bool) {
	word, rest, ok := strings.Cut(name, "_")
	if !ok {
		return "", nil, false
	}
	if word == wordConst {
		enumWord, enumRest, ok := strings.Cut(rest, "_")
		n, known := exactArity[enumWord]
		if !ok || !known || enumWord == wordLayout || enumWord == wordAssociation || enumWord == wordType {
			return "", nil, false
		}
		parts, ok := readParts(enumRest)
		if !ok || len(parts) != n+1 {
			return "", nil, false
		}
		return word, append([]string{enumWord}, parts...), true
	}
	n, known := exactArity[word]
	if !known {
		return "", nil, false
	}
	parts, ok := readParts(rest)
	if !ok || len(parts) != n {
		return "", nil, false
	}
	return word, parts, true
}

// readParts reads parts written by exactPart and joined by "__".
func readParts(s string) ([]string, bool) {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '_' && i+1 < len(s) && s[i+1] == '_':
			parts = append(parts, cur.String())
			cur.Reset()
			i += 2
		case r == '_':
			end := strings.IndexByte(s[i+1:], '_')
			if end <= 0 {
				return nil, false
			}
			hex := s[i+1 : i+1+end]
			if strings.ToUpper(hex) != hex {
				return nil, false
			}
			var code rune
			if _, err := fmt.Sscanf(hex, "%X", &code); err != nil || unicode.IsLetter(code) || unicode.IsDigit(code) || fmt.Sprintf("%X", code) != hex {
				return nil, false
			}
			cur.WriteRune(code)
			i += end + 2
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur.WriteRune(r)
			i += size
		default:
			return nil, false
		}
	}
	return append(parts, cur.String()), true
}

// randomText draws a string from runes that exercise every path of the
// escape: letters, digits (a non-ASCII one too), "_", "X", separators, a
// combining mark and a CJK letter.
func randomText(r *rand.Rand, maxLen int) string {
	pool := []rune("aZ09_X-. éé中٣́/%")
	n := r.IntN(maxLen + 1)
	var b strings.Builder
	for range n {
		b.WriteRune(pool[r.IntN(len(pool))])
	}
	return b.String()
}

// TestExactSpelling_ReadsBackToItsIdentityAndNoBareSpellingHoldsIt pins what
// the rule rests on: every exact spelling reads back to one family and one
// identity, so no two entities share one, and no bare spelling of any family
// reads as an exact one.
func TestExactSpelling_ReadsBackToItsIdentityAndNoBareSpellingHoldsIt(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed makes the identities reproducible
	for range 20000 {
		words := slices.Sorted(maps.Keys(exactArity))
		word := words[r.IntN(len(words))]
		parts := make([]string, exactArity[word])
		for i := range parts {
			parts[i] = randomText(r, 6)
		}
		spelled := exactSpelling(word, parts...)
		if gotWord, gotParts, ok := readExact(spelled); !ok || gotWord != word || !slices.Equal(gotParts, parts) {
			t.Fatalf("exactSpelling(%q, %q) = %q reads back as %q %q (ok=%v)", word, parts, spelled, gotWord, gotParts, ok)
		}
		if !token.IsIdentifier(spelled) || !unicode.IsUpper([]rune(spelled)[0]) {
			t.Fatalf("exactSpelling(%q, %q) = %q is not an exported Go identifier", word, parts, spelled)
		}
		if word == wordEnum || word == wordAssociationEnum || word == wordElement || word == wordDataType {
			value := randomText(r, 6)
			c := constExact(spelled, value)
			gotWord, gotParts, ok := readExact(c)
			if want := append(append([]string{word}, parts...), value); !ok || gotWord != wordConst || !slices.Equal(gotParts, want) {
				t.Fatalf("constExact(%q, %q) = %q reads back as %q %q (ok=%v)", spelled, value, c, gotWord, gotParts, ok)
			}
		}

		a, b := goExportedIdent(randomText(r, 8), defaultInitialisms), goExportedIdent(randomText(r, 8), defaultInitialisms)
		for _, bare := range []string{
			a, a + b, a + "Element", layoutTypeBase(randomText(r, 8)),
			"EDGE_" + a + "_" + strings.ToLower(randomText(r, 6)) + "_" + b,
			"EDGE_" + a + "_x_" + b + a,
		} {
			if w, p, ok := readExact(bare); ok {
				t.Fatalf("the bare spelling %q reads as the exact spelling of %s %q", bare, w, p)
			}
		}
	}
	for _, reserved := range reservedNames {
		if w, p, ok := readExact(reserved); ok {
			t.Errorf("the reserved name %q reads as the exact spelling of %s %q", reserved, w, p)
		}
	}
}

// TestFieldNames_AContestedFieldTakesItsWireKey pins the rule in a struct's
// namespace: bare spellings no other field claims stay, and every claimant of
// a shared one takes "Field_" and its wire key, which no bare spelling holds.
func TestFieldNames_AContestedFieldTakesItsWireKey(t *testing.T) {
	got := fieldNames([]fieldWire{
		{wire: "foo_1", bare: "Foo1"},
		{wire: "foo1", bare: "Foo1"},
		{wire: "_target_id", bare: "TargetID"},
		{wire: "target_id", bare: "TargetID"},
		{wire: "note", bare: "Note"},
	})
	want := map[string]string{
		"foo_1": "Field_foo_1", "foo1": "Field_foo1",
		"_target_id": "Field__target_id", "target_id": "Field_target_id",
		"note": "Note",
	}
	if !maps.Equal(got, want) {
		t.Errorf("fieldNames = %v, want %v", got, want)
	}
}

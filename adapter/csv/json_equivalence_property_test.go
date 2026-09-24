package csv

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const generatedSchema = `schema "generated"

type Company {
	company_id String primary
}

type Office {
	cid String primary
	d Date primary
}

part type Address {
	street String
}

type Person {
	id String primary
	name String required
	age Integer
	nick String
	--> WORKS_AT (_:one) Company {
		since Integer
	}
	--> KNOWS (_:many) Company {
		weight Integer
		tag String required
	}
	--> HOUSED (_:one) Office
	*-> ADDRESS (one) Address
}
`

// The generated class: every plain spelling and field spelling below may join a
// header, each group with its first suffix and any of the others, in any order,
// every cell drawn from the pools. Spellings are exact, case-folded, colliding, or name nothing,
// and fields name an association, a composition, a property or nothing.
var (
	generatedPlain  = []string{"name", "Name", "NAME", "age", "Age", "AGE", "nick", "NICK", "NIC\u212a", "works_at", "Works_At", "knows", "address", "zzz"}
	generatedGroups = []struct {
		fields   []string
		suffixes []string
	}{
		{[]string{"works_at", "Works_at", "WORKS_AT"}, []string{"_target_company_id", "_TARGET_COMPANY_ID", "since", "SINCE", "note"}},
		{[]string{"knows", "KNOWS"}, []string{"_target_company_id", "_Target_Company_Id", "weight", "WEIGHT", "tag", "TAG", "note"}},
		{[]string{"housed", "HOUSED"}, []string{"_target_cid", "_TARGET_CID", "_target_d", "_Target_D", "note"}},
		{[]string{"address", "Address"}, []string{"street", "STREET", "zip"}},
		{[]string{"name", "Age", "age", "nosuch"}, []string{"a", "first"}},
	}
	generatedPlainCells = []string{"", "", "Ann", "42", "1e2", "1.5", "abc", "a|b"}
	generatedGroupCells = []string{"", "", "c1", "c1|c2", "5", "5|6", "x", "|6", "2020-01-01", "bad", "Main|Elm"}
)

// A row parses as the JSON adapter parses the document the row states, and the
// validator answers the two alike, for every header the generator draws under
// every mode a caller can match: folding and strict, with and without
// WithSchema, unknown fields refused and allowed. The document is built by the
// rule the package documentation states, and which key claims a member is
// asked of the validator itself, by validating the row's keys alone.
func TestColumnMapping_GeneratedHeadersParseAsTheJSONEquivalent(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), generatedSchema, "generated.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	person, _ := s.Type("Person")
	rng := rand.New(rand.NewPCG(20260922, 131)) //nolint:gosec // a fixed seed makes the class reproducible
	const cases = 800
	failures, checked := 0, 0
	stats := map[string]int{}
	for range cases {
		header, rows := generateFile(rng)
		var file strings.Builder
		file.WriteString(csvLine(append([]string{"id"}, header...)))
		for _, row := range rows {
			file.WriteString(csvLine(append([]string{"p1"}, row...)))
		}
		csvText := file.String()
		for _, strict := range []bool{false, true} {
			for _, withSchema := range []bool{true, false} {
				checked++
				o := oracle{s: s, person: person, strict: strict, withSchema: withSchema, stats: stats}
				docs := make([]string, len(rows))
				var csvOnly []string
				for k, row := range rows {
					var more []string
					docs[k], more = o.document(t.Context(), header, row)
					csvOnly = append(csvOnly, more...)
				}
				why := checkEquivalence(t, o, csvText, docs, csvOnly)
				if why == "" {
					continue
				}
				failures++
				if failures <= 12 {
					t.Errorf("strict=%v WithSchema=%v\ncsv:\n%sjson: %s\n%s", strict, withSchema, csvText, strings.Join(docs, "\n      "), why)
				}
			}
		}
	}
	t.Logf("%d cases; rows holding each rule: %v", checked, stats)
	if failures > 0 {
		t.Errorf("%d of %d generated cases differ", failures, checked)
	}
}

func checkEquivalence(t *testing.T, o oracle, csvText string, docs, csvOnly []string) string {
	t.Helper()
	byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"), []byte(`{"Person":[`+strings.Join(docs, ",")+`]}`))
	if len(byType["Person"]) != len(docs) {
		t.Fatalf("the oracle's documents do not parse: %s\n%s", docs, jres)
	}
	opts := []Option{WithStrictPropertyNames(o.strict)}
	if o.withSchema {
		opts = append(opts, WithSchema(o.s))
	}
	raws, pres := New(opts...).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person", strings.NewReader(csvText), o.person)
	if len(raws) != len(docs) {
		return fmt.Sprintf("csv parse: %d instances, %s", len(raws), pres)
	}
	var why []string
	want := slices.Sorted(slices.Values(append(errorCodes(jres), csvOnly...)))
	if got := errorCodes(pres); !slices.Equal(got, want) {
		why = append(why, fmt.Sprintf("parse Error codes %q, want %q\n  csv: %s", got, want, pres))
	}
	for k, fromJSON := range byType["Person"] {
		if !reflect.DeepEqual(raws[k].Properties, fromJSON.Properties) {
			why = append(why, fmt.Sprintf("row %d: properties differ\n  csv:  %#v\n  json: %#v", k, raws[k].Properties, fromJSON.Properties))
		}
		for _, allow := range []bool{false, true} {
			vo := []instance.Option{instance.WithStrictPropertyNames(o.strict), instance.WithAllowUnknownFields(allow)}
			if a, b := verdict(t, o.s, raws[k], vo...), verdict(t, o.s, fromJSON, vo...); !slices.Equal(a, b) {
				why = append(why, fmt.Sprintf("row %d, allow=%v: verdicts differ\n  csv:  %q\n  json: %q", k, allow, a, b))
			}
		}
	}
	return strings.Join(why, "\n")
}

// generateFile draws a header and three rows under it, so rows that repeat a
// header's key set, and rows that do not, meet in one parse.
func generateFile(rng *rand.Rand) (header []string, rows [][]string) {
	seen := map[string]bool{"id": true}
	var pools [][]string
	add := func(name string, pool []string) {
		if !seen[name] {
			seen[name] = true
			header, pools = append(header, name), append(pools, pool)
		}
	}
	// A density per file, so small headers, where one rule decides the row, are
	// as common as large ones.
	plainOdds, groupOdds := 2+rng.IntN(8), 2+rng.IntN(8)
	for _, name := range generatedPlain {
		if rng.IntN(plainOdds) == 0 {
			add(name, generatedPlainCells)
		}
	}
	for _, g := range generatedGroups {
		for _, field := range g.fields {
			if rng.IntN(groupOdds) != 0 {
				continue
			}
			for k, suffix := range g.suffixes {
				if k == 0 || rng.IntN(2) == 0 {
					add(field+"."+suffix, generatedGroupCells)
				}
			}
		}
	}
	perm := rng.Perm(len(header))
	h, ps := make([]string, len(header)), make([][]string, len(header))
	for i, j := range perm {
		h[i], ps[i] = header[j], pools[j]
	}
	rows = make([][]string, 3)
	for k := range rows {
		rows[k] = make([]string, len(h))
		for i, pool := range ps {
			// The second row fills exactly the cells the first fills, each
			// drawn again, so a decision kept for the first row's key set is
			// read again.
			if k == 1 && rows[0][i] == "" {
				continue
			}
			rows[k][i] = pool[rng.IntN(len(pool))]
			for k == 1 && rows[k][i] == "" {
				rows[k][i] = pool[rng.IntN(len(pool))]
			}
		}
	}
	return h, rows
}

func csvLine(fields []string) string {
	return strings.Join(fields, ",") + "\n"
}

// oracle builds the document a row states, by the rule the package
// documentation states, asking the validator which key claims each member.
type oracle struct {
	s                  *schema.Schema
	person             *schema.Type
	strict, withSchema bool
	stats              map[string]int
}

func (o oracle) count(rule string) {
	if o.stats != nil {
		o.stats[rule]++
	}
}

type jsonMember struct{ key, raw string }

func jsonObject(members []jsonMember) string {
	parts := make([]string, len(members))
	for i, m := range members {
		parts[i] = strconv.Quote(m.key) + ":" + m.raw
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// resolved asks the validator which of keys it reads as a member when an object
// holds exactly those keys: every key it reports neither unknown nor colliding.
func (o oracle) resolved(ctx context.Context, obj map[string]any, keys []string, unknownCode diag.Code) map[string]bool {
	_, res := instance.NewValidator(o.s, instance.WithStrictPropertyNames(o.strict)).
		ValidateOne(ctx, "Person", instance.RawInstance{Properties: obj})
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	for issue := range res.Issues() {
		switch issue.Code() {
		case unknownCode:
			for _, d := range issue.Details() {
				if d.Key == diag.DetailKeyField {
					delete(out, d.Value)
				}
			}
		case instance.ErrCaseFoldCollision:
			msg := issue.Message()
			open, end := strings.Index(msg, "["), strings.Index(msg, "]")
			for k := range strings.FieldsSeq(msg[open+1 : end]) {
				delete(out, k)
			}
		}
	}
	return out
}

func (o oracle) resolvedNodeKeys(ctx context.Context, keys []string) map[string]bool {
	obj := make(map[string]any, len(keys))
	for _, k := range keys {
		obj[k] = map[string]any{}
	}
	return o.resolved(ctx, obj, keys, instance.ErrUnknownField)
}

func (o oracle) resolvedEdgeKeys(ctx context.Context, rel *schema.Relation, keys []string) map[string]bool {
	target := make(map[string]any, len(keys))
	for _, k := range keys {
		target[k] = "s"
	}
	var value any = target
	if rel.IsMany() {
		value = []any{target}
	}
	return o.resolved(ctx, map[string]any{"id": "p1", "name": "n", rel.FieldName(): value}, keys, instance.ErrUnknownEdgeField)
}

// fold is the ASCII fold, or "" under strict names or for a name holding
// another byte.
func (o oracle) fold(name string) string {
	if o.strict {
		return ""
	}
	lower, ok := instance.FoldKey(name)
	if !ok {
		return ""
	}
	return lower
}

// nodeMember is the member name spells exactly or folds onto.
func (o oracle) nodeMember(name string) (*schema.Property, *schema.Relation) {
	if p, ok := o.person.Property(name); ok {
		return p, nil
	}
	if r, ok := o.person.RelationByField(name); ok {
		return nil, r
	}
	lower := o.fold(name)
	if lower == "" {
		return nil, nil
	}
	if canonical, ok := o.person.CanonicalPropertyName(lower); ok {
		p, _ := o.person.Property(canonical)
		return p, nil
	}
	if r, ok := o.person.RelationByField(lower); ok {
		return nil, r
	}
	return nil, nil
}

// edgeMemberOf is what a suffix names: a key component (known only with
// WithSchema) or an edge property, and whether the suffix spells it exactly.
// openKey reports a key component whose target is out of reach.
type edgeMemberOf struct {
	key, prop *schema.Property
	exact     bool
	openKey   bool
}

func (o oracle) edgeMember(rel *schema.Relation, suffix string) edgeMemberOf {
	lower := o.fold(suffix)
	if strings.HasPrefix(suffix, keyPrefixForTest) || strings.HasPrefix(lower, keyPrefixForTest) {
		if !o.withSchema {
			return edgeMemberOf{openKey: true}
		}
		target, _ := o.s.TypeByID(rel.TargetID())
		for pk := range target.PrimaryKeys() {
			field := keyPrefixForTest + pk.Name()
			if field == suffix {
				return edgeMemberOf{key: pk, exact: true}
			}
			if lower != "" && strings.ToLower(field) == lower {
				return edgeMemberOf{key: pk}
			}
		}
		return edgeMemberOf{}
	}
	if p, ok := rel.Property(suffix); ok {
		return edgeMemberOf{prop: p, exact: true}
	}
	if lower != "" {
		if p, ok := rel.PropertyFold(lower); ok {
			return edgeMemberOf{prop: p}
		}
	}
	return edgeMemberOf{}
}

const keyPrefixForTest = "_target_"

func (m edgeMemberOf) member() *schema.Property {
	if m.key != nil {
		return m.key
	}
	return m.prop
}

// literal is the JSON a claimed cell states for its member, and false where the
// text does not read as that member's kind, which the parser reports as
// E_CSV_COERCE and carries as text. An Integer cell reads as one exactly when
// encoding/json decodes the text, with nothing around it, into an int64: a
// float literal is never an Integer, and null is no number. A Date key component
// reads when its layout parses; every other member the generated schema
// declares is a String, whose text always reads.
func literal(c schema.Constraint, text string) (string, bool) {
	switch schema.ResolveAlias(c).Kind() {
	case schema.KindInteger:
		var n int64
		if strings.TrimSpace(text) == text && text != "null" && json.Unmarshal([]byte(text), &n) == nil {
			return text, true
		}
		return quoted(text), false
	case schema.KindDate:
		_, err := time.Parse("2006-01-02", text)
		return quoted(text), err == nil
	default:
		return quoted(text), true
	}
}

// emptyProperty is the JSON an empty cell states for a property: null for an
// optional property or a kind with no empty value, "" for a required String.
func emptyProperty(p *schema.Property) string {
	if !p.IsOptional() && schema.ResolveAlias(p.Constraint()).Kind() == schema.KindString {
		return `""`
	}
	return "null"
}

// emptyEdge is what an empty segment states for an edge member, or "" where it
// is absent: "" for a String key component or a required String edge property.
func (o oracle) emptyEdge(m edgeMemberOf) string {
	switch {
	case m.key != nil && o.withSchema && schema.ResolveAlias(m.key.Constraint()).Kind() == schema.KindString:
		return `""`
	case m.prop != nil && !m.prop.IsOptional() && schema.ResolveAlias(m.prop.Constraint()).Kind() == schema.KindString:
		return `""`
	}
	return ""
}

func (o oracle) document(ctx context.Context, header, row []string) (string, []string) {
	var csvOnly []string
	plainAt := map[string]int{}
	var fields []string
	groupCols := map[string][]int{}
	for i, name := range header {
		field, suffix, dotted := strings.Cut(name, ".")
		_ = suffix
		if !dotted {
			plainAt[name] = i
			continue
		}
		if _, seen := groupCols[field]; !seen {
			fields = append(fields, field)
		}
		groupCols[field] = append(groupCols[field], i)
	}
	present := map[string]bool{}
	for _, f := range fields {
		for _, i := range groupCols[f] {
			present[f] = present[f] || row[i] != ""
		}
	}
	dropped := map[string]bool{}
	keysOf := func() []string {
		keys := []string{"id"}
		for name, i := range plainAt {
			if p, _ := o.nodeMember(name); row[i] != "" || p != nil && p.Name() == name {
				keys = append(keys, name)
			}
		}
		for _, f := range fields {
			if present[f] && !dropped[f] {
				keys = append(keys, f)
			}
		}
		return keys
	}
	association := func(res map[string]bool, f string) *schema.Relation {
		if !res[f] {
			return nil
		}
		if _, r := o.nodeMember(f); r != nil && !r.IsComposition() {
			return r
		}
		return nil
	}

	var res map[string]bool
	for {
		res = o.resolvedNodeKeys(ctx, keysOf())
		changed := false
		for _, f := range fields {
			if !present[f] || dropped[f] {
				continue
			}
			if rel := association(res, f); rel != nil {
				if _, clash, _, _ := o.shape(rel, header, row, groupCols[f]); clash {
					o.count("clash")
					csvOnly = append(csvOnly, "E_ADAPTER_PARSE")
					dropped[f], changed = true, true
				}
			}
		}
		if !changed {
			break
		}
	}
	keys := keysOf()
	folded := map[string]bool{}
	for _, k := range keys {
		if l := o.fold(k); l != "" {
			folded[l] = true
		}
	}
	candidates := map[string][]string{}
	for name, i := range plainAt {
		p, _ := o.nodeMember(name)
		if row[i] != "" || p == nil || p.Name() == name || folded[strings.ToLower(p.Name())] {
			continue
		}
		candidates[p.Name()] = append(candidates[p.Name()], name)
	}
	for _, names := range candidates {
		if len(names) == 1 {
			o.count("empty folded column placed")
			keys = append(keys, names[0])
		}
	}
	res = o.resolvedNodeKeys(ctx, keys)
	for _, k := range keys {
		p, r := o.nodeMember(k)
		switch {
		case (p != nil || r != nil) && !res[k]:
			o.count("member key shadowed or colliding")
		case p != nil && p.Name() != k || r != nil && r.FieldName() != k:
			o.count("folded key claims")
		}
	}
	inKeys := map[string]bool{}
	for _, k := range keys {
		inKeys[k] = true
	}

	members := []jsonMember{{"id", `"p1"`}}
	written := map[string]bool{}
	for _, f := range fields {
		if !present[f] || dropped[f] {
			continue
		}
		written[f] = true
		var value string
		rel := association(res, f)
		if rel == nil {
			o.count("group carried as text")
		}
		if rel != nil {
			var more []string
			value, more = o.edgeGroup(ctx, rel, header, row, groupCols[f])
			csvOnly = append(csvOnly, more...)
		} else {
			var obj []jsonMember
			for _, i := range groupCols[f] {
				if row[i] != "" {
					_, suffix, _ := strings.Cut(header[i], ".")
					obj = append(obj, jsonMember{suffix, quoted(row[i])})
				}
			}
			value = jsonObject(obj)
		}
		i, hasPlain := plainAt[f]
		switch {
		case !hasPlain || row[i] == "":
			members = append(members, jsonMember{f, value})
		case rel != nil:
			o.count("two writers, group kept")
			members = append(members, jsonMember{f, quoted(row[i])}, jsonMember{f, value})
		default:
			o.count("two writers, plain kept")
			plainRaw, _ := o.plainValue(res, f, row[i], &csvOnly)
			members = append(members, jsonMember{f, value}, jsonMember{f, plainRaw})
		}
	}
	for _, name := range header {
		i, ok := plainAt[name]
		if !ok || written[name] || !inKeys[name] {
			continue
		}
		raw, ok := o.plainValue(res, name, row[i], &csvOnly)
		if ok {
			members = append(members, jsonMember{name, raw})
		}
	}
	return jsonObject(members), csvOnly
}

// plainValue is what a plain column's cell states under the row's claims.
func (o oracle) plainValue(res map[string]bool, name, cell string, csvOnly *[]string) (string, bool) {
	p, _ := o.nodeMember(name)
	if p == nil || !res[name] {
		return quoted(cell), cell != ""
	}
	if cell == "" {
		return emptyProperty(p), true
	}
	raw, ok := literal(p.Constraint(), cell)
	if !ok {
		*csvOnly = append(*csvOnly, "E_CSV_COERCE")
	}
	return raw, true
}

// shape is the group's target count and each column's segments; clash reports
// two member columns disagreeing on the count.
func (o oracle) shape(rel *schema.Relation, header, row []string, cols []int) (int, bool, map[int][]string, map[int]bool) {
	n := -1
	clash := false
	segs := map[int][]string{}
	deciding := map[int]bool{}
	// A column decides the count where no other spelling of its member can: its
	// exact spelling, or its one folded spelling where none is exact. Without
	// WithSchema no key spelling is known, so every key column decides.
	exact, folded := map[*schema.Property]int{}, map[*schema.Property]int{}
	for _, i := range cols {
		_, suffix, _ := strings.Cut(header[i], ".")
		if m := o.edgeMember(rel, suffix); m.member() != nil && m.exact {
			exact[m.member()]++
		} else if m.member() != nil {
			folded[m.member()]++
		}
	}
	for _, i := range cols {
		if row[i] == "" {
			continue
		}
		_, suffix, _ := strings.Cut(header[i], ".")
		segs[i] = strings.Split(row[i], "|")
		m := o.edgeMember(rel, suffix)
		deciding[i] = m.openKey || m.member() != nil && (m.exact || exact[m.member()] == 0 && folded[m.member()] == 1)
		if !deciding[i] {
			continue
		}
		if n == -1 {
			n = len(segs[i])
		} else if len(segs[i]) != n {
			clash = true
		}
	}
	if n == -1 {
		n = 1
	}
	return n, clash, segs, deciding
}

func (o oracle) edgeGroup(ctx context.Context, rel *schema.Relation, header, row []string, cols []int) (string, []string) {
	var csvOnly []string
	n, _, segs, deciding := o.shape(rel, header, row, cols)
	seg := func(i, t int) (string, bool) {
		s, ok := segs[i]
		if !ok {
			return "", true
		}
		switch {
		case len(s) == n:
			return s[t], true
		case deciding[i]:
			panic("a deciding column's count differs without a clash")
		case n == 1:
			return row[i], true
		}
		return "", false
	}
	for _, i := range cols {
		if _, ok := seg(i, 0); !ok {
			o.count("stray column")
			csvOnly = append(csvOnly, "E_ADAPTER_PARSE")
		}
	}
	targets := make([]string, n)
	for t := range n {
		var keys []string
		for _, i := range cols {
			_, suffix, _ := strings.Cut(header[i], ".")
			v, ok := seg(i, t)
			m := o.edgeMember(rel, suffix)
			if ok && (v != "" || m.exact && o.emptyEdge(m) != "") {
				keys = append(keys, suffix)
			}
		}
		folded := map[string]bool{}
		for _, k := range keys {
			if l := o.fold(k); l != "" {
				folded[l] = true
			}
		}
		candidates := map[*schema.Property][]string{}
		for _, i := range cols {
			_, suffix, _ := strings.Cut(header[i], ".")
			v, ok := seg(i, t)
			m := o.edgeMember(rel, suffix)
			if !ok || v != "" || m.exact || m.member() == nil || o.emptyEdge(m) == "" {
				continue
			}
			name := strings.ToLower(m.member().Name())
			if m.key != nil {
				name = strings.ToLower(keyPrefixForTest + m.key.Name())
			}
			if folded[name] {
				continue
			}
			candidates[m.member()] = append(candidates[m.member()], suffix)
		}
		for _, suffixes := range candidates {
			if len(suffixes) == 1 {
				o.count("empty folded suffix placed")
				keys = append(keys, suffixes[0])
			}
		}
		inKeys := map[string]bool{}
		for _, k := range keys {
			inKeys[k] = true
		}
		res := o.resolvedEdgeKeys(ctx, rel, keys)
		var obj []jsonMember
		for _, i := range cols {
			_, suffix, _ := strings.Cut(header[i], ".")
			v, ok := seg(i, t)
			if !ok || !inKeys[suffix] {
				continue
			}
			m := o.edgeMember(rel, suffix)
			claimed := res[suffix] && (m.prop != nil || m.key != nil && o.withSchema)
			if m.member() != nil && !res[suffix] {
				o.count("edge key shadowed or colliding")
			}
			switch {
			case !claimed:
				if v != "" {
					obj = append(obj, jsonMember{suffix, quoted(v)})
				}
			case v == "":
				if e := o.emptyEdge(m); e != "" {
					obj = append(obj, jsonMember{suffix, e})
				}
			default:
				raw, ok := literal(m.member().Constraint(), v)
				if !ok {
					csvOnly = append(csvOnly, "E_CSV_COERCE")
				}
				obj = append(obj, jsonMember{suffix, raw})
			}
		}
		targets[t] = jsonObject(obj)
	}
	if !rel.IsMany() && n == 1 {
		return targets[0], csvOnly
	}
	return "[" + strings.Join(targets, ",") + "]", csvOnly
}

package snapshot_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// revalidationFindings returns the code and the validator's own message of
// each issue res holds from the revalidation of the root named by prefix.
func revalidationFindings(res diag.Result, prefix string) []string {
	var out []string
	for issue := range res.Issues() {
		if msg, ok := strings.CutPrefix(issue.Message(), prefix); ok {
			out = append(out, issue.Code().String()+": "+msg)
		}
	}
	return out
}

// The warning belongs to a record no check refused, so a refused record's break
// is one issue under the option too. A refused root is not a refused record: a
// sound record stays reported, at that root and under its other associations.
func TestLoad_ARefusedRecordDrawsNoRequiredWarning(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	withKey := strings.Replace(sbAbsent, `"target_key":null`, `"target_key":["c1"]`, 1)
	two := sbLost("p2", "EMPLOYER", 1, `["c8"]`, `{"since":8888}`) + `,` + sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`)
	for name, c := range map[string]struct {
		edit       func(string) string
		wantClient int
		wantEmploy int
	}{
		"a refused record, beside a sound one at its root": {
			edit:       func(d string) string { return strings.Replace(d, sbAbsent, withKey, 1) },
			wantClient: 1,
		},
		"two sound records at a root their count refuses": {
			edit:       func(d string) string { return strings.Replace(d, sbAbsent, two, 1) },
			wantClient: 1, wantEmploy: 2,
		},
		"two sound records at a root whose stored key is refused": {
			edit: func(d string) string {
				return strings.Replace(d, sbP2, strings.Replace(sbP2, `"key":["p2"]`, `"key":["px"]`, 1), 1)
			},
			wantClient: 1, wantEmploy: 1,
		},
		"two sound records in a document whose walk accepts no root": {
			edit: func(d string) string {
				return regexp.MustCompile(`"key":\[[^\]]*\],"properties":\{`).ReplaceAllString(d, `$0"ghost":1,`)
			},
			wantClient: 1, wantEmploy: 1,
		},
	} {
		for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
			t.Run(name+"/"+sev.String(), func(t *testing.T) {
				t.Parallel()
				_, res := snapshot.Load(t.Context(), []byte(c.edit(doc)), s,
					snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
				client, employ := 0, 0
				for issue := range res.Issues() {
					if issue.Code() != diag.W_SNAPSHOT_UNRESOLVED_REQUIRED {
						continue
					}
					switch {
					case strings.Contains(issue.Message(), `"CLIENTS"`):
						client++
					case strings.Contains(issue.Message(), `"EMPLOYER"`):
						employ++
					}
				}
				if client != c.wantClient || employ != c.wantEmploy {
					t.Errorf("%d warnings under CLIENTS and %d under EMPLOYER, want %d and %d:\n%s", client, employ, c.wantClient, c.wantEmploy, res)
				}
			})
		}
	}
}

// The warnings precede every root's findings, so a capped read of warnings and
// findings alike keeps the warnings.
func TestLoad_RequiredWarningsPrecedeARootsFindings(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	bad := strings.Replace(doc, sbP2, strings.Replace(sbP2, `"id":"p2"`, `"age":999,"id":"p2"`, 1), 1)
	if bad == doc {
		t.Fatal("the edit did not apply")
	}
	_, res := snapshot.Load(t.Context(), []byte(bad), s, snapshot.WithIssueLimit(2), snapshot.WithRevalidation(diag.Warning))
	warned := 0
	for issue := range res.Issues() {
		if issue.Code() == diag.W_SNAPSHOT_UNRESOLVED_REQUIRED {
			warned++
		}
	}
	if warned != 2 {
		t.Errorf("%d warnings kept under a cap of 2, want 2:\n%s", warned, res)
	}
	_, whole := snapshot.Load(t.Context(), []byte(bad), s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(diag.Warning))
	if !whole.HasCode(diag.E_CONSTRAINT_FAIL) {
		t.Errorf("no finding for the value the cap dropped:\n%s", whole)
	}
}

// The warning names its relation in a detail and cuts a source key the document
// wrote at any length.
func TestLoad_RequiredWarningCutsASourceKeyAndNamesItsRelation(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	long := strings.Repeat("k", 80)
	_, res := snapshot.Load(t.Context(), []byte(strings.ReplaceAll(doc, `"p2"`, `"`+long+`"`)), s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(diag.Warning))
	relations := map[string]bool{}
	for issue := range res.Issues() {
		if issue.Code() != diag.W_SNAPSHOT_UNRESOLVED_REQUIRED {
			continue
		}
		if strings.Contains(issue.Message(), long) {
			t.Errorf("the warning repeats the key whole: %s", issue.Message())
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyRelationName {
				relations[d.Value] = true
			}
		}
	}
	if !relations["EMPLOYER"] || !relations["CLIENTS"] {
		t.Errorf("the warnings' relation details are %v, want EMPLOYER and CLIENTS:\n%s", relations, res)
	}
}

// A target_missing record is an edge whose target the graph does not hold, so
// the validator is the oracle: revalidation reports for the record what the
// validator reports for the raw edge the record states.
func TestLoad_RevalidationJudgesARecordAsTheValidatorJudgesItsEdge(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	v := instance.NewValidator(s)
	for name, c := range map[string]struct {
		from, to string
		root     string
		raw      map[string]any
	}{
		"under a (one) association": {
			from: sbAbsent, to: sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`), root: "p2",
			raw: map[string]any{"id": "p2", "CLIENTS": []any{}, "EMPLOYER": map[string]any{"_target_id": "c9", "since": json.Number("9999")}},
		},
		"under a (many) association": {
			from: sbKnowsLost, to: strings.Replace(sbKnowsLost, `{"note":"m"}`, `{"note":7}`, 1), root: "p2",
			raw: map[string]any{"id": "p2", "CLIENTS": []any{}, "KNOWS": []any{map[string]any{"_target_id": "p8", "note": json.Number("7")}}},
		},
		"the second of two records under a (many) association": {
			from: sbKnowsLost, to: sbKnowsLost + `,` + sbLost("p2", "KNOWS", 4, `["p7"]`, `{"note":7}`), root: "p2",
			raw: map[string]any{"id": "p2", "CLIENTS": []any{}, "KNOWS": []any{
				map[string]any{"_target_id": "p8", "note": "m"},
				map[string]any{"_target_id": "p7", "note": json.Number("7")},
			}},
		},
		"beside an edge of the association": {
			from: sbUnresolved, to: sbUnresolved + sbLost("p1", "KNOWS", 4, `["p8"]`, `{"note":7}`) + `,`, root: "p1",
			raw: map[string]any{"id": "p1", "KNOWS": []any{
				map[string]any{"_target_id": "p2", "note": "n"},
				map[string]any{"_target_id": "p8", "note": json.Number("7")},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, vres := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: c.raw})
			var want []string
			for issue := range vres.Issues() {
				want = append(want, issue.Code().String()+": "+issue.Message())
			}
			if len(want) != 1 {
				t.Fatalf("the validator reports %d issues for the raw edge, want 1: %s", len(want), vres)
			}
			edited := []byte(editOnce(t, doc, c.from, c.to))
			prefix := `revalidation of sb#Person[["` + c.root + `"]]: `
			for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
				_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
				got := revalidationFindings(res, prefix)
				if len(got) != 1 || got[0] != want[0] {
					t.Errorf("revalidation at %s reports %q, the validator %q:\n%s", sev, got, want, res)
				}
				if res.HasErrors() != (sev == diag.Error) {
					t.Errorf("revalidation at %s: HasErrors = %v:\n%s", sev, res.HasErrors(), res)
				}
			}
			if _, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0)); !res.OK() {
				t.Errorf("a load without revalidation judges no value: %s", res)
			}
		})
	}
}

// A record a check refuses is no edge the validator is given: a
// target_missing record that names a root draws its record fact alone, at every
// revalidation severity, and its values are judged no further.
func TestLoad_RevalidationJudgesNoRecordACheckRefuses(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for name, c := range map[string]struct{ from, to string }{
		"under a (one) association": {from: sbAbsent, to: sbLost("p2", "EMPLOYER", 1, `["c1"]`, `{"since":9999}`)},
		"under a (many) association": {
			from: sbKnowsLost, to: strings.Replace(strings.Replace(sbKnowsLost, `{"note":"m"}`, `{"note":7}`, 1), `["p8"]`, `["p1"]`, 1),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, doc, c.from, c.to))
			for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
				_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
				if got := revalidationFindings(res, `revalidation of sb#Person[["p2"]]: `); len(got) != 0 {
					t.Errorf("revalidation at %s judged a refused record: %q:\n%s", sev, got, res)
				}
				found := false
				for issue := range res.Issues() {
					found = found || issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), "which a root holds")
				}
				if !found {
					t.Errorf("revalidation at %s: no record fact naming the root:\n%s", sev, res)
				}
			}
		})
	}
}

const gatedSchema = `schema "gr"

type B {
	id String primary
}

type A {
	id String primary
	--> KNOWS (one:many) B {
		since Integer[0, 100]
	}
	--> BEST (_:one) B {
		since Integer[0, 100]
	}
	! "knows nonempty" KNOWS -> Len > 0
}
`

// gatedDoc is gatedHashedDoc with its integrity hash blanked.
func gatedDoc(t *testing.T) (*schema.Schema, string) {
	t.Helper()
	s, doc := gatedHashedDoc(t)
	return s, sbIntegrity.ReplaceAllString(doc, `"integrity_hash":""`)
}

// gatedHashedDoc is a document Graph.Add built under gatedSchema: B b1, and A
// a1 whose one KNOWS edge and one BEST edge name b9, which no root holds, so
// each is a target_missing record.
func gatedHashedDoc(t *testing.T) (*schema.Schema, string) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), gatedSchema, "gr.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, c := range []struct {
		typ   string
		props map[string]any
	}{
		{"B", map[string]any{"id": "b1"}},
		{"A", map[string]any{
			"id": "a1", "KNOWS": []any{map[string]any{"_target_id": "b9", "since": 1}},
			"BEST": map[string]any{"_target_id": "b9", "since": 1},
		}},
	} {
		vi, res := v.ValidateOne(t.Context(), c.typ, instance.RawInstance{Properties: c.props})
		if res.HasErrors() {
			t.Fatalf("validate %s: %s", c.typ, res)
		}
		if res := g.Add(t.Context(), vi); res.HasErrors() {
			t.Fatalf("add %s: %s", c.typ, res)
		}
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres)
	}
	return s, string(data)
}

// A root one of whose units a structural check refused is not run through the
// validator: its records, its required associations and its (one) associations
// are its units as its tree is, so the refusal is the one issue, whatever the
// validator would read of what is left, and whatever order the records stand in.
func TestLoad_RevalidationRunsNoRootACheckRefusedAUnitOf(t *testing.T) {
	t.Parallel()
	s, doc := gatedDoc(t)
	knows := `"relation":"KNOWS","target_type":1,"target_key":["b9"],"required":true,"reason":"target_missing","properties":{"since":1}`
	best := `"relation":"BEST","target_type":1,"target_key":["b9"],"required":false,"reason":"target_missing","properties":{"since":1}`
	for name, c := range map[string]struct {
		from, to string
		code     diag.Code
		phrase   string
	}{
		"a record its fact refuses, under an association an invariant reads": {
			from: knows, to: strings.Replace(knows, `["b9"]`, `["b1"]`, 1),
			code: diag.E_SNAPSHOT_MALFORMED, phrase: "which a root holds",
		},
		"a record its own check refuses, under an association an invariant reads": {
			from: knows, to: strings.Replace(knows, `{"since":1}`, `{"ghost":1,"since":1}`, 1),
			code: diag.E_SNAPSHOT_MALFORMED, phrase: `"ghost"`,
		},
		"a required association holding no record, which an invariant reads": {
			from: `,{"source_type":0,"source_key":["a1"],` + knows + `}`, to: "",
			code: diag.E_SNAPSHOT_MALFORMED, phrase: `under the required association "KNOWS"`,
		},
		"a record whose source row is absent, under an association an invariant reads": {
			from: `{"source_type":0,"source_key":["a1"],` + knows, to: `{"source_type":null,"source_key":["a1"],` + knows,
			code: diag.E_SNAPSHOT_MALFORMED, phrase: "carries no types-table row",
		},
		"a record whose source row the table does not hold, under an association an invariant reads": {
			from: `{"source_type":0,"source_key":["a1"],` + knows, to: `{"source_type":99,"source_key":["a1"],` + knows,
			code: diag.E_SNAPSHOT_MALFORMED, phrase: "references types table row 99",
		},
		"a (one) association holding two records, the first outside its bounds": {
			from: best, to: strings.Replace(best, `{"since":1}`, `{"since":500}`, 1) + `},{"source_type":0,"source_key":["a1"],` + best,
			code: diag.E_GRAPH_CARDINALITY, phrase: `(one) association "BEST"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, doc, c.from, c.to))
			for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
				for _, read := range []struct {
					name string
					res  func() diag.Result
				}{
					{"Load", func() diag.Result {
						_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
						return res
					}},
					{"Verify", func() diag.Result {
						return snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
					}},
				} {
					res := read.res()
					if got := revalidationFindings(res, `revalidation of gr#A[["a1"]]: `); len(got) != 0 {
						t.Errorf("%s at %s ran a root a check refused a unit of: %q:\n%s", read.name, sev, got, res)
					}
					found := false
					for issue := range res.Issues() {
						found = found || issue.Code() == c.code && strings.Contains(issue.Message(), c.phrase)
					}
					if !found {
						t.Errorf("%s at %s: no %s naming %q:\n%s", read.name, sev, c.code, c.phrase, res)
					}
				}
			}
		})
	}
}

// At an address two roots stand at, a record names the address and not a
// root, so neither root's records are known and neither is run: the duplicate
// is the one issue, whatever the validator would read of either root or of a
// record there.
func TestLoad_RevalidationRunsNoRootAtAnAddressAnotherRootStandsAt(t *testing.T) {
	t.Parallel()
	s, doc := gatedDoc(t)
	holder := `{"key":["a1"],"properties":{"id":"a1"},"provenance":null}`
	best := `{"target_type":1,"target_key":["b1"],"properties":{"since":1}}`
	bestRecord := `"relation":"BEST","target_type":1,"target_key":["b9"],"required":false,"reason":"target_missing","properties":{"since":1}`
	knowsRecord := `"relation":"KNOWS","target_type":1,"target_key":["b9"],"required":true,"reason":"target_missing","properties":{"since":1}`
	for name, edits := range map[string][][2]string{
		"a duplicate whose (one) association holds two edges, beside a record outside its bounds": {
			{holder, holder + `,{"key":["a1"],"properties":{"id":"a1"},"edges":{"BEST":[` + best + `,` + best + `]},"provenance":null}`},
			{knowsRecord, strings.Replace(knowsRecord, `{"since":1}`, `{"since":500}`, 1)},
		},
		"a (one) record outside its bounds at the address": {
			{holder, holder + `,` + holder},
			{bestRecord, strings.Replace(bestRecord, `{"since":1}`, `{"since":500}`, 1)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
				_, res := snapshot.Load(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
				if got := revalidationFindings(res, `revalidation of gr#A[["a1"]]: `); len(got) != 0 {
					t.Errorf("revalidation at %s ran a root at a shared address: %q:\n%s", sev, got, res)
				}
				found := false
				for issue := range res.Issues() {
					found = found || issue.Code() == diag.E_DUPLICATE_PK
				}
				if !found {
					t.Errorf("revalidation at %s: no %s:\n%s", sev, diag.E_DUPLICATE_PK, res)
				}
			}
		})
	}
}

// The option's findings follow every structural issue, the integrity hash's
// among them, so a capped collector keeps the mismatch over a value the edit
// put outside its bounds.
func TestLoad_RevalidationFindingsFollowTheIntegrityCheck(t *testing.T) {
	t.Parallel()
	s, doc := gatedHashedDoc(t)
	knows := `"relation":"KNOWS","target_type":1,"target_key":["b9"],"required":true,"reason":"target_missing","properties":{"since":1}`
	edited := []byte(editOnce(t, doc, knows, strings.Replace(knows, `{"since":1}`, `{"since":500}`, 1)))
	for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
		_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(1), snapshot.WithRevalidation(sev))
		var got []diag.Code
		for issue := range res.Issues() {
			got = append(got, issue.Code())
		}
		if len(got) != 1 || got[0] != diag.E_SNAPSHOT_INTEGRITY_MISMATCH {
			t.Errorf("a cap of 1 at %s keeps %v, want the integrity mismatch:\n%s", sev, got, res)
		}
	}
}

// The option's findings follow every structural issue, its record warnings
// among them, so a capped collector keeps the structural break.
func TestLoad_RevalidationFindingsFollowEveryStructuralIssue(t *testing.T) {
	t.Parallel()
	s, doc := gatedDoc(t)
	knows := `"relation":"KNOWS","target_type":1,"target_key":["b9"]`
	edited := []byte(editOnce(t, doc, knows, strings.Replace(knows, `["b9"]`, `["b1"]`, 1)))
	for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
		_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(1), snapshot.WithRevalidation(sev))
		var got []string
		for issue := range res.Issues() {
			got = append(got, issue.Code().String()+": "+issue.Message())
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], "E_SNAPSHOT_MALFORMED: ") || !strings.Contains(got[0], "which a root holds") {
			t.Errorf("a cap of 1 at %s keeps %q, want the record fact", sev, got)
		}
	}
}

const recordSourceSchema = `schema "rs"

type Event {
	at Timestamp primary
	--> NEXT (_:one) Event {
		weight Integer[0, 10]
	}
}
`

// A record reaches its root by the root's canonical address, as every other
// position that addresses an instance does: each document below spells one
// instant in two offset forms, at the root's stored key and at the record's
// source key.
func TestLoad_RevalidationFindsARecordByItsCanonicalSourceKey(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), recordSourceSchema, "rs.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "Event", instance.RawInstance{Properties: map[string]any{
		"at":   "2024-01-01T00:00:00Z",
		"NEXT": map[string]any{"_target_at": "2030-01-01T00:00:00Z", "weight": 5},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	if res := g.Add(t.Context(), vi); res.HasErrors() {
		t.Fatalf("add: %s", res)
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	for name, respell := range map[string]string{
		"the record spelled otherwise": `"source_key":["2024-01-01T00:00:00Z"]`,
		"the root spelled otherwise":   `{"key":["2024-01-01T00:00:00Z"]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := editOnce(t, string(data), `"weight":5`, `"weight":50`)
			doc = editOnce(t, doc, respell, strings.Replace(respell, "00Z", "00+00:00", 1))

			opts := []snapshot.LoadOption{snapshot.WithIntegrityCheck(false), snapshot.WithIssueLimit(0)}
			if _, res := snapshot.Load(t.Context(), []byte(doc), s, opts...); !res.OK() {
				t.Fatalf("the document is structurally sound, and a load without revalidation reports: %s", res)
			}
			_, res := snapshot.Load(t.Context(), []byte(doc), s, append(opts, snapshot.WithRevalidation(diag.Error))...)
			got := revalidationFindings(res, "revalidation of rs#Event[[")
			if len(got) != 1 || !strings.Contains(got[0], "E_CONSTRAINT_FAIL: ") || res.Len() != 1 {
				t.Errorf("revalidation reports %q, want one E_CONSTRAINT_FAIL for the record's weight:\n%s", got, res)
			}
		})
	}
}

// Revalidation rebuilds a root's whole composed tree, so a value two levels
// down, in a Box inside a Box, is judged as the validator judges it there.
func TestLoad_RevalidationJudgesAGrandchildsValues(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := []byte(editOnce(t, doc, sbInnerBox, strings.Replace(sbInnerBox, `"n":2`, `"n":"x"`, 1)))
	if _, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0)); !res.OK() {
		t.Fatalf("a load without revalidation judges no value: %s", res)
	}
	_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(diag.Error))
	got := revalidationFindings(res, `revalidation of sb#Order[["o1"]]: `)
	if len(got) != 1 || !strings.HasPrefix(got[0], "E_TYPE_MISMATCH: ") || !strings.Contains(got[0], `"n"`) {
		t.Errorf("revalidation reports %q, want one E_TYPE_MISMATCH for the inner Box's n:\n%s", got, res)
	}
}

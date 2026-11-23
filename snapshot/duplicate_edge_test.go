package snapshot_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const duplicateEdgeSchema = `schema "de"

type U {
	id String primary
}

type K {
	id UUID primary
}

type T {
	id String primary
	--> TO_U (_:many) U { w Integer }
	--> TO_K (_:many) K
	--> ONE_U (_:one) U
}
`

const (
	keyA      = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	keyAUpper = "6BA7B810-9DAD-11D1-80B4-00C04FD430C8"
	keyB      = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
)

// duplicateEdgeDoc marshals a graph whose root t1 holds edges to u1 and u3 and
// target_missing records naming u8 and u9 under TO_U, edges to keyA and keyB
// under TO_K, and an edge to u1 under ONE_U.
func duplicateEdgeDoc(t *testing.T) (schema.Schema, []byte) {
	t.Helper()
	ctx := t.Context()
	s, err := schema.LoadString(ctx, duplicateEdgeSchema, "de.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, raw := range []struct {
		typ   string
		props map[string]any
	}{
		{"U", map[string]any{"id": "u1"}},
		{"U", map[string]any{"id": "u3"}},
		{"K", map[string]any{"id": keyA}},
		{"K", map[string]any{"id": keyB}},
		{"T", map[string]any{
			"id": "t1",
			"to_u": []any{
				map[string]any{"_target_id": "u1", "w": int64(1)},
				map[string]any{"_target_id": "u3", "w": int64(3)},
				map[string]any{"_target_id": "u8"},
				map[string]any{"_target_id": "u9"},
			},
			"to_k":  []any{map[string]any{"_target_id": keyA}, map[string]any{"_target_id": keyB}},
			"one_u": map[string]any{"_target_id": "u1"},
		}},
	} {
		vi, err := v.ValidateOne(ctx, raw.typ, instance.RawInstance{Properties: raw.props})
		if err != nil {
			t.Fatalf("validate %s: %s", raw.typ, err)
		}
		if err := g.Add(ctx, vi); err != nil {
			t.Fatalf("add %s: %s", raw.typ, err)
		}
	}
	doc, readErr := snapshot.Marshal(ctx, g.Snapshot())
	if readErr != nil {
		t.Fatalf("marshal: %s", readErr)
	}
	return s, doc
}

// A document that names one target twice under one (many) association of a
// root draws one Error E_SNAPSHOT_MALFORMED for each repeat, an edge's or a
// target_missing record's, and no snapshot. Revalidation adds nothing: the root is
// refused. A repeat whose target or source no sound root holds alone is that
// address's break, and a (one) association's repeat is its count's break.
func TestRead_ATargetNamedTwiceUnderOneAssociationIsRefused(t *testing.T) {
	t.Parallel()
	s, doc := duplicateEdgeDoc(t)
	const (
		oneU   = `"ONE_U":[{"target_type":2,"target_key":["u1"],"properties":{}}]`
		edgeU1 = `"target_key":["u1"],"properties":{"w":1}`
		edgeU3 = `"target_key":["u3"]`
		lostU8 = `"target_key":["u8"]`
		lostU9 = `"target_key":["u9"]`
		rootU3 = `{"key":["u3"],"properties":{"id":"u3"}`
		rootT1 = `{"key":["t1"],"properties":{"id":"t1"}`
	)
	storesU3 := diag.E_SNAPSHOT_MALFORMED.String() + `: instance of de#U stores key ["u3"]`
	for _, c := range []struct {
		name  string
		edits [][2]string
		want  []string
	}{
		{name: "untouched"},
		{
			name:  "an edge",
			edits: [][2]string{{edgeU3, `"target_key":["u1"]`}},
			want:  []string{diag.E_SNAPSHOT_MALFORMED.String() + `: edge 1 under "TO_U" of de#T[["t1"]] names target de#U[["u1"]], which an edge before it names`},
		},
		{
			name:  "an edge under the key an edge before it names",
			edits: [][2]string{{`"target_key":["` + keyB + `"]`, `"target_key":["` + keyA + `"]`}},
			want:  []string{diag.E_SNAPSHOT_MALFORMED.String() + `: edge 1 under "TO_K" of de#T[["t1"]] names target de#K[`},
		},
		{
			name:  "a record",
			edits: [][2]string{{lostU8, lostU9}},
			want:  []string{diag.E_SNAPSHOT_MALFORMED.String() + `: unresolved record 1 names target de#U[["u9"]] under "TO_U" of de#T[["t1"]], which a record before it names`},
		},
		{
			name:  "a (one) association",
			edits: [][2]string{{oneU, `"ONE_U":[{"target_type":2,"target_key":["u1"],"properties":{}},{"target_type":2,"target_key":["u1"],"properties":{}}]`}},
			want:  []string{diag.E_GRAPH_CARDINALITY.String() + `: (one) association "ONE_U" of de#T[["t1"]] holds 2 records`},
		},
		{
			name:  "two edges to a root whose key its key property contradicts",
			edits: [][2]string{{rootU3, `{"key":["u3"],"properties":{"id":"u4"}`}, {edgeU1, `"target_key":["u3"],"properties":{"w":1}`}},
			want:  []string{storesU3},
		},
		{
			name:  "two edges to an address a refused root states",
			edits: [][2]string{{rootU3, `{"key":["u3"],"properties":{"id":"u7"}`}, {edgeU1, `"target_key":["u7"],"properties":{"w":1}`}, {edgeU3, `"target_key":["u7"]`}},
			want:  []string{storesU3},
		},
		{
			name:  "two records naming a root whose key its key property contradicts",
			edits: [][2]string{{rootU3, `{"key":["u3"],"properties":{"id":"u4"}`}, {lostU8, `"target_key":["u3"]`}, {lostU9, `"target_key":["u3"]`}},
			want:  []string{storesU3},
		},
		{
			name:  "two records naming an address a refused root states",
			edits: [][2]string{{rootU3, `{"key":["u3"],"properties":{"id":"u7"}`}, {lostU8, `"target_key":["u7"]`}, {lostU9, `"target_key":["u7"]`}},
			want:  []string{storesU3},
		},
		{
			name:  "two records of a source whose key its key property contradicts",
			edits: [][2]string{{rootT1, `{"key":["t1"],"properties":{"id":"t9"}`}, {lostU8, lostU9}},
			want:  []string{diag.E_SNAPSHOT_MALFORMED.String() + `: instance of de#T stores key ["t1"]`},
		},
	} {
		data := edited(t, doc, c.edits...)
		for _, mode := range []struct {
			name string
			opts []snapshot.Option
		}{
			{"plain", nil},
			{"revalidate", []snapshot.Option{snapshot.WithRevalidation(diag.Error)}},
		} {
			opts := append([]snapshot.Option{snapshot.WithIntegrityCheck(false)}, mode.opts...)
			t.Run(c.name+"/Load/"+mode.name, func(t *testing.T) {
				t.Parallel()
				snap, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
					return snapshot.Unmarshal(t.Context(), data, s, append([]snapshot.Option{snapshot.WithWarnings(warn)}, opts...)...)
				})
				matchIssues(t, readIssues(res), c.want)
				if len(c.want) > 0 && (!snap.IsZero() || diagtest.Err(res) == nil) {
					t.Errorf("a refused document yields a snapshot, or no Error: %s", res)
				}
			})
			t.Run(c.name+"/Verify/"+mode.name, func(t *testing.T) {
				t.Parallel()
				matchIssues(t, readIssues(diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
					return snapshot.Verify(t.Context(), data, s, append([]snapshot.Option{snapshot.WithWarnings(warn)}, opts...)...)
				})), c.want)
			})
		}
	}
}

// edited applies each pair of edits once, failing when the document lacks one.
func edited(t *testing.T, doc []byte, edits ...[2]string) []byte {
	t.Helper()
	for _, e := range edits {
		if !bytes.Contains(doc, []byte(e[0])) {
			t.Fatalf("the document holds no %s", e[0])
		}
		doc = bytes.Replace(doc, []byte(e[0]), []byte(e[1]), 1)
	}
	return doc
}

// A read without a schema knows no association's multiplicity, so it judges no
// repeated target, under a (many) association or a (one) one.
func TestInfo_JudgesNoRepeatedTarget(t *testing.T) {
	t.Parallel()
	_, doc := duplicateEdgeDoc(t)
	for name, edit := range map[string][2]string{
		"an edge":             {`"target_key":["u3"]`, `"target_key":["u1"]`},
		"a record":            {`"target_key":["u8"]`, `"target_key":["u9"]`},
		"a (one) association": {`"ONE_U":[{"target_type":2,"target_key":["u1"],"properties":{}}]`, `"ONE_U":[{"target_type":2,"target_key":["u1"],"properties":{}},{"target_type":2,"target_key":["u1"],"properties":{}}]`},
	} {
		_, err := snapshot.Stat(t.Context(), edited(t, doc, edit))
		if errors.Is(err, diag.E_SNAPSHOT_MALFORMED) || errors.Is(err, diag.E_GRAPH_CARDINALITY) {
			t.Errorf("%s: Info: %s, want no structural refusal", name, err)
		}
	}
	if _, err := snapshot.Stat(t.Context(), doc); err != nil {
		t.Errorf("Info of the untouched document: %s", err)
	}
}

// A repeated edge, a repeated record and an overfull (one) association each
// name the source root's key.
func TestRead_EachRefusalOfARootsRecordsNamesItsKey(t *testing.T) {
	t.Parallel()
	s, doc := duplicateEdgeDoc(t)
	for _, c := range []struct {
		name string
		edit [2]string
		code diag.Code
		want []diag.Detail
	}{
		{
			name: "a repeated edge",
			edit: [2]string{`"target_key":["u3"]`, `"target_key":["u1"]`},
			code: diag.E_SNAPSHOT_MALFORMED,
			want: []diag.Detail{
				{Key: diag.DetailKeyTypeName, Value: "de#T"},
				{Key: diag.DetailKeyPrimaryKey, Value: `["t1"]`},
				{Key: diag.DetailKeyRelationName, Value: "TO_U"},
				{Key: diag.DetailKeyTargetPK, Value: `["u1"]`},
			},
		},
		{
			name: "a repeated record",
			edit: [2]string{`"target_key":["u8"]`, `"target_key":["u9"]`},
			code: diag.E_SNAPSHOT_MALFORMED,
			want: []diag.Detail{
				{Key: diag.DetailKeyTypeName, Value: "de#T"},
				{Key: diag.DetailKeyPrimaryKey, Value: `["t1"]`},
				{Key: diag.DetailKeyRelationName, Value: "TO_U"},
				{Key: diag.DetailKeyTargetPK, Value: `["u9"]`},
			},
		},
		{
			name: "an overfull (one) association",
			edit: [2]string{`"ONE_U":[{"target_type":2,"target_key":["u1"],"properties":{}}]`, `"ONE_U":[{"target_type":2,"target_key":["u1"],"properties":{}},{"target_type":2,"target_key":["u3"],"properties":{}}]`},
			code: diag.E_GRAPH_CARDINALITY,
			want: []diag.Detail{
				{Key: diag.DetailKeyTypeName, Value: "de#T"},
				{Key: diag.DetailKeyPrimaryKey, Value: `["t1"]`},
				{Key: diag.DetailKeyRelationName, Value: "ONE_U"},
			},
		},
	} {
		_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
			return snapshot.Unmarshal(t.Context(), edited(t, doc, c.edit), s, snapshot.WithIntegrityCheck(false), snapshot.WithWarnings(warn))
		})
		var got [][]diag.Detail
		for issue := range res.Issues() {
			if issue.Code() == c.code {
				got = append(got, issue.Details())
			}
		}
		if len(got) != 1 || !slices.Equal(got[0], c.want) {
			t.Errorf("%s: %s details %v, want one issue holding %v: %s", c.name, c.code, got, c.want, res)
		}
	}
}

// Revalidation raises no E_DUPLICATE_EDGE for a target no root holds: each
// edge's reference issue stands for it.
func TestRead_RevalidationRaisesNoSecondIssueForATargetNoRootHolds(t *testing.T) {
	t.Parallel()
	s, doc := duplicateEdgeDoc(t)
	for name, c := range map[string]struct {
		edits    [][2]string
		dangling int
	}{
		"two edges": {
			edits:    [][2]string{{`"target_key":["u1"],"properties":{"w":1}`, `"target_key":["zz"],"properties":{"w":1}`}, {`"target_key":["u3"]`, `"target_key":["zz"]`}},
			dangling: 2,
		},
		"an edge and a record": {
			edits:    [][2]string{{`"target_key":["u3"]`, `"target_key":["zz"]`}, {`"target_key":["u8"]`, `"target_key":["zz"]`}},
			dangling: 1,
		},
	} {
		data := edited(t, doc, c.edits...)
		for _, sev := range []diag.Severity{diag.Error, diag.Warning} {
			_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(t.Context(), data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(sev), snapshot.WithWarnings(warn))
			})
			dangling := 0
			for issue := range res.Issues() {
				switch issue.Code() {
				case diag.E_DUPLICATE_EDGE:
					t.Errorf("%s at %s: %s", name, sev, issue.Message())
				case diag.E_SNAPSHOT_DANGLING_REFERENCE:
					dangling++
				}
			}
			if dangling != c.dangling {
				t.Errorf("%s at %s: %d reference issues, want %d: %s", name, sev, dangling, c.dangling, res)
			}
		}
	}
}

// Under a required (many) association a refused repeat is no record standing
// there, so revalidation warns once, for the record the read keeps.
func TestRead_ARefusedRepeatUnderARequiredAssociationDrawsNoWarning(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(ctx, "schema \"rq\"\ntype U {\n id String primary\n}\ntype T {\n id String primary\n --> TO_U (one:many) U\n}\n", "rq.yammm", schema.WithWarnings(warn))
	})
	if diagtest.Err(res) != nil {
		t.Fatal(res)
	}
	g := graph.New(s)
	vi, validateErr := instance.NewValidator(s).ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{
		"id": "t1", "to_u": []any{map[string]any{"_target_id": "u8"}, map[string]any{"_target_id": "u9"}},
	}})
	if validateErr != nil {
		t.Fatal(validateErr)
	}
	if err := g.Add(ctx, vi); err != nil {
		t.Fatal(err)
	}
	doc, err := snapshot.Marshal(ctx, g.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	data := edited(t, doc, [2]string{`"target_key":["u8"]`, `"target_key":["u9"]`})
	_, res = diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(ctx, data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error), snapshot.WithWarnings(warn))
	})
	matchIssues(t, readIssues(res), []string{
		diag.E_SNAPSHOT_MALFORMED.String() + `: unresolved record 1 names target rq#U[["u9"]]`,
		diag.W_SNAPSHOT_UNRESOLVED_REQUIRED.String() + `: `,
	})
}

// Revalidation judges a root beside a repeated target no root holds whole: the
// reference issues stand for the repeat, and the root's invariant is still run.
func TestRead_RevalidationRunsTheInvariantOfARootBesideARepeatNoRootHolds(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, err := schema.LoadString(ctx, "schema \"ri\"\ntype U {\n id String primary\n}\ntype T {\n id String primary\n name String\n --> TO_U (_:many) U\n ! \"name is not bad\" name != \"bad\"\n}\n", "ri.yammm")
	if err != nil {
		t.Fatal(err)
	}
	g := graph.New(s)
	v := instance.NewValidator(s)
	for _, raw := range []struct {
		typ   string
		props map[string]any
	}{
		{"U", map[string]any{"id": "u1"}},
		{"U", map[string]any{"id": "u2"}},
		{"T", map[string]any{"id": "t1", "name": "good", "to_u": []any{map[string]any{"_target_id": "u1"}, map[string]any{"_target_id": "u2"}}}},
	} {
		vi, err := v.ValidateOne(ctx, raw.typ, instance.RawInstance{Properties: raw.props})
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Add(ctx, vi); err != nil {
			t.Fatal(err)
		}
	}
	doc, readErr := snapshot.Marshal(ctx, g.Snapshot())
	if readErr != nil {
		t.Fatal(readErr)
	}
	data := edited(t, doc,
		[2]string{`"name":"good"`, `"name":"bad"`},
		[2]string{`"target_key":["u1"]`, `"target_key":["u7"]`},
		[2]string{`"target_key":["u2"]`, `"target_key":["u7"]`})
	for _, sev := range []diag.Severity{diag.Error, diag.Warning} {
		_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
			return snapshot.Unmarshal(ctx, data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(sev), snapshot.WithWarnings(warn))
		})
		matchIssues(t, readIssues(res), []string{
			diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + `: edge target ri#U[["u7"]]`,
			diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + `: edge target ri#U[["u7"]]`,
			diag.E_INVARIANT_FAIL.String() + `: `,
		})
	}
}

// A repeat of a target no root holds is judged by revalidation for its own
// values: a property its edge declares otherwise draws its issue, and only the
// listing the validator refused as a repeat leaves the second judgement.
func TestRead_RevalidationJudgesTheValuesOfARepeatNoRootHolds(t *testing.T) {
	t.Parallel()
	s, doc := duplicateEdgeDoc(t)
	data := edited(t, doc,
		[2]string{`"target_key":["u1"],"properties":{"w":1}`, `"target_key":["zz"],"properties":{"w":1}`},
		[2]string{`"target_key":["u3"],"properties":{"w":3}`, `"target_key":["zz"],"properties":{"w":"x"}`},
		[2]string{`"target_key":["u8"]`, `"target_key":["zz"]`})
	_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(t.Context(), data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Warning), snapshot.WithWarnings(warn))
	})
	matchIssues(t, readIssues(res), []string{
		diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + `: edge target de#U[["zz"]]`,
		diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + `: edge target de#U[["zz"]]`,
		diag.E_TYPE_MISMATCH.String() + `: revalidation of de#T[["t1"]]`,
	})
}

// The read's issue limit drops no repeat from the second judgement, at any
// revalidation severity: every repeat the row raised is named, so the reference
// issues stand for it and it draws no revalidation finding of its own.
func TestRead_RevalidationNamesARepeatTheIssueLimitDropped(t *testing.T) {
	t.Parallel()
	s, doc := duplicateEdgeDoc(t)
	data := edited(t, doc,
		[2]string{`"target_key":["u1"],"properties":{"w":1}`, `"target_key":["zz"],"properties":{"w":1}`},
		[2]string{`"target_key":["u3"],"properties":{"w":3}`, `"target_key":["zz"],"properties":{"w":"x"}`},
		[2]string{`"target_key":["u8"]`, `"target_key":["zz"]`})
	for _, sev := range []diag.Severity{diag.Fatal, diag.Error, diag.Warning} {
		t.Run(sev.String(), func(t *testing.T) {
			t.Parallel()
			_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(t.Context(), data, s, snapshot.WithIntegrityCheck(false),
					snapshot.WithRevalidation(sev), snapshot.WithIssueLimit(1), snapshot.WithWarnings(warn))
			})
			if got := res.CodeCounts(sev); got[diag.E_DUPLICATE_EDGE] != 0 || got[diag.E_TYPE_MISMATCH] != 1 {
				t.Errorf("revalidation findings %v, want one E_TYPE_MISMATCH and no E_DUPLICATE_EDGE: %s", got, res)
			}
			if got := res.CodeCounts(diag.Error)[diag.E_SNAPSHOT_DANGLING_REFERENCE]; got != 2 {
				t.Errorf("E_SNAPSHOT_DANGLING_REFERENCE seen %d times, want 2: %s", got, res)
			}
		})
	}
}

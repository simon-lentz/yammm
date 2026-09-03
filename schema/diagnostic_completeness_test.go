package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// This file pins the diagnostic-completeness contract for schema loading:
// one analysis pass reports every independent error in a schema and its
// import closure — each exactly once — while the public all-or-nothing
// contract (any error ⇒ nil Schema) is unchanged. The suppression tests
// double as cascade guards: a failed import produces one diagnostic at its
// declaration, and references through its alias stay silent rather than
// piling on E_UNKNOWN_TYPE / E_INVALID_PRIMARY_KEY_TYPE / E_NO_PRIMARY_KEY
// noise with the same root cause.

// codeCounts tallies a result's issues by diagnostic code, enabling
// exact-count assertions ("exactly two E_IMPORT_RESOLVE, zero
// E_UNKNOWN_TYPE") rather than presence-only checks.
func codeCounts(res diag.Result) map[diag.Code]int {
	counts := make(map[diag.Code]int)
	for issue := range res.Issues() {
		counts[issue.Code()]++
	}
	return counts
}

// wantCounts asserts an exact per-code issue count for every code in want,
// and that the result holds exactly the sum — no unexpected extras.
func wantCounts(t *testing.T, res diag.Result, want map[diag.Code]int) {
	t.Helper()
	counts := codeCounts(res)
	total := 0
	for code, n := range want {
		total += n
		if got := counts[code]; got != n {
			t.Errorf("code %s: got %d, want %d\n%v", code, got, n, res)
		}
	}
	if res.Len() != total {
		t.Errorf("issue count: got %d, want exactly %d\n%v", res.Len(), total, res)
	}
}

// loadSourcesErr loads in-memory sources with main.yammm as the entry and
// requires failure: a nil schema with error diagnostics. Returns the result
// for code-count assertions.
func loadSourcesErr(t *testing.T, sources map[string][]byte) diag.Result {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", t.TempDir())
	if s != nil {
		t.Fatalf("load should fail, got schema %q", s.Name())
	}
	if !res.HasErrors() {
		t.Fatal("load should produce error diagnostics")
	}
	return res
}

// loadStringErr loads source from a string and requires failure: a nil
// schema with error diagnostics.
//
// This intentionally mirrors load_test.go's loadErr rather than reusing it:
// loadErr is testify-based, and the depguard testify-freeze ratchet keeps this
// file (and every new test file) testify-free, so the two must-fail helpers
// stay separate by design — the apparent duplication is the cost of that
// boundary, not an oversight.
func loadStringErr(t *testing.T, source string) diag.Result {
	t.Helper()
	s, res := schema.LoadString(t.Context(), source, "main.yammm")
	if s != nil {
		t.Fatalf("load should fail, got schema %q", s.Name())
	}
	if !res.HasErrors() {
		t.Fatal("load should produce error diagnostics")
	}
	return res
}

// TestLoad_TwoUnresolvableImports_BothReported pins that every unresolvable
// import is reported, not just the first: two missing import files produce
// two E_IMPORT_RESOLVE diagnostics, one at each declaration.
func TestLoad_TwoUnresolvableImports_BothReported(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost_one" as g1
import "./ghost_two" as g2
type Thing { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE: 2,
	})
}

// TestLoad_UnresolvableImportAndLocalSemanticError_BothReported pins that an
// import failure does not suppress the entry schema's own semantic
// diagnostics: a missing import and a primary-key-less local type are
// independent errors and both must surface. The local type deliberately has
// no extends clause (so the primary-key check is not deferred) and no
// alias-typed primary (suppression of those is pinned separately).
func TestLoad_UnresolvableImportAndLocalSemanticError_BothReported(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as g
type NoKey { name String }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE: 1,
		diag.E_NO_PRIMARY_KEY: 1,
	})
}

// TestLoad_BrokenAndCleanImports_NoFalseUpstreamFail pins per-schema error
// attribution under a shared collector: a clean import loaded after a
// broken sibling must still compile — it draws no E_UPSTREAM_FAIL and its
// types stay referenceable. A load judged on "any errors collected so far"
// rather than on the errors it contributed would nil the clean import and
// misattribute the sibling's failure to it.
func TestLoad_BrokenAndCleanImports_NoFalseUpstreamFail(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as g
import "./fine" as f
type Holder {
	id String primary
	--> REL (one) f.Ok
}`),
		"fine.yammm": []byte(`schema "fine"
type Ok { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE: 1,
		diag.E_UPSTREAM_FAIL:  0,
		diag.E_UNKNOWN_TYPE:   0,
	})
}

// TestLoad_FailingImportCompile_LocalErrorsStillReported pins the
// compile-failure flavor: an import that exists but fails to compile yields
// the imported file's own diagnostic (at its source), E_UPSTREAM_FAIL at the
// entry's import declaration, and the entry's own independent semantic error.
func TestLoad_FailingImportCompile_LocalErrorsStillReported(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./broken" as b
type LocalNoKey { name String }`),
		"broken.yammm": []byte(`schema "broken"
type RemoteNoKey { name String }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_NO_PRIMARY_KEY: 2,
		diag.E_UPSTREAM_FAIL:  1,
	})

	// The two primary-key errors must live in different sources: one in the
	// imported file, one in the entry.
	var pkSources []string
	for issue := range res.Issues() {
		if issue.Code() == diag.E_NO_PRIMARY_KEY {
			pkSources = append(pkSources, issue.Span().Source.String())
		}
	}
	if len(pkSources) == 2 && pkSources[0] == pkSources[1] {
		t.Errorf("primary-key errors should be attributed to their own sources, both landed in %q", pkSources[0])
	}
}

// TestLoad_FailedImport_ReferenceCascadeSuppressed pins cascade suppression:
// a failed import produces exactly one E_IMPORT_RESOLVE at its declaration,
// and qualified references through its alias — an extends clause and an
// association target — produce zero E_UNKNOWN_TYPE. Both local types carry
// their own primary keys so the import error is the only legitimate issue.
func TestLoad_FailedImport_ReferenceCascadeSuppressed(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as other
type Local extends other.X { id String primary }
type Holder {
	id String primary
	--> REL (one) other.Y
}`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE: 1,
		diag.E_UNKNOWN_TYPE:   0,
	})
}

// TestLoad_DuplicateAliasAndDatatypeCollision_BothReported pins intra-phase
// accumulation for import-declaration validation: a duplicate alias across
// two resolvable imports and a third import whose alias collides with a
// local datatype name are independent declaration errors and both must
// surface — including the datatype flavor of the collision check.
func TestLoad_DuplicateAliasAndDatatypeCollision_BothReported(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./a" as x
import "./b" as x
import "./c" as Money
type Money = String[3,3]
type Thing { id String primary }`),
		"a.yammm": []byte(`schema "a"`),
		"b.yammm": []byte(`schema "b"`),
		"c.yammm": []byte(`schema "c"`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_DUPLICATE_IMPORT:       1,
		diag.E_IMPORT_ALIAS_COLLISION: 1,
	})
}

// TestLoad_CollisionRejectedAlias_ReferencesDeferred pins that references
// through an import alias rejected for colliding with a local name defer to
// the collision — the single root cause — exactly as references through a
// failed import do. Before the fix the two reference kinds diverged: a
// datatype reference (resolved via the loader's residual resolution map)
// silently resolved against the rejected import, while an extends/relation
// reference (resolved via the schema's Import index, which has no entry for a
// rejected declaration) re-blamed E_UNKNOWN_TYPE. Both must now be silent: the
// collision is reported once, and nothing through the alias adds noise.
func TestLoad_CollisionRejectedAlias_ReferencesDeferred(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./other" as Money
type Money = String[3,3]
type Holder {
	id String primary
	pay Money.Code
	--> REL (one) Money.Item
}`),
		"other.yammm": []byte(`schema "other"
type Code = String[2,2]
type Item { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_ALIAS_COLLISION: 1,
		diag.E_UNKNOWN_TYPE:           0,
	})
}

// TestLoad_CollisionRejectedAlias_NotResolvedAgainstLoaderImport pins the
// deferRejectedAlias overwrite on the Load path: the loader resolves the import
// before the completer rejects its alias (here colliding with a local datatype),
// so the rejection must overwrite that resolution in the resolution map — else a
// reference through the alias would silently resolve against the still-loaded
// import. The reference names a datatype ABSENT from the imported schema, so a
// silent resolution would surface as E_UNKNOWN_TYPE ("datatype ... does not exist
// in the schema imported as ..."); its absence proves the alias deferred to the
// collision's root cause instead. Distinct from
// TestLoad_CollisionRejectedAlias_ReferencesDeferred, whose reference names a
// member that DOES exist in the import and so cannot tell deferral from silent
// resolution apart.
func TestLoad_CollisionRejectedAlias_NotResolvedAgainstLoaderImport(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./other" as Money
type Money = String[3,3]
type Holder {
	id String primary
	pay Money.NoSuchDatatype
}`),
		"other.yammm": []byte(`schema "other"
type Code = String[2,2]
type Item { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_ALIAS_COLLISION: 1,
		diag.E_UNKNOWN_TYPE:           0,
	})
}

// TestLoad_DiamondBrokenImport_DiagnosticsExactlyOnce pins failure
// memoization on a diamond import graph: the entry imports B and C, which
// both import broken D (a single pure semantic defect). D's own diagnostic
// must appear exactly once — not once per importer — while each importer
// still gets its own E_UPSTREAM_FAIL at its own declaration: B at its D
// import, C at its D import, and the entry at its B and C imports.
func TestLoad_DiamondBrokenImport_DiagnosticsExactlyOnce(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./b" as b
import "./c" as c
type Thing { id String primary }`),
		"b.yammm": []byte(`schema "b"
import "./d" as d
type ThingB { id String primary }`),
		"c.yammm": []byte(`schema "c"
import "./d" as d
type ThingC { id String primary }`),
		"d.yammm": []byte(`schema "d"
type Broken { name String }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_NO_PRIMARY_KEY: 1,
		diag.E_UPSTREAM_FAIL:  4,
	})

	// Two of the upstream failures belong to the entry (its B and C
	// declarations); the other two belong to B and C (their D declarations).
	entryUpstream := 0
	for issue := range res.Issues() {
		if issue.Code() == diag.E_UPSTREAM_FAIL && strings.HasSuffix(issue.Span().Source.String(), "main.yammm") {
			entryUpstream++
		}
	}
	if entryUpstream != 2 {
		t.Errorf("the entry reports one upstream failure per direct import: got %d, want 2\n%v", entryUpstream, res)
	}
}

// TestLoad_SameFileTwoSpellings_SingleDuplicateReport pins that importing
// the same file under two path spellings is reported exactly once — a
// tripwire against the duplicate-resolved-SourceID check being reported by
// more than one validation layer — and deterministically: declarations are
// checked in source order, so the LATER declaration is always the one
// blamed, with the first/duplicate detail pair oriented accordingly.
func TestLoad_SameFileTwoSpellings_SingleDuplicateReport(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./a" as x
import "./a.yammm" as y
type Thing { id String primary }`),
		"a.yammm": []byte(`schema "a"`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_DUPLICATE_IMPORT: 1,
	})

	for issue := range res.Issues() {
		if issue.Code() != diag.E_DUPLICATE_IMPORT {
			continue
		}
		if got := issue.Span().Start.Line; got != 3 {
			t.Errorf("blamed declaration line: got %d, want 3 (the later declaration, independent of map order)", got)
		}
		details := make(map[string]string)
		for _, d := range issue.Details() {
			details[d.Key] = d.Value
		}
		if details[diag.DetailKeyFirstAlias] != "x" {
			t.Errorf("first_alias detail: got %q, want %q (the earlier declaration is the kept one)", details[diag.DetailKeyFirstAlias], "x")
		}
		if details[diag.DetailKeyDuplicateAlias] != "y" {
			t.Errorf("duplicate_alias detail: got %q, want %q (the later declaration is the duplicate)", details[diag.DetailKeyDuplicateAlias], "y")
		}
	}
}

// TestLoad_SameBrokenFileTwoSpellings_DuplicateStillReported pins that the
// duplicate-import error surfaces even when the doubly-imported file is itself
// broken. Two spellings of the same file that fails to compile each draw their
// own E_UPSTREAM_FAIL (per-importer, as the diamond case documents), and the
// duplication is reported once as E_DUPLICATE_IMPORT — it is an independent
// error that previously stayed hidden until the broken file was fixed, because
// the duplicate check only saw successfully-resolved imports. A compile failure
// now retains its resolved SourceID, so the check sees the collision in one
// pass.
func TestLoad_SameBrokenFileTwoSpellings_DuplicateStillReported(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./d" as d1
import "./d.yammm" as d2
type Thing { id String primary }`),
		"d.yammm": []byte(`schema "d"
type Broken { name String }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_NO_PRIMARY_KEY:   1, // d.yammm's own defect, reported once
		diag.E_UPSTREAM_FAIL:    2, // one per importing declaration
		diag.E_DUPLICATE_IMPORT: 1, // the two spellings name the same file
	})
}

// TestLoad_DeclaredButFailingImport_ExtendsDeferredNotUnknown pins the
// deferral side of the suppression contract: an extends clause through an
// alias that IS declared but whose import failed must not produce
// E_UNKNOWN_TYPE — the import failure already carries the root cause. The
// contrasting control (a qualifier that names no import at all stays a
// genuine E_UNKNOWN_TYPE) is pinned by
// [TestLoad_QualifiedExtends_UndefinedAlias_Errors].
func TestLoad_DeclaredButFailingImport_ExtendsDeferredNotUnknown(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as other
type Doc extends other.Base { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE: 1,
		diag.E_UNKNOWN_TYPE:   0,
	})
}

// TestLoad_DeclErrorAndSemanticError_CrossPhaseBothReported pins cross-phase
// accumulation: an import-declaration error (duplicate alias over two
// resolvable files) and a later-phase semantic error (a primary-key-less
// type) must both surface in one pass.
func TestLoad_DeclErrorAndSemanticError_CrossPhaseBothReported(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./a" as x
import "./b" as x
type NoKey { name String }`),
		"a.yammm": []byte(`schema "a"`),
		"b.yammm": []byte(`schema "b"`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_DUPLICATE_IMPORT: 1,
		diag.E_NO_PRIMARY_KEY:   1,
	})
}

// TestLoad_FailedImport_InvariantAndPrimaryKeySkipsCompose pins that the
// poisoned-entity skips compose on one type: a local type extending through
// a failed import is skipped by the primary-key presence check (its key may
// live in the unreachable parent) AND by invariant property validation (its
// merged member set is incomplete, so referencing an inherited property must
// not false-positive). The import failure stays the only diagnostic.
func TestLoad_FailedImport_InvariantAndPrimaryKeySkipsCompose(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as other
type Child extends other.Parent {
	extra String
	! "inherited must be set" inherited_prop != ""
}`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE:   1,
		diag.E_UNKNOWN_PROPERTY: 0,
		diag.E_NO_PRIMARY_KEY:   0,
		diag.E_UNKNOWN_TYPE:     0,
	})
}

// TestLoad_FailedImport_AliasTypedPrimaryKeySuppressed pins the primary-key
// flavor of cascade suppression: a primary property typed through a failed
// import's alias must produce neither E_INVALID_PRIMARY_KEY_TYPE (the type
// is unknowable, not invalid — the root cause is the import) nor
// E_NO_PRIMARY_KEY (the declaration IS a primary key; absence-blame is
// reserved for types that never declared one).
func TestLoad_FailedImport_AliasTypedPrimaryKeySuppressed(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as other
type Thing { id other.IdType primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE:           1,
		diag.E_INVALID_PRIMARY_KEY_TYPE: 0,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestLoad_UnknownNamePrimaryKey_StaysRejected is the strictness control for
// the suppression above: an undeclared LOCAL name used as a primary's type
// has no import failure to defer to, so the load must keep failing — with
// exactly one diagnostic carrying the correct attribution: E_UNKNOWN_TYPE
// at the unknown name. No E_INVALID_PRIMARY_KEY_TYPE re-blames the key for
// its unknowable type, and no E_NO_PRIMARY_KEY blames the declared primary
// for absence. A suppression guard keyed on mere unresolvedness without a
// reported root cause would turn this load failure into a clean load — a
// breaking loosening.
func TestLoad_UnknownNamePrimaryKey_StaysRejected(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Thing { id Mystery primary }`)

	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE:             1,
		diag.E_INVALID_PRIMARY_KEY_TYPE: 0,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestLoad_NonKeyablePrimaryKey_NoAbsenceBlame pins that rejecting a
// declared primary for its type (Integer is not a permitted key type) does
// not additionally blame the type for having no primary key: the property
// is a declared primary whose type was rejected — one defect, one
// diagnostic.
func TestLoad_NonKeyablePrimaryKey_NoAbsenceBlame(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Thing { id Integer primary }`)

	wantCounts(t, res, map[diag.Code]int{
		diag.E_INVALID_PRIMARY_KEY_TYPE: 1,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestLoad_IssueLimit_TruncationObservable pins that hitting the collector's
// default issue cap is observable on the result: collection continues
// (counting drops) rather than failing, and LimitReached/DroppedCount carry
// the truncation. 150 independent errors against the default limit of 100.
func TestLoad_IssueLimit_TruncationObservable(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("schema \"many\"\n")
	for i := range 150 {
		fmt.Fprintf(&b, "type NoKey%03d { name String }\n", i)
	}

	res := loadStringErr(t, b.String())

	if !res.LimitReached() {
		t.Error("the default cap must be reported as reached")
	}
	if res.Len() != 100 {
		t.Errorf("surviving issues: got %d, want the first hundred", res.Len())
	}
	if res.DroppedCount() != 50 {
		t.Errorf("dropped count: got %d, want 50 — the overflow is counted, not silently lost", res.DroppedCount())
	}
}

// TestLoad_DroppedErrorStillFailsLoad pins that the all-or-nothing contract
// survives truncation. The per-schema error gates compare Collector.ErrorCount
// deltas; if a real error is dropped at the cap without moving that count, the
// gate passes and a broken schema is sealed and returned non-nil with
// OK()==true — silently violating "any error ⇒ nil Schema". Here a single
// Integer[-_, N] property emits a parse Warning that fills a limit of 1, so the
// concrete type's E_NO_PRIMARY_KEY is dropped: it must still nil the schema.
func TestLoad_DroppedErrorStillFailsLoad(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "main"
type Orphan {
	n Integer[-_, 5]
}`, "main.yammm", schema.WithIssueLimit(1))

	if s != nil {
		t.Fatalf("a schema whose only error was dropped at the cap must still fail load, got schema %q", s.Name())
	}
	if res.OK() {
		t.Error("result.OK() = true; want false — the dropped E_NO_PRIMARY_KEY is still an error")
	}
	if !res.HasErrors() {
		t.Error("result.HasErrors() = false; want true")
	}
	if !res.LimitReached() {
		t.Error("result.LimitReached() = false; want true (the warning filled the 1-issue cap)")
	}
}

// TestLoad_DuplicateAlias_KeepFirstBinding_NoFalseUnknown pins keep-first
// alias binding: when one alias is declared twice over two different
// resolvable files, the first declaration's binding wins everywhere, so a
// reference valid only against the FIRST file must not produce
// E_UNKNOWN_TYPE. A last-write-wins resolution map would wire the kept
// import to the second file and emit false reference noise.
func TestLoad_DuplicateAlias_KeepFirstBinding_NoFalseUnknown(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./a" as x
import "./b" as x
type Holder {
	id String primary
	--> REL (one) x.TypeA
}`),
		"a.yammm": []byte(`schema "a"
type TypeA { id String primary }`),
		"b.yammm": []byte(`schema "b"
type TypeB { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_DUPLICATE_IMPORT: 1,
		diag.E_UNKNOWN_TYPE:     0,
	})
}

// TestLoad_DuplicateAlias_FirstBindingFails_BothReportedDeferred pins
// keep-first composing with import failure: when the FIRST binding of a
// duplicated alias fails to resolve and the second would resolve fine, the
// failure is reported at the first declaration, the duplicate at the
// second, and references through the alias are deferred against the kept
// (failed) first binding — never resolved against the skipped second one.
func TestLoad_DuplicateAlias_FirstBindingFails_BothReportedDeferred(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./ghost" as x
import "./b" as x
type Holder {
	id String primary
	--> REL (one) x.Anything
}`),
		"b.yammm": []byte(`schema "b"
type TypeB { id String primary }`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_RESOLVE:   1,
		diag.E_DUPLICATE_IMPORT: 1,
		diag.E_UNKNOWN_TYPE:     0,
	})
}

// TestLoad_InheritedRejectedPrimaryKey_ReportedOnce pins that a parent's
// type-rejected primary key (Integer is not a valid key type) is reported
// exactly once — at the declaring parent — not re-reported at every descendant
// whose merged property set carries the same inherited *Property. The "each
// independent error exactly once" contract must hold across inheritance, not
// just for the E_NO_PRIMARY_KEY flavor that hasDeclaredPrimary suppresses.
func TestLoad_InheritedRejectedPrimaryKey_ReportedOnce(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Base { id Integer primary }
type C1 extends Base { a String }
type C2 extends Base { b String }`)

	wantCounts(t, res, map[diag.Code]int{
		diag.E_INVALID_PRIMARY_KEY_TYPE: 1,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestLoad_CrossPhaseAccumulation pins that a failure in one completion
// phase does not suppress later phases' independent findings: each subtest
// pairs a defect from a specific phase (duplicate indexing, edge-property
// validation, type completion, collision detection, relation targets,
// invariants) with an unrelated defect from a different phase, and asserts
// both surface in one pass with exact counts.
func TestLoad_CrossPhaseAccumulation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		source string
		want   map[diag.Code]int
	}{
		"duplicate type and missing primary key": {
			source: `schema "main"
type Dup { id String primary }
type Dup { id UUID primary }
type NoKey { name String }`,
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1, diag.E_NO_PRIMARY_KEY: 1},
		},
		"duplicate datatype and missing primary key": {
			source: `schema "main"
type Money = String[3,3]
type Money = Integer
type NoKey { name String }`,
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1, diag.E_NO_PRIMARY_KEY: 1},
		},
		"list edge property and missing primary key": {
			source: `schema "main"
type A {
	id String primary
	--> REL (one) B { tags List<String> }
}
type B { id String primary }
type NoKey { name String }`,
			want: map[diag.Code]int{diag.E_LIST_ON_EDGE: 1, diag.E_NO_PRIMARY_KEY: 1},
		},
		"rejected primary type and missing primary key": {
			// The rejected primary draws exactly one diagnostic; the
			// absence-blame belongs to the type that never declared one.
			source: `schema "main"
type Bad { id Integer primary }
type NoKey { name String }`,
			want: map[diag.Code]int{diag.E_INVALID_PRIMARY_KEY_TYPE: 1, diag.E_NO_PRIMARY_KEY: 1},
		},
		"collision, unknown relation target, and bad invariant": {
			source: `schema "main"
type T {
	id String primary
	thing String
	--> THING (one) Nope
}
type V {
	id String primary
	! "check" missing_prop > 0
}`,
			want: map[diag.Code]int{
				diag.E_PROPERTY_RELATION_COLLISION: 1,
				diag.E_UNKNOWN_TYPE:                1,
				diag.E_UNKNOWN_PROPERTY:            1,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, tc.source)
			wantCounts(t, res, tc.want)
		})
	}
}

// TestBuild_AliasCycleAndMissingPrimaryKey_BothReported pins cross-phase
// accumulation through the alias-resolution phase, on the Builder path: a
// cyclic datatype alias chain is only constructible programmatically (the
// DSL grammar requires a builtin constraint on a datatype's right-hand
// side), and its cycle findings must not suppress an unrelated type's
// missing-primary-key finding from a later phase. Each datatype's
// resolution reports the cycle from its own starting point.
func TestBuild_AliasCycleAndMissingPrimaryKey_BothReported(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("main").
		AddDataType("A", schema.NewAliasConstraint("B", nil)).
		AddDataType("B", schema.NewAliasConstraint("A", nil)).
		AddType("NoKey").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()
	if s != nil {
		t.Fatalf("build should fail, got schema %q", s.Name())
	}
	if !res.HasErrors() {
		t.Fatal("build should produce error diagnostics")
	}

	wantCounts(t, res, map[diag.Code]int{
		diag.E_INVALID_CONSTRAINT: 2,
		diag.E_NO_PRIMARY_KEY:     1,
	})
}

// TestLoadString_ImportsDisallowed_OtherDiagnosticsStillReported pins the
// structural-rejection continuation: a string-loaded source that declares
// imports is rejected with a single E_IMPORT_NOT_ALLOWED (positioned at the
// first import declaration, carrying the import count) — and that rejection
// must not suppress the source's own independent diagnostics. Categorically
// rejected imports are never probed, so no E_IMPORT_RESOLVE appears, and
// references through the rejected aliases are deferred, not unknown.
func TestLoadString_ImportsDisallowed_OtherDiagnosticsStillReported(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
import "./imp" as imp
import "./imp2" as imp2
type NoKey { name String }
type Keyed extends imp.X { id String primary }`)

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_NOT_ALLOWED: 1,
		diag.E_NO_PRIMARY_KEY:     1,
		diag.E_UNKNOWN_TYPE:       0,
		diag.E_IMPORT_RESOLVE:     0,
	})

	for issue := range res.Issues() {
		if issue.Code() != diag.E_IMPORT_NOT_ALLOWED {
			continue
		}
		if got := issue.Span().Start.Line; got != 2 {
			t.Errorf("rejection span line: got %d, want 2 (the first import declaration)", got)
		}
		var importCount string
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyImportCount {
				importCount = d.Value
			}
		}
		if importCount != "2" {
			t.Errorf("import_count detail: got %q, want %q", importCount, "2")
		}
	}
}

// TestLoad_UnknownPropertyDatatype_FailsAtLoad pins that a property typed
// with a name that cannot name a datatype fails at load with
// E_UNKNOWN_TYPE — instead of loading clean and surfacing per-value at
// instance-validation time as "unresolved alias constraint". Three failing
// shapes; the declared-but-failed import stays silent (its import failure
// is the root cause), which is the deferral the suppression tests pin.
func TestLoad_UnknownPropertyDatatype_FailsAtLoad(t *testing.T) {
	t.Parallel()

	t.Run("undeclared local name", func(t *testing.T) {
		t.Parallel()
		res := loadStringErr(t, `schema "main"
type Thing {
	id String primary
	a Mystery
}`)
		wantCounts(t, res, map[diag.Code]int{
			diag.E_UNKNOWN_TYPE: 1,
		})
	})

	t.Run("resolved import lacking the datatype", func(t *testing.T) {
		t.Parallel()
		res := loadSourcesErr(t, map[string][]byte{
			"main.yammm": []byte(`schema "main"
import "./other" as other
type Thing {
	id String primary
	a other.NoSuchType
}`),
			"other.yammm": []byte(`schema "other"
type Code = String[2,2]`),
		})
		wantCounts(t, res, map[diag.Code]int{
			diag.E_UNKNOWN_TYPE:   1,
			diag.E_IMPORT_RESOLVE: 0,
		})
	})

	t.Run("qualifier naming no declared import", func(t *testing.T) {
		t.Parallel()
		res := loadStringErr(t, `schema "main"
type Thing {
	id String primary
	a nope.SomeType
}`)
		wantCounts(t, res, map[diag.Code]int{
			diag.E_UNKNOWN_TYPE: 1,
		})
	})

	t.Run("declared but failed import stays deferred", func(t *testing.T) {
		t.Parallel()
		res := loadSourcesErr(t, map[string][]byte{
			"main.yammm": []byte(`schema "main"
import "./ghost" as other
type Thing {
	id String primary
	a other.Code
}`),
		})
		wantCounts(t, res, map[diag.Code]int{
			diag.E_IMPORT_RESOLVE: 1,
			diag.E_UNKNOWN_TYPE:   0,
		})
	})
}

// TestLoad_ImportedDatatype_Resolves is the positive control for the
// unknown-datatype tightening: a property typed by a datatype that exists
// in a resolved import loads cleanly.
func TestLoad_ImportedDatatype_Resolves(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./other" as other
type Thing {
	id String primary
	code other.Code
}`),
		"other.yammm": []byte(`schema "other"
type Code = String[2,2]`),
	}, "main.yammm", t.TempDir())
	if res.HasErrors() {
		t.Fatalf("a resolvable imported datatype must load cleanly: %v", res)
	}
	if s == nil {
		t.Fatal("expected a non-nil schema")
	}
}

// requireBuildFailed asserts a Builder produced no schema — the all-or-nothing
// contract (any error ⇒ nil schema) on the programmatic front door.
func requireBuildFailed(t *testing.T, s *schema.Schema) {
	t.Helper()
	if s == nil {
		return
	}
	t.Fatalf("build should fail, got schema %q", s.Name())
}

// TestLoad_RepeatedCollidingAlias_ReportedAsDuplicate pins that a repeated
// import alias that also collides with a local datatype name is reported once
// for the collision (the first declaration) and once as a duplicate (the later
// declaration) — not twice as a collision. The keep-first alias slot is claimed
// even for a rejected declaration, so a repeat draws E_DUPLICATE_IMPORT rather
// than re-firing the collision, holding the "each independent error exactly
// once" contract for the repeated-rejected-alias shape.
func TestLoad_RepeatedCollidingAlias_ReportedAsDuplicate(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./a" as Money
import "./b" as Money
type Money = String[3,3]
type Thing { id String primary }`),
		"a.yammm": []byte(`schema "a"`),
		"b.yammm": []byte(`schema "b"`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_IMPORT_ALIAS_COLLISION: 1,
		diag.E_DUPLICATE_IMPORT:       1,
	})
}

// TestBuild_RegistryQualifiedExtends_UndeclaredImport_Errors pins that a Builder
// schema with a registry but zero imports rejects a qualified extends naming an
// undeclared import: with a registry present, a nil resolution map can only mean
// a zero-import schema, so the qualifier names no import and can never resolve —
// a genuine E_UNKNOWN_TYPE that nils the schema, not a silent deferral. The
// regression dropped the supertype and loaded clean.
func TestBuild_RegistryQualifiedExtends_UndeclaredImport_Errors(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("d").
		WithRegistry(schema.NewRegistry()).
		AddType("Person").
		WithPrimaryKey("name", schema.NewStringConstraint()).
		Extends(schema.NewTypeRef("bogus", "Base", location.Span{})).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE: 1,
	})
}

// TestBuild_RegistryQualifiedRelation_UndeclaredImport_Errors pins the same
// regression on a relation target: a qualified association target naming an
// undeclared import is reported, not silently dropped.
func TestBuild_RegistryQualifiedRelation_UndeclaredImport_Errors(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("d").
		WithRegistry(schema.NewRegistry()).
		AddType("Person").
		WithPrimaryKey("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("bogus", "Organization", location.Span{}), true, false).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE: 1,
	})
}

// TestBuild_RegistryQualifiedPropertyDatatype_UndeclaredImport_Errors pins the
// same regression on a property's datatype reference (the resolveAliasChain
// path, distinct from the extends/relation resolveTypeRef path).
func TestBuild_RegistryQualifiedPropertyDatatype_UndeclaredImport_Errors(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("d").
		WithRegistry(schema.NewRegistry()).
		AddType("Thing").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("code", schema.NewAliasConstraint("bogus.Code", nil)).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE: 1,
	})
}

// TestBuild_RegistryQualifiedPrimaryKey_UndeclaredImport_SingleError pins that a
// primary key typed through an undeclared import reports exactly one diagnostic:
// E_UNKNOWN_TYPE from alias resolution. The primary-key type check defers to
// that report (no E_INVALID_PRIMARY_KEY_TYPE), and absence-blame does not fire
// (the property IS a declared primary). Without the coordinated primaryKeyType
// Deferred change, the resolveAliasChain fix would double-report.
func TestBuild_RegistryQualifiedPrimaryKey_UndeclaredImport_SingleError(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("d").
		WithRegistry(schema.NewRegistry()).
		AddType("Thing").
		WithPrimaryKey("id", schema.NewAliasConstraint("bogus.IdType", nil)).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE:             1,
		diag.E_INVALID_PRIMARY_KEY_TYPE: 0,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestBuild_NoRegistryQualifiedExtends_Errors pins that a registry-less Builder
// rejects a qualified extends the same way the registry-present controls above
// do: without a registry no link step ever comes, so the dangling supertype can
// never resolve — deferring it would seal a schema that silently dropped its
// inheritance.
func TestBuild_NoRegistryQualifiedExtends_Errors(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("d").
		AddType("Person").
		WithPrimaryKey("name", schema.NewStringConstraint()).
		Extends(schema.NewTypeRef("bogus", "Base", location.Span{})).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE: 1,
	})
}

// TestBuild_NoRegistryQualifiedPrimaryKey_Errors pins the same rejection on the
// resolveAliasChain path: a primary key typed by a dangling qualified datatype
// draws exactly one E_UNKNOWN_TYPE, with the primary-key type check deferring
// to that report rather than stacking E_INVALID_PRIMARY_KEY_TYPE.
func TestBuild_NoRegistryQualifiedPrimaryKey_Errors(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("d").
		AddType("Thing").
		WithPrimaryKey("id", schema.NewAliasConstraint("bogus.IdType", nil)).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE:             1,
		diag.E_INVALID_PRIMARY_KEY_TYPE: 0,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestBuild_DatatypeAliasChainToUnknown_ReportedOnce pins that a datatype alias
// chain bottoming out in an unknown name blames that name exactly once, even
// though the chain is re-walked once per reference (the datatype's own
// resolution plus every property that chains through it). Constructible only via
// the Builder: the DSL grammar requires a builtin on a datatype's right-hand
// side, so `type X = Mystery` is a parse error in .yammm text.
func TestBuild_DatatypeAliasChainToUnknown_ReportedOnce(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("main").
		AddDataType("X", schema.NewAliasConstraint("Mystery", nil)).
		AddType("Thing").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("a", schema.NewAliasConstraint("X", nil)).
		WithProperty("b", schema.NewAliasConstraint("X", nil)).
		Done().
		Build()
	requireBuildFailed(t, s)
	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE: 1,
	})
}

// TestLoad_DistinctPropertiesSameUnknownDatatype_EachReported pins that the
// chain-recursion suppression is scoped to the chain, not to direct references:
// two properties typed by the same undeclared name draw two E_UNKNOWN_TYPE, one
// per declaration site — mirroring how two relations to the same unknown target
// each report. A datatype-mediated chain (above) is the only shape deduplicated.
func TestLoad_DistinctPropertiesSameUnknownDatatype_EachReported(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Thing {
	id String primary
	a Mystery
	b Mystery
}`)

	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE: 2,
	})
}

// TestLoad_PrimaryKeyTypedByResolvedImportMissingDatatype_SingleUnknownType pins
// the reachable primary-key deferral path: a primary key typed by a qualified
// name whose import resolves but does not declare the datatype is reported once
// (E_UNKNOWN_TYPE by alias resolution), with the primary-key type check deferring
// to that report (no E_INVALID_PRIMARY_KEY_TYPE) and no absence blame.
func TestLoad_PrimaryKeyTypedByResolvedImportMissingDatatype_SingleUnknownType(t *testing.T) {
	t.Parallel()
	res := loadSourcesErr(t, map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./other" as other
type Thing { id other.Missing primary }`),
		"other.yammm": []byte(`schema "other"
type Code = String[2,2]`),
	})

	wantCounts(t, res, map[diag.Code]int{
		diag.E_UNKNOWN_TYPE:             1,
		diag.E_INVALID_PRIMARY_KEY_TYPE: 0,
		diag.E_NO_PRIMARY_KEY:           0,
	})
}

// TestLoad_PrimaryKeyTypedByResolvedImportedDatatype_Resolves is the positive
// control: a primary key typed by a datatype that exists in a resolved import
// and bottoms out in a key-eligible builtin loads cleanly.
func TestLoad_PrimaryKeyTypedByResolvedImportedDatatype_Resolves(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./other" as other
type Thing { id other.Code primary }`),
		"other.yammm": []byte(`schema "other"
type Code = String[2,2]`),
	}, "main.yammm", t.TempDir())
	if res.HasErrors() {
		t.Fatalf("a primary key typed by a resolved imported datatype must load cleanly: %v", res)
	}
	if s == nil {
		t.Fatal("expected a non-nil schema")
	}
}

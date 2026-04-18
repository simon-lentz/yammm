package diag_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// Pre-registered codes reused across tests so TestRegisteredCodesDocumented's
// documentation-parity invariant is not perturbed. Severity is attached at
// Issue construction time, so any code can play any role — the names are
// chosen to read intuitively against each test fixture.
var (
	testCodeError       = diag.E_SYNTAX
	testCodeDuplicatePK = diag.E_DUPLICATE_PK
	testCodeWarning     = diag.W_UPDATE_METADATA_FALLBACK
	testCodeFatal       = diag.E_INTERNAL
)

func testResultWithError(tb testing.TB, code diag.Code) diag.Result {
	tb.Helper()
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Error, code, "test error message").Build())
	return c.Result()
}

func testResultOK() diag.Result {
	return diag.NewCollector(0).Result()
}

func testResultWithWarning(tb testing.TB) diag.Result {
	tb.Helper()
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "test warning").Build())
	return c.Result()
}

func testResultMixed(tb testing.TB) diag.Result {
	tb.Helper()
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Fatal, testCodeFatal, "fatal issue").Build())
	c.Collect(diag.NewIssue(diag.Error, testCodeError, "error issue").Build())
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "warning issue").Build())
	return c.Result()
}

func TestResult_WithContext_ReturnsValue(t *testing.T) {
	t.Parallel()
	res := testResultWithError(t, testCodeError)
	cr := res.WithContext("schema_load")
	if cr.Tag != "schema_load" {
		t.Errorf("Tag = %q, want %q", cr.Tag, "schema_load")
	}
	if cr.Result.Len() != res.Len() {
		t.Errorf("Result.Len() = %d, want %d", cr.Result.Len(), res.Len())
	}
}

func TestResultWithContext_Err_OK(t *testing.T) {
	t.Parallel()
	cr := testResultOK().WithContext("noop")
	if err := cr.Err(); err != nil {
		t.Errorf("Err() on OK result = %v, want nil", err)
	}
	if !cr.OK() {
		t.Error("OK() on OK result = false, want true")
	}
	if cr.HasErrors() {
		t.Error("HasErrors() on OK result = true, want false")
	}
}

func TestResultWithContext_Err_WithErrors(t *testing.T) {
	t.Parallel()
	cr := testResultWithError(t, testCodeError).WithContext("validation")
	err := cr.Err()
	if err == nil {
		t.Fatal("Err() on failing result = nil, want non-nil")
	}

	var ewc *diag.ErrorWithContext
	if !errors.As(err, &ewc) {
		t.Fatalf("errors.As(&ErrorWithContext) = false, want true; err = %v", err)
	}
	if ewc.Tag != "validation" {
		t.Errorf("recovered Tag = %q, want %q", ewc.Tag, "validation")
	}
	if cr.OK() {
		t.Error("OK() on failing result = true, want false")
	}
	if !cr.HasErrors() {
		t.Error("HasErrors() on failing result = false, want true")
	}
}

func TestResultWithContext_WarningsOnly(t *testing.T) {
	t.Parallel()
	cr := testResultWithWarning(t).WithContext("preflight")
	if err := cr.Err(); err != nil {
		t.Errorf("Err() on warnings-only result = %v, want nil", err)
	}
	if !cr.OK() {
		t.Error("OK() on warnings-only result = false, want true")
	}
	if cr.HasErrors() {
		t.Error("HasErrors() on warnings-only result = true, want false")
	}
}

func TestErrorWithContext_Error_Format(t *testing.T) {
	t.Parallel()
	cr := testResultWithError(t, testCodeError).WithContext("schema_load")
	err := cr.Err()
	msg := err.Error()
	if !strings.HasPrefix(msg, "schema_load: ") {
		t.Errorf("Error() prefix = %q, want prefix %q", msg, "schema_load: ")
	}
	if !strings.Contains(msg, "test error message") {
		t.Errorf("Error() missing issue message: %q", msg)
	}
}

func TestErrorWithContext_Unwrap_ChainsToResultError(t *testing.T) {
	t.Parallel()
	res := testResultWithError(t, testCodeDuplicatePK)
	err := res.WithContext("validation").Err()

	var re *diag.ResultError
	if !errors.As(err, &re) {
		t.Fatalf("errors.As(&ResultError) via Unwrap = false, want true; err = %v", err)
	}
	if re.Result.Len() != res.Len() {
		t.Errorf("recovered Result.Len() = %d, want %d", re.Result.Len(), res.Len())
	}
	// Confirm direct Unwrap also reaches the same value.
	unwrapped := errors.Unwrap(err)
	if unwrapped == nil {
		t.Fatal("errors.Unwrap(ErrorWithContext) = nil, want non-nil")
	}
}

func TestErrorWithContext_NilSafe(t *testing.T) {
	t.Parallel()
	var e *diag.ErrorWithContext
	msg := e.Error()
	if msg == "" {
		t.Error("nil *ErrorWithContext.Error() = \"\", want stable fallback string")
	}
	if !strings.Contains(msg, "nil") {
		t.Errorf("nil *ErrorWithContext.Error() = %q, want mention of nil", msg)
	}
	if got := e.Unwrap(); got != nil {
		t.Errorf("nil *ErrorWithContext.Unwrap() = %v, want nil", got)
	}
}

// --- LogValue shape ---

func TestResultWithContext_LogValue_ContextAttrAlwaysPresent(t *testing.T) {
	t.Parallel()
	cr := testResultWithError(t, testCodeError).WithContext("validation")
	val := cr.LogValue()
	if val.Kind() != slog.KindGroup {
		t.Fatalf("LogValue().Kind() = %v, want Group", val.Kind())
	}
	ctxVal, ok := lookupAttr(val, "context")
	if !ok {
		t.Fatal("LogValue() missing context attribute")
	}
	if ctxVal.String() != "validation" {
		t.Errorf("context = %q, want %q", ctxVal.String(), "validation")
	}
}

func TestResultWithContext_LogValue_TopLevelCode_WhenError(t *testing.T) {
	t.Parallel()
	cr := testResultWithError(t, testCodeDuplicatePK).WithContext("validation")
	val := cr.LogValue()
	codeVal, ok := lookupAttr(val, "code")
	if !ok {
		t.Fatal("LogValue() missing top-level code attribute")
	}
	if codeVal.String() != testCodeDuplicatePK.String() {
		t.Errorf("top-level code = %q, want %q", codeVal.String(), testCodeDuplicatePK.String())
	}
}

func TestResultWithContext_LogValue_TopLevelCode_OmittedOnWarningsOnly(t *testing.T) {
	t.Parallel()
	cr := testResultWithWarning(t).WithContext("preflight")
	val := cr.LogValue()
	if _, ok := lookupAttr(val, "code"); ok {
		t.Error("LogValue() emitted top-level code on warnings-only result; want omitted")
	}
}

func TestResultWithContext_LogValue_TopLevelCode_OmittedOnOK(t *testing.T) {
	t.Parallel()
	cr := testResultOK().WithContext("noop")
	val := cr.LogValue()
	if _, ok := lookupAttr(val, "code"); ok {
		t.Error("LogValue() emitted top-level code on OK result; want omitted")
	}
}

func TestResultWithContext_LogValue_Counts_MixedSeverity(t *testing.T) {
	t.Parallel()
	cr := testResultMixed(t).WithContext("audit")
	val := cr.LogValue()
	countsVal, ok := lookupAttr(val, "counts")
	if !ok {
		t.Fatal("LogValue() missing counts group")
	}
	if countsVal.Kind() != slog.KindGroup {
		t.Fatalf("counts.Kind() = %v, want Group", countsVal.Kind())
	}
	errorsVal, ok := lookupAttr(countsVal, "errors")
	if !ok {
		t.Fatal("counts group missing 'errors' attribute")
	}
	// Fatal + Error = 2.
	if errorsVal.Int64() != 2 {
		t.Errorf("counts.errors = %d, want 2", errorsVal.Int64())
	}
	warningsVal, ok := lookupAttr(countsVal, "warnings")
	if !ok {
		t.Fatal("counts group missing 'warnings' attribute")
	}
	if warningsVal.Int64() != 1 {
		t.Errorf("counts.warnings = %d, want 1", warningsVal.Int64())
	}
}

func TestResultWithContext_LogValue_Counts_OK(t *testing.T) {
	t.Parallel()
	cr := testResultOK().WithContext("noop")
	val := cr.LogValue()
	countsVal, ok := lookupAttr(val, "counts")
	if !ok {
		t.Fatal("LogValue() missing counts group on OK result")
	}
	errorsVal, _ := lookupAttr(countsVal, "errors")
	if errorsVal.Int64() != 0 {
		t.Errorf("counts.errors (OK) = %d, want 0", errorsVal.Int64())
	}
	warningsVal, _ := lookupAttr(countsVal, "warnings")
	if warningsVal.Int64() != 0 {
		t.Errorf("counts.warnings (OK) = %d, want 0", warningsVal.Int64())
	}
}

func TestResultWithContext_LogValue_IssuesSliceShape(t *testing.T) {
	t.Parallel()
	// Emit via JSONHandler and decode so we can assert the shape
	// downstream consumers will actually see.
	cr := testResultMixed(t).WithContext("audit")

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)
	logger.Error("op failed", slog.Any("diagnostic", cr))

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal JSON log output: %v\noutput: %s", err, buf.String())
	}
	diagEntry, ok := out["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic entry not a map; got %T: %v", out["diagnostic"], out["diagnostic"])
	}
	issuesEntry, ok := diagEntry["issues"].([]any)
	if !ok {
		t.Fatalf("issues entry not a JSON array; got %T: %v", diagEntry["issues"], diagEntry["issues"])
	}
	if len(issuesEntry) != 3 {
		t.Errorf("issues length = %d, want 3", len(issuesEntry))
	}
	// Each element must be an object.
	for i, raw := range issuesEntry {
		if _, isMap := raw.(map[string]any); !isMap {
			t.Errorf("issues[%d] not a JSON object; got %T: %v", i, raw, raw)
		}
	}
	// Confirm there is no positional attribute "issue_0" at top level.
	for key := range diagEntry {
		if strings.HasPrefix(key, "issue_") {
			t.Errorf("diagnostic has positional attribute %q; want slice-only shape", key)
		}
	}
}

func TestResultWithContext_LogValue_AttributeGolden(t *testing.T) {
	t.Parallel()
	// Build a full-shape issue: span, path, code, hint, details.
	source := location.MustNewSourceID("test://schema.yammm")
	span := location.Point(source, 10, 4)
	issue := diag.NewIssue(diag.Error, testCodeError, "full-shape issue").
		WithSpan(span).
		WithPath("data.json", "$.Foo[0].bar").
		WithHint("fix it").
		WithDetails(
			diag.Detail{Key: diag.DetailKeyTypeName, Value: "Foo"},
			diag.Detail{Key: diag.DetailKeyPropertyName, Value: "bar"},
		).
		Build()
	c := diag.NewCollector(0)
	c.Collect(issue)
	cr := c.Result().WithContext("schema_load")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Error("fail", slog.Any("diagnostic", cr))

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal log: %v\nraw: %s", err, buf.String())
	}
	d := out["diagnostic"].(map[string]any)
	if got := d["context"]; got != "schema_load" {
		t.Errorf("context = %v, want %v", got, "schema_load")
	}
	if got := d["code"]; got != testCodeError.String() {
		t.Errorf("top-level code = %v, want %v", got, testCodeError.String())
	}
	counts := d["counts"].(map[string]any)
	if counts["errors"].(float64) != 1 {
		t.Errorf("counts.errors = %v, want 1", counts["errors"])
	}
	if counts["warnings"].(float64) != 0 {
		t.Errorf("counts.warnings = %v, want 0", counts["warnings"])
	}
	issues := d["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("issues len = %d, want 1", len(issues))
	}
	iss := issues[0].(map[string]any)
	if iss["severity"] != "error" {
		t.Errorf("issue.severity = %v, want error", iss["severity"])
	}
	if iss["message"] != "full-shape issue" {
		t.Errorf("issue.message = %v, want 'full-shape issue'", iss["message"])
	}
	if iss["code"] != testCodeError.String() {
		t.Errorf("issue.code = %v, want %v", iss["code"], testCodeError.String())
	}
	if iss["path"] != "$.Foo[0].bar" {
		t.Errorf("issue.path = %v", iss["path"])
	}
	if iss["hint"] != "fix it" {
		t.Errorf("issue.hint = %v", iss["hint"])
	}
	loc := iss["location"].(map[string]any)
	if loc["source"] != source.String() {
		t.Errorf("issue.location.source = %v, want %v", loc["source"], source.String())
	}
	if loc["line"].(float64) != 10 {
		t.Errorf("issue.location.line = %v, want 10", loc["line"])
	}
	if loc["column"].(float64) != 4 {
		t.Errorf("issue.location.column = %v, want 4", loc["column"])
	}
	details := iss["details"].(map[string]any)
	if details[diag.DetailKeyTypeName] != "Foo" {
		t.Errorf("issue.details.type = %v, want Foo", details[diag.DetailKeyTypeName])
	}
	if details[diag.DetailKeyPropertyName] != "bar" {
		t.Errorf("issue.details.property = %v, want bar", details[diag.DetailKeyPropertyName])
	}
}

func TestResultWithContext_StructuredOutput_TextHandler(t *testing.T) {
	t.Parallel()
	// Mirrors rdata's integration shape so Phase 1a adopters can compare
	// output on a known-good reference.
	cr := testResultWithError(t, testCodeError).WithContext("schema_load")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Error("op failed", slog.Any("diagnostic", cr))
	out := buf.String()
	if !strings.Contains(out, "schema_load") {
		t.Errorf("structured text-handler log missing context: %s", out)
	}
	if !strings.Contains(out, "test error message") {
		t.Errorf("structured text-handler log missing issue message: %s", out)
	}
}

// --- AsResultWithContext ---

func TestAsResultWithContext_Direct(t *testing.T) {
	t.Parallel()
	orig := testResultWithError(t, testCodeError).WithContext("validation").Err()
	got, ok := diag.AsResultWithContext(orig, "fallback")
	if !ok {
		t.Fatal("AsResultWithContext returned false for *ErrorWithContext")
	}
	if got.Tag != "validation" {
		t.Errorf("recovered tag = %q, want %q (fallback must not override)", got.Tag, "validation")
	}
	if got.Result.Len() != 1 {
		t.Errorf("recovered Result.Len() = %d, want 1", got.Result.Len())
	}
}

func TestAsResultWithContext_Wrapped(t *testing.T) {
	t.Parallel()
	orig := testResultWithError(t, testCodeError).WithContext("schema_load").Err()
	wrapped := fmt.Errorf("pipeline failed: %w", orig)
	got, ok := diag.AsResultWithContext(wrapped, "fallback")
	if !ok {
		t.Fatal("AsResultWithContext returned false for wrapped *ErrorWithContext")
	}
	if got.Tag != "schema_load" {
		t.Errorf("recovered tag = %q, want %q", got.Tag, "schema_load")
	}
}

func TestAsResultWithContext_Wrapped_DoubleWrap(t *testing.T) {
	t.Parallel()
	orig := testResultWithError(t, testCodeError).WithContext("inner").Err()
	w1 := fmt.Errorf("middle: %w", orig)
	w2 := fmt.Errorf("outer: %w", w1)
	got, ok := diag.AsResultWithContext(w2, "fallback")
	if !ok {
		t.Fatal("AsResultWithContext returned false for double-wrapped")
	}
	if got.Tag != "inner" {
		t.Errorf("recovered tag through double wrap = %q, want %q", got.Tag, "inner")
	}
}

func TestAsResultWithContext_ResultError_SynthesizesFallback(t *testing.T) {
	t.Parallel()
	// Bare *ResultError, not wrapped in ErrorWithContext.
	err := testResultWithError(t, testCodeError).Err()
	got, ok := diag.AsResultWithContext(err, "validation")
	if !ok {
		t.Fatal("AsResultWithContext returned false for *ResultError")
	}
	if got.Tag != "validation" {
		t.Errorf("synthesized tag = %q, want %q (fallbackTag)", got.Tag, "validation")
	}
	if got.Result.Len() != 1 {
		t.Errorf("recovered Result.Len() = %d, want 1", got.Result.Len())
	}
}

func TestAsResultWithContext_Wrapped_ResultError(t *testing.T) {
	t.Parallel()
	inner := testResultWithError(t, testCodeError).Err()
	wrapped := fmt.Errorf("op: %w", inner)
	got, ok := diag.AsResultWithContext(wrapped, "synth")
	if !ok {
		t.Fatal("AsResultWithContext returned false for wrapped *ResultError")
	}
	if got.Tag != "synth" {
		t.Errorf("synthesized tag on wrapped = %q, want synth", got.Tag)
	}
}

// TestAsResultWithContext_ErrorsJoin pins the tree-chain walk: errors.Join
// builds a multi-child error whose Unwrap() returns []error, and errors.As
// is required to visit every sibling. Both join-operand positions are
// exercised so a future refactor that accidentally short-circuits on the
// first non-matching branch is caught.
func TestAsResultWithContext_ErrorsJoin(t *testing.T) {
	t.Parallel()
	orig := testResultWithError(t, testCodeError).WithContext("validation").Err()
	other := errors.New("unrelated sibling")

	joined := errors.Join(orig, other)
	got, ok := diag.AsResultWithContext(joined, "fallback")
	if !ok {
		t.Fatal("AsResultWithContext returned false for errors.Join(ctx, other)")
	}
	if got.Tag != "validation" {
		t.Errorf("recovered tag = %q, want %q", got.Tag, "validation")
	}

	joinedRev := errors.Join(other, orig)
	gotRev, okRev := diag.AsResultWithContext(joinedRev, "fallback")
	if !okRev {
		t.Fatal("AsResultWithContext returned false for errors.Join(other, ctx)")
	}
	if gotRev.Tag != "validation" {
		t.Errorf("recovered tag (reversed order) = %q, want %q", gotRev.Tag, "validation")
	}
}

func TestAsResultWithContext_NotFound(t *testing.T) {
	t.Parallel()
	got, ok := diag.AsResultWithContext(errors.New("generic error"), "fallback")
	if ok {
		t.Error("AsResultWithContext returned true for unrelated error")
	}
	if got.Tag != "" {
		t.Errorf("recovered tag on not-found = %q, want empty", got.Tag)
	}
}

func TestAsResultWithContext_Nil(t *testing.T) {
	t.Parallel()
	got, ok := diag.AsResultWithContext(nil, "fallback")
	if ok {
		t.Error("AsResultWithContext(nil) returned true, want false")
	}
	if got.Tag != "" {
		t.Errorf("AsResultWithContext(nil) tag = %q, want empty", got.Tag)
	}
}

// --- helpers ---

func lookupAttr(v slog.Value, key string) (slog.Value, bool) {
	if v.Kind() != slog.KindGroup {
		return slog.Value{}, false
	}
	for _, attr := range v.Group() {
		if attr.Key == key {
			return attr.Value, true
		}
	}
	return slog.Value{}, false
}

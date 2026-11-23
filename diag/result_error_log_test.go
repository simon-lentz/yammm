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
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
	"github.com/simon-lentz/yammm/location"
)

// Pre-registered codes reused across tests so TestRegisteredCodesDocumented's
// documentation-parity invariant is not perturbed. Severity is attached at
// Issue construction time, so any code can play any role — the names are
// chosen to read intuitively against each test fixture.
var (
	testCodeError       = diag.E_SYNTAX
	testCodeDuplicatePK = diag.E_DUPLICATE_PK
	testCodeWarning     = diag.W_SNAPSHOT_PATH_EXTENSION
	testCodeFatal       = diag.E_INTERNAL
)

// failure returns the *diag.ResultError c builds, failing tb when c holds no
// failure.
func failure(tb testing.TB, c *diag.Collector) *diag.ResultError {
	tb.Helper()
	re, ok := errors.AsType[*diag.ResultError](c.Err())
	if !ok {
		tb.Fatalf("Err() = %v; want a *diag.ResultError", c.Err())
	}
	return re
}

func testErrorWithCode(tb testing.TB, code diag.Code) *diag.ResultError {
	tb.Helper()
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Error, code, "test error message").Build())
	return failure(tb, c)
}

func testErrorMixed(tb testing.TB) *diag.ResultError {
	tb.Helper()
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Fatal, testCodeFatal, "fatal issue").Build())
	c.Collect(diag.NewIssue(diag.Error, testCodeError, "error issue").Build())
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "warning issue").Build())
	return failure(tb, c)
}

func TestResultError_ErrorIsTheResultText(t *testing.T) {
	t.Parallel()
	re := testErrorWithCode(t, testCodeError)
	if got, want := re.Error(), re.Result.String(); got != want {
		t.Errorf("Error() = %q; want Result.String() %q", got, want)
	}
	if !strings.Contains(re.Error(), "test error message") {
		t.Errorf("Error() lacks the issue message: %q", re.Error())
	}
}

// TestResultError_ReadThroughATagAndAJoin holds errors.AsType and errors.Is to
// finding the failure through fmt.Errorf's %w and through either operand of
// errors.Join.
func TestResultError_ReadThroughATagAndAJoin(t *testing.T) {
	t.Parallel()
	re := testErrorWithCode(t, testCodeDuplicatePK)
	other := errors.New("unrelated sibling")
	for name, err := range map[string]error{
		"tag":           fmt.Errorf("schema_load: %w", re),
		"double tag":    fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", re)),
		"join first":    errors.Join(re, other),
		"join second":   errors.Join(other, re),
		"tag over join": fmt.Errorf("op: %w", errors.Join(other, re)),
	} {
		got, ok := errors.AsType[*diag.ResultError](err)
		if !ok || got != re {
			t.Errorf("%s: errors.AsType = %p, %t; want the failure in the chain, %p", name, got, ok, re)
		}
		if !errors.Is(err, testCodeDuplicatePK) {
			t.Errorf("%s: errors.Is(err, %v) = false", name, testCodeDuplicatePK)
		}
	}
	if _, ok := errors.AsType[*diag.ResultError](other); ok {
		t.Error("errors.AsType found a failure in an unrelated error")
	}
}

func TestResultErrorLogValue_TopLevelCode(t *testing.T) {
	t.Parallel()
	val := testErrorWithCode(t, testCodeDuplicatePK).LogValue()
	if val.Kind() != slog.KindGroup {
		t.Fatalf("LogValue().Kind() = %v, want Group", val.Kind())
	}
	codeVal, ok := lookupAttr(val, "code")
	if !ok {
		t.Fatal("LogValue() missing top-level code attribute")
	}
	if codeVal.String() != testCodeDuplicatePK.String() {
		t.Errorf("top-level code = %q, want %q", codeVal.String(), testCodeDuplicatePK.String())
	}
	if _, ok := lookupAttr(val, "context"); ok {
		t.Error("LogValue() emitted a context attribute")
	}
}

// TestResultErrorLogValue_CountsTheFailuresAlone holds the counts group to the
// failure's own Result: a collector's warning never reaches it.
func TestResultErrorLogValue_CountsTheFailuresAlone(t *testing.T) {
	t.Parallel()
	val := testErrorMixed(t).LogValue()
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
	if warningsVal.Int64() != 0 {
		t.Errorf("counts.warnings = %d, want 0", warningsVal.Int64())
	}
}

func TestResultErrorLogValue_IssuesArrayShape(t *testing.T) {
	t.Parallel()
	// Emit via JSONHandler and decode so we can assert the shape downstream
	// consumers will actually see.
	re := testErrorMixed(t)

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)
	logger.Error("op failed", slog.Any("diagnostic", re))

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
	if len(issuesEntry) != 2 {
		t.Errorf("issues length = %d, want 2", len(issuesEntry))
	}
	for i, raw := range issuesEntry {
		if _, isMap := raw.(map[string]any); !isMap {
			t.Errorf("issues[%d] not a JSON object; got %T: %v", i, raw, raw)
		}
	}
	for key := range diagEntry {
		if strings.HasPrefix(key, "issue_") {
			t.Errorf("diagnostic has positional attribute %q; want array-only shape", key)
		}
	}
}

func TestResultErrorLogValue_AttributeGolden(t *testing.T) {
	t.Parallel()
	// Build a full-shape issue: span, path, code, hint, details.
	source := sourcetest.ID("test://schema.yammm")
	span := location.Span{Source: source, Start: location.Position{Line: 10, Column: 4}, End: location.Position{Line: 10, Column: 4}}
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

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Error("fail", slog.Any("diagnostic", fmt.Errorf("schema_load: %w", failure(t, c))))

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal log: %v\nraw: %s", err, buf.String())
	}
	if _, ok := out["diagnostic"].(string); !ok {
		t.Fatalf("a tagged failure logged as %T; want its error text, since slog reads no LogValuer through a wrap", out["diagnostic"])
	}

	buf.Reset()
	logger.Error("fail", slog.Any("diagnostic", failure(t, c)))
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal log: %v\nraw: %s", err, buf.String())
	}
	// Golden only the diagnostic attribute tree (the slog envelope carries a
	// timestamp); it pins the top-level code, counts, and the complete
	// per-issue shape — severity, message, code, path, hint, span, details.
	yammmtest.GoldenJSON(t, "result_error_log_value", out["diagnostic"])
}

func TestResultErrorLogValue_TextHandler(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Error("op failed", slog.Any("diagnostic", testErrorWithCode(t, testCodeError)))
	out := buf.String()
	if !strings.Contains(out, "test error message") {
		t.Errorf("structured text-handler log missing issue message: %s", out)
	}
	if !strings.Contains(out, testCodeError.String()) {
		t.Errorf("structured text-handler log missing the code: %s", out)
	}
}

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

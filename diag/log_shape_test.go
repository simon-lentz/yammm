package diag

import (
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// attrsOf returns v's group attributes by name.
func attrsOf(t *testing.T, v slog.Value) map[string]slog.Value {
	t.Helper()
	if v.Kind() != slog.KindGroup {
		t.Fatalf("value kind = %v; want a group", v.Kind())
	}
	out := make(map[string]slog.Value)
	for _, a := range v.Group() {
		out[a.Key] = a.Value
	}
	return out
}

// TestIssueLogValue_CarriesTheSourceName holds both log shapes to naming the
// document an issue came from. A path without its source names a position in a
// file the reader cannot identify.
func TestIssueLogValue_CarriesTheSourceName(t *testing.T) {
	t.Parallel()

	issue := NewIssue(Error, E_TYPE_MISMATCH, "wrong type").
		WithPath("data.json", "$.Car[0].regNbr").
		Build()

	t.Run("Issue.LogValue", func(t *testing.T) {
		t.Parallel()
		attrs := attrsOf(t, issue.LogValue())
		got, ok := attrs["source_name"]
		if !ok {
			t.Fatalf("no source_name attribute; got %v", attrKeys(attrs))
		}
		if got.String() != "data.json" {
			t.Errorf("source_name = %q; want %q", got.String(), "data.json")
		}
	})

	t.Run("issueLogMap", func(t *testing.T) {
		t.Parallel()
		m := issueLogMap(issue)
		if m["source_name"] != "data.json" {
			t.Errorf("source_name = %v; want %q", m["source_name"], "data.json")
		}
	})

	t.Run("omitted when unset", func(t *testing.T) {
		t.Parallel()
		bare := NewIssue(Error, E_SYNTAX, "no provenance").
			WithSpan(location.Point(location.MustNewSourceID("test://x"), 1, 1)).
			Build()
		if _, ok := attrsOf(t, bare.LogValue())["source_name"]; ok {
			t.Error("source_name is emitted for an issue that has none")
		}
		if _, ok := issueLogMap(bare)["source_name"]; ok {
			t.Error("issueLogMap emits source_name for an issue that has none")
		}
	})
}

// attrKeys returns the keys of m, for a failure message.
func attrKeys(m map[string]slog.Value) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestContextualErrorLogValue_CarriesTruncation holds the log shape to saying
// when the issues it carries are not all of them. A consumer reading the slice
// alone cannot tell a complete result from a truncated one.
func TestContextualErrorLogValue_CarriesTruncation(t *testing.T) {
	t.Parallel()

	t.Run("truncated", func(t *testing.T) {
		t.Parallel()
		c := NewCollector(1)
		c.Collect(NewIssue(Error, E_INTERNAL, "kept").Build())
		c.Collect(NewIssue(Error, E_INTERNAL, "dropped one").Build())
		c.Collect(NewIssue(Error, E_INTERNAL, "dropped two").Build())

		ce := &ContextualError{Result: c.Result(), Tag: "validation"}
		attrs := attrsOf(t, ce.LogValue())

		reached, ok := attrs["limit_reached"]
		if !ok {
			t.Fatalf("no limit_reached attribute; got %v", attrKeys(attrs))
		}
		if !reached.Bool() {
			t.Error("limit_reached = false on a truncated result")
		}
		dropped, ok := attrs["dropped"]
		if !ok {
			t.Fatalf("no dropped attribute; got %v", attrKeys(attrs))
		}
		if got := dropped.Int64(); got != 2 {
			t.Errorf("dropped = %d; want 2", got)
		}
	})

	t.Run("not truncated", func(t *testing.T) {
		t.Parallel()
		c := NewCollectorUnlimited()
		c.Collect(NewIssue(Error, E_INTERNAL, "only").Build())

		attrs := attrsOf(t, (&ContextualError{Result: c.Result(), Tag: "validation"}).LogValue())
		if _, ok := attrs["limit_reached"]; ok {
			t.Error("limit_reached is emitted for a result that was not truncated")
		}
		if _, ok := attrs["dropped"]; ok {
			t.Error("dropped is emitted for a result that was not truncated")
		}
	})
}

package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestCaptureSources_KeepsAFailedLoadsSources holds the option to what a caller
// uses it for: a load that fails returns no schema, and the sources it captured
// still serve the failure's excerpt.
func TestCaptureSources_KeepsAFailedLoadsSources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yammm")
	const content = "schema \"bad\"\n\ntype T {\n\tid String primary\n\tname Strin\n}\n"
	writeHostPathFile(t, p, content)

	var captured *schema.Sources
	s, res := schema.Load(t.Context(), p, schema.WithModuleRoot(dir), schema.CaptureSources(&captured))
	if s != nil || res.OK() {
		t.Fatalf("the fixture loaded: schema %v, result %v", s, res)
	}
	var issue diag.Issue
	for is := range res.Issues() {
		if is.Code() == diag.E_UNKNOWN_TYPE && is.HasSpan() {
			issue = is
		}
	}
	if !issue.HasSpan() {
		t.Fatalf("no spanned E_UNKNOWN_TYPE in %v", issueCodes(res))
	}
	if captured == nil {
		t.Fatal("the load captured no sources")
	}
	if got, ok := captured.Content(issue.Span()); !ok || string(got) != content {
		t.Errorf("the captured sources serve %q, %t for the failure's span; want the file's content", got, ok)
	}
}

// TestCaptureSources_AnUnreadableEntryLeavesThisLoadsEmptyRegistry holds a
// load that fails before it reads its entry to the option's contract: dst
// holds that load's empty registry, never the registry of an earlier load
// through the same dst.
func TestCaptureSources_AnUnreadableEntryLeavesThisLoadsEmptyRegistry(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		entry func(t *testing.T, dir string) string
	}{
		{"a directory named as the entry", func(t *testing.T, dir string) string {
			t.Helper()
			p := filepath.Join(dir, "dir.yammm")
			if err := os.Mkdir(p, 0o750); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"a missing entry", func(_ *testing.T, dir string) string {
			return filepath.Join(dir, "missing.yammm")
		}},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			var captured *schema.Sources
			captureOneSource(t, dir, &captured)

			s, res := schema.Load(t.Context(), row.entry(t, dir), schema.WithModuleRoot(dir), schema.CaptureSources(&captured))
			if s != nil || !res.HasFatal() {
				t.Fatalf("the entry loaded: schema %v, result %v", s, res)
			}
			if captured == nil {
				t.Fatal("the failed load captured no sources")
			}
			if ids := captured.SourceIDs(); len(ids) != 0 {
				t.Errorf("the failed load's capture holds %v; want an empty registry", ids)
			}
		})
	}
}

// TestCaptureSources_AnEmptySourcesMapLeavesThisLoadsEmptyRegistry holds
// LoadSourcesWithEntry's refusal of an empty map to the same contract.
func TestCaptureSources_AnEmptySourcesMapLeavesThisLoadsEmptyRegistry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var captured *schema.Sources
	captureOneSource(t, dir, &captured)

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{}, "main.yammm", dir, schema.CaptureSources(&captured))
	if s != nil || !res.HasFatal() {
		t.Fatalf("the empty map loaded: schema %v, result %v", s, res)
	}
	if captured == nil {
		t.Fatal("the refused load captured no sources")
	}
	if ids := captured.SourceIDs(); len(ids) != 0 {
		t.Errorf("the refused load's capture holds %v; want an empty registry", ids)
	}
}

// TestCaptureSources_LoadStringCapturesTheString holds LoadString to the
// option: the capture holds the string's source whether or not it parsed.
func TestCaptureSources_LoadStringCapturesTheString(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name, source string
		loads        bool
	}{
		{"a string that loads", "schema \"main\"\n\ntype T {\n\tid String primary\n}\n", true},
		{"a string that fails to parse", "schema \"main\"\n\ntype T {\n", false},
	}
	want := location.NewSourceID("string://main.yammm")

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			var captured *schema.Sources
			s, res := schema.LoadString(t.Context(), row.source, "main.yammm", schema.CaptureSources(&captured))
			if loaded := s != nil && res.OK(); loaded != row.loads {
				t.Fatalf("schema loaded: %t, result %v; want loaded %t", loaded, res, row.loads)
			}
			if captured == nil {
				t.Fatal("the load captured no sources")
			}
			if ids := captured.SourceIDs(); len(ids) != 1 || ids[0] != want {
				t.Errorf("the capture holds %v; want %v alone", ids, want)
			}
			if got, ok := captured.ContentBySource(want); !ok || string(got) != row.source {
				t.Errorf("the capture serves %q, %t for the string; want its content", got, ok)
			}
		})
	}
}

// captureOneSource loads a well-formed entry from dir through CaptureSources(dst) and
// holds the capture to that entry, so a later load through dst starts from
// a registry that holds one source.
func captureOneSource(t *testing.T, dir string, dst **schema.Sources) {
	t.Helper()
	p := filepath.Join(dir, "ok.yammm")
	writeHostPathFile(t, p, "schema \"ok\"\n\ntype T {\n\tid String primary\n}\n")
	id, _, err := location.ResolveSourcePath(p)
	if err != nil {
		t.Fatal(err)
	}

	s, res := schema.Load(t.Context(), p, schema.WithModuleRoot(dir), schema.CaptureSources(dst))
	if s == nil || !res.OK() {
		t.Fatalf("the fixture failed to load: %v", res)
	}
	if ids := (*dst).SourceIDs(); len(ids) != 1 || ids[0] != id {
		t.Fatalf("the fixture's capture holds %v; want %v alone", ids, id)
	}
}

// TestCaptureSources_NilCapturesNothing pins the documented no-op.
func TestCaptureSources_NilCapturesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "ok.yammm")
	writeHostPathFile(t, p, "schema \"ok\"\n\ntype T {\n\tid String primary\n}\n")

	s, res := schema.Load(t.Context(), p, schema.WithModuleRoot(dir), schema.CaptureSources(nil))
	if s == nil || !res.OK() {
		t.Fatalf("a load with CaptureSources(nil) failed: %v", res)
	}
}

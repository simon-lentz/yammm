package instance_test

import (
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// builderFor returns a builder for typeName in s, failing the test on error.
func builderFor(t *testing.T, s schemaSource, typeName string) *instance.SchemaBuilder {
	t.Helper()
	b, err := instance.BuilderFor(s(t), typeName)
	if err != nil {
		t.Fatalf("BuilderFor(%q): %v", typeName, err)
	}
	return b
}

// buildErr runs Build and returns its error's text, failing the test when Build
// succeeds.
func buildErr(t *testing.T, b *instance.SchemaBuilder) string {
	t.Helper()
	_, err := b.Build()
	if err == nil {
		t.Fatal("Build succeeded, want a shape error")
	}
	return err.Error()
}

func TestSchemaBuilder_EdgeWith_WritesTheEdgesProperties(t *testing.T) {
	s := edgePropsSchema(t)
	b, err := instance.BuilderFor(s, "Employee")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := b.Property("id", "e1").
		EdgeWith("WORKS_AT", map[string]any{"role": "lead", "since": "2024"}, "c1").
		EdgeWith("visits", map[string]any{"note": "first"}, "o1").
		EdgeWith("VISITS", nil, "o2").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if diff := cmp.Diff(map[string]any{"_target_id": "c1", "role": "lead", "since": "2024"}, raw.Properties["works_at"]); diff != "" {
		t.Errorf("works_at (-want +got):\n%s", diff)
	}
	wantVisits := []any{
		map[string]any{"_target_id": "o1", "note": "first"},
		map[string]any{"_target_id": "o2"},
	}
	if diff := cmp.Diff(wantVisits, raw.Properties["visits"]); diff != "" {
		t.Errorf("visits (-want +got):\n%s", diff)
	}

	valid, err := instance.NewValidator(s).ValidateOne(t.Context(), "Employee", raw)
	if err != nil {
		t.Fatalf("ValidateOne: %v", err)
	}
	edge, ok := valid.Edge("WORKS_AT")
	if !ok {
		t.Fatal("the valid instance holds no WORKS_AT edge")
	}
	for target := range edge.Targets() {
		role, ok := target.Property("role")
		if !ok || role.String() != "lead" {
			t.Errorf("WORKS_AT's role = %v, %v; want lead, true", role, ok)
		}
	}
}

func TestSchemaBuilder_EdgeWith_RefusesAnUndeclaredEdgeProperty(t *testing.T) {
	b := builderFor(t, edgePropsSchema, "Employee")

	_, _, captureLine, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	b.EdgeWith("WORKS_AT", map[string]any{"role": "lead", "since": "2024", "rank": 1}, "c1") // DO NOT MOVE: counted from captureLine.
	got := buildErr(t, b)
	for _, want := range []string{
		`Employee: relation "WORKS_AT": unknown edge property "rank"`,
		"schema_builder_edge_with_test.go:" + strconv.Itoa(captureLine+4),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Build error = %q, want it to hold %q", got, want)
		}
	}
}

func TestSchemaBuilder_EdgeWith_RefusesATargetKeyFieldAsAnEdgeProperty(t *testing.T) {
	b := builderFor(t, edgePropsSchema, "Employee")
	b.EdgeWith("WORKS_AT", map[string]any{"role": "lead", "since": "2024", "_target_id": "c2"}, "c1")
	if got, want := buildErr(t, b), `unknown edge property "_target_id"`; !strings.Contains(got, want) {
		t.Errorf("Build error = %q, want it to hold %q", got, want)
	}
}

func TestSchemaBuilder_EdgeWith_ReportsUndeclaredPropertiesInNameOrder(t *testing.T) {
	const want = `relation "WORKS_AT": unknown edge property "alpha" (and 2 more build error(s))`
	for range 20 {
		b := builderFor(t, personSchema, "Person")
		b.EdgeWith("WORKS_AT", map[string]any{"zeta": 1, "alpha": 2, "mid": 3}, "c1")
		if got := buildErr(t, b); !strings.Contains(got, want) {
			t.Fatalf("Build error = %q, want it to hold %q", got, want)
		}
	}
}

func TestSchemaBuilder_EdgeWith_CopiesTheProperties(t *testing.T) {
	b := builderFor(t, edgePropsSchema, "Employee")
	props := map[string]any{"role": "lead", "since": "2024"}
	b.Property("id", "e1").EdgeWith("WORKS_AT", props, "c1")
	props["role"] = "changed"
	raw, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := raw.Properties["works_at"].(map[string]any)["role"]; got != "lead" {
		t.Errorf("works_at's role = %v after the caller's map changed, want lead", got)
	}
}

func TestSchemaBuilder_EdgeWith_OnCompositionRelation(t *testing.T) {
	b := builderFor(t, personSchema, "Person")
	b.EdgeWith("ADDRESSES", map[string]any{"street": "x"}, "a1")
	if got, want := buildErr(t, b), "does not support composed children"; !strings.Contains(got, want) {
		t.Errorf("Build error = %q, want it to hold %q", got, want)
	}
}

func TestSchemaBuilder_EdgeWith_ZeroTargetKey(t *testing.T) {
	b := builderFor(t, edgePropsSchema, "Employee")
	b.EdgeWith("WORKS_AT", map[string]any{"role": "lead"})
	if got, want := buildErr(t, b), `relation "WORKS_AT": an edge requires at least one target-key component`; !strings.Contains(got, want) {
		t.Errorf("Build error = %q, want it to hold %q", got, want)
	}
}

func TestSchemaBuilder_EdgeWith_AnUndeclaredPropertyLeavesTheCallsOtherBreaksCounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*instance.SchemaBuilder)
	}{
		{"no target key", func(b *instance.SchemaBuilder) {
			b.EdgeWith("WORKS_AT", map[string]any{"rank": 1})
		}},
		{"an excess target on a (one) relation", func(b *instance.SchemaBuilder) {
			b.EdgeWith("WORKS_AT", map[string]any{"role": "lead", "since": "2024"}, "c1")
			b.EdgeWith("WORKS_AT", map[string]any{"rank": 1}, "c2")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := builderFor(t, edgePropsSchema, "Employee")
			tc.call(b)
			const want = `relation "WORKS_AT": unknown edge property "rank" (and 1 more build error(s))`
			if got := buildErr(t, b); !strings.Contains(got, want) {
				t.Errorf("Build error = %q, want it to hold %q", got, want)
			}
		})
	}
}

func TestSchemaBuilder_Build_SharesNoEdgeObjectWithTheBuilder(t *testing.T) {
	b := builderFor(t, edgePropsSchema, "Employee")
	b.Property("id", "e1").
		EdgeWith("WORKS_AT", map[string]any{"role": "lead", "since": "2024"}, "c1").
		EdgeWith("VISITS", map[string]any{"note": "first"}, "o1")
	first, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	first.Properties["works_at"].(map[string]any)["role"] = "changed"
	first.Properties["visits"].([]any)[0].(map[string]any)["note"] = "changed"

	second, err := b.Build()
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if got := second.Properties["works_at"].(map[string]any)["role"]; got != "lead" {
		t.Errorf("works_at's role = %v after the first instance changed, want lead", got)
	}
	if got := second.Properties["visits"].([]any)[0].(map[string]any)["note"]; got != "first" {
		t.Errorf("visits' note = %v after the first instance changed, want first", got)
	}
}

// schemaSource is a fixture loader such as personSchema or edgePropsSchema.
type schemaSource = func(*testing.T) schema.Schema

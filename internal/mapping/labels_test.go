package mapping

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func loadInline(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), src, "mapping.yammm")
	if err := result.Err(); err != nil {
		t.Fatalf("fixture does not load: %v", err)
	}
	return s
}

// The collision check must walk types whose label is an invalid identifier,
// which is what LabeledTypes is split out from EmittableTypes to keep:
// EmittableTypes skips such a type after reporting it, so a check built on that
// walk would go silent for the labels most likely to collide.
func TestLabeledTypes_IncludesInvalidLabels(t *testing.T) {
	t.Parallel()
	s := loadInline(t, "schema \"index_test\"\n\ntype Document {\n\tid String primary\n}\n\ntype Note {\n\tid String primary\n}\n")
	// "-" makes every label an invalid Neo4j identifier.
	l := Labels{Separator: "-"}

	var seen int
	for _, label := range l.LabeledTypes(s) {
		seen++
		if err := ValidateIdentifier(label, "label"); err == nil {
			t.Errorf("label %q should be an invalid identifier under this separator", label)
		}
	}
	if seen != 2 {
		t.Fatalf("LabeledTypes yielded %d types, want 2", seen)
	}

	collector := diag.NewCollector(0)
	emitted := 0
	for range l.EmittableTypes(s, collector) {
		emitted++
	}
	if emitted != 0 {
		t.Errorf("EmittableTypes yielded %d types with invalid labels; it must skip them", emitted)
	}
	if !collector.Result().HasCode(InvalidIdentifier) {
		t.Error("EmittableTypes should report each skipped invalid label")
	}
}

// An empty Separator stands for the default, and a configured one replaces it.
func TestLabels_Label(t *testing.T) {
	t.Parallel()
	s := loadInline(t, "schema \"book_catalog\"\n\ntype Publisher {\n\tid String primary\n}\n")
	ty, ok := s.Type("Publisher")
	if !ok {
		t.Fatal("no type Publisher")
	}
	for _, tc := range []struct {
		l    Labels
		want string
	}{
		{Labels{}, "book_catalog__Publisher"},
		{Labels{Separator: "_"}, "book_catalog_Publisher"},
		{Labels{Prefix: "app_"}, "app_book_catalog__Publisher"},
	} {
		if got := tc.l.Label(ty); got != tc.want {
			t.Errorf("%+v.Label(Publisher) = %q, want %q", tc.l, got, tc.want)
		}
	}
}

// A prefix and a separator compose together, each as configured, and neither
// reaches a label that has no schema. A name of spaces alone is no name, and
// each name is sanitized.
func TestLabels_Compose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefix, separator, schema, typ, want string
	}{
		{"", "", "book_catalog", "Publisher", "book_catalog__Publisher"},
		{"", "", "geo_regions", "District", "geo_regions__District"},
		{"", "", "", "Person", "Person"},
		{"", "", "book_catalog", "", ""},
		{"", "_", "book_catalog", "Publisher", "book_catalog_Publisher"},
		{"app_", "", "book_catalog", "Publisher", "app_book_catalog__Publisher"},
		{"app_", "_x_", "book_catalog", "Publisher", "app_book_catalog_x_Publisher"},
		{"Tenant1_", "Z", " geo ", "District", "Tenant1_geoZDistrict"},
		{"app_", "_x_", "", "Person", "Person"},
		{"app_", "_x_", "   ", "Person", "Person"},
		{"app_", "_x_", "book_catalog", "   ", ""},
		{"", "", "geo-regions", "District", "geo_regions__District"},
		{"", "", "my.schema", "Foo", "my_schema__Foo"},
		{"", "", "path/to", "Bar", "path_to__Bar"},
	}
	for _, tt := range tests {
		l := Labels{Prefix: tt.prefix, Separator: tt.separator}
		if got := l.Compose(tt.schema, tt.typ); got != tt.want {
			t.Errorf("prefix %q, separator %q: Compose(%q, %q) = %q; want %q",
				tt.prefix, tt.separator, tt.schema, tt.typ, got, tt.want)
		}
	}
}

// Distinct raw names whose sanitized forms coincide compose one label, which
// is the collision the label check guards against. The DSL's name productions
// keep such names out of a loaded schema, on which sanitize is the identity, so
// the check's collision branch defends construction paths outside the parser.
func TestLabels_SanitizationCollision(t *testing.T) {
	t.Parallel()
	l := Labels{}
	if got, want := l.Compose("collide", "Foo-Bar"), l.Compose("collide", "Foo_Bar"); got != want {
		t.Fatalf("Compose(Foo-Bar) = %q, Compose(Foo_Bar) = %q; sanitization should map both to one label", got, want)
	}
}

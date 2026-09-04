package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// CanonicalPropertyName answers the same way before and after sealing: the
// unsealed walk and the sealed map both keep the last declaration for a folded
// name, and a sealed type never rebuilds the map.
func TestType_CanonicalPropertyName_AgreesBeforeAndAfterSealing(t *testing.T) {
	t.Parallel()

	prop := func(name string) *schema.Property {
		return schema.TestNewProperty(name, location.Span{}, "", schema.NewStringConstraint(),
			schema.DataTypeRef{}, true, false, schema.DeclaringScope{}, nil)
	}
	typ := schema.TestNewType("T", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSetTypeAllProperties(typ, []*schema.Property{prop("id"), prop("Name")})

	if got, ok := typ.CanonicalPropertyName("name"); !ok || got != "Name" {
		t.Errorf("unsealed: CanonicalPropertyName(name) = %q, %v; want Name", got, ok)
	}
	if _, ok := typ.CanonicalPropertyName("missing"); ok {
		t.Error("unsealed: an unknown name must not resolve")
	}
	schema.TestSealType(typ)
	if got, ok := typ.CanonicalPropertyName("name"); !ok || got != "Name" {
		t.Errorf("sealed: CanonicalPropertyName(name) = %q, %v; want Name", got, ok)
	}
}

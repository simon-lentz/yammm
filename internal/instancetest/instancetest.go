package instancetest

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Valid validates props as one typeName instance over s and fails tb when the
// validator refuses them. A fixture that states an edge or a composition writes
// it as a validator's input: a relation's field name holding a target object, a
// child object, or an array of them.
func Valid(tb testing.TB, s schema.Schema, typeName string, props map[string]any) instance.ValidInstance {
	tb.Helper()
	return ValidRaw(tb, s, typeName, instance.RawInstance{Properties: props})
}

// ValidRaw is [Valid] over a whole raw instance, for a fixture that states a
// provenance.
func ValidRaw(tb testing.TB, s schema.Schema, typeName string, raw instance.RawInstance) instance.ValidInstance {
	tb.Helper()
	valid, res := instance.NewValidator(s).ValidateOne(tb.Context(), typeName, raw)
	if res.HasErrors() || valid.IsZero() {
		tb.Fatalf("instancetest: validate %s: %s", typeName, res)
	}
	return valid
}

// TypeID returns the identity of a type named typeName in a schema loaded under
// sourceName, which gives a test an identity its own schema does not declare.
func TypeID(tb testing.TB, sourceName, typeName string) schema.TypeID {
	tb.Helper()
	s, res := schema.LoadString(tb.Context(), "schema \"identity\"\n\ntype "+typeName+" {\n\tid String primary\n}\n", sourceName)
	if res.HasErrors() {
		tb.Fatalf("instancetest.TypeID: load %s: %s", typeName, res)
	}
	typ, ok := s.Type(typeName)
	if !ok {
		tb.Fatalf("instancetest.TypeID: the schema declares no type %s", typeName)
	}
	return typ.ID()
}

package diag

import "testing"

func TestDetailKeyConstants(t *testing.T) {
	// Verify all standard detail keys are non-empty and follow naming conventions
	keys := []struct {
		name  string
		value string
	}{
		{"DetailKeyExpected", DetailKeyExpected},
		{"DetailKeyGot", DetailKeyGot},
		{"DetailKeyTypeName", DetailKeyTypeName},
		{"DetailKeyPropertyName", DetailKeyPropertyName},
		{"DetailKeyPrefix", DetailKeyPrefix},
		{"DetailKeyRelationName", DetailKeyRelationName},
		{"DetailKeyPrimaryKey", DetailKeyPrimaryKey},
		{"DetailKeyReason", DetailKeyReason},
		{"DetailKeyField", DetailKeyField},
		{"DetailKeyJsonField", DetailKeyJsonField},
		{"DetailKeyDetail", DetailKeyDetail},
		{"DetailKeyFormat", DetailKeyFormat},
		{"DetailKeyTargetType", DetailKeyTargetType},
		{"DetailKeyTargetPK", DetailKeyTargetPK},
		{"DetailKeyImportPath", DetailKeyImportPath},
		{"DetailKeyAlias", DetailKeyAlias},
		{"DetailKeyCycle", DetailKeyCycle},
		{"DetailKeyName", DetailKeyName},
		{"DetailKeyContext", DetailKeyContext},
		{"DetailKeyId", DetailKeyId},
		{"DetailKeyFunction", DetailKeyFunction},
	}

	for _, k := range keys {
		t.Run(k.name, func(t *testing.T) {
			if k.value == "" {
				t.Errorf("%s is empty", k.name)
			}
			// Verify lower_snake_case (no uppercase letters)
			for _, r := range k.value {
				if r >= 'A' && r <= 'Z' {
					t.Errorf("%s contains uppercase: %q", k.name, k.value)
					break
				}
			}
		})
	}
}

func TestDetailKeyConstants_Uniqueness(t *testing.T) {
	keys := []string{
		DetailKeyExpected,
		DetailKeyGot,
		DetailKeyTypeName,
		DetailKeyPropertyName,
		DetailKeyPrefix,
		DetailKeyRelationName,
		DetailKeyPrimaryKey,
		DetailKeyReason,
		DetailKeyField,
		DetailKeyJsonField,
		DetailKeyDetail,
		DetailKeyFormat,
		DetailKeyTargetType,
		DetailKeyTargetPK,
		DetailKeyImportPath,
		DetailKeyAlias,
		DetailKeyCycle,
		DetailKeyName,
		DetailKeyContext,
		DetailKeyId,
		DetailKeyFunction,
	}

	seen := make(map[string]bool)
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate key: %q", k)
		}
		seen[k] = true
	}
}

func TestExpectedGot(t *testing.T) {
	details := ExpectedGot("string", "int")

	if len(details) != 2 {
		t.Fatalf("ExpectedGot returned %d details; want 2", len(details))
	}

	if details[0].Key != DetailKeyExpected {
		t.Errorf("first detail key = %q; want %q", details[0].Key, DetailKeyExpected)
	}
	if details[0].Value != "string" {
		t.Errorf("first detail value = %q; want %q", details[0].Value, "string")
	}

	if details[1].Key != DetailKeyGot {
		t.Errorf("second detail key = %q; want %q", details[1].Key, DetailKeyGot)
	}
	if details[1].Value != "int" {
		t.Errorf("second detail value = %q; want %q", details[1].Value, "int")
	}
}

func TestTypeProp(t *testing.T) {
	details := TypeProp("Person", "name")

	if len(details) != 2 {
		t.Fatalf("TypeProp returned %d details; want 2", len(details))
	}

	if details[0].Key != DetailKeyTypeName {
		t.Errorf("first detail key = %q; want %q", details[0].Key, DetailKeyTypeName)
	}
	if details[0].Value != "Person" {
		t.Errorf("first detail value = %q; want %q", details[0].Value, "Person")
	}

	if details[1].Key != DetailKeyPropertyName {
		t.Errorf("second detail key = %q; want %q", details[1].Key, DetailKeyPropertyName)
	}
	if details[1].Value != "name" {
		t.Errorf("second detail value = %q; want %q", details[1].Value, "name")
	}
}

func TestTypeRelation(t *testing.T) {
	details := TypeRelation("Person", "friends")

	if len(details) != 2 {
		t.Fatalf("TypeRelation returned %d details; want 2", len(details))
	}

	if details[0].Key != DetailKeyTypeName {
		t.Errorf("first detail key = %q; want %q", details[0].Key, DetailKeyTypeName)
	}
	if details[0].Value != "Person" {
		t.Errorf("first detail value = %q; want %q", details[0].Value, "Person")
	}

	if details[1].Key != DetailKeyRelationName {
		t.Errorf("second detail key = %q; want %q", details[1].Key, DetailKeyRelationName)
	}
	if details[1].Value != "friends" {
		t.Errorf("second detail value = %q; want %q", details[1].Value, "friends")
	}
}

func TestRelationField(t *testing.T) {
	details := RelationField("owns", "unknownField")

	if len(details) != 2 {
		t.Fatalf("RelationField returned %d details; want 2", len(details))
	}

	if details[0].Key != DetailKeyRelationName {
		t.Errorf("first detail key = %q; want %q", details[0].Key, DetailKeyRelationName)
	}
	if details[0].Value != "owns" {
		t.Errorf("first detail value = %q; want %q", details[0].Value, "owns")
	}

	if details[1].Key != DetailKeyField {
		t.Errorf("second detail key = %q; want %q", details[1].Key, DetailKeyField)
	}
	if details[1].Value != "unknownField" {
		t.Errorf("second detail value = %q; want %q", details[1].Value, "unknownField")
	}
}

func TestTypeField(t *testing.T) {
	details := TypeField("Car", "invalidField")

	if len(details) != 2 {
		t.Fatalf("TypeField returned %d details; want 2", len(details))
	}

	if details[0].Key != DetailKeyTypeName {
		t.Errorf("first detail key = %q; want %q", details[0].Key, DetailKeyTypeName)
	}
	if details[0].Value != "Car" {
		t.Errorf("first detail value = %q; want %q", details[0].Value, "Car")
	}

	if details[1].Key != DetailKeyField {
		t.Errorf("second detail key = %q; want %q", details[1].Key, DetailKeyField)
	}
	if details[1].Value != "invalidField" {
		t.Errorf("second detail value = %q; want %q", details[1].Value, "invalidField")
	}
}

func TestPathRelation(t *testing.T) {
	details := PathRelation("OwnedCars", "owned_cars")

	if len(details) != 2 {
		t.Fatalf("PathRelation returned %d details; want 2", len(details))
	}

	if details[0].Key != DetailKeyRelationName {
		t.Errorf("first detail key = %q; want %q", details[0].Key, DetailKeyRelationName)
	}
	if details[0].Value != "OwnedCars" {
		t.Errorf("first detail value = %q; want %q", details[0].Value, "OwnedCars")
	}

	if details[1].Key != DetailKeyJsonField {
		t.Errorf("second detail key = %q; want %q", details[1].Key, DetailKeyJsonField)
	}
	if details[1].Value != "owned_cars" {
		t.Errorf("second detail value = %q; want %q", details[1].Value, "owned_cars")
	}
}

func TestDetail_ZeroValue(t *testing.T) {
	var d Detail
	if d.Key != "" {
		t.Errorf("zero Detail.Key = %q; want empty", d.Key)
	}
	if d.Value != "" {
		t.Errorf("zero Detail.Value = %q; want empty", d.Value)
	}
}

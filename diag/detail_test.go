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
		{"DetailKeyRelationName", DetailKeyRelationName},
		{"DetailKeyPrimaryKey", DetailKeyPrimaryKey},
		{"DetailKeyReason", DetailKeyReason},
		{"DetailKeyField", DetailKeyField},
		{"DetailKeyJSONField", DetailKeyJSONField},
		{"DetailKeyDetail", DetailKeyDetail},
		{"DetailKeyFormat", DetailKeyFormat},
		{"DetailKeyTargetType", DetailKeyTargetType},
		{"DetailKeyTargetPK", DetailKeyTargetPK},
		{"DetailKeyImportPath", DetailKeyImportPath},
		{"DetailKeyAlias", DetailKeyAlias},
		{"DetailKeyName", DetailKeyName},
		{"DetailKeyContext", DetailKeyContext},
		{"DetailKeyID", DetailKeyID},
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
		DetailKeyRelationName,
		DetailKeyPrimaryKey,
		DetailKeyReason,
		DetailKeyField,
		DetailKeyJSONField,
		DetailKeyDetail,
		DetailKeyFormat,
		DetailKeyTargetType,
		DetailKeyTargetPK,
		DetailKeyImportPath,
		DetailKeyAlias,
		DetailKeyName,
		DetailKeyContext,
		DetailKeyID,
	}

	seen := make(map[string]bool)
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate key: %q", k)
		}
		seen[k] = true
	}
}

// TestDetailPairBuilders covers every two-detail convenience constructor:
// each returns exactly two details with its documented key pair in order.
func TestDetailPairBuilders(t *testing.T) {
	tests := []struct {
		name            string
		got             []Detail
		wantKey0, want0 string
		wantKey1, want1 string
	}{
		{"ExpectedGot", ExpectedGot("string", "int"), DetailKeyExpected, "string", DetailKeyGot, "int"},
		{"TypeProp", TypeProp("Person", "name"), DetailKeyTypeName, "Person", DetailKeyPropertyName, "name"},
		{"RelationField", RelationField("owns", "unknownField"), DetailKeyRelationName, "owns", DetailKeyField, "unknownField"},
		{"TypeField", TypeField("Car", "invalidField"), DetailKeyTypeName, "Car", DetailKeyField, "invalidField"},
		{"PathRelation", PathRelation("OwnedCars", "owned_cars"), DetailKeyRelationName, "OwnedCars", DetailKeyJSONField, "owned_cars"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != 2 {
				t.Fatalf("%s returned %d details; want 2", tt.name, len(tt.got))
			}
			if tt.got[0].Key != tt.wantKey0 || tt.got[0].Value != tt.want0 {
				t.Errorf("first detail = {%q, %q}; want {%q, %q}", tt.got[0].Key, tt.got[0].Value, tt.wantKey0, tt.want0)
			}
			if tt.got[1].Key != tt.wantKey1 || tt.got[1].Value != tt.want1 {
				t.Errorf("second detail = {%q, %q}; want {%q, %q}", tt.got[1].Key, tt.got[1].Value, tt.wantKey1, tt.want1)
			}
		})
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

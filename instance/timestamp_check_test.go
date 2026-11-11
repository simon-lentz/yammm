package instance_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// `=~ Timestamp` reads RFC 3339 whatever layout the property declares: an
// invariant sees the held value and no property's constraint.
func TestDatatypeCheck_TimestampReadsRFC3339WhateverAPropertyDeclares(t *testing.T) {
	t.Parallel()
	load := func(t *testing.T, property string) *instance.Validator {
		t.Helper()
		s, res := schema.LoadString(t.Context(), "schema \"t\"\n\ntype T {\n\tid String primary\n\td "+property+"\n\t! \"rfc\" d =~ Timestamp\n}\n", "t.yammm")
		if res.HasErrors() {
			t.Fatal(res)
		}
		return instance.NewValidator(s)
	}
	rows := []struct {
		name     string
		property string
		value    any
		wantCode diag.Code
	}{
		{"a date under a date layout fails the check", `Timestamp["2006-01-02"]`, "2030-01-02", diag.E_INVARIANT_FAIL},
		{"a time.Time under a date layout fails the check", `Timestamp["2006-01-02"]`, time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC), diag.E_INVARIANT_FAIL},
		{"RFC 3339 text under no layout passes the check", "Timestamp", "2030-01-02T00:00:00Z", diag.Code{}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			_, res := load(t, r.property).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "1", "d": r.value}})
			if r.wantCode.IsZero() {
				if res.HasErrors() {
					t.Fatalf("ValidateOne = %v, want no error", res)
				}
				return
			}
			var codes []diag.Code
			for issue := range res.Issues() {
				codes = append(codes, issue.Code())
			}
			if len(codes) != 1 || codes[0] != r.wantCode {
				t.Fatalf("ValidateOne codes = %v, want exactly %v", codes, r.wantCode)
			}
		})
	}
}

// A time.Time and the text it renders as are one value: the validator stores
// both as one text, at a key too, so a graph holds one instance for the two.
// RFC 3339 writes an offset under a minute as +00:00, which is stored as Z.
func TestValidateOne_ATimeAndItsTextReachOneKey(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"t\"\n\ntype T {\n\tat Timestamp primary\n\tseen Timestamp\n}\n", "t.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	validator := instance.NewValidator(s)
	aSecondEast := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 1))
	const stored = "2024-01-02T03:04:05Z"
	for _, given := range []any{aSecondEast, "2024-01-02T03:04:05+00:00", stored} {
		valid, res := validator.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"at": given, "seen": given}})
		if res.HasErrors() {
			t.Fatalf("%#v: ValidateOne = %v", given, res)
		}
		if key := valid.PrimaryKey().String(); key != `["`+stored+`"]` {
			t.Errorf("%#v: the key is %s; want [%q]", given, key, stored)
		}
		for _, name := range []string{"at", "seen"} {
			if held, _ := valid.Property(name); held.Unwrap() != stored {
				t.Errorf("%#v: %s holds %#v; want %q", given, name, held.Unwrap(), stored)
			}
		}
	}
}

package instance_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// `=~ Timestamp` reads RFC 3339 whatever the property declares: an invariant
// sees the held value and no property's constraint.
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
		{"a date at a Date property fails the check", "Date", "2030-01-02", diag.E_INVARIANT_FAIL},
		{"a time.Time at a Date property fails the check", "Date", time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC), diag.E_INVARIANT_FAIL},
		{"a date at a String property fails the check", "String", "2030-01-02", diag.E_INVARIANT_FAIL},
		{"RFC 3339 text at a String property passes the check", "String", "2030-01-02T00:00:00Z", diag.Code{}},
		{"RFC 3339 text at a Timestamp property passes the check", "Timestamp", "2030-01-02T00:00:00Z", diag.Code{}},
		{"a time.Time at a Timestamp property passes the check", "Timestamp", time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC), diag.Code{}},
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

// The validator stores a Timestamp only as text time.Time's own encoder
// writes. It refuses a UTC offset hour of 24 in each form and at each
// position, and a time.Time exchanges through JSON what it stores.
func TestValidateOne_AnOffsetHourOfTwentyFourIsRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"t\"\n\ntype T {\n\tid String primary\n\tseen Timestamp\n\tall List<Timestamp>\n}\n\ntype K {\n\tat Timestamp primary\n}\n", "t.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	validator := instance.NewValidator(s)
	at := func(offsetSeconds int) time.Time {
		return time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", offsetSeconds))
	}
	for _, given := range []any{
		"2024-01-02T03:04:05+24:00",
		"2024-01-02T03:04:05-24:59",
		"2024-01-02T03:04:05+23:60",
		at(24 * 3600),
		at(-24 * 3600),
	} {
		for position, raw := range map[string]struct {
			typ   string
			props map[string]any
		}{
			"a property":     {"T", map[string]any{"id": "1", "seen": given}},
			"a list element": {"T", map[string]any{"id": "1", "all": []any{"2024-01-02T03:04:05Z", given}}},
			"a key":          {"K", map[string]any{"at": given}},
		} {
			_, res := validator.ValidateOne(t.Context(), raw.typ, instance.RawInstance{Properties: raw.props})
			var codes []diag.Code
			for issue := range res.Issues() {
				codes = append(codes, issue.Code())
			}
			if len(codes) != 1 || codes[0] != diag.E_CONSTRAINT_FAIL {
				t.Errorf("%#v at %s: ValidateOne codes = %v; want exactly %v", given, position, codes, diag.E_CONSTRAINT_FAIL)
			}
		}
	}

	// The controls: the widest offset either side, which is stored and which
	// a time.Time decodes and encodes as written.
	for given, stored := range map[any]string{
		"2024-01-02T03:04:05+23:59": "2024-01-02T03:04:05+23:59",
		"2024-01-02T03:04:05-23:59": "2024-01-02T03:04:05-23:59",
		at(24*3600 - 1):             "2024-01-02T03:04:05+23:59",
		at(-(24*3600 - 1)):          "2024-01-02T03:04:05-23:59",
	} {
		valid, res := validator.ValidateOne(t.Context(), "K", instance.RawInstance{Properties: map[string]any{"at": given}})
		if res.HasErrors() {
			t.Errorf("%#v: ValidateOne = %v; want it stored", given, res)
			continue
		}
		held, _ := valid.Property("at")
		if held.Unwrap() != stored {
			t.Errorf("%#v: at holds %#v; want %q", given, held.Unwrap(), stored)
		}
		var decoded time.Time
		encoded, err := json.Marshal(stored)
		if err == nil {
			err = json.Unmarshal(encoded, &decoded)
		}
		if err != nil {
			t.Errorf("%#v: a time.Time does not decode the stored %q: %v", given, stored, err)
			continue
		}
		if again, err := json.Marshal(decoded); err != nil || string(again) != string(encoded) {
			t.Errorf("%#v: a time.Time encodes the stored %q as %s (%v)", given, stored, again, err)
		}
	}
}

package instance_test

import (
	"maps"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const utf8Schema = `schema "utf8"

type Target {
	key String primary
}

type Holder {
	id String primary
	text String
	code Pattern["^a.b$"]
	kind Enum["ab", "cd"]
	uid UUID
	day Date
	at Timestamp
	stamp Timestamp["2006-01-02 MST"]
	tags List<String>
	codes List<Pattern["^a.b$"]>
	--> LINK (_:many) Target {
		label String
		mark Pattern["^a.b$"]
	}
}
`

// namedText and its pointer are the carriers [instance.Validator] reads a
// string through beside the plain Go string.
type namedText string

// invalidText is a string whose middle byte is no UTF-8 sequence. Go's regexp
// reads that byte as U+FFFD, so it matches the schema's "^a.b$".
const invalidText = "a\xffb"

func utf8Raw(over map[string]any) instance.RawInstance {
	props := map[string]any{"id": "h1"}
	maps.Copy(props, over)
	return instance.RawInstance{Properties: props}
}

func utf8Link(over map[string]any) map[string]any {
	target := map[string]any{"_target_key": "t1"}
	maps.Copy(target, over)
	return map[string]any{"link": []any{target}}
}

// A string value is valid UTF-8, at every position that holds text: the
// validator refuses one that is not as a type mismatch, and the diagnostic is
// itself valid UTF-8.
func TestValidateOne_RefusesAStringThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), utf8Schema, "utf8.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	v := instance.NewValidator(s)
	ptr := invalidText

	for _, c := range []struct {
		name string
		raw  instance.RawInstance
	}{
		{"a String property", utf8Raw(map[string]any{"text": invalidText})},
		{"a String property held in a named type", utf8Raw(map[string]any{"text": namedText(invalidText)})},
		{"a String property held behind a pointer", utf8Raw(map[string]any{"text": &ptr})},
		{"a Pattern property its pattern matches", utf8Raw(map[string]any{"code": invalidText})},
		{"an Enum property", utf8Raw(map[string]any{"kind": "a\xff"})},
		{"a UUID property", utf8Raw(map[string]any{"uid": "6ba7b810-9dad-11d1-80b4-00c04fd430c\xff"})},
		{"a Date property", utf8Raw(map[string]any{"day": "2026-01-0\xff"})},
		{"a Timestamp property", utf8Raw(map[string]any{"at": "2026-01-02T03:04:05\xff"})},
		{"a Timestamp property with a declared layout", utf8Raw(map[string]any{"stamp": "2026-01-02 \xff\xff\xff"})},
		{"a List<String> element", utf8Raw(map[string]any{"tags": []any{"ok", invalidText}})},
		{"a List<String> element in a []string", utf8Raw(map[string]any{"tags": []string{"ok", invalidText}})},
		{"a List<Pattern> element", utf8Raw(map[string]any{"codes": []any{invalidText}})},
		{"a primary key component", instance.RawInstance{Properties: map[string]any{"id": invalidText}}},
		{"a foreign key component", utf8Raw(map[string]any{"link": []any{map[string]any{"_target_key": invalidText}}})},
		{"a String edge property", utf8Raw(utf8Link(map[string]any{"label": invalidText}))},
		{"a Pattern edge property", utf8Raw(utf8Link(map[string]any{"mark": invalidText}))},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			inst, res := v.ValidateOne(t.Context(), "Holder", c.raw)
			if inst != nil {
				t.Errorf("ValidateOne returned an instance holding a string that is not valid UTF-8")
			}
			n := 0
			for issue := range res.Issues() {
				n++
				if issue.Code() != diag.E_TYPE_MISMATCH {
					t.Errorf("issue %s, want %s: %s", issue.Code(), diag.E_TYPE_MISMATCH, issue.Message())
				}
				if !utf8.ValidString(issue.Message()) {
					t.Errorf("the message is not valid UTF-8: %q", issue.Message())
				}
				for _, d := range issue.Details() {
					if !utf8.ValidString(d.Value) {
						t.Errorf("detail %s is not valid UTF-8: %q", d.Key, d.Value)
					}
				}
			}
			if n != 1 {
				t.Errorf("ValidateOne reported %d issues, want one: %s", n, res)
			}
		})
	}

	// The same positions hold valid text, U+FFFD itself included: the rule
	// refuses the bytes, never the character a decoder writes for them.
	t.Run("valid text at every position is accepted", func(t *testing.T) {
		t.Parallel()
		const valid = "a�b"
		raw := utf8Raw(map[string]any{
			"text": namedText(valid), "code": valid, "kind": "ab",
			"uid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8", "day": "2026-01-02",
			"at": "2026-01-02T03:04:05Z", "stamp": "2026-01-02 UTC",
			"tags": []string{"é", valid}, "codes": []any{valid},
			"link": []any{map[string]any{"_target_key": valid, "label": valid, "mark": valid}},
		})
		raw.Properties["id"] = valid
		inst, res := v.ValidateOne(t.Context(), "Holder", raw)
		if inst == nil || res.HasErrors() {
			t.Fatalf("ValidateOne refused valid text: %s", res)
		}
		if got, _ := inst.Property("text"); got.Unwrap() != valid {
			t.Errorf("text = %q, want %q", got.Unwrap(), valid)
		}
	})
}

// The exported check and coercion, and the key read, apply the rule the
// validator applies: none of them passes a string that is not valid UTF-8.
func TestCheckValue_RefusesAStringThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), utf8Schema, "utf8.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	holder, _ := s.Type("Holder")
	for _, name := range []string{"id", "text", "code", "kind", "uid", "day", "at", "stamp"} {
		prop, _ := holder.Property(name)
		if err := instance.CheckValue(invalidText, prop.Constraint()); err == nil {
			t.Errorf("CheckValue passed a string that is not valid UTF-8 at %s", name)
		} else if !utf8.ValidString(err.Error()) {
			t.Errorf("CheckValue's error at %s is not valid UTF-8: %q", name, err.Error())
		}
	}
	for _, name := range []string{"tags", "codes"} {
		prop, _ := holder.Property(name)
		if err := instance.CheckValue([]any{invalidText}, prop.Constraint()); err == nil {
			t.Errorf("CheckValue passed a list holding a string that is not valid UTF-8 at %s", name)
		}
	}

	v := instance.NewValidator(s)
	if _, _, ok := v.PrimaryKeyOf("Holder", instance.RawInstance{Properties: map[string]any{"id": invalidText}}); ok {
		t.Errorf("PrimaryKeyOf returned a key holding a string that is not valid UTF-8")
	}
	if _, key, ok := v.PrimaryKeyOf("Holder", instance.RawInstance{Properties: map[string]any{"id": "a�b"}}); !ok || key.String() != `["a�b"]` && key.String() != "[\"a�b\"]" {
		t.Errorf("PrimaryKeyOf = %s, %v for a valid key", key, ok)
	}
}

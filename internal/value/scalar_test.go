package value

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
)

type (
	namedText   string
	namedInt    int16
	namedFloat  float32
	selfMarshal string
)

func (selfMarshal) MarshalJSON() ([]byte, error) { return []byte(`"spelled"`), nil }

// Scalar reads a value as the one form every writer writes: a scalar through
// any pointers and interfaces, a named type as its base value, three library
// types as themselves, and nothing else.
func TestScalar_ReadsTheOneFormEveryWriterWrites(t *testing.T) {
	t.Parallel()
	text := "a"
	ptrText := &text
	var held any = json.Number("")
	var heldPtr any = ptrText
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	var nilMap map[int]string
	var nilPtr *string
	var self selfPointer
	self = &self
	for _, c := range []struct {
		name string
		in   any
		want any
		ok   bool
	}{
		{"nil", nil, nil, true},
		{"a string", "a", "a", true},
		{"a named string", namedText("a"), "a", true},
		{"a self-spelling named string is its base value", selfMarshal("a"), "a", true},
		{"a pointer to a string", ptrText, "a", true},
		{"a pointer to a pointer to a string", &ptrText, "a", true},
		{"a nil pointer", nilPtr, nil, true},
		{"a pointer to an interface holding a json.Number", &held, json.Number(""), true},
		{"a pointer to an interface holding a pointer to a string", &heldPtr, "a", true},
		{"a nil unsafe.Pointer", unsafe.Pointer(nil), nil, true},
		{"a named int", namedInt(-3), int64(-3), true},
		{"a uint8", uint8(7), uint64(7), true},
		{"a uintptr", uintptr(9), uint64(9), true},
		{"a named float32 keeps its width", namedFloat(0.1), float32(0.1), true},
		{"a bool", true, true, true},
		{"a json.Number", json.Number("1e400"), json.Number("1e400"), true},
		{"a time", now, now, true},
		{"a pointer to a time", &now, now, true},
		{"a uuid", id, id, true},
		{"a nil map keyed by an integer", nilMap, nil, true},
		{"a struct", struct{ S string }{"a"}, nil, false},
		{"an array", [1]string{"a"}, nil, false},
		{"a map keyed by an integer", map[int]string{1: "a"}, nil, false},
		{"a raw slice", []string{"a"}, nil, false},
		{"a pointer to a slice", &[]string{"a"}, nil, false},
		{"a complex number", complex(1, 2), nil, false},
		{"a channel", make(chan int), nil, false},
		{"a pointer that points to itself", self, nil, false},
	} {
		got, ok := Scalar(c.in)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Scalar = %#v, %v; want %#v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

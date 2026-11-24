package immutable

import (
	"fmt"
	"reflect"
)

// Value holds one value immutably: a string-keyed map or a slice as the
// container [Wrap] copied it into, a map with another key type as a deep copy,
// and any other value as given.
//
// Value is safe for concurrent read access.
type Value struct {
	// val is a [Properties], a [Slice], a [Key], a deep copy of a map with a
	// non-string key, or any other value as given, nil included.
	val any
}

// Wrap returns v held immutably. It copies every map and slice it reaches: a
// string-keyed map into a [Properties], a slice into a [Slice] and a map with
// another key type into a deep copy, so a later change to v reaches nothing Wrap
// returned. Any other value, a struct, a pointer or an array among them, is
// stored as given, with what it refers to; the package doc states the rules.
func Wrap(v any) Value {
	return Value{val: wrapValue(v)}
}

// Any returns the stored value: a scalar as itself, and a container as the
// [Properties] or [Slice] that holds it, not as a Go map or slice. A map with a
// non-string key is stored as a Go map, so Any returns a fresh deep copy of it.
// [Value.Clone] returns Go data.
func (v Value) Any() any {
	switch v.val.(type) {
	case nil, string, bool, int64, float64, Properties, Slice, Key:
		return v.val
	}
	if reflect.TypeOf(v.val).Kind() == reflect.Map {
		return cloneValue(v)
	}
	return v.val
}

// Clone returns the stored value as mutable Go data: a [Properties] as a
// map[string]any, a [Slice] or [Key] as a []any, a map with another key type as
// a deep copy, and anything else as it is. A nil container clones to a typed
// nil, so a nil container and an empty one stay apart. Any value Wrap stored as
// given, and an element a map with a non-string key keeps as itself, is shared
// with the Value.
func (v Value) Clone() any {
	return cloneValue(v)
}

// String returns the stored value formatted as [fmt.Sprint] formats
// [Value.Clone]'s result, so a container prints as Go data; [Value.AsString]
// reads a string value. A cycle fmt follows exhausts the stack.
func (v Value) String() string {
	return fmt.Sprint(cloneValue(v))
}

// AsBool returns the value and true when it is a bool, and false, false
// otherwise.
func (v Value) AsBool() (bool, bool) {
	b, ok := v.val.(bool)
	return b, ok
}

// AsInt returns the value and true when it is an int64, the form an Integer is
// stored in, and 0, false otherwise. It reads no other integer type and no
// float, whole or not.
func (v Value) AsInt() (int64, bool) {
	n, ok := v.val.(int64)
	return n, ok
}

// AsFloat returns the value and true when it is a float64, the form a Float is
// stored in, and 0, false otherwise. It reads no integer and no float32.
func (v Value) AsFloat() (float64, bool) {
	f, ok := v.val.(float64)
	return f, ok
}

// AsString returns the value and true when it is a string, and "", false
// otherwise.
func (v Value) AsString() (string, bool) {
	s, ok := v.val.(string)
	return s, ok
}

// AsSlice returns the value and true when it is a [Slice], and the zero Slice,
// false otherwise. A nil slice wrapped is a nil Slice, which reports true;
// [IsNilContainer] tells it from an empty one.
func (v Value) AsSlice() (Slice, bool) {
	s, ok := v.val.(Slice)
	return s, ok
}

// wrapValue copies v into what a Value stores.
func wrapValue(v any) any {
	return wrapValueAt(v, 0, nil)
}

// wrapValueAt is wrapValue on one walk's depth and path. A Value contributes
// its content, so no Value directly holds a Value; a container is a struct, so
// the default arm stores it as itself.
func wrapValueAt(v any, depth int, seen map[cyclePtr]struct{}) any {
	if v == nil {
		return nil
	}
	if w, ok := v.(Value); ok {
		return w.val
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return wrapMapValue(rv, depth, seen)
	case reflect.Slice:
		return wrapSliceValue(rv, depth, seen)
	default:
		// Scalars, structs, pointers and arrays are stored as they are.
		return v
	}
}

// wrapMapValue copies a map: a string-keyed one into a Properties, whose
// values are wrapped in turn, and any other into a deep copy of the map.
func wrapMapValue(rv reflect.Value, depth int, seen map[cyclePtr]struct{}) any {
	if rv.Type().Key().Kind() != reflect.String {
		return deepCloneMap(rv, depth, seen)
	}
	if rv.IsNil() {
		// A nil Properties, so a nil map stays apart from literal nil.
		return Properties{}
	}
	seen = enterCycle(rv, depth, seen)
	defer leaveCycle(rv, depth, seen)
	m := make(map[string]Value, rv.Len())
	for it := rv.MapRange(); it.Next(); {
		m[it.Key().String()] = Value{val: wrapValueAt(it.Value().Interface(), depth+1, seen)}
	}
	return newProperties(m)
}

// wrapSliceValue copies a slice into a Slice, whose elements are wrapped in
// turn.
func wrapSliceValue(rv reflect.Value, depth int, seen map[cyclePtr]struct{}) any {
	if rv.IsNil() {
		// A nil Slice, so a nil slice stays apart from literal nil.
		return Slice{}
	}
	seen = enterCycle(rv, depth, seen)
	defer leaveCycle(rv, depth, seen)

	elements := make([]Value, rv.Len())
	for i := range rv.Len() {
		elements[i] = Value{val: wrapValueAt(rv.Index(i).Interface(), depth+1, seen)}
	}
	return Slice{elements: elements}
}

// cloneValue returns a mutable deep copy of v's content, as [Value.Clone]
// states.
func cloneValue(v Value) any {
	return deepCloneAt(v.val, 0, nil)
}

// deepCloneAt deep-clones v on one walk's depth and path. A Value or a
// container clones to its content as Go data.
func deepCloneAt(v any, depth int, seen map[cyclePtr]struct{}) any {
	if v == nil {
		return nil
	}
	if w, ok := v.(Value); ok {
		return deepCloneAt(w.val, depth, seen)
	}
	if w, ok := asWrapper(v); ok {
		return w.cloneToAny()
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return deepCloneMap(rv, depth, seen)
	case reflect.Slice:
		return deepCloneSlice(rv, depth, seen)
	default:
		return v
	}
}

// deepCloneMap deep-clones a map, each element as [cloneElem] clones it.
func deepCloneMap(rv reflect.Value, depth int, seen map[cyclePtr]struct{}) any {
	if rv.IsNil() {
		return rv.Interface() // the typed nil, which an untyped nil would lose
	}
	seen = enterCycle(rv, depth, seen)
	defer leaveCycle(rv, depth, seen)

	newMap := reflect.MakeMapWithSize(rv.Type(), rv.Len())
	elemType := rv.Type().Elem()
	for iter := rv.MapRange(); iter.Next(); {
		newMap.SetMapIndex(iter.Key(), cloneElem(iter.Value().Interface(), elemType, depth+1, seen))
	}
	return newMap.Interface()
}

// deepCloneSlice deep-clones a slice, each element as [cloneElem] clones it.
func deepCloneSlice(rv reflect.Value, depth int, seen map[cyclePtr]struct{}) any {
	if rv.IsNil() {
		return rv.Interface() // the typed nil, which an untyped nil would lose
	}
	seen = enterCycle(rv, depth, seen)
	defer leaveCycle(rv, depth, seen)

	newSlice := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	elemType := rv.Type().Elem()
	for i := range rv.Len() {
		newSlice.Index(i).Set(cloneElem(rv.Index(i).Interface(), elemType, depth+1, seen))
	}
	return newSlice.Interface()
}

// cloneElem returns val deep-cloned for an element of type elem. A [Value] or a
// container whose Go data elem cannot hold, as in a map[int]Value, is kept as
// itself: it never changes, so sharing it is safe.
func cloneElem(val any, elem reflect.Type, depth int, seen map[cyclePtr]struct{}) reflect.Value {
	// Decided by type before the clone, which a kept element would discard.
	if t, adopted := contentType(val); adopted && t != nil && !t.AssignableTo(elem) {
		return reflect.ValueOf(val)
	}
	cloned := deepCloneAt(val, depth, seen)
	if cloned == nil {
		// reflect.ValueOf(nil) is invalid, and a nil interface element is elem's zero.
		return reflect.Zero(elem)
	}
	return reflect.ValueOf(cloned)
}

// contentType returns the type of the Go data a [Value] or a container clones
// to, and false for any other value, which clones to its own type. A Value that
// holds nil has no content type.
func contentType(v any) (reflect.Type, bool) {
	switch w := v.(type) {
	case Value:
		if w.val == nil {
			return nil, true
		}
		if t, ok := contentType(w.val); ok {
			return t, true
		}
		return reflect.TypeOf(w.val), true
	case Properties:
		return reflect.TypeFor[map[string]any](), true
	case Slice, Key:
		return reflect.TypeFor[[]any](), true
	}
	return nil, false
}

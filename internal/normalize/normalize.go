package normalize

import (
	"encoding"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// structFieldCache caches the result of collectStructFields by reflect.Type.
// This avoids redundant reflection traversal when normalizing multiple instances
// of the same struct type.
var structFieldCache sync.Map // map[reflect.Type][]structField

// Normalize converts a graph containing maps, slices, structs, and primitives
// into a plain tree of map[string]any / []any / primitives suitable for
// logging or serialization.
//
// Values implementing encoding.TextMarshaler or fmt.Stringer are converted
// to strings via their respective methods. Struct fields are extracted using
// reflection with support for yammm struct tags.
func Normalize(v any) any {
	if v == nil {
		return nil
	}

	// Check TextMarshaler/Stringer first
	if normalized, ok := normalizeMarshalers(v); ok {
		return normalized
	}

	// Direct type switches for common cases (avoid reflection)
	switch val := v.(type) {
	case map[string]any:
		if val == nil {
			return nil // Return untyped nil to avoid interface nil trap
		}
		return normalizeStringMap(val)
	case []any:
		if val == nil {
			return nil // Return untyped nil to avoid interface nil trap
		}
		return normalizeSlice(val)
	case string:
		return val
	}

	// Reflection fallback for other types
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil
	}

	// Handle pointers first
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Map:
		// Only normalize string-keyed maps
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
		if rv.IsNil() {
			return nil // Return untyped nil to avoid interface nil trap
		}
		return normalizeReflectMap(rv)
	case reflect.Slice:
		if rv.IsNil() {
			return nil // Return untyped nil to avoid interface nil trap
		}
		return normalizeReflectSlice(rv)
	case reflect.Array:
		return normalizeReflectSlice(rv)
	case reflect.Struct:
		return normalizeStruct(rv)
	default:
		return v
	}
}

// normalizeStringMap normalizes a map[string]any by recursively normalizing values.
func normalizeStringMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = Normalize(v)
	}
	return out
}

// normalizeSlice normalizes a []any by recursively normalizing elements.
func normalizeSlice(s []any) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = Normalize(v)
	}
	return out
}

// normalizeReflectMap normalizes a reflect.Value map with string keys.
func normalizeReflectMap(rv reflect.Value) map[string]any {
	if rv.IsNil() {
		return nil
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		k := iter.Key().String()
		out[k] = Normalize(iter.Value().Interface())
	}
	return out
}

// normalizeReflectSlice normalizes a reflect.Value slice or array.
func normalizeReflectSlice(rv reflect.Value) []any {
	length := rv.Len()
	out := make([]any, length)
	for i := range length {
		out[i] = Normalize(rv.Index(i).Interface())
	}
	return out
}

// normalizeStruct converts a struct to map[string]any.
func normalizeStruct(rv reflect.Value) map[string]any {
	fields := collectStructFields(rv.Type())
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		val := fieldValue(rv, f.path)
		if val.IsValid() && val.CanInterface() {
			out[f.name] = Normalize(val.Interface())
		}
	}
	return out
}

// normalizeMarshalers checks for TextMarshaler and Stringer interfaces.
//
// If the value implements encoding.TextMarshaler, MarshalText() is called first.
// If MarshalText() returns an error, the error is silently ignored and the
// function falls back to checking for fmt.Stringer. This fallback behavior is
// intentional: it prioritizes producing a normalized result over propagating
// marshaling errors that may not be relevant for normalization purposes.
func normalizeMarshalers(v any) (any, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, false
	}

	// Handle nil pointers
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil, true
	}

	// Try TextMarshaler
	if tm := asTextMarshaler(v); tm != nil {
		if text, err := tm.MarshalText(); err == nil {
			return string(text), true
		}
	}

	// Try Stringer
	if s, ok := v.(fmt.Stringer); ok {
		return s.String(), true
	}

	return nil, false
}

// asTextMarshaler returns a TextMarshaler if the value implements it.
func asTextMarshaler(v any) encoding.TextMarshaler {
	if tm, ok := v.(encoding.TextMarshaler); ok {
		return tm
	}
	return nil
}

// structField represents a collected struct field with its name and access path.
type structField struct {
	name string
	path []int // field index path for nested fields
}

// collectedField is used during field collection to track tag status for dominance.
type collectedField struct {
	name   string
	index  []int
	tagged bool
}

// collectStructFields extracts all normalizable fields from a struct type.
// This handles embedded structs, tag processing, and field shadowing using BFS
// traversal similar to encoding/json.
//
// Results are cached by reflect.Type using sync.Map for thread-safe access.
// This optimization benefits hot paths that normalize many instances of the
// same struct type (e.g., validating thousands of instances against a schema).
func collectStructFields(t reflect.Type) []structField {
	// Check cache first
	if cached, ok := structFieldCache.Load(t); ok {
		if fields, ok := cached.([]structField); ok {
			return fields
		}
	}

	// Compute fields
	fields := collectStructFieldsUncached(t)

	// Store in cache (concurrent stores for the same type are safe and idempotent)
	structFieldCache.Store(t, fields)
	return fields
}

// collectStructFieldsUncached is the uncached implementation of collectStructFields.
func collectStructFieldsUncached(t reflect.Type) []structField {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	type queueItem struct {
		typ   reflect.Type
		index []int
	}

	var (
		current   []queueItem
		next      = []queueItem{{typ: t}}
		nextCount = map[reflect.Type]int{}
		count     map[reflect.Type]int
		fields    []collectedField
		visited   = map[reflect.Type]bool{}
	)

	// BFS through embedded structs
	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[reflect.Type]int{}

		for _, item := range current {
			if visited[item.typ] {
				continue
			}
			visited[item.typ] = true

			for i := range item.typ.NumField() {
				sf := item.typ.Field(i)

				// Handle anonymous embedded fields
				if sf.Anonymous {
					ft := sf.Type
					if ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					// Skip unexported non-struct anonymous fields
					if !sf.IsExported() && ft.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					// Skip unexported non-anonymous fields
					continue
				}

				tag := sf.Tag.Get("yammm")
				if tag == "-" {
					continue
				}
				tagName, tagged := parseTagName(tag)

				index := make([]int, len(item.index)+1)
				copy(index, item.index)
				index[len(item.index)] = i

				ft := sf.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}

				// If it has a name (from tag or field name) or isn't an embedded struct,
				// collect it as a field
				if tagName != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					name := tagName
					if name == "" {
						name = decapitalize(sf.Name)
					}
					field := collectedField{name: name, index: index, tagged: tagged}
					fields = append(fields, field)
					if count[item.typ] > 1 {
						fields = append(fields, field)
					}
					continue
				}

				// Queue embedded struct for processing
				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, queueItem{typ: ft, index: index})
				}
			}
		}
	}

	// Sort by name, then by depth, then by tagged status, then by index
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].name != fields[j].name {
			return fields[i].name < fields[j].name
		}
		if len(fields[i].index) != len(fields[j].index) {
			return len(fields[i].index) < len(fields[j].index)
		}
		if fields[i].tagged != fields[j].tagged {
			return fields[i].tagged
		}
		return indexLess(fields[i].index, fields[j].index)
	})

	// Apply dominance rules: remove ambiguous duplicates
	out := fields[:0]
	for i := 0; i < len(fields); {
		j := i + 1
		for j < len(fields) && fields[j].name == fields[i].name {
			j++
		}
		if j == i+1 {
			// Single field with this name
			out = append(out, fields[i])
		} else if dominant, ok := selectDominant(fields[i:j]); ok {
			// Multiple fields, but one dominates
			out = append(out, dominant)
		}
		// If no dominant field, all are dropped (ambiguous)
		i = j
	}

	// Re-sort by index order for consistent output
	sort.Slice(out, func(i, j int) bool {
		return indexLess(out[i].index, out[j].index)
	})

	// Convert to result type
	result := make([]structField, len(out))
	for i, f := range out {
		result[i] = structField{name: f.name, path: f.index}
	}
	return result
}

// parseTagName extracts the field name from a tag value.
// Returns (name, tagged) where tagged is true if the tag was non-empty.
// An empty name with tagged=true means use the default field name but the field IS tagged.
// This aligns with encoding/json precedence: any explicit tag should dominate untagged fields.
func parseTagName(tag string) (string, bool) {
	if tag == "" {
		return "", false
	}
	name := strings.Split(tag, ",")[0]
	return name, true // Any non-empty tag string means the field is tagged
}

// selectDominant picks the dominant field among fields with the same name.
// A field dominates if it's at a shallower depth or has a tag.
func selectDominant(fields []collectedField) (collectedField, bool) {
	if len(fields) > 1 &&
		len(fields[0].index) == len(fields[1].index) &&
		fields[0].tagged == fields[1].tagged {
		// Same depth and tag status = ambiguous
		return collectedField{}, false
	}
	return fields[0], true
}

// indexLess compares two index paths lexicographically.
func indexLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// fieldValue navigates to a nested field value using an index path.
func fieldValue(rv reflect.Value, path []int) reflect.Value {
	for _, idx := range path {
		// Dereference pointers along the way
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return reflect.Value{}
			}
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			return reflect.Value{}
		}
		rv = rv.Field(idx)
	}
	return rv
}

// decapitalize converts FirstName to firstName.
func decapitalize(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

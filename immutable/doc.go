// Package immutable holds Go values immutably for the public API of the YAMMM
// library. It depends only on the standard library.
//
// # Core Types
//
// [Value] holds one value. [Properties] holds a string-keyed map, such as an
// instance's or an edge's properties. [Wrap] makes one of each string-keyed map
// it reaches, save inside a map with a non-string key.
// [Slice] holds a list. [Key] holds the components of a primary or foreign key.
//
// [Wrap] takes any Go value and copies each map and slice it reaches into the
// matching container. A map with a non-string key is the exception: Wrap stores
// a deep copy of it as Go data (see Copies). A typed map such as map[string]int
// and a typed slice such as []string need no conversion first:
//
//	val := immutable.Wrap(someData)
//	if s, ok := val.AsString(); ok {
//	    fmt.Println(s)
//	}
//
// [WrapProperties], [WrapSlice] and [WrapKey] take a map[string]any or a
// []any and return the container itself:
//
//	props := immutable.WrapProperties(data)
//	for name, v := range props.All() {
//	    fmt.Printf("%s: %v\n", name, v)
//	}
//	if v, ok := props.GetFold("UserName"); ok {
//	    // matches "username", "USERNAME" and "UserName" (ASCII letters only)
//	}
//
//	key := immutable.WrapKey([]any{"us", int64(12345)})
//	fmt.Println(key.String()) // ["us",12345]
//
// # Reading a Value
//
// Each As method reads one stored form, and reports false for any other.
// [Value.AsInt] reads an int64, the form an Integer is stored in. It reads no
// other integer type and no float. [Value.AsFloat] reads a float64 alone.
// [Value.AsBool], [Value.AsString] and [Value.AsSlice] read a bool, a string
// and a [Slice]. A [Properties] or a [Key] is read with a type assertion on
// [Value.Any]:
//
//	if props, ok := val.Any().(immutable.Properties); ok {
//	    // ...
//	}
//
// [Value.Any] returns the stored value: a scalar as itself, and a container as
// the container. A map with a non-string key is stored as a Go map, so Any
// returns a fresh deep copy of it. [Value.Clone] returns the stored value as
// mutable Go data.
// [Value.String] formats what Clone returns, so a Value prints as Go data.
//
// # Copies
//
// Every constructor copies what it is given. After the call, a change the
// caller makes to its value reaches nothing the constructor returned, save
// through a value the rules store as given. The copy follows these rules:
//
//   - A map whose key kind is string becomes a [Properties]. Its values are
//     copied by the same rules.
//   - A slice of any element type becomes a [Slice]. Its elements are copied by
//     the same rules.
//   - A map with another key type becomes a deep copy of its own type. Each map
//     and slice inside it is copied as Go data. A [Value] or a container inside
//     it contributes its content as Go data where the element type holds that
//     data, and is kept as itself where it does not, as in a map[int]Value.
//   - Any other value, a scalar, a struct, a pointer, an array, a channel or a
//     function among them, is stored as given.
//
// A value stored as given is not walked, so what it refers to stays shared
// with the caller. To keep such data apart from the caller, give a map or a
// slice in its place, or do not change it after the call.
//
// # Arrays
//
// An array is stored as it is, not wrapped as a [Slice]. This library spells a
// scalar carrier as an array, not a list: uuid.UUID is [16]byte. A Slice
// would turn one UUID into sixteen byte values. No decoder here produces an
// array for a list position.
//
// # Wrapping a wrapper
//
// A constructor that gets one of this package's own types adopts it. It does
// not store it as an opaque struct. A [Value] contributes its content, so no
// Value directly holds a Value. A [Properties], a [Slice] or a [Key] is already
// immutable, and is stored as itself. Inside a map with a non-string key the
// copy rule decides instead. As a result, a Clone and a [Key]'s
// canonical string read the content, not an empty struct. The one exception
// is an element a map with a non-string key keeps as itself, as in a
// map[int]Value: Clone shares it, and a Key's canonical string renders it as
// an empty object, as encoding/json does. A pointer to one of
// these types is a pointer, and a struct that embeds one is a struct. Each is
// stored as given.
//
// # Cyclic values
//
// The walk refuses a map or a slice that refers to itself with a panic, which a
// caller can recover from. A value Wrap stores as given is not walked, so a
// cycle through one is stored with it. Two readers walk it anyway. [WrapKey]
// renders its key with encoding/json, so it panics on a cycle encoding/json
// follows. [Value.String] formats with fmt, so String exhausts the stack on a
// cycle fmt follows.
// The walk counts its depth. Past the depth where real data
// stops, it records the maps and slices on its path, as encoding/json does. An
// ordinary value pays for a counter and nothing more. Without this guard, a cyclic value exhausts
// the stack, and a caller cannot recover from that fatal runtime error.
//
// # Nil Semantics
//
// [Wrap] stores literal nil as nil, so [Value.Any] returns nil. A nil
// string-keyed map becomes a nil [Properties], and a nil slice becomes a nil
// [Slice]. Each is a container, so it stays apart from literal nil.
// [IsNilContainer] tells a nil container from an empty one:
//
//	var m map[string]any // nil map
//	v := immutable.Wrap(m)
//	v.Any()                                   // Properties{}: a container
//	immutable.IsNilContainer(v.Any())         // true: a nil container
//	immutable.IsNilContainer(immutable.Wrap(map[string]any{}).Any()) // false
//
// A nil map with another key type is copied as that typed nil. A nil pointer,
// a nil channel and a nil function are stored as given. A Clone of a nil
// container returns a typed nil, so a nil container and an empty one stay apart
// as Go data too.
//
// # Thread Safety
//
// All types in this package are safe for concurrent read access. Their content
// does not change after construction. A [Properties] sorts its names on the
// first Keys, All or GetFold miss, and builds its case-folded index on the
// first GetFold miss, each once, under a sync.Once. Every copy of that
// Properties shares the result, and concurrent first use is safe.
//
// # Performance Characteristics
//
// | Operation | Cost | Notes |
// | --------- | ---- | ----- |
// | Wrap(scalar) | O(1) | The scalar is stored as given |
// | Wrap(map) / Wrap(slice) | O(n) deep | Copies each map and slice it reaches |
// | Get(name) / At(i) | O(1) | A map or slice lookup |
// | GetFold(name) | O(1) after the first miss | A miss of the exact name reads the folded index; the first miss sorts the names and builds it in O(n log n), and a later one allocates nothing for a name of up to 64 bytes |
// | Keys() / All() / Values() | O(1) start | Keys and All visit names in sorted order; the first Keys, All or GetFold miss sorts them in O(n log n), and after that a range allocates nothing |
// | Clone() | O(n) deep | Copies every container to Go data; a value stored as given and an element a map with a non-string key keeps as itself are returned as stored |
//
// # Escape Hatch
//
// Each type has a Clone method that returns a mutable copy of its content as Go
// data. A caller that must change a value uses it:
//
//	props := instance.Properties()
//	mutable := props.Clone()
//	mutable["newKey"] = "newValue"
//
// # Dependencies
//
//	immutable  ──imports──▶  (stdlib only)
//
// It must not import schema, instance, graph, schema/cypher or neo4j.
package immutable

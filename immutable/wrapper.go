package immutable

import (
	"reflect"
	"strings"
)

// wrapper is what each of this package's containers implements: it clones to
// plain Go data and reports whether it is nil. A wrapper is a struct, so a
// reflect kind answers neither question, and Map is generic, so only a method
// reaches it. The package doc states how a wrapper handed back to Wrap is read.
type wrapper interface {
	cloneToAny() any
	isNil() bool
}

// asWrapper returns v as one of this package's containers, which it is by its
// type alone: a pointer to one and a struct that embeds one hold wrapper's
// methods, and the package stores each as it is. No method decides it, since a
// promoted one can reach a nil pointer.
func asWrapper(v any) (wrapper, bool) {
	// Asserted from v, so the answer shares v's storage and allocates nothing.
	w, ok := v.(wrapper)
	if !ok {
		return nil, false
	}
	switch v.(type) {
	case Slice, Properties, Key:
		return w, true
	}
	// A type switch cannot list the instances of Map.
	if isMapInstance(reflect.TypeOf(v)) {
		return w, true
	}
	return nil, false
}

// packagePath is this package's import path, as reflect reports it.
var packagePath = reflect.TypeFor[Slice]().PkgPath()

// isMapInstance reports whether t is Map[K] for some K. reflect names an
// instance by the generic type's name and its type arguments in brackets, and
// a bracket is in no declared type's name.
func isMapInstance(t reflect.Type) bool {
	return t.PkgPath() == packagePath && strings.HasPrefix(t.Name(), "Map[")
}

// IsNilContainer reports whether v is a [Map], [Slice], [Properties] or [Key]
// that holds nil, as each constructor stores a nil map or slice it wraps as a
// container. A nil container and an empty one stay apart, and neither a pointer
// to a container nor a struct that embeds one is a container.
func IsNilContainer(v any) bool {
	w, ok := asWrapper(v)
	return ok && w.isNil()
}

func (m Map[K]) cloneToAny() any { return m.Clone() }

func (m Map[K]) isNil() bool { return m.entries == nil }

func (s Slice) cloneToAny() any { return s.Clone() }

func (s Slice) isNil() bool { return s.elements == nil }

func (p Properties) cloneToAny() any { return p.Clone() }

func (p Properties) isNil() bool { return p.entries == nil }

func (k Key) cloneToAny() any { return k.Clone() }

func (k Key) isNil() bool { return k.components == nil }

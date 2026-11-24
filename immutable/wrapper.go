package immutable

// wrapper is what each of this package's containers implements: it clones to
// plain Go data. A container is a struct, so a reflect kind cannot tell it apart.
type wrapper interface {
	cloneToAny() any
}

// asWrapper returns v as one of this package's containers, which it is by its
// type alone: a pointer to one and a struct that embeds one are stored as they
// are.
func asWrapper(v any) (wrapper, bool) {
	// Asserted from v, so the answer shares v's storage and allocates nothing;
	// a pointer to a container also implements wrapper, so the type is checked.
	w, ok := v.(wrapper)
	if !ok {
		return nil, false
	}
	switch v.(type) {
	case Slice, Properties, Key:
		return w, true
	}
	return nil, false
}

// IsNilContainer reports whether v is a [Slice], [Properties] or [Key] that
// holds nil, as each constructor stores a nil string-keyed map or a nil slice
// it wraps as a container. A nil container and an empty one stay apart, and neither a pointer
// to a container nor a struct that embeds one is a container.
func IsNilContainer(v any) bool {
	switch w := v.(type) {
	case Slice:
		return w.elements == nil
	case Properties:
		return w.entries == nil
	case Key:
		return w.components == nil
	}
	return false
}

func (s Slice) cloneToAny() any { return s.Clone() }

func (p Properties) cloneToAny() any { return p.Clone() }

func (k Key) cloneToAny() any { return k.Clone() }

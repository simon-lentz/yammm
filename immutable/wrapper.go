package immutable

// wrapper is what each of this package's containers implements: it clones to
// plain Go data and reports whether it is nil. A wrapper is a struct, so a
// reflect kind answers neither question, and Map is generic, so only a method
// reaches it. The package doc states how a wrapper handed back to Wrap is read.
type wrapper interface {
	cloneToAny() any
	isNil() bool
}

func (m Map[K]) cloneToAny() any { return m.Clone() }

func (m Map[K]) isNil() bool { return m.entries == nil }

func (s Slice) cloneToAny() any { return s.Clone() }

func (s Slice) isNil() bool { return s.elements == nil }

func (p Properties) cloneToAny() any { return p.Clone() }

func (p Properties) isNil() bool { return p.entries == nil }

func (k Key) cloneToAny() any { return k.Clone() }

func (k Key) isNil() bool { return k.components == nil }

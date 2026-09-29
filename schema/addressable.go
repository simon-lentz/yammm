package schema

// AddressableTag returns the name s addresses id by and reports whether s can
// address it at all: the bare name for a type s declares, "alias.Name" for one
// s imports directly, and no tag for one s reaches only through an
// intermediate import. It is the write half of [Schema.ResolveTypeName]'s
// contract — an accepted id's tag resolves back to that same identity, and no
// entry-relative name denotes a refused one — held together over a closure by
// [TestAddressableTag_IsResolveTypeNameInverse]. [TagForm] renders every
// identity for display and is lossy on purpose; this refuses instead.
func AddressableTag(s *Schema, id TypeID) (string, bool) {
	if s == nil || id.IsZero() {
		return "", false
	}
	if id.SchemaPath() == s.SourceID() {
		if _, ok := s.Type(id.Name()); !ok {
			return "", false
		}
		return id.Name(), true
	}
	alias := s.FindImportAlias(id.SchemaPath())
	if alias == "" {
		return "", false
	}
	imp, ok := s.ImportByAlias(alias)
	if !ok || imp.Schema() == nil {
		return "", false
	}
	// A caller may build any TypeID, so the name is checked against the schema
	// the alias resolves to rather than assumed to exist in it.
	if _, ok := imp.Schema().Type(id.Name()); !ok {
		return "", false
	}
	return alias + "." + id.Name(), true
}

// Addressable reports whether s can name id. It is [AddressableTag] without
// the tag, for a caller deciding eligibility rather than rendering a name.
func Addressable(s *Schema, id TypeID) bool {
	_, ok := AddressableTag(s, id)
	return ok
}

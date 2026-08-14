package schema

// TagForm renders a type identity as the name a snapshot carries: the bare
// name for a type s declares, alias-qualified for one s imports directly.
//
// The rendering is lossy, and deliberately so — it is a display and lookup
// form, not an identity. A type s reaches only through an intermediate import
// has no alias to qualify with and renders bare, as does a type from a schema
// s does not import at all, so two same-named types in different schemas can
// render identically. Use [TypeID] wherever the answer has to be exact.
func TagForm(s *Schema, id TypeID) string {
	if s == nil || id.IsZero() || id.SchemaPath() == s.SourceID() {
		return id.Name()
	}
	if alias := s.FindImportAlias(id.SchemaPath()); alias != "" {
		return alias + "." + id.Name()
	}
	return id.Name()
}

package schema

// TagForm renders a type identity by its schema path, as a graph and its
// diagnostics name it: bare for a path of s's own source, "alias.Name" for one s
// imports directly, "schema#Name" (that schema's name) for one of any other
// schema of s's closure, and bare otherwise. Within one load each rendering
// denotes one type, since a load refuses two schemas of one name and no type
// name holds "." or "#".
func TagForm(s Schema, id TypeID) string {
	if s.IsZero() || id.IsZero() || id.SchemaPath() == s.SourceID() {
		return id.Name()
	}
	if alias, ok := s.FindImportAlias(id.SchemaPath()); ok {
		return alias + "." + id.Name()
	}
	for member := range s.Closure() {
		if member.SourceID() == id.SchemaPath() {
			return member.Name() + "#" + id.Name()
		}
	}
	return id.Name()
}

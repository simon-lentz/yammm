package clean

// UsesJSON does not import any json package, so [json.Local] falls back to the
// unique in-module package of that name and resolves.
func UsesJSON() {}

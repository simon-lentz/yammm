// Package importer imports the standard library's json, so its own doc links
// must resolve json against that import and not against the fixture package of
// the same name.
package importer

import "encoding/json"

// Marshal wraps [json.Marshal]. The import binds json to encoding/json, which
// is out of module, so this gate skips it. Resolving the name against the
// fixture package of the same name would report it instead.
func Marshal(v any) ([]byte, error) { return json.Marshal(v) }

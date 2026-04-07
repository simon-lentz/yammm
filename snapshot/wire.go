package snapshot

// FIELD ORDER IN WIRE STRUCTS IS PART OF THE FORMAT CONTRACT.
//
// encoding/json serializes struct fields in declaration order. The integrity
// hash covers the exact serialized bytes. Reordering fields changes the
// output and invalidates every previously-saved .ys file's integrity hash.
//
// Rules:
//   - Do NOT reorder existing fields within a format version.
//   - New fields may be appended (with omitempty for backward compatibility).
//   - Removing or reordering fields requires a format version bump.
//   - The wire struct field ordering test in snapshot_test.go enforces this.

const (
	currentVersion         = 1
	currentHashAlgoVersion = 1
)

// headerWire is used for decoding .ys headers. All fields are present
// including version and hash algorithm version (which are written as
// literal constants during marshal).
type headerWire struct {
	Version             int               `json:"version"`
	SchemaName          string            `json:"schema_name"`
	SchemaSource        string            `json:"schema_source"`
	SchemaHash          string            `json:"schema_hash"`
	SchemaHashAlgorithm int               `json:"schema_hash_algorithm"`
	IntegrityHash       string            `json:"integrity_hash"`
	Features            []string          `json:"features"`
	CreatedAt           string            `json:"created_at,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

// marshalHeaderWire is used for encoding .ys headers. Version and
// SchemaHashAlgorithm are written as literal constants in the manual
// byte assembly, not serialized from this struct.
type marshalHeaderWire struct {
	SchemaName    string            `json:"schema_name"`
	SchemaSource  string            `json:"schema_source"`
	SchemaHash    string            `json:"schema_hash"`
	IntegrityHash string            `json:"integrity_hash"`
	Features      []string          `json:"features"`
	CreatedAt     string            `json:"created_at,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// instWire is the wire representation of a single instance.
// Used for both root instances and composed children (recursive).
//
// Provenance uses json:"provenance" (no omitempty) — null provenance is
// semantically meaningful (no source tracking). Edges and Composed use
// omitempty because absence is the default state.
type instWire struct {
	Key        []any                 `json:"key"`
	TypeID     *typeIDWire           `json:"type_id,omitempty"`
	Properties map[string]any        `json:"properties"`
	Edges      map[string][]edgeWire `json:"edges,omitempty"`
	Composed   map[string][]instWire `json:"composed,omitempty"`
	Provenance *provenanceWire       `json:"provenance"`
}

// typeIDWire is the optional cross-validation type identity.
// Omitted when recoverable from context (single-schema case).
type typeIDWire struct {
	SchemaPath string `json:"schema_path"`
	Name       string `json:"name"`
}

// edgeWire represents an edge target within an instance's edges map.
// Properties is always present (no omitempty) — even when empty, edge
// properties are a typed field on a resolved edge, serialized as {}.
type edgeWire struct {
	TargetType string         `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Properties map[string]any `json:"properties"`
}

// provenanceWire is the wire representation of instance provenance.
type provenanceWire struct {
	SourceName string `json:"source_name"`
	Path       string `json:"path"`
}

// diagWire is the wire representation of the diagnostics section.
type diagWire struct {
	Duplicates []dupWire        `json:"duplicates"`
	Unresolved []unresolvedWire `json:"unresolved"`
}

// dupWire is the wire representation of a duplicate record.
type dupWire struct {
	Type     string   `json:"type"`
	Key      []any    `json:"key"`
	Instance instWire `json:"instance"`
}

// unresolvedWire is the wire representation of an unresolved edge record.
type unresolvedWire struct {
	SourceType string `json:"source_type"`
	SourceKey  []any  `json:"source_key"`
	Relation   string `json:"relation"`
	TargetType string `json:"target_type"`
	TargetKey  []any  `json:"target_key"`
	Required   bool   `json:"required"`
	Reason     string `json:"reason"`
}

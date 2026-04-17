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
//
// TOP-LEVEL KEY ORDER AND BODY-SUFFIX STABILITY CONTRACT.
//
// The .ys wire format commits to two complementary structural contracts
// beyond the per-struct field order above. Both are load-bearing for
// UpdateMetadata's byte-surgery primitive; relaxing either silently
// breaks every existing and future metadata-rewrite call site.
//
//  1. Field-order contract (established pre-v0.3.0). The top-level
//     document has exactly four keys in a fixed order:
//     yammm_snapshot → types → instances → diagnostics. No future Marshal
//     change may reorder these or insert a new key in between.
//
//  2. Body-byte-range stability contract (introduced v0.3.0). The byte
//     range starting at the ',' immediately following the yammm_snapshot
//     header value's closing '}' and running through the document's
//     final '}' is the "body suffix". UpdateMetadata preserves this
//     suffix verbatim: it rebuilds only the header, recomputes the
//     integrity hash over new_header + body_suffix, and emits
//     new_header + body_suffix as the output. Future Marshal changes
//     that insert a fifth top-level key, shift the header-body
//     transition, or change the inter-key separator pattern silently
//     break UpdateMetadata even without relaxing the field-order rule.
//
// Both contracts are tested in snapshot/wire_test.go via:
//   - TestWireFormat_TopLevelKeyOrder (token-level key-order pin)
//   - TestWireFormat_BodySuffixContract (bodyOffset-capture + shape pin)
//
// These tests run across a representative corpus of Marshal outputs
// under every supported Option combination, so a future Marshal-side
// change that silently breaks either contract fails at the wire-format
// test level before reaching the consumers that depend on it.
// See snapshot/update.go for the UpdateMetadata primitive that consumes
// these contracts.

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

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
//   - TestWireStructs_FieldOrder in wire_internal_test.go pins every wire
//     struct's field order and tags by name, and
//     TestWireStructs_EveryStructIsPinned fails when a new wire struct is
//     added without a row there.
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
//     UpdateMetadata(x, newMeta) == Marshal(Load(x)) for a document the
//     current Marshal produced that carries no created_at, when newMeta
//     is empty. The two conditions are not the same kind of condition.
//     created_at is a condition on the document: UpdateMetadata preserves
//     it from the input header and Load does not return it, so a
//     re-marshal cannot reproduce it. Metadata is a condition on the
//     call: UpdateMetadata writes newMeta and discards whatever the
//     input carried, so a document holding metadata still matches a
//     re-marshal when newMeta is empty, and a document holding none
//     diverges when newMeta is not. A legacy document carrying
//     int-shaped floats diverges for a third reason: UpdateMetadata
//     preserves its body while a re-marshal heals KindFloat values into
//     decimal form.
//
//  3. Version preservation (introduced v0.12.0). UpdateMetadata rebuilds
//     the header at the version it read, so it is a header rewrite and
//     never a migration. From v0.12.0 the only readable version is 3:
//     an older document is refused with
//     E_SNAPSHOT_UNSUPPORTED_VERSION rather than rewritten, and
//     UpdateMetadataOrReMarshal's Load + Marshal fallback refuses it
//     the same way.
//
// These contracts are tested in snapshot/wire_test.go via:
//   - TestWireFormat_TopLevelKeyOrder (token-level key-order pin)
//   - TestWireFormat_BodySuffixContract (bodyOffset-capture + shape pin)
//
// the equivalence above in snapshot/wire_equivalence_test.go via
// TestWireContract_UpdateMetadataMatchesReMarshal, which pins the
// equality, the created_at divergence and both metadata directions in
// the same test, and version preservation in
// snapshot/update_contract_test.go via
// TestUpdateMetadata_PreservesInputVersion.
//
// These tests run across a representative corpus of Marshal outputs
// under every supported Option combination, so a future Marshal-side
// change that silently breaks either contract fails at the wire-format
// test level before reaching the consumers that depend on it.
// See snapshot/update.go for the UpdateMetadata primitive that consumes
// these contracts.

// MinReadableVersion is the lowest .ys wire-format version this package
// accepts on read paths (Load, Verify, Info, HeaderOnly, HeaderOnlyRead).
// The accept range is the closed interval [MinReadableVersion, currentVersion],
// which is [3, 3]: v0.12.0 introduced v3 and retired every earlier version.
// A document outside the range draws an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION]. The package doc's Wire Format
// Versions section and docs/VERSIONING.md carry the full policy.
const MinReadableVersion = 3

const (
	currentVersion         = 3
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

// provenanceWire is the wire representation of instance provenance.
type provenanceWire struct {
	SourceName string `json:"source_name"`
	Path       string `json:"path"`
}

// typeTableEntry is one row of the v3 types table, where schema_path + name is
// the lossless identity. Tag is carried rather than recomputed, because the
// rule that renders it cannot express a transitively imported type.
type typeTableEntry struct {
	SchemaPath string `json:"schema_path"`
	Name       string `json:"name"`
	Tag        string `json:"tag"`
}

// instWireV3 is the v3 wire representation of a single instance. Type indexes
// the types table; it is omitted for a root instance, whose position in the
// instances array denotes its type, and present at every other position.
type instWireV3 struct {
	Key        []any                   `json:"key"`
	Type       *int                    `json:"type,omitempty"`
	Properties map[string]any          `json:"properties"`
	Edges      map[string][]edgeWireV3 `json:"edges,omitempty"`
	Composed   map[string][]instWireV3 `json:"composed,omitempty"`
	Provenance *provenanceWire         `json:"provenance"`
}

// edgeWireV3 is the v3 wire representation of an edge target.
type edgeWireV3 struct {
	TargetType int            `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Properties map[string]any `json:"properties"`
}

// dupWireV3 is the v3 wire representation of a duplicate record. The parent
// coordinates are present only for a composed-child duplicate, whose conflict
// resolves by walking parent → relation → child key rather than the instances
// array, which composed children never appear in.
type dupWireV3 struct {
	Type       int        `json:"type"`
	Key        []any      `json:"key"`
	Instance   instWireV3 `json:"instance"`
	ParentType *int       `json:"parent_type,omitempty"`
	ParentKey  []any      `json:"parent_key,omitempty"`
	Relation   string     `json:"relation,omitempty"`
}

// unresolvedWireV3 is the v3 wire representation of an unresolved edge record.
type unresolvedWireV3 struct {
	SourceType int            `json:"source_type"`
	SourceKey  []any          `json:"source_key"`
	Relation   string         `json:"relation"`
	TargetType int            `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Required   bool           `json:"required"`
	Reason     string         `json:"reason"`
	Properties map[string]any `json:"properties,omitempty"`
}

// diagWireV3 is the v3 wire representation of the diagnostics section.
type diagWireV3 struct {
	Duplicates []dupWireV3        `json:"duplicates"`
	Unresolved []unresolvedWireV3 `json:"unresolved"`
}

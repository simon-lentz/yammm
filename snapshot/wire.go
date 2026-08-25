package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/simon-lentz/yammm/schema"
)

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
// The .ys wire format commits to two structural contracts beyond the
// per-struct field order above, and UpdateMetadata adds a third rule of its
// own. The two format contracts are load-bearing for UpdateMetadata's
// byte-surgery primitive: they are properties of the bytes Marshal emits, and
// relaxing either silently breaks every existing and future metadata-rewrite
// call site. The third is a behaviour of UpdateMetadata rather than of the
// format, and relaxing it would announce itself through the version field.
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
//     diverges when newMeta is not.
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

// topLevelKeys is the exact key sequence contract 1 fixes. A document that
// does not present these four keys, in this order, exactly once each, and none
// of them null, did not come from Marshal.
var topLevelKeys = [...]string{"yammm_snapshot", "types", "instances", "diagnostics"}

// checkTopLevelKeys is the one definition of the document's outermost shape:
// presence, order, uniqueness and non-nullness of the four top-level keys, and
// that the document ends where the object does. Every surface holding the whole
// document runs it. [HeaderOnlyRead] and [ScanDir] cannot and say so on their
// own godoc: they read through a capped reader and never hold the body.
//
// It is not free: it scans the whole document and copies each top-level value
// to skip it, so a caller that never decodes the body still pays a pass over
// it. That cost is pinned by TestUpdateMetadataRatioFloor rather
// than described here, because the first draft of this comment asserted a cost
// profile nobody had measured and was false.
func checkTopLevelKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("expected JSON object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected JSON object, got %v", tok)
	}

	seen := 0
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("expected top-level key: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected a top-level key, got %v", tok)
		}
		if seen == len(topLevelKeys) {
			return fmt.Errorf("document carries a fifth top-level key %q; the format has exactly %d",
				key, len(topLevelKeys))
		}
		if key != topLevelKeys[seen] {
			return fmt.Errorf("top-level key %d is %q, expected %q", seen, key, topLevelKeys[seen])
		}
		if err := skipValue(dec, key); err != nil {
			return err
		}
		seen++
	}
	if seen != len(topLevelKeys) {
		return fmt.Errorf("document carries %d top-level keys, expected %d; %q is missing",
			seen, len(topLevelKeys), topLevelKeys[seen])
	}

	// More reports false at the object's closing delimiter and at end of input
	// alike, so the loop above exits identically on a complete document and on
	// one truncated before its final brace. Reading the next token separates
	// them, and there is no third outcome: a decoder that reports no more values
	// at object level returns either that closing brace or an error, so the
	// brace itself is not worth a branch nothing can drive.
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("document ends before the top-level object closes: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("document carries trailing data after the top-level object")
	}
	return nil
}

// skipValue advances past one top-level value and reports a null.
//
// It decodes into a json.RawMessage, which copies the value. A token walk
// avoids the copy and is *slower* — encoding/json finds a value's bounds with
// its scanner and only tokenizes on demand, so walking every token to skip a
// section costs more than copying it. Measured: the token form put
// UpdateMetadata below the 3x floor TestUpdateMetadataRatioFloor
// pins. The copy is the cheaper of the two.
func skipValue(dec *json.Decoder, key string) error {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("failed to read the value of %q: %w", key, err)
	}
	if string(raw) == "null" {
		return fmt.Errorf("top-level key %q is null", key)
	}
	return nil
}

// MinReadableVersion is the lowest .ys wire-format version this package
// accepts on read paths (Load, Verify, Info, HeaderOnly, HeaderOnlyRead).
// The accept range is the closed interval [MinReadableVersion, currentVersion],
// which is [3, 3]: v0.12.0 introduced v3 and retired every earlier version.
// A document outside the range draws an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION]. The package doc's Wire Format
// Versions section and docs/VERSIONING.md carry the full policy.
const MinReadableVersion = 3

const (
	currentVersion = 3

	// currentHashAlgoVersion follows the schema constant structurally: the
	// writer's label and the reader's comparison were two bare literals
	// once, and bumping one alone makes every fresh document self-reject.
	currentHashAlgoVersion = schema.StructuralHashVersion
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
	Attestation         *attestationWire  `json:"attestation,omitempty"`
}

// marshalHeaderWire is used for encoding .ys headers. Version and
// SchemaHashAlgorithm are written as literal parameters in the manual
// byte assembly, not serialized from this struct.
type marshalHeaderWire struct {
	SchemaName    string            `json:"schema_name"`
	SchemaSource  string            `json:"schema_source"`
	SchemaHash    string            `json:"schema_hash"`
	IntegrityHash string            `json:"integrity_hash"`
	Features      []string          `json:"features"`
	CreatedAt     string            `json:"created_at,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Attestation   *attestationWire  `json:"attestation,omitempty"`
}

// attestationWire is the header's validity claim. Marshal always writes
// it; the pointer and omitempty state the read side — a pre-v0.15.0
// document carries no field, and UpdateMetadata preserves that absence
// rather than fabricating a claim.
type attestationWire struct {
	Values       bool `json:"values"`
	Associations bool `json:"associations"`
}

// provenanceWire is the wire representation of instance provenance.
type provenanceWire struct {
	SourceName string `json:"source_name"`
	Path       string `json:"path"`
}

// typeTableEntry is one row of the types table — the document's denotation
// set. SchemaPath plus Name is the lossless identity; every other position
// references a row by index.
type typeTableEntry struct {
	SchemaPath string `json:"schema_path"`
	Name       string `json:"name"`
}

// instanceGroupWire is one entry of the instances section: one table row's
// root instances. Present with empty Items means the snapshot holds the
// type with no instances; absent means the snapshot does not hold the type.
type instanceGroupWire struct {
	Type  *int       `json:"type"`
	Items []instWire `json:"items"`
}

// instWire is the wire representation of a single instance. Type is
// required at every composed-child position and absent for a root, whose
// group entry denotes its type; a root carrying one is cross-checked.
type instWire struct {
	Key        []any                 `json:"key"`
	Type       *int                  `json:"type,omitempty"`
	Properties map[string]any        `json:"properties"`
	Edges      map[string][]edgeWire `json:"edges,omitempty"`
	Composed   map[string][]instWire `json:"composed,omitempty"`
	Provenance *provenanceWire       `json:"provenance"`
}

// edgeWire is the wire representation of an edge target. TargetType is a
// nullable row index: a null or absent reference is a malformed document
// and never binds to row 0.
type edgeWire struct {
	TargetType *int           `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Properties map[string]any `json:"properties"`
}

// conflictWire is the address of the instance a duplicate collided with.
// Key is null for a keyless slot occupant, which the reader resolves
// through the parent slot alone.
type conflictWire struct {
	Type *int  `json:"type"`
	Key  []any `json:"key"`
}

// dupWire is the wire representation of a duplicate record. Type and Key
// identify the rejected instance; Conflict addresses the instance it
// collided with. The parent coordinates are present only for a
// composed-child duplicate and must name a root instance.
type dupWire struct {
	Type       *int          `json:"type"`
	Key        []any         `json:"key"`
	Instance   instWire      `json:"instance"`
	Conflict   *conflictWire `json:"conflict"`
	ParentType *int          `json:"parent_type,omitempty"`
	ParentKey  []any         `json:"parent_key,omitempty"`
	Relation   string        `json:"relation,omitempty"`
}

// unresolvedWire is the wire representation of an unresolved edge record.
type unresolvedWire struct {
	SourceType *int           `json:"source_type"`
	SourceKey  []any          `json:"source_key"`
	Relation   string         `json:"relation"`
	TargetType *int           `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Required   bool           `json:"required"`
	Reason     string         `json:"reason"`
	Properties map[string]any `json:"properties,omitempty"`
}

// diagWire is the wire representation of the diagnostics section.
type diagWire struct {
	Duplicates []dupWire        `json:"duplicates"`
	Unresolved []unresolvedWire `json:"unresolved"`
}

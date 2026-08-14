package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Marshal serializes a Snapshot to the yammm snapshot persistence format (.ys).
//
// The output is deterministic by default: serializing the same snapshot with
// the same options always produces identical bytes, enabling content-addressable
// storage and diff-based comparison. The created_at header field is omitted by
// default; use [WithCreatedAt] to opt in to a timestamp.
//
// Context cancellation: Marshal checks ctx.Err() at the start and once per
// type during instance iteration. On cancellation, Marshal returns
// (nil, result) where result contains a Fatal-severity E_CONTEXT_CANCELLED
// diagnostic.
//
// Panics if snap is nil (programming error).
func Marshal(ctx context.Context, snap *graph.Snapshot, opts ...Option) ([]byte, diag.Result) {
	if snap == nil {
		panic("snapshot.Marshal: nil Snapshot")
	}
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	cfg := applyOptions(opts)
	s := snap.Schema()

	// Step 1: Compute schema structural hash.
	schemaHash := schema.StructuralHash(s)
	schemaSource := s.SourceID().String()

	// Step 2: Serialize payload sections. Identity is carried once, in the
	// types table; every other position references a row by index.
	tt := buildTypeTable(snap, s)

	instanceSections, err := marshalInstancesV3(ctx, snap, s, tt)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			c := diag.NewCollector(0)
			c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, ctxErr.Error()).Build())
			return nil, c.Result()
		}
		return nil, marshalFatalErr("instances", err)
	}

	diags, err := marshalDiagnosticsV3(snap, s, tt)
	if err != nil {
		return nil, marshalFatalErr("diagnostics", err)
	}

	// Step 2 continued: Serialize each section to JSON.
	var typesJSON, instancesJSON, diagJSON []byte
	var marshalErr error

	if cfg.indent != "" {
		typesJSON, marshalErr = json.MarshalIndent(tt.entries, cfg.indent, cfg.indent) //nolint:errchkjson // []typeTableEntry is always safe
	} else {
		typesJSON, marshalErr = json.Marshal(tt.entries) //nolint:errchkjson // []typeTableEntry is always safe
	}
	if marshalErr != nil {
		return nil, marshalFatalErr("types", marshalErr)
	}

	if cfg.indent != "" {
		instancesJSON, marshalErr = json.MarshalIndent(instanceSections, cfg.indent, cfg.indent)
	} else {
		instancesJSON, marshalErr = json.Marshal(instanceSections)
	}
	if marshalErr != nil {
		return nil, marshalFatalErr("instances", marshalErr)
	}

	if cfg.indent != "" {
		diagJSON, marshalErr = json.MarshalIndent(diags, cfg.indent, cfg.indent)
	} else {
		diagJSON, marshalErr = json.Marshal(diags)
	}
	if marshalErr != nil {
		return nil, marshalFatalErr("diagnostics", marshalErr)
	}

	// Build header and assemble document.
	createdAt := ""
	if !cfg.createdAt.IsZero() {
		createdAt = cfg.createdAt.UTC().Format(time.RFC3339)
	}

	features := []string{}
	var metadata map[string]string
	if len(cfg.metadata) > 0 {
		metadata = cfg.metadata
	}

	return assembleDocument(
		schemaHash, s.Name(), schemaSource, createdAt, features, metadata,
		typesJSON, instancesJSON, diagJSON,
		cfg.indent, currentVersion,
	)
}

// assembleDocument builds the full .ys document with integrity hash.
func assembleDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string, version int,
) ([]byte, diag.Result) {
	// Step 3-5: Build canonical-form document (integrity_hash = "").
	canonBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		"", // integrity_hash placeholder
		typesJSON, instancesJSON, diagJSON,
		indent, version,
	)

	// Step 6: Compute integrity hash.
	h := sha256.Sum256(canonBytes)
	integrityHash := fmt.Sprintf("sha256:%x", h)

	// Step 7: Re-assemble with computed hash.
	finalBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		integrityHash,
		typesJSON, instancesJSON, diagJSON,
		indent, version,
	)

	return finalBytes, diag.OK()
}

// buildDocument assembles a complete .ys document from pre-serialized sections.
func buildDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	integrityHash string,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string, version int,
) []byte {
	hdr := marshalHeaderWire{
		SchemaName:    schemaName,
		SchemaSource:  schemaSource,
		SchemaHash:    schemaHash,
		IntegrityHash: integrityHash,
		Features:      features,
		CreatedAt:     createdAt,
		Metadata:      metadata,
	}

	// Build the header JSON with schema_hash_algorithm as a literal constant,
	// matching the headerWire field order exactly.
	headerJSON := buildHeaderBytes(hdr, indent, version)

	if indent != "" {
		return assembleIndented(headerJSON, typesJSON, instancesJSON, diagJSON, indent)
	}
	return assembleCompact(headerJSON, typesJSON, instancesJSON, diagJSON)
}

// buildHeaderBytes returns the JSON-encoded header object, compact or
// indented per the indent parameter. Version is a parameter because
// UpdateMetadata reuses its input's body verbatim, so writing the current
// version there would label an older body as the current format.
func buildHeaderBytes(hdr marshalHeaderWire, indent string, version int) []byte {
	if indent != "" {
		return buildHeaderIndented(hdr, indent, version)
	}
	return buildHeaderCompact(hdr, version)
}

// buildHeaderCompact produces the header object JSON in compact mode.
// Fields are written in headerWire declaration order with
// schema_hash_algorithm as a literal constant.
func buildHeaderCompact(hdr marshalHeaderWire, version int) []byte {
	var b strings.Builder
	b.WriteByte('{')

	// version
	b.WriteString(`"version":`)
	fmt.Fprintf(&b, "%d", version)

	// schema_name
	b.WriteString(`,"schema_name":`)
	writeJSONString(&b, hdr.SchemaName)

	// schema_source
	b.WriteString(`,"schema_source":`)
	writeJSONString(&b, hdr.SchemaSource)

	// schema_hash
	b.WriteString(`,"schema_hash":`)
	writeJSONString(&b, hdr.SchemaHash)

	// schema_hash_algorithm (literal constant)
	b.WriteString(`,"schema_hash_algorithm":`)
	fmt.Fprintf(&b, "%d", currentHashAlgoVersion)

	// integrity_hash
	b.WriteString(`,"integrity_hash":`)
	writeJSONString(&b, hdr.IntegrityHash)

	// features
	b.WriteString(`,"features":`)
	featJSON, _ := json.Marshal(hdr.Features)
	b.Write(featJSON)

	// created_at (omitempty)
	if hdr.CreatedAt != "" {
		b.WriteString(`,"created_at":`)
		writeJSONString(&b, hdr.CreatedAt)
	}

	// metadata (omitempty)
	if len(hdr.Metadata) > 0 {
		b.WriteString(`,"metadata":`)
		metaJSON, _ := json.Marshal(hdr.Metadata)
		b.Write(metaJSON)
	}

	b.WriteByte('}')
	return []byte(b.String())
}

// buildHeaderIndented produces the header object JSON with indentation.
func buildHeaderIndented(hdr marshalHeaderWire, indent string, version int) []byte {
	var b strings.Builder
	pfx := indent + indent // header fields are nested inside yammm_snapshot
	b.WriteString("{\n")

	// version
	b.WriteString(pfx)
	b.WriteString(`"version": `)
	fmt.Fprintf(&b, "%d", version)

	// schema_name
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_name": `)
	writeJSONString(&b, hdr.SchemaName)

	// schema_source
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_source": `)
	writeJSONString(&b, hdr.SchemaSource)

	// schema_hash
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_hash": `)
	writeJSONString(&b, hdr.SchemaHash)

	// schema_hash_algorithm
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_hash_algorithm": `)
	fmt.Fprintf(&b, "%d", currentHashAlgoVersion)

	// integrity_hash
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"integrity_hash": `)
	writeJSONString(&b, hdr.IntegrityHash)

	// features
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"features": `)
	featJSON, _ := json.Marshal(hdr.Features)
	b.Write(featJSON)

	// created_at (omitempty)
	if hdr.CreatedAt != "" {
		b.WriteString(",\n")
		b.WriteString(pfx)
		b.WriteString(`"created_at": `)
		writeJSONString(&b, hdr.CreatedAt)
	}

	// metadata (omitempty)
	if len(hdr.Metadata) > 0 {
		b.WriteString(",\n")
		b.WriteString(pfx)
		b.WriteString(`"metadata": `)
		metaJSON, _ := json.MarshalIndent(hdr.Metadata, pfx, indent)
		b.Write(metaJSON)
	}

	b.WriteString("\n")
	b.WriteString(indent)
	b.WriteByte('}')
	return []byte(b.String())
}

// assembleCompact builds the full document in compact mode.
func assembleCompact(headerJSON, typesJSON, instancesJSON, diagJSON []byte) []byte {
	var b []byte
	b = append(b, `{"yammm_snapshot":`...)
	b = append(b, headerJSON...)
	b = append(b, `,"types":`...)
	b = append(b, typesJSON...)
	b = append(b, `,"instances":`...)
	b = append(b, instancesJSON...)
	b = append(b, `,"diagnostics":`...)
	b = append(b, diagJSON...)
	b = append(b, '}')
	return b
}

// assembleIndented builds the full document with indentation.
func assembleIndented(headerJSON, typesJSON, instancesJSON, diagJSON []byte, indent string) []byte {
	var b []byte
	b = append(b, "{\n"...)
	b = append(b, indent...)
	b = append(b, `"yammm_snapshot": `...)
	b = append(b, headerJSON...)
	b = append(b, ",\n"...)
	b = append(b, indent...)
	b = append(b, `"types": `...)
	b = append(b, typesJSON...)
	b = append(b, ",\n"...)
	b = append(b, indent...)
	b = append(b, `"instances": `...)
	b = append(b, instancesJSON...)
	b = append(b, ",\n"...)
	b = append(b, indent...)
	b = append(b, `"diagnostics": `...)
	b = append(b, diagJSON...)
	b = append(b, '\n')
	b = append(b, '}')
	return b
}

// parseTargetKey parses an UnresolvedEdge.TargetKey string into []any.
// TargetKey is in Key.String() canonical form: e.g., `["c99"]` or `[1]`.
func parseTargetKey(keyStr string) []any {
	if keyStr == "" || keyStr == "[]" {
		return nil
	}
	var result []any
	if err := json.Unmarshal([]byte(keyStr), &result); err != nil {
		return nil
	}
	// Normalize json.Number values.
	for i, v := range result {
		result[i] = immutable.NormalizeValue(v)
	}
	return result
}

// writeJSONString writes a JSON-encoded string to a strings.Builder.
func writeJSONString(b *strings.Builder, s string) {
	encoded, _ := json.Marshal(s)
	b.Write(encoded)
}

// marshalFatalErr creates a diag.Result with a Fatal E_INTERNAL for marshal failures.
func marshalFatalErr(section string, err error) diag.Result {
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
		fmt.Sprintf("snapshot.Marshal: failed to serialize %s: %v", section, err)).Build())
	return c.Result()
}

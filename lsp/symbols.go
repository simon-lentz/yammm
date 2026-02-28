package lsp

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// SymbolKind represents the kind of a symbol.
type SymbolKind int

const (
	SymbolSchema SymbolKind = iota
	SymbolImport
	SymbolType
	SymbolDataType
	SymbolProperty
	SymbolAssociation
	SymbolComposition
	SymbolInvariant
)

// String returns a string representation of the symbol kind.
func (k SymbolKind) String() string {
	switch k {
	case SymbolSchema:
		return "Schema"
	case SymbolImport:
		return "Import"
	case SymbolType:
		return "Type"
	case SymbolDataType:
		return "DataType"
	case SymbolProperty:
		return "Property"
	case SymbolAssociation:
		return "Association"
	case SymbolComposition:
		return "Composition"
	case SymbolInvariant:
		return "Invariant"
	default:
		return "Unknown"
	}
}

// Symbol represents a declaration symbol in a schema.
type Symbol struct {
	Name       string
	Kind       SymbolKind
	SourceID   location.SourceID
	Range      location.Span // Full declaration span
	Selection  location.Span // Name-only span
	ParentName string        // Owning schema/type name
	Detail     string        // Signature-ish detail
	Data       any           // Semantic pointer (*schema.Type, *schema.Property, etc.)
}

// ReferenceKind represents the kind of a reference.
type ReferenceKind int

const (
	RefExtends  ReferenceKind = iota // "extends Foo"
	RefTarget                        // relation target
	RefDataType                      // property datatype
)

// ReferenceSymbol represents a reference to a type or import.
type ReferenceSymbol struct {
	Kind       ReferenceKind
	Span       location.Span // The reference location
	TargetName string        // Type name being referenced
	Qualifier  string        // Import alias (empty for local)
}

// SymbolIndex holds extracted symbols and references for a schema.
type SymbolIndex struct {
	// Symbols sorted by span start position for binary search
	Symbols []Symbol
	// References sorted by span start position
	References []ReferenceSymbol
}

// ExtractSymbols extracts all declaration symbols from a schema.
// The sources parameter is optional; if provided, it enables more precise
// name span computation for types without parser-generated NameSpan.
func ExtractSymbols(s *schema.Schema, sources *source.Registry) []Symbol {
	if s == nil {
		return nil
	}

	var symbols []Symbol
	schemaName := s.Name()
	sourceID := s.SourceID()

	// Add schema symbol
	if !s.Span().IsZero() {
		symbols = append(symbols, Symbol{
			Name:      schemaName,
			Kind:      SymbolSchema,
			SourceID:  sourceID,
			Range:     s.Span(),
			Selection: s.Span(), // Use full span as selection for schema
			Detail:    fmt.Sprintf("schema %q", schemaName),
		})
	}

	// Add imports
	for imp := range s.Imports() {
		if !imp.Span().IsZero() {
			symbols = append(symbols, Symbol{
				Name:       imp.Alias(),
				Kind:       SymbolImport,
				SourceID:   sourceID,
				Range:      imp.Span(),
				Selection:  imp.Span(),
				ParentName: schemaName,
				Detail:     fmt.Sprintf("import %q as %s", imp.Path(), imp.Alias()),
				Data:       imp,
			})
		}
	}

	// Add data types
	for name, dt := range s.DataTypes() {
		if !dt.Span().IsZero() {
			symbols = append(symbols, Symbol{
				Name:       name,
				Kind:       SymbolDataType,
				SourceID:   sourceID,
				Range:      dt.Span(),
				Selection:  dt.Span(),
				ParentName: schemaName,
				Detail:     fmt.Sprintf("datatype %s = %s", name, dt.Constraint().String()),
				Data:       dt,
			})
		}
	}

	// Add types and their members
	for _, t := range s.Types() {
		symbols = append(symbols, extractTypeSymbols(t, schemaName, sourceID, sources)...)
	}

	// Sort by span start for binary search
	sortSymbolsByPosition(symbols)

	return symbols
}

// extractTypeSymbols extracts symbols from a single type.
// The sources parameter is optional; if provided, enables precise name span fallback.
func extractTypeSymbols(t *schema.Type, schemaName string, sourceID location.SourceID, sources *source.Registry) []Symbol {
	var symbols []Symbol

	if t.Span().IsZero() {
		return symbols
	}

	// Type symbol
	detail := "type " + t.Name()
	if t.IsAbstract() {
		detail = "abstract " + detail
	}
	if t.IsPart() {
		detail = "part " + detail
	}

	// Use the precise name span from parser when available;
	// fall back to heuristic for programmatically-created types
	nameSpan := t.NameSpan()
	if nameSpan.IsZero() {
		// Get source content for precise fallback computation
		var content []byte
		if sources != nil {
			content, _ = sources.ContentBySource(sourceID)
		}
		nameSpan = computeTypeNameSpan(t, content)
	}

	symbols = append(symbols, Symbol{
		Name:       t.Name(),
		Kind:       SymbolType,
		SourceID:   sourceID,
		Range:      t.Span(),
		Selection:  nameSpan,
		ParentName: schemaName,
		Detail:     detail,
		Data:       t,
	})

	typeName := t.Name()

	// Properties (own only, not inherited)
	for prop := range t.Properties() {
		if !prop.Span().IsZero() {
			symbols = append(symbols, Symbol{
				Name:       prop.Name(),
				Kind:       SymbolProperty,
				SourceID:   sourceID,
				Range:      prop.Span(),
				Selection:  prop.Span(),
				ParentName: typeName,
				Detail:     formatPropertyDetail(prop),
				Data:       prop,
			})
		}
	}

	// Associations
	for rel := range t.Associations() {
		if !rel.Span().IsZero() {
			symbols = append(symbols, Symbol{
				Name:       rel.Name(),
				Kind:       SymbolAssociation,
				SourceID:   sourceID,
				Range:      rel.Span(),
				Selection:  rel.Span(),
				ParentName: typeName,
				Detail:     formatRelationDetail(rel, "-->"),
				Data:       rel,
			})
		}
	}

	// Compositions
	for rel := range t.Compositions() {
		if !rel.Span().IsZero() {
			symbols = append(symbols, Symbol{
				Name:       rel.Name(),
				Kind:       SymbolComposition,
				SourceID:   sourceID,
				Range:      rel.Span(),
				Selection:  rel.Span(),
				ParentName: typeName,
				Detail:     formatRelationDetail(rel, "*->"),
				Data:       rel,
			})
		}
	}

	// Invariants
	for inv := range t.Invariants() {
		if !inv.Span().IsZero() {
			symbols = append(symbols, Symbol{
				Name:       inv.Name(),
				Kind:       SymbolInvariant,
				SourceID:   sourceID,
				Range:      inv.Span(),
				Selection:  inv.Span(),
				ParentName: typeName,
				Detail:     "invariant: " + inv.Name(),
				Data:       inv,
			})
		}
	}

	return symbols
}

// ExtractReferences extracts all type references from a schema.
func ExtractReferences(s *schema.Schema) []ReferenceSymbol {
	if s == nil {
		return nil
	}

	var refs []ReferenceSymbol

	// Extract references from types
	for _, t := range s.Types() {
		// Extends references
		for ref := range t.Inherits() {
			if !ref.Span().IsZero() {
				refs = append(refs, ReferenceSymbol{
					Kind:       RefExtends,
					Span:       ref.Span(),
					TargetName: ref.Name(),
					Qualifier:  ref.Qualifier(),
				})
			}
		}

		// Property datatype references
		for prop := range t.Properties() {
			dtRef := prop.DataTypeRef()
			if !dtRef.IsZero() && !dtRef.Span().IsZero() {
				refs = append(refs, ReferenceSymbol{
					Kind:       RefDataType,
					Span:       dtRef.Span(),
					TargetName: dtRef.Name(),
					Qualifier:  dtRef.Qualifier(),
				})
			}
		}

		// Relation target references
		for rel := range t.Associations() {
			target := rel.Target()
			if !target.Span().IsZero() {
				refs = append(refs, ReferenceSymbol{
					Kind:       RefTarget,
					Span:       target.Span(),
					TargetName: target.Name(),
					Qualifier:  target.Qualifier(),
				})
			}
			// Edge property datatype references
			for prop := range rel.Properties() {
				dtRef := prop.DataTypeRef()
				if !dtRef.IsZero() && !dtRef.Span().IsZero() {
					refs = append(refs, ReferenceSymbol{
						Kind:       RefDataType,
						Span:       dtRef.Span(),
						TargetName: dtRef.Name(),
						Qualifier:  dtRef.Qualifier(),
					})
				}
			}
		}
		for rel := range t.Compositions() {
			target := rel.Target()
			if !target.Span().IsZero() {
				refs = append(refs, ReferenceSymbol{
					Kind:       RefTarget,
					Span:       target.Span(),
					TargetName: target.Name(),
					Qualifier:  target.Qualifier(),
				})
			}
		}
	}

	// Sort by span start for binary search
	sortReferencesByPosition(refs)

	return refs
}

// BuildSymbolIndex builds a complete symbol index for a schema.
// The sources parameter is optional; if provided, enables more precise
// name span computation for types without parser-generated NameSpan.
func BuildSymbolIndex(s *schema.Schema, sources *source.Registry) *SymbolIndex {
	return &SymbolIndex{
		Symbols:    ExtractSymbols(s, sources),
		References: ExtractReferences(s),
	}
}

// SymbolAtPosition finds the smallest symbol whose Range contains the given position.
// It returns the most specific (innermost) symbol at that position—for example,
// a property symbol rather than its containing type symbol.
//
// The algorithm uses linear search over all symbols, comparing by span containment.
// This is acceptable for typical schema sizes (<1000 symbols). The symbols slice
// is sorted by start position, but containment queries don't benefit from binary
// search since we need the smallest containing span, not an exact match.
//
// Returns nil if no symbol contains the position or if idx is nil.
func (idx *SymbolIndex) SymbolAtPosition(pos location.Position) *Symbol {
	if idx == nil || len(idx.Symbols) == 0 {
		return nil
	}

	var best *Symbol
	for i := range idx.Symbols {
		sym := &idx.Symbols[i]
		if sym.Range.Contains(pos) {
			if best == nil || isSmaller(sym.Range, best.Range) {
				best = sym
			}
		}
	}
	return best
}

// ReferenceAtPosition finds the reference at the given position.
func (idx *SymbolIndex) ReferenceAtPosition(pos location.Position) *ReferenceSymbol {
	if idx == nil || len(idx.References) == 0 {
		return nil
	}

	for i := range idx.References {
		ref := &idx.References[i]
		if ref.Span.Contains(pos) {
			return ref
		}
	}
	return nil
}

// formatPropertyDetail formats a property for display.
func formatPropertyDetail(p *schema.Property) string {
	var detail string
	if c := p.Constraint(); c != nil {
		detail = p.Name() + " " + c.String()
	} else {
		detail = p.Name() + " <unknown>"
	}
	if p.IsPrimaryKey() {
		detail += " primary"
	}
	if p.IsRequired() {
		detail += " required"
	}
	return detail
}

// formatRelationDetail formats a relation for display.
func formatRelationDetail(r *schema.Relation, arrow string) string {
	mult := "one"
	if r.IsMany() {
		mult = "many"
	}
	return fmt.Sprintf("%s %s (%s) %s", arrow, r.Name(), mult, r.Target().String())
}

// sortSymbolsByPosition sorts symbols by their start position.
func sortSymbolsByPosition(symbols []Symbol) {
	slices.SortFunc(symbols, func(a, b Symbol) int {
		return positionCompare(a.Range.Start, b.Range.Start)
	})
}

// sortReferencesByPosition sorts references by their start position.
func sortReferencesByPosition(refs []ReferenceSymbol) {
	slices.SortFunc(refs, func(a, b ReferenceSymbol) int {
		return positionCompare(a.Span.Start, b.Span.Start)
	})
}

// positionCompare compares two positions, returning -1, 0, or +1.
func positionCompare(a, b location.Position) int {
	return cmp.Or(
		cmp.Compare(a.Line, b.Line),
		cmp.Compare(a.Column, b.Column),
	)
}

// positionBefore returns true if a comes before b.
func positionBefore(a, b location.Position) bool {
	return positionCompare(a, b) < 0
}

// isSmaller returns true if a is smaller (more specific) than b.
func isSmaller(a, b location.Span) bool {
	// Compare by line count first
	aLines := a.End.Line - a.Start.Line
	bLines := b.End.Line - b.Start.Line
	if aLines != bLines {
		return aLines < bLines
	}
	// Same line count, compare by column span
	if a.Start.Line == a.End.Line && b.Start.Line == b.End.Line {
		return (a.End.Column - a.Start.Column) < (b.End.Column - b.Start.Column)
	}
	return false
}

// computeTypeNameSpan computes a selection span for a type's name.
// This is a fallback for programmatically-created types that don't have NameSpan set.
// Prefer using Type.NameSpan() which provides accurate byte-level positions from the parser.
//
// If sourceContent is provided, this function searches for the actual name position
// in the source text, handling non-standard spacing. If sourceContent is nil or the
// search fails, it falls back to a fixed-offset approximation based on modifiers:
//   - "type Name" (offset 5)
//   - "abstract type Name" (offset 14)
//   - "part type Name" (offset 10)
//   - "part abstract type Name" or "abstract part type Name" (offset 19)
//
// The selection span covers just the name, enabling precise go-to-definition.
func computeTypeNameSpan(t *schema.Type, sourceContent []byte) location.Span {
	span := t.Span()
	if span.IsZero() {
		return span
	}

	typeName := t.Name()

	// If we have source content and valid byte offsets, search for actual name position
	if len(sourceContent) > 0 && span.Start.Byte >= 0 {
		startByte := span.Start.Byte
		endByte := len(sourceContent)
		if span.End.Byte > 0 && span.End.Byte < endByte {
			endByte = span.End.Byte
		}

		if startByte < endByte {
			snippet := sourceContent[startByte:endByte]

			// Find "type" keyword first, then find the name after it
			typeKeywordIdx := bytes.Index(snippet, []byte("type"))
			if typeKeywordIdx >= 0 {
				afterType := snippet[typeKeywordIdx+4:] // len("type") = 4

				// Skip whitespace after "type"
				nameStart := 0
				for nameStart < len(afterType) && (afterType[nameStart] == ' ' || afterType[nameStart] == '\t') {
					nameStart++
				}

				// Verify we found the expected name
				nameBytes := []byte(typeName)
				if nameStart < len(afterType) && bytes.HasPrefix(afterType[nameStart:], nameBytes) {
					// Calculate actual byte offsets
					actualNameStart := startByte + typeKeywordIdx + 4 + nameStart
					actualNameEnd := actualNameStart + len(nameBytes)

					// Compute column by counting runes from span start to name start
					columnOffset := utf8.RuneCount(snippet[:typeKeywordIdx+4+nameStart])
					startCol := span.Start.Column + columnOffset
					endCol := startCol + utf8.RuneCountInString(typeName)

					return location.Span{
						Source: span.Source,
						Start:  location.NewPosition(span.Start.Line, startCol, actualNameStart),
						End:    location.NewPosition(span.Start.Line, endCol, actualNameEnd),
					}
				}
			}
		}
	}

	// Fallback: calculate offset to the type name based on modifiers
	// This is used when source content is unavailable or search failed
	var offset int
	switch {
	case t.IsAbstract() && t.IsPart():
		// "part abstract type " or "abstract part type " = 19 characters
		offset = 19
	case t.IsAbstract():
		// "abstract type " = 14 characters
		offset = 14
	case t.IsPart():
		// "part type " = 10 characters
		offset = 10
	default:
		// "type " = 5 characters
		offset = 5
	}

	// Compute start and end positions for the name (byte offset unknown: -1)
	startCol := span.Start.Column + offset
	endCol := startCol + utf8.RuneCountInString(typeName)

	return location.Span{
		Source: span.Source,
		Start:  location.NewPosition(span.Start.Line, startCol, -1),
		End:    location.NewPosition(span.Start.Line, endCol, -1),
	}
}

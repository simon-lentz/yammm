package symbols

import (
	"cmp"
	"fmt"
	"slices"

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
	Data       any           // The schema record: a handle such as schema.Type, or schema.Property
}

// ReferenceKind represents the kind of a reference.
type ReferenceKind int

const (
	RefExtends  ReferenceKind = iota // "extends Foo"
	RefTarget                        // relation target
	RefDataType                      // property datatype
)

// String returns a human-readable name for the ReferenceKind.
func (k ReferenceKind) String() string {
	switch k {
	case RefExtends:
		return "extends"
	case RefTarget:
		return "target"
	case RefDataType:
		return "datatype"
	default:
		return "unknown"
	}
}

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
	// nameIndex maps symbol name to matching symbols for O(1) lookup.
	// Populated by BuildSymbolIndex; nil for manually constructed indices.
	nameIndex map[string][]*Symbol
}

// ExtractSymbols extracts all declaration symbols from a schema.
func ExtractSymbols(s schema.Schema) []Symbol {
	if s.IsZero() {
		return nil
	}

	var symbols []Symbol
	schemaName := s.Name()
	sourceID := s.SourceID()

	// Add schema symbol
	symbols = append(symbols, Symbol{
		Name:      schemaName,
		Kind:      SymbolSchema,
		SourceID:  sourceID,
		Range:     s.Span(),
		Selection: s.Span(), // Use full span as selection for schema
		Detail:    fmt.Sprintf("schema %q", schemaName),
	})

	// Add imports
	for imp := range s.Imports() {
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

	// Add data types
	for dt := range s.DataTypes() {
		name := dt.Name()
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

	// Add types and their members
	for t := range s.Types() {
		symbols = append(symbols, extractTypeSymbols(t, schemaName, sourceID)...)
	}

	// Sort by span start for binary search
	sortSymbolsByPosition(symbols)

	return symbols
}

// extractTypeSymbols extracts symbols from a single type.
func extractTypeSymbols(t schema.Type, schemaName string, sourceID location.SourceID) []Symbol {
	var symbols []Symbol

	// Type symbol
	detail := "type " + t.Name()
	if t.IsAbstract() {
		detail = "abstract " + detail
	}
	if t.IsPart() {
		detail = "part " + detail
	}

	symbols = append(symbols, Symbol{
		Name:       t.Name(),
		Kind:       SymbolType,
		SourceID:   sourceID,
		Range:      t.Span(),
		Selection:  t.NameSpan(),
		ParentName: schemaName,
		Detail:     detail,
		Data:       t,
	})

	typeName := t.Name()

	// Properties (own only, not inherited)
	for prop := range t.Properties() {
		symbols = append(symbols, Symbol{
			Name:       prop.Name(),
			Kind:       SymbolProperty,
			SourceID:   sourceID,
			Range:      prop.Span(),
			Selection:  prop.Span(),
			ParentName: typeName,
			Detail:     FormatPropertyDetail(prop),
			Data:       prop,
		})
	}

	// Associations
	for rel := range t.Associations() {
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

	// Compositions
	for rel := range t.Compositions() {
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

	// Invariants
	for inv := range t.Invariants() {
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

	return symbols
}

// ExtractReferences extracts all type references from a schema.
func ExtractReferences(s schema.Schema) []ReferenceSymbol {
	if s.IsZero() {
		return nil
	}

	var refs []ReferenceSymbol

	// Extract references from types
	for t := range s.Types() {
		// Extends references, one per spelling so each answers at its own position.
		for ref := range t.ExtendsClause() {
			refs = append(refs, ReferenceSymbol{
				Kind:       RefExtends,
				Span:       ref.Span(),
				TargetName: ref.Name(),
				Qualifier:  ref.Qualifier(),
			})
		}

		// Property datatype references
		for prop := range t.Properties() {
			dtRef := prop.DataTypeRef()
			if !dtRef.IsZero() {
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
			refs = append(refs, ReferenceSymbol{
				Kind:       RefTarget,
				Span:       target.Span(),
				TargetName: target.Name(),
				Qualifier:  target.Qualifier(),
			})
			// Edge property datatype references
			for prop := range rel.Properties() {
				dtRef := prop.DataTypeRef()
				if !dtRef.IsZero() {
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
			refs = append(refs, ReferenceSymbol{
				Kind:       RefTarget,
				Span:       target.Span(),
				TargetName: target.Name(),
				Qualifier:  target.Qualifier(),
			})
		}
	}

	// Sort by span start for binary search
	sortReferencesByPosition(refs)

	return refs
}

// BuildSymbolIndex builds a complete symbol index for a schema.
func BuildSymbolIndex(s schema.Schema) *SymbolIndex {
	syms := ExtractSymbols(s)

	// Build name index for O(1) lookup by name
	nameIdx := make(map[string][]*Symbol, len(syms))
	for i := range syms {
		sym := &syms[i]
		nameIdx[sym.Name] = append(nameIdx[sym.Name], sym)
	}

	return &SymbolIndex{
		Symbols:    syms,
		References: ExtractReferences(s),
		nameIndex:  nameIdx,
	}
}

// FindByName finds a symbol by name and kind.
// If the name index is populated (via BuildSymbolIndex), this is an O(1) map
// lookup followed by a short linear scan over same-name symbols.
// Falls back to a full linear scan for manually constructed indices.
func (idx *SymbolIndex) FindByName(name string, kind SymbolKind) *Symbol {
	if idx == nil || len(idx.Symbols) == 0 {
		return nil
	}

	// Fast path: use the name index when available
	if idx.nameIndex != nil {
		for _, sym := range idx.nameIndex[name] {
			if sym.Kind == kind {
				return sym
			}
		}
		return nil
	}

	// Fallback: linear scan for manually constructed indices
	for i := range idx.Symbols {
		sym := &idx.Symbols[i]
		if sym.Name == name && sym.Kind == kind {
			return sym
		}
	}
	return nil
}

// SymbolAtPosition finds the smallest symbol whose Range contains the given position.
// It returns the most specific (innermost) symbol at that position--for example,
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
			if best == nil || IsSmaller(sym.Range, best.Range) {
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

// FormatPropertyDetail formats a property for display.
func FormatPropertyDetail(p schema.Property) string {
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
func formatRelationDetail(r schema.Relation, arrow string) string {
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

// IsSmaller returns true if a is smaller (more specific) than b.
func IsSmaller(a, b location.Span) bool {
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

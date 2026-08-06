package schema

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
)

// schemaParser adapts the parser's node tree onto this package's decl model.
// Spans, constraint values and recovery are settled before a node arrives, so
// this layer re-parses no spelling; it owns only the loading decisions the
// parser leaves out, which is alias derivation and the reserved-word refusal.
type schemaParser struct {
	sourceID  location.SourceID
	collector *diag.Collector
}

// newParser creates a new schemaParser for the given source.
func newParser(sourceID location.SourceID, collector *diag.Collector) *schemaParser {
	return &schemaParser{sourceID: sourceID, collector: collector}
}

// Parse parses the source content and returns an AST model, collecting parse
// errors in the provided collector. It returns nil when no usable schema name
// was produced, which stops the loader before completion: a nameless model
// draws a cascade of diagnostics that all restate the one real defect.
func (p *schemaParser) Parse(source []byte) *model {
	file, issues := parse.Parse(source, p.sourceID)
	p.collector.CollectAll(issues)

	if file.SchemaNameFailed {
		return nil
	}

	m := &model{
		Name:          file.Name,
		Span:          file.Span,
		Documentation: file.Doc,
	}
	for _, imp := range file.Imports {
		if decl := p.importDecl(imp); decl != nil {
			m.Imports = append(m.Imports, decl)
		}
	}
	for _, dt := range file.DataTypes {
		constraint, _ := p.constraint(dt.Constraint)
		m.DataTypes = append(m.DataTypes, &dataTypeDecl{
			Name:          dt.Name,
			Constraint:    constraint,
			Documentation: dt.Doc,
			Span:          dt.Span,
		})
	}
	for _, t := range file.Types {
		m.Types = append(m.Types, p.typeDecl(t))
	}
	return m
}

// importDecl resolves an import's alias and returns nil for a reserved one,
// dropping the declaration. The parser records only an alias the source wrote,
// because deriving one from the path is a loading decision; the reserved-word
// refusal belongs with it for the same reason.
func (p *schemaParser) importDecl(imp *parse.Import) *importDecl {
	alias := imp.Alias
	if !imp.HasAlias {
		alias = deriveAliasFromPath(imp.Path)
	}
	if isReservedKeyword(alias) {
		p.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_ALIAS,
			fmt.Sprintf("import alias %q is a reserved keyword", alias)).
			WithSpan(imp.Span).Build())
		return nil
	}
	return &importDecl{Path: imp.Path, Alias: alias, Span: imp.Span}
}

func (p *schemaParser) typeDecl(t *parse.TypeDecl) *typeDecl {
	out := &typeDecl{
		Name:          t.Name,
		NameSpan:      t.NameSpan,
		IsAbstract:    t.IsAbstract,
		IsPart:        t.IsPart,
		Documentation: t.Doc,
		Span:          t.Span,
	}
	for _, ref := range t.Extends {
		out.Inherits = append(out.Inherits, typeRef(ref))
	}
	for _, prop := range t.Properties {
		out.Properties = append(out.Properties, p.property(prop))
	}
	for _, rel := range t.Relations {
		out.Relations = append(out.Relations, p.relationDecl(rel))
	}
	for _, inv := range t.Invariants {
		// The decl layer calls the invariant's message string its name.
		out.Invariants = append(out.Invariants, &invariantDecl{
			Name:          inv.Message,
			Expr:          inv.Expr,
			Documentation: inv.Doc,
			Span:          inv.Span,
		})
	}
	for _, ann := range t.Annotations {
		out.Annotations = append(out.Annotations, annotationDeclOf(ann))
	}
	return out
}

// property maps a type-body property. A primary key is required by definition,
// which is why Optional reads both modifiers where the edge-property form below
// reads one.
func (p *schemaParser) property(prop *parse.Property) *propertyDecl {
	constraint, ref := p.constraint(prop.Constraint)
	out := &propertyDecl{
		Name:          prop.Name,
		Constraint:    constraint,
		DataTypeRef:   ref,
		Optional:      !prop.IsRequired && !prop.IsPrimaryKey,
		IsPrimaryKey:  prop.IsPrimaryKey,
		Documentation: prop.Doc,
		Span:          prop.Span,
	}
	for _, ann := range prop.Annotations {
		out.Annotations = append(out.Annotations, annotationDeclOf(ann))
	}
	return out
}

// edgeProperty maps a relation's edge property, which carries no annotations
// and never a primary key — the grammar admits neither in that position.
func (p *schemaParser) edgeProperty(prop *parse.Property) *propertyDecl {
	constraint, ref := p.constraint(prop.Constraint)
	return &propertyDecl{
		Name:          prop.Name,
		Constraint:    constraint,
		DataTypeRef:   ref,
		Optional:      !prop.IsRequired,
		Documentation: prop.Doc,
		Span:          prop.Span,
	}
}

func (p *schemaParser) relationDecl(rel *parse.Relation) *relationDecl {
	out := &relationDecl{
		Kind:            relationKind(rel.Kind),
		Name:            rel.Name,
		Target:          typeRef(rel.Target),
		Optional:        rel.Optional,
		Many:            rel.Many,
		Backref:         rel.Backref,
		ReverseOptional: rel.ReverseOptional,
		ReverseMany:     rel.ReverseMany,
		Documentation:   rel.Doc,
		Span:            rel.Span,
	}
	for _, prop := range rel.Properties {
		out.Properties = append(out.Properties, p.edgeProperty(prop))
	}
	return out
}

func relationKind(k parse.RelationKind) RelationKind {
	if k == parse.RelationComposition {
		return RelationComposition
	}
	return RelationAssociation
}

func typeRef(r *parse.TypeRef) *astTypeRef {
	if r == nil {
		return nil
	}
	return &astTypeRef{Qualifier: r.Qualifier, Name: r.Name, Span: r.Span}
}

func annotationDeclOf(a *parse.Annotation) *annotationDecl {
	out := &annotationDecl{
		Name:             a.Name,
		Documentation:    a.Doc,
		Span:             a.Span,
		DetachedFromLine: a.DetachedFromLine,
	}
	if len(a.Args) == 0 {
		return out
	}
	out.Args = make([]annotationArgDecl, 0, len(a.Args))
	for _, arg := range a.Args {
		out.Args = append(out.Args, annotationArgDecl{
			Text:  arg.Text,
			Token: annotationToken(arg.Kind),
			Span:  arg.Span,
			Raw:   arg.Raw,
		})
	}
	return out
}

func annotationToken(k parse.ArgKind) annotationTokenKind {
	//exhaustive:enforce
	switch k {
	case parse.ArgIdentifier:
		return tokenIdentifier
	case parse.ArgLiteral:
		return tokenLiteral
	case parse.ArgString:
		return tokenString
	}
	return tokenIdentifier // unreachable: ArgKind has exactly the three forms above
}

// constraint builds the decl layer's constraint from a parsed one, plus the
// data-type reference an alias carries for go-to-definition. A nil result means
// an unusable datatype — a rejected Vector dimension, or a List whose element
// was rejected — and both already carry their own diagnostic.
func (p *schemaParser) constraint(c *parse.Constraint) (Constraint, DataTypeRef) {
	if c == nil {
		return nil, DataTypeRef{}
	}
	//exhaustive:enforce
	switch c.Kind {
	case parse.ConstraintInteger:
		return integerConstraint(c), DataTypeRef{}
	case parse.ConstraintFloat:
		return floatConstraint(c), DataTypeRef{}
	case parse.ConstraintBoolean:
		return NewBooleanConstraint(), DataTypeRef{}
	case parse.ConstraintString:
		return stringConstraint(c), DataTypeRef{}
	case parse.ConstraintEnum:
		return NewEnumConstraint(c.EnumValues()), DataTypeRef{}
	case parse.ConstraintPattern:
		return NewPatternConstraint(c.PatternRegexps()), DataTypeRef{}
	case parse.ConstraintTimestamp:
		if format, ok := c.Format(); ok {
			return NewTimestampConstraintFormatted(format), DataTypeRef{}
		}
		return NewTimestampConstraint(), DataTypeRef{}
	case parse.ConstraintDate:
		return NewDateConstraint(), DataTypeRef{}
	case parse.ConstraintUUID:
		return NewUUIDConstraint(), DataTypeRef{}
	case parse.ConstraintVector:
		if c.VectorDims == nil {
			return nil, DataTypeRef{}
		}
		return NewVectorConstraint(*c.VectorDims), DataTypeRef{}
	case parse.ConstraintList:
		return p.listConstraint(c)
	case parse.ConstraintAlias:
		return aliasConstraint(c)
	}
	return nil, DataTypeRef{} // unreachable: ConstraintKind has exactly the twelve forms above
}

// aliasConstraint is the one form yielding two outputs from one node: the
// constraint resolved during completion, and the reference the LSP navigates
// from. The span covers the whole reference, qualifier included, so
// go-to-definition selects what the author wrote.
func aliasConstraint(c *parse.Constraint) (Constraint, DataTypeRef) {
	ref := c.Alias
	if ref == nil {
		return nil, DataTypeRef{}
	}
	fullName := ref.Name
	if ref.Qualifier != "" {
		fullName = ref.Qualifier + "." + ref.Name
	}
	return NewAliasConstraint(fullName, nil), NewDataTypeRef(ref.Qualifier, ref.Name, ref.Span)
}

func integerConstraint(c *parse.Constraint) Constraint {
	switch {
	case c.IntMin != nil && c.IntMax != nil:
		return IntegerBetween(*c.IntMin, *c.IntMax)
	case c.IntMin != nil:
		return IntegerMin(*c.IntMin)
	case c.IntMax != nil:
		return IntegerMax(*c.IntMax)
	}
	return NewIntegerConstraint()
}

func floatConstraint(c *parse.Constraint) Constraint {
	switch {
	case c.FloatMin != nil && c.FloatMax != nil:
		return FloatBetween(*c.FloatMin, *c.FloatMax)
	case c.FloatMin != nil:
		return FloatMin(*c.FloatMin)
	case c.FloatMax != nil:
		return FloatMax(*c.FloatMax)
	}
	return NewFloatConstraint()
}

func stringConstraint(c *parse.Constraint) Constraint {
	switch {
	case c.LenMin != nil && c.LenMax != nil:
		return StringLenBetween(*c.LenMin, *c.LenMax)
	case c.LenMin != nil:
		return StringMinLen(*c.LenMin)
	case c.LenMax != nil:
		return StringMaxLen(*c.LenMax)
	}
	return NewStringConstraint()
}

// listConstraint returns nil when the element type is unusable, because a list
// of nothing is not a datatype the layer above can validate against. The
// element's data-type reference passes through: List<Alias> is a property whose
// declared type names that alias, and go-to-definition on it must land there.
func (p *schemaParser) listConstraint(c *parse.Constraint) (Constraint, DataTypeRef) {
	elem, ref := p.constraint(c.Elem)
	if elem == nil {
		return nil, DataTypeRef{}
	}
	switch {
	case c.LenMin != nil && c.LenMax != nil:
		return ListLenBetween(elem, *c.LenMin, *c.LenMax), ref
	case c.LenMin != nil:
		return ListMinLen(elem, *c.LenMin), ref
	case c.LenMax != nil:
		return ListMaxLen(elem, *c.LenMax), ref
	}
	return NewListConstraint(elem), ref
}

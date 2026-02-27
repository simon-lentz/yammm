package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/grammar"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
	"github.com/simon-lentz/yammm/schema/internal/alias"
)

// Parser parses YAMMM schema source into an AST Model.
type Parser struct {
	sourceID  location.SourceID
	collector *diag.Collector
	spans     *SpanBuilder
}

// NewParser creates a new Parser for the given source.
func NewParser(
	sourceID location.SourceID,
	collector *diag.Collector,
	registry location.PositionRegistry,
	converter location.RuneOffsetConverter,
) *Parser {
	return &Parser{
		sourceID:  sourceID,
		collector: collector,
		spans:     NewSpanBuilder(sourceID, registry, converter),
	}
}

// Parse parses the source content and returns an AST Model.
// Parse errors are collected in the provided collector.
func (p *Parser) Parse(source []byte) *Model {
	input := antlr.NewInputStream(string(source))
	lexer := grammar.NewYammmGrammarLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar.NewYammmGrammarParser(stream)

	// Remove default error listeners and add our own
	parser.RemoveErrorListeners()
	parser.AddErrorListener(&errorListener{
		collector: p.collector,
		sourceID:  p.sourceID,
		spans:     p.spans,
	})

	// Parse the schema
	tree := parser.Schema()

	// Build the AST using a listener
	listener := &astBuilder{
		parser:    p,
		sourceID:  p.sourceID,
		collector: p.collector,
		spans:     p.spans,
	}
	antlr.ParseTreeWalkerDefault.Walk(listener, tree)

	return listener.model
}

// errorListener converts ANTLR errors to diagnostic issues.
type errorListener struct {
	*antlr.DefaultErrorListener
	collector *diag.Collector
	sourceID  location.SourceID
	spans     *SpanBuilder
}

func (e *errorListener) SyntaxError(
	_ antlr.Recognizer,
	offendingSymbol any,
	line, column int,
	msg string,
	_ antlr.RecognitionException,
) {
	var span location.Span

	// Try to get proper byte offsets from the offending token.
	// Per, schema parsing is "trusted provenance" — all spans
	// should have byte offsets for LSP conversion.
	if token, ok := offendingSymbol.(antlr.Token); ok && token != nil {
		span = e.spans.FromToken(token)
	} else {
		// Fallback: ANTLR provides line/column but no token (rare edge case).
		// Use Byte=-1 to signal unknown byte offset.
		pos := location.Position{Line: line, Column: column + 1, Byte: -1}
		span = location.Span{Source: e.sourceID, Start: pos, End: pos}
	}

	e.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, msg).WithSpan(span).Build())
}

// astBuilder is an ANTLR listener that builds the AST Model.
type astBuilder struct {
	*grammar.BaseYammmGrammarListener
	parser    *Parser
	sourceID  location.SourceID
	collector *diag.Collector
	spans     *SpanBuilder

	// Current parsing state
	model          *Model
	currentType    *TypeDecl
	currentDT      schema.Constraint
	currentDTRef   schema.DataTypeRef // Reference to DataType (for alias constraints)
	currentProps   []*PropertyDecl
	currentImports []*ImportDecl
}

// ExitSchema_name is called when exiting the schema_name production.
func (b *astBuilder) ExitSchema_name(ctx *grammar.Schema_nameContext) {
	nameToken := ctx.GetToken(grammar.YammmGrammarLexerSTRING, 0)
	if nameToken == nil {
		b.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_SYNTAX, "missing schema name").Build())
		return
	}

	name, err := unquoteString(nameToken.GetText())
	if err != nil {
		span := b.spans.FromToken(nameToken.GetSymbol())
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			fmt.Sprintf("invalid schema name: %v", err)).WithSpan(span).Build())
		return
	}

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	b.model = &Model{
		Name:          name,
		Span:          b.spans.FromContext(ctx),
		Documentation: doc,
	}
}

// ExitImport_decl is called when exiting the import_decl production.
func (b *astBuilder) ExitImport_decl(ctx *grammar.Import_declContext) {
	pathToken := ctx.GetPath()
	if pathToken == nil {
		return
	}

	path, err := unquoteString(pathToken.GetText())
	if err != nil {
		span := b.spans.FromToken(pathToken)
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			fmt.Sprintf("invalid import path: %v", err)).WithSpan(span).Build())
		return
	}

	var importAlias string
	if aliasCtx := ctx.GetAlias(); aliasCtx != nil {
		importAlias = aliasCtx.GetText()
	} else {
		importAlias = alias.DeriveAliasFromPath(path)
	}

	// Check for reserved keyword alias
	if alias.IsReservedKeyword(importAlias) {
		span := b.spans.FromContext(ctx)
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_ALIAS,
			fmt.Sprintf("import alias %q is a reserved keyword", importAlias)).WithSpan(span).Build())
		return
	}

	imp := &ImportDecl{
		Path:  path,
		Alias: importAlias,
		Span:  b.spans.FromContext(ctx),
	}
	b.currentImports = append(b.currentImports, imp)
}

// ExitSchema is called when exiting the schema production.
func (b *astBuilder) ExitSchema(_ *grammar.SchemaContext) {
	if b.model != nil {
		b.model.Imports = b.currentImports
	}
}

// EnterType is called when entering the type production.
func (b *astBuilder) EnterType(ctx *grammar.TypeContext) {
	typeNameCtx := ctx.Type_name()
	if typeNameCtx == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"type declaration missing name").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}
	typeName := typeNameCtx.GetText()

	// Capture precise span of the type name token for accurate go-to-definition
	nameSpan := b.spans.FromContext(typeNameCtx)

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	b.currentType = &TypeDecl{
		Name:          typeName,
		NameSpan:      nameSpan,
		IsAbstract:    ctx.GetIs_abstract() != nil,
		IsPart:        ctx.GetIs_part() != nil,
		Documentation: doc,
		Span:          b.spans.FromContext(ctx),
	}
	b.currentProps = nil
}

// ExitType is called when exiting the type production.
func (b *astBuilder) ExitType(_ *grammar.TypeContext) {
	if b.model != nil && b.currentType != nil {
		b.model.Types = append(b.model.Types, b.currentType)
	}
	b.currentType = nil
}

// ExitExtends_types is called when exiting the extends_types production.
func (b *astBuilder) ExitExtends_types(ctx *grammar.Extends_typesContext) {
	if b.currentType == nil {
		return
	}
	for _, refCtx := range ctx.AllType_ref() {
		ref := b.buildTypeRef(refCtx)
		if ref != nil {
			b.currentType.Inherits = append(b.currentType.Inherits, ref)
		}
	}
}

// ExitProperty is called when exiting the property production.
func (b *astBuilder) ExitProperty(ctx *grammar.PropertyContext) {
	if b.currentType == nil {
		return
	}

	propNameCtx := ctx.Property_name()
	if propNameCtx == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"property declaration missing name").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}
	propName := propNameCtx.GetText()
	isPrimary := ctx.GetIs_primary() != nil
	isRequired := ctx.GetIs_required() != nil || isPrimary

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	prop := &PropertyDecl{
		Name:          propName,
		Constraint:    b.currentDT,
		DataTypeRef:   b.currentDTRef,
		Optional:      !isRequired,
		IsPrimaryKey:  isPrimary,
		Documentation: doc,
		Span:          b.spans.FromContext(ctx),
	}
	b.currentType.Properties = append(b.currentType.Properties, prop)
	b.currentDT = nil
	b.currentDTRef = schema.DataTypeRef{} // Clear for next property
}

// ExitDatatype is called when exiting the datatype production.
func (b *astBuilder) ExitDatatype(ctx *grammar.DatatypeContext) {
	typeNameCtx := ctx.Type_name()
	if typeNameCtx == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"datatype declaration missing name").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}
	typeName := typeNameCtx.GetText() // Preserve declared case

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	dt := &DataTypeDecl{
		Name:          typeName,
		Constraint:    b.currentDT,
		Documentation: doc,
		Span:          b.spans.FromContext(ctx),
	}

	if b.model != nil {
		b.model.DataTypes = append(b.model.DataTypes, dt)
	}
	b.currentDT = nil
}

// EnterAssociation is called when entering the association production.
func (b *astBuilder) EnterAssociation(_ *grammar.AssociationContext) {
	b.currentProps = nil
}

// ExitAssociation is called when exiting the association production.
func (b *astBuilder) ExitAssociation(ctx *grammar.AssociationContext) {
	if b.currentType == nil {
		return
	}

	thisNameCtx := ctx.GetThisName()
	if thisNameCtx == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"association missing relation name").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}

	optional, many := handleMultiplicity(ctx.GetThisMp())
	relName := thisNameCtx.GetText()
	target := b.buildTypeRef(ctx.GetToType())

	reverseOptional, reverseMany := true, false
	backref := ""
	if ctx.GetReverse_name() != nil {
		backref = ctx.GetReverse_name().GetText()
		reverseOptional, reverseMany = handleMultiplicity(ctx.GetReverseMp())
	}

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	rel := &RelationDecl{
		Kind:            RelationAssociation,
		Name:            relName,
		Target:          target,
		Optional:        optional,
		Many:            many,
		Backref:         backref,
		ReverseOptional: reverseOptional,
		ReverseMany:     reverseMany,
		Properties:      b.currentProps,
		Documentation:   doc,
		Span:            b.spans.FromContext(ctx),
	}
	b.currentType.Relations = append(b.currentType.Relations, rel)
	b.currentProps = nil
}

// EnterComposition is called when entering the composition production.
func (b *astBuilder) EnterComposition(_ *grammar.CompositionContext) {
	b.currentProps = nil
}

// ExitComposition is called when exiting the composition production.
func (b *astBuilder) ExitComposition(ctx *grammar.CompositionContext) {
	if b.currentType == nil {
		return
	}

	optional, many := handleMultiplicity(ctx.GetThisMp())
	target := b.buildTypeRef(ctx.GetToType())

	var relName string
	if ctx.GetThisName() != nil {
		relName = ctx.GetThisName().GetText()
	}
	if relName == "" {
		// Composition requires a name for edge object encoding.
		// This should not happen with valid grammar, but we validate defensively.
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"composition must have a name").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}

	reverseOptional, reverseMany := true, false
	backref := ""
	if ctx.GetReverse_name() != nil {
		backref = ctx.GetReverse_name().GetText()
		reverseOptional, reverseMany = handleMultiplicity(ctx.GetReverseMp())
	}

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	rel := &RelationDecl{
		Kind:            RelationComposition,
		Name:            relName,
		Target:          target,
		Optional:        optional,
		Many:            many,
		Backref:         backref,
		ReverseOptional: reverseOptional,
		ReverseMany:     reverseMany,
		Documentation:   doc,
		Span:            b.spans.FromContext(ctx),
	}
	b.currentType.Relations = append(b.currentType.Relations, rel)
}

// ExitRel_property is called when exiting the rel_property production.
func (b *astBuilder) ExitRel_property(ctx *grammar.Rel_propertyContext) {
	propNameCtx := ctx.Property_name()
	if propNameCtx == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"edge property missing name").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}
	propName := propNameCtx.GetText()
	isRequired := ctx.GetIs_required() != nil

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	prop := &PropertyDecl{
		Name:          propName,
		Constraint:    b.currentDT,
		DataTypeRef:   b.currentDTRef,
		Optional:      !isRequired,
		IsPrimaryKey:  false,
		Documentation: doc,
		Span:          b.spans.FromContext(ctx),
	}
	b.currentProps = append(b.currentProps, prop)
	b.currentDT = nil
	b.currentDTRef = schema.DataTypeRef{} // Clear for next property
}

// ExitInvariant is called when exiting the invariant production.
func (b *astBuilder) ExitInvariant(ctx *grammar.InvariantContext) {
	if b.currentType == nil {
		return
	}

	msgToken := ctx.GetMessage()
	if msgToken == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"invariant missing message string").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}
	name, err := unquoteString(msgToken.GetText())
	if err != nil {
		span := b.spans.FromToken(msgToken)
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			fmt.Sprintf("invalid invariant message: %v", err)).WithSpan(span).Build())
		return
	}

	// Compile the invariant expression
	var compiledExpr expr.Expression
	if exprCtx := ctx.Expr(); exprCtx != nil {
		compiledExpr = expr.Compile(
			exprCtx,
			b.collector,
			b.sourceID,
			b.spans.Registry(),
			b.spans.Converter(),
		)
	}

	var doc string
	if ctx.DOC_COMMENT() != nil {
		doc = stripDelimiters(ctx.DOC_COMMENT().GetText())
	}

	inv := &InvariantDecl{
		Name:          name,
		Expr:          compiledExpr,
		Documentation: doc,
		Span:          b.spans.FromContext(ctx),
	}
	b.currentType.Invariants = append(b.currentType.Invariants, inv)
}

// --- Constraint builders ---

// boundSpan returns a span covering a bound value, including the optional
// leading minus sign. When neg is nil, it falls back to spanning only the
// value token.
func (b *astBuilder) boundSpan(neg, value antlr.Token) location.Span {
	if neg != nil {
		return b.spans.FromTokens(neg, value)
	}
	return b.spans.FromToken(value)
}

func (b *astBuilder) ExitIntegerT(ctx *grammar.IntegerTContext) {
	var min, max int64
	hasMin, hasMax := false, false
	parseErr := false

	if minToken := ctx.GetMin(); minToken != nil && minToken.GetText() != "_" {
		minText := minToken.GetText()
		if ctx.GetNegMin() != nil {
			minText = "-" + minText
		}
		v, err := strconv.ParseInt(minText, 10, 64)
		if err != nil {
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid integer bound: %v", err)).
				WithSpan(b.boundSpan(ctx.GetNegMin(), minToken)).Build())
			parseErr = true
		} else {
			min, hasMin = v, true
		}
	} else if minToken != nil && minToken.GetText() == "_" && ctx.GetNegMin() != nil {
		b.collector.Collect(diag.NewIssue(diag.Warning, diag.E_INVALID_CONSTRAINT,
			"minus sign before '_' (unbounded) has no effect").
			WithSpan(b.spans.FromToken(ctx.GetNegMin())).Build())
	}
	if maxToken := ctx.GetMax(); maxToken != nil && maxToken.GetText() != "_" {
		maxText := maxToken.GetText()
		if ctx.GetNegMax() != nil {
			maxText = "-" + maxText
		}
		v, err := strconv.ParseInt(maxText, 10, 64)
		if err != nil {
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid integer bound: %v", err)).
				WithSpan(b.boundSpan(ctx.GetNegMax(), maxToken)).Build())
			parseErr = true
		} else {
			max, hasMax = v, true
		}
	} else if maxToken := ctx.GetMax(); maxToken != nil && maxToken.GetText() == "_" && ctx.GetNegMax() != nil {
		b.collector.Collect(diag.NewIssue(diag.Warning, diag.E_INVALID_CONSTRAINT,
			"minus sign before '_' (unbounded) has no effect").
			WithSpan(b.spans.FromToken(ctx.GetNegMax())).Build())
	}

	// Validate min <= max when both are present
	if !parseErr && hasMin && hasMax && min > max {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("integer bounds inverted: min %d > max %d", min, max)).
			WithSpan(b.spans.FromContext(ctx)).Build())
	}

	b.currentDT = schema.NewIntegerConstraintBounded(min, hasMin, max, hasMax)
}

func (b *astBuilder) ExitFloatT(ctx *grammar.FloatTContext) {
	var min, max float64
	hasMin, hasMax := false, false
	parseErr := false

	if minToken := ctx.GetMin(); minToken != nil && minToken.GetText() != "_" {
		minText := minToken.GetText()
		if ctx.GetNegMin() != nil {
			minText = "-" + minText
		}
		v, err := strconv.ParseFloat(minText, 64)
		if err != nil {
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid float bound: %v", err)).
				WithSpan(b.boundSpan(ctx.GetNegMin(), minToken)).Build())
			parseErr = true
		} else {
			min, hasMin = v, true
		}
	} else if minToken != nil && minToken.GetText() == "_" && ctx.GetNegMin() != nil {
		b.collector.Collect(diag.NewIssue(diag.Warning, diag.E_INVALID_CONSTRAINT,
			"minus sign before '_' (unbounded) has no effect").
			WithSpan(b.spans.FromToken(ctx.GetNegMin())).Build())
	}
	if maxToken := ctx.GetMax(); maxToken != nil && maxToken.GetText() != "_" {
		maxText := maxToken.GetText()
		if ctx.GetNegMax() != nil {
			maxText = "-" + maxText
		}
		v, err := strconv.ParseFloat(maxText, 64)
		if err != nil {
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid float bound: %v", err)).
				WithSpan(b.boundSpan(ctx.GetNegMax(), maxToken)).Build())
			parseErr = true
		} else {
			max, hasMax = v, true
		}
	} else if maxToken := ctx.GetMax(); maxToken != nil && maxToken.GetText() == "_" && ctx.GetNegMax() != nil {
		b.collector.Collect(diag.NewIssue(diag.Warning, diag.E_INVALID_CONSTRAINT,
			"minus sign before '_' (unbounded) has no effect").
			WithSpan(b.spans.FromToken(ctx.GetNegMax())).Build())
	}

	// Validate min <= max when both are present
	if !parseErr && hasMin && hasMax && min > max {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("float bounds inverted: min %v > max %v", min, max)).
			WithSpan(b.spans.FromContext(ctx)).Build())
	}

	b.currentDT = schema.NewFloatConstraintBounded(min, hasMin, max, hasMax)
}

func (b *astBuilder) ExitBoolT(_ *grammar.BoolTContext) {
	b.currentDT = schema.NewBooleanConstraint()
}

func (b *astBuilder) ExitStringT(ctx *grammar.StringTContext) {
	var minLen, maxLen int64 = -1, -1
	parseErr := false

	if minToken := ctx.GetMin(); minToken != nil && minToken.GetText() != "_" {
		v, err := strconv.ParseInt(minToken.GetText(), 10, 64)
		switch {
		case err != nil:
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid string length bound: %v", err)).
				WithSpan(b.spans.FromToken(minToken)).Build())
			parseErr = true
		case v < 0:
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("string minimum length cannot be negative: %d", v)).
				WithSpan(b.spans.FromToken(minToken)).Build())
			parseErr = true
		default:
			minLen = v
		}
	}
	if maxToken := ctx.GetMax(); maxToken != nil && maxToken.GetText() != "_" {
		v, err := strconv.ParseInt(maxToken.GetText(), 10, 64)
		switch {
		case err != nil:
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid string length bound: %v", err)).
				WithSpan(b.spans.FromToken(maxToken)).Build())
			parseErr = true
		case v < 0:
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("string maximum length cannot be negative: %d", v)).
				WithSpan(b.spans.FromToken(maxToken)).Build())
			parseErr = true
		default:
			maxLen = v
		}
	}

	// Validate min <= max when both are present
	if !parseErr && minLen >= 0 && maxLen >= 0 && minLen > maxLen {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("string length bounds inverted: min %d > max %d", minLen, maxLen)).
			WithSpan(b.spans.FromContext(ctx)).Build())
	}

	b.currentDT = schema.NewStringConstraintBounded(minLen, maxLen)
}

func (b *astBuilder) ExitEnumT(ctx *grammar.EnumTContext) {
	var values []string
	seen := make(map[string]bool)
	for _, token := range ctx.AllSTRING() {
		val, err := unquoteString(token.GetText())
		if err != nil {
			span := b.spans.FromToken(token.GetSymbol())
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
				fmt.Sprintf("invalid enum value: %v", err)).WithSpan(span).Build())
			continue
		}
		if val == "" {
			span := b.spans.FromToken(token.GetSymbol())
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				"enum value cannot be empty").WithSpan(span).Build())
			continue
		}
		if seen[val] {
			span := b.spans.FromToken(token.GetSymbol())
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("duplicate enum value %q", val)).WithSpan(span).Build())
			continue
		}
		seen[val] = true
		values = append(values, val)
	}
	if len(values) < 2 {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("enum must have at least two values (got %d)", len(values))).WithSpan(b.spans.FromContext(ctx)).Build())
	}
	b.currentDT = schema.NewEnumConstraint(values)
}

func (b *astBuilder) ExitPatternT(ctx *grammar.PatternTContext) {
	var patterns []*regexp.Regexp
	for _, token := range ctx.AllSTRING() {
		patStr, err := unquoteString(token.GetText())
		if err != nil {
			span := b.spans.FromToken(token.GetSymbol())
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
				fmt.Sprintf("invalid pattern: %v", err)).WithSpan(span).Build())
			continue
		}
		re, err := regexp.Compile(patStr)
		if err != nil {
			span := b.spans.FromToken(token.GetSymbol())
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("invalid regex pattern %q: %v", patStr, err)).WithSpan(span).Build())
			continue
		}
		patterns = append(patterns, re)
	}
	// Emit error if more than 2 patterns provided (max 2 allowed for performance)
	if len(patterns) > 2 {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("pattern constraint exceeds maximum of 2 patterns (got %d)", len(patterns))).
			WithSpan(b.spans.FromContext(ctx)).Build())
		patterns = patterns[:2] // Continue with first 2 for error recovery
	}
	b.currentDT = schema.NewPatternConstraint(patterns)
}

func (b *astBuilder) ExitTimestampT(ctx *grammar.TimestampTContext) {
	if ctx.GetFormat() == nil {
		b.currentDT = schema.NewTimestampConstraint()
	} else {
		format, err := unquoteString(ctx.GetFormat().GetText())
		if err != nil {
			span := b.spans.FromToken(ctx.GetFormat())
			b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
				fmt.Sprintf("invalid timestamp format: %v", err)).WithSpan(span).Build())
			b.currentDT = schema.NewTimestampConstraint()
			return
		}
		b.currentDT = schema.NewTimestampConstraintFormatted(format)
	}
}

func (b *astBuilder) ExitDateT(_ *grammar.DateTContext) {
	b.currentDT = schema.NewDateConstraint()
}

func (b *astBuilder) ExitUuidT(_ *grammar.UuidTContext) {
	b.currentDT = schema.NewUUIDConstraint()
}

const (
	minVectorDimensions = 1
	maxVectorDimensions = 65536
)

func (b *astBuilder) ExitVectorT(ctx *grammar.VectorTContext) {
	dimToken := ctx.GetDimensions()
	if dimToken == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"vector type missing dimensions").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}

	dimText := dimToken.GetText()
	if dimText == "" {
		// ANTLR error recovery may produce a token with empty text
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"vector type missing dimensions").WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}

	dim, err := strconv.Atoi(dimText)
	if err != nil {
		// Use context span since token may have invalid position from error recovery
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("invalid vector dimensions: %v", err)).WithSpan(b.spans.FromContext(ctx)).Build())
		return
	}

	if dim < minVectorDimensions {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("vector dimensions must be at least %d (got %d)", minVectorDimensions, dim)).
			WithSpan(b.spans.FromToken(dimToken)).Build())
		return
	}

	if dim > maxVectorDimensions {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
			fmt.Sprintf("vector dimensions exceed maximum of %d (got %d)", maxVectorDimensions, dim)).
			WithSpan(b.spans.FromToken(dimToken)).Build())
		return
	}

	b.currentDT = schema.NewVectorConstraint(dim)
}

func (b *astBuilder) ExitQualified_alias(ctx *grammar.Qualified_aliasContext) {
	nameToken := ctx.GetName()
	if nameToken == nil {
		// Syntax error recovery - no name token
		return
	}

	qualifier := ""
	if q := ctx.GetQualifier(); q != nil {
		qualifier = q.GetText()
	}
	name := nameToken.GetText() // Preserve declared case (not toLowerFirst)

	var fullName string
	if qualifier != "" {
		fullName = qualifier + "." + name
	} else {
		fullName = name
	}

	// AliasConstraint needs to be resolved during completion
	// For now, store a placeholder with just the name
	b.currentDT = schema.NewAliasConstraint(fullName, nil)

	// Capture the DataTypeRef with span for LSP navigation
	b.currentDTRef = schema.NewDataTypeRef(qualifier, name, b.spans.FromContext(ctx))
}

// --- Helper methods ---

func (b *astBuilder) buildTypeRef(ctx grammar.IType_refContext) *TypeRef {
	if ctx == nil {
		return nil
	}

	nameCtx := ctx.GetName()
	if nameCtx == nil {
		b.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX,
			"type reference missing type name").WithSpan(b.spans.FromContext(ctx)).Build())
		return nil
	}

	qualifier := ""
	if q := ctx.GetQualifier(); q != nil {
		qualifier = q.GetText()
	}
	name := nameCtx.GetText()
	return &TypeRef{
		Qualifier: qualifier,
		Name:      name,
		Span:      b.spans.FromContext(ctx),
	}
}

// --- Utility functions ---

// handleMultiplicity parses the multiplicity context and returns (optional, many).
// When multiplicity is omitted (ctx == nil), defaults to optional/one (true, false).
// This matches the grammar where multiplicity is optional for relation declarations.
func handleMultiplicity(ctx grammar.IMultiplicityContext) (optional, many bool) {
	if ctx == nil {
		// Default: optional/one - the relation is not required and has at most one target.
		return true, false
	}
	text := ctx.GetText()
	switch text {
	case "(_)", "(_:one)":
		return true, false
	case "(one)":
		return false, false
	case "(many)", "(_:many)":
		return true, true
	case "(one:one)":
		return false, false
	case "(one:many)":
		return false, true
	default:
		return true, false
	}
}

// unquoteString removes surrounding quotes from a string literal and processes
// escape sequences. Handles both single and double quoted strings.
// Returns the original string unchanged if not properly quoted.
func unquoteString(s string) (string, error) {
	if len(s) < 2 {
		return s, nil
	}
	// Handle both single and double quoted strings
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		result, err := strconv.Unquote(`"` + s[1:len(s)-1] + `"`)
		if err != nil {
			return "", fmt.Errorf("unquote string: %w", err)
		}
		return result, nil
	}
	return s, nil
}

// stripDelimiters removes block comment delimiters (/* */) from documentation
// comments and trims surrounding whitespace from the inner content.
func stripDelimiters(s string) string {
	if len(s) >= 4 && strings.HasPrefix(s, "/*") && strings.HasSuffix(s, "*/") {
		return strings.TrimSpace(s[2 : len(s)-2])
	}
	return s
}

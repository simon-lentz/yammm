package schema

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// Name productions mirrored from the grammar's lexer rules. The Builder and
// the parser are two front doors to the same object model: a Builder-built
// schema must remain expressible in the DSL, so declared names are held to
// the same productions the parser enforces structurally. Schema names and
// invariant names are quoted strings in the DSL and stay free-form.
var (
	// builderTypeNameRE mirrors UC_WORD (type and datatype names).
	builderTypeNameRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)
	// builderPropertyNameRE mirrors LC_WORD, which subsumes the lc_keyword
	// alternatives the property_name production also admits.
	builderPropertyNameRE = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)
	// builderRelationNameRE is the parser's RelationName production, UPPER_SNAKE.
	builderRelationNameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// unsupportedLiteral finds the first literal in e whose Go type the expression
// language does not define. A caller-built tree may hold any Go value, and the
// structural hash, which completion now runs over invariants, encodes only the
// language's kinds; refusing here keeps Build's contract of result.Err().
func unsupportedLiteral(e expr.Expression) (any, bool) {
	switch ex := e.(type) {
	case *expr.Literal:
		switch v := ex.Val.(type) {
		case nil, string, int64, float64, bool, *regexp.Regexp, []string:
			return nil, false
		case []expr.Expression:
			for _, arg := range v {
				if bad, found := unsupportedLiteral(arg); found {
					return bad, true
				}
			}
			return nil, false
		default:
			return v, true
		}
	case expr.SExpr:
		for _, child := range ex.Children() {
			if bad, found := unsupportedLiteral(child); found {
				return bad, true
			}
		}
	}
	return nil, false
}

// ImportResolver resolves import paths to SourceIDs for synthetic sources.
// This is required when the builder's SourceID is synthetic (not file-backed)
// and imports use relative paths like "./common".
type ImportResolver func(path string) (location.SourceID, bool)

// Builder provides a fluent API for programmatically constructing schemas.
//
// Use NewBuilder() to create a Builder, then chain method calls to define
// the schema structure. Call Build() to produce the final Schema.
//
// # Validation Contract
//
// Build refuses what the DSL refuses in a name or a constraint, so a
// Builder-built schema's names and constraints remain expressible in the DSL.
// A declared name that the DSL's name productions
// refuse is E_INVALID_NAME: type and datatype names are an uppercase letter
// followed by letters, digits or underscores, and none is one of the eleven
// built-in type names; property names start with a lowercase letter and are
// none of as, part, in, nil, true and false; relation names are UPPER_SNAKE
// and are not UUID. A constraint no .yammm source can state is
// E_INVALID_CONSTRAINT: inverted bounds, a non-finite Float bound, a negative
// length, an enum value empty or repeated or fewer than two values, a Pattern
// with no pattern, more than two or one regexp.Compile refuses, a Vector
// dimension outside 1 to 65536, a List with no element, and a Go type this
// package does not construct. So is a datatype declared as a bare reference to
// another datatype. Schema names and
// invariant names are quoted strings in the DSL and stay free-form. Import
// aliases are validated during completion.
//
// # Import Requirements
//
// AddImport() requires a non-zero source ID, set with WithSourceID() before or
// after it. Without one, Build returns E_MISSING_SOURCE_ID.
type Builder struct {
	name           string
	sourceID       location.SourceID
	sourceIDSet    bool
	imports        []*importDecl
	types          []*typeBuilderState
	dataTypes      []*dataTypeDecl
	documentation  string
	registry       *Registry
	issueLimit     int
	importResolver ImportResolver
}

// typeBuilderState holds the state for a type being built.
type typeBuilderState struct {
	name                       string
	inherits                   []*astTypeRef
	properties                 []*propertyDecl
	relations                  []*relationDecl
	invariants                 []*invariantDecl
	annotations                []*annotationDecl // type-level @@name annotations
	pendingPropertyAnnotations []pendingPropertyAnnotation
	isPart                     bool
	isAbstract                 bool
	documentation              string
}

// pendingPropertyAnnotation holds a property-level annotation added via
// WithPropertyAnnotation until convertTypes attaches it to the matching
// property. The property may be added before or after the annotation, so the
// binding is resolved by name at Build time; an unmatched name is reported by
// validateInput.
type pendingPropertyAnnotation struct {
	propertyName string
	decl         *annotationDecl
}

// NewBuilder creates a new schema builder.
//
// Pre-allocates empty slices for imports, types, and dataTypes to support the
// Add* builder pattern. Using make(..., 0) rather than nil avoids nil-check
// guards in builder methods. Memory overhead is minimal (~72 bytes per builder
// for 3 slice headers).
func NewBuilder() *Builder {
	return &Builder{
		imports:    make([]*importDecl, 0),
		types:      make([]*typeBuilderState, 0),
		dataTypes:  make([]*dataTypeDecl, 0),
		issueLimit: 100, // default limit
	}
}

// WithName sets the schema name.
func (b *Builder) WithName(name string) *Builder {
	b.name = name
	return b
}

// WithSourceID sets the source ID for this schema.
//
// This is required if AddImport is used. The source ID provides the
// namespace for TypeIDs and enables import resolution.
func (b *Builder) WithSourceID(id location.SourceID) *Builder {
	b.sourceID = id
	b.sourceIDSet = true
	return b
}

// WithDocumentation sets the schema-level documentation.
func (b *Builder) WithDocumentation(doc string) *Builder {
	b.documentation = doc
	return b
}

// WithRegistry provides a schema registry for cross-schema type resolution.
//
// If imports reference other schemas, those schemas must be in the registry.
func (b *Builder) WithRegistry(r *Registry) *Builder {
	b.registry = r
	return b
}

// WithIssueLimit sets the maximum number of diagnostics to collect.
//
// Default is 100. Use 0 for unlimited (not recommended for large schemas).
func (b *Builder) WithIssueLimit(limit int) *Builder {
	b.issueLimit = limit
	return b
}

// WithImportResolver sets a custom resolver for import paths.
//
// This is only needed when:
//  1. The builder's SourceID is synthetic (not file-backed), AND
//  2. Imports use relative paths (./foo, ../bar)
//
// For file-backed SourceIDs, relative paths are resolved automatically
// against the schema's directory. For synthetic SourceIDs without a
// resolver, import paths are treated as schema names and looked up via
// registry.LookupByName().
//
// Example:
//
//	resolver := func(path string) (location.SourceID, bool) {
//	    switch path {
//	    case "./common":
//	        return commonSchema.SourceID(), true
//	    default:
//	        return location.SourceID{}, false
//	    }
//	}
//	s, result := NewBuilder().
//	    WithSourceID(location.MustNewSourceID("test://main.yammm")).
//	    WithImportResolver(resolver).
//	    AddImport("./common", "common").
//	    // ...
//	    Build()
func (b *Builder) WithImportResolver(resolver ImportResolver) *Builder {
	b.importResolver = resolver
	return b
}

// AddImport adds an import declaration.
//
// It requires a non-zero source ID, set with WithSourceID() before or after
// it; without one, Build() returns E_MISSING_SOURCE_ID.
func (b *Builder) AddImport(path, alias string) *Builder {
	b.imports = append(b.imports, &importDecl{
		Path:  path,
		Alias: alias,
		Span:  location.Span{}, // Synthetic - no source location
	})
	return b
}

// AddType begins building a new type definition.
//
// Returns a TypeBuilder that allows fluent definition of the type's
// properties, relations, and other attributes. Call Done() on the
// TypeBuilder to return to this Builder.
func (b *Builder) AddType(name string) *TypeBuilder {
	state := &typeBuilderState{
		name:       name,
		inherits:   make([]*astTypeRef, 0),
		properties: make([]*propertyDecl, 0),
		relations:  make([]*relationDecl, 0),
		invariants: make([]*invariantDecl, 0),
	}
	b.types = append(b.types, state)
	return &TypeBuilder{
		parent: b,
		state:  state,
	}
}

// AddDataType adds a named data type alias.
func (b *Builder) AddDataType(name string, constraint Constraint) *Builder {
	b.dataTypes = append(b.dataTypes, &dataTypeDecl{
		Name:       name,
		Constraint: constraint,
		Span:       location.Span{}, // Synthetic
	})
	return b
}

// Build constructs the final Schema from the builder state.
//
// If validation fails (missing SourceID for imports, semantic errors),
// returns (nil, Result) where Result.HasErrors() is true.
//
// Callers should check Result.HasErrors() to determine success.
// Builder never returns Go errors; all issues are diagnostics.
func (b *Builder) Build() (*Schema, diag.Result) {
	collector := diag.NewCollector(b.issueLimit)

	// Validate schema name is non-empty (required for stable SourceID)
	if b.name == "" {
		collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
			"schema name is required; call WithName() before Build()").
			WithDetail(diag.DetailKeyContext, "Builder").Build())
		return nil, collector.Result()
	}

	// Validate SourceID requirement for imports - must be set AND non-zero
	if len(b.imports) > 0 && (!b.sourceIDSet || b.sourceID.IsZero()) {
		collector.Collect(diag.NewIssue(diag.Error, diag.E_MISSING_SOURCE_ID,
			"AddImport requires WithSourceID to be called with a non-zero SourceID").
			WithDetail(diag.DetailKeyContext, "Builder").Build())
		return nil, collector.Result()
	}

	// Validate basic input invariants before completion
	if !b.validateInput(collector) {
		return nil, collector.Result()
	}

	// If WithSourceID() not called, SourceID defaults to zero (identity-less).
	// Zero SourceID is permitted for single-schema usage without imports.
	sourceID := b.sourceID

	// If WithSourceID() was called with a synthetic SourceID (not file-backed),
	// validate it via ValidateSyntheticSourceID. Emit E_INVALID_SYNTHETIC_ID if it
	// resembles an absolute file path.
	// Detection: !IsFilePath() means not file-backed; !IsZero() means it was set
	if b.sourceIDSet && !sourceID.IsZero() && !sourceID.IsFilePath() {
		if err := location.ValidateSyntheticSourceID(sourceID.String()); err != nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_SYNTHETIC_ID,
				fmt.Sprintf("invalid synthetic source ID: %s", err)).
				WithDetail(diag.DetailKeyID, sourceID.String()).Build())
			return nil, collector.Result()
		}
	}

	// Convert builder state to model
	m := &model{
		Name:          b.name,
		Imports:       b.imports,
		Types:         unresolveTypes(b.convertTypes()),
		DataTypes:     unresolveDataTypes(b.dataTypes),
		Documentation: b.documentation,
		Span:          location.Span{}, // Synthetic
	}

	// Create a registry adapter if one was provided
	var registry completionRegistry
	if b.registry != nil {
		registry = &registryAdapter{r: b.registry}
	}

	// Resolve imports via registry (returns nil if no imports or resolution fails)
	resolvedImports := b.resolveImports(collector)
	if len(b.imports) > 0 && resolvedImports == nil {
		// Import resolution failed - diagnostics already collected
		return nil, collector.Result()
	}

	// Complete the schema with resolved imports
	s := completeModel(m, sourceID, collector, registry, resolvedImports)
	if s == nil {
		return nil, collector.Result()
	}

	// Ensure schema is nil when errors exist
	// This catches cases where completion succeeded but errors were collected
	// (e.g., property/relation conflicts that don't abort completion)
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Wire import schema pointers and seal imports
	b.wireImports(s)

	// Seal the schema to prevent further mutation
	s.seal()

	return s, collector.Result()
}

// convertTypes converts the internal type state to typeDecl.
func (b *Builder) convertTypes() []*typeDecl {
	result := make([]*typeDecl, len(b.types))
	for i, state := range b.types {
		result[i] = &typeDecl{
			Name:          state.name,
			Inherits:      state.inherits,
			Properties:    propertiesWithPendingAnnotations(state),
			Relations:     state.relations,
			Invariants:    state.invariants,
			Annotations:   state.annotations,
			IsPart:        state.isPart,
			IsAbstract:    state.isAbstract,
			Documentation: state.documentation,
			Span:          location.Span{}, // Synthetic
		}
	}
	return result
}

// propertiesWithPendingAnnotations returns state's property declarations with
// the annotations added via [TypeBuilder.WithPropertyAnnotation] attached to
// their targets. validateInput has already reported any that name no property,
// so an unmatched entry here is simply skipped, and — matching that check — an
// annotation binds to the FIRST property of its name.
//
// A property that gains an annotation is copied, along with its annotation
// slice, so nothing the Builder retains is mutated: Build is a pure function of
// builder state and may be called repeatedly. Appending to the retained
// declarations instead made every later Build see each annotation once more and
// fail the duplicate-annotation check on input the first Build accepted.
func propertiesWithPendingAnnotations(state *typeBuilderState) []*propertyDecl {
	if len(state.pendingPropertyAnnotations) == 0 {
		return state.properties
	}
	out := slices.Clone(state.properties)
	for _, pend := range state.pendingPropertyAnnotations {
		for i, pd := range out {
			if pd.Name != pend.propertyName {
				continue
			}
			// out starts as an element-wise clone of state.properties, so
			// out[i] still being the retained pointer is exactly "not copied
			// yet" — no separate bookkeeping, and no way for a second
			// annotation on the same property to re-copy and discard the first.
			if pd == state.properties[i] {
				dup := *pd
				dup.Annotations = slices.Clone(pd.Annotations)
				out[i] = &dup
			}
			out[i].Annotations = append(out[i].Annotations, pend.decl)
			break
		}
	}
	return out
}

// validateInput performs shallow validation of builder input before completion:
// nil-checks; grammar-conformance of declared names against the DSL's
// productions (type/datatype names UC_WORD less the datatype keywords, property
// names LC_WORD less the six excluded spellings, relation names UPPER_SNAKE less
// UUID); every constraint argument [constraintFault] refuses, and a datatype
// declared as another datatype alone; the Go type of every literal an invariant
// holds; and the property each WithPropertyAnnotation names. An invariant's
// message and expression are completion's to check.
// Returns true if validation passes, false otherwise (with diagnostics collected).
//
// Uses semantic diagnostic codes per:
//   - E_INVALID_NAME for empty/invalid identifiers
//   - E_INVALID_CONSTRAINT for nil constraints, for any argument no .yammm
//     source can state (see [constraintFault]), and for a datatype declared as
//     another datatype alone
//   - E_INVALID_INVARIANT for invalid invariant declarations
//   - E_UNKNOWN_ANNOTATION_TARGET for a property annotation naming no property
func (b *Builder) validateInput(collector *diag.Collector) bool {
	hasErrors := false

	for _, t := range b.types {
		switch {
		case t.name == "":
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				"type name cannot be empty").
				WithDetail(diag.DetailKeyName, "").
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		case !builderTypeNameRE.MatchString(t.name):
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				fmt.Sprintf("type name %q is not a valid DSL type name: type names start with an uppercase letter, followed by letters, digits, or underscores", t.name)).
				WithDetail(diag.DetailKeyName, t.name).
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		case parse.IsDatatypeKeyword(t.name):
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				fmt.Sprintf("type name %q is a built-in type name, which the DSL refuses as a declared name", t.name)).
				WithDetail(diag.DetailKeyName, t.name).
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		}

		for _, p := range t.properties {
			switch {
			case p.Name == "":
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("property name cannot be empty in type %q", t.name)).
					WithDetail(diag.DetailKeyName, "").
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			case !builderPropertyNameRE.MatchString(p.Name):
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("property name %q in type %q is not a valid DSL property name: property names start with a lowercase letter, followed by letters, digits, or underscores", p.Name, t.name)).
					WithDetail(diag.DetailKeyName, p.Name).
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			case parse.IsExcludedPropertyName(p.Name):
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("property name %q in type %q is a keyword or literal, which the DSL refuses as a property name", p.Name, t.name)).
					WithDetail(diag.DetailKeyName, p.Name).
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			}
			if p.Constraint == nil {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
					fmt.Sprintf("property %q in type %q has nil constraint", p.Name, t.name)).
					WithDetail(diag.DetailKeyTypeName, t.name).
					WithDetail(diag.DetailKeyPropertyName, p.Name).Build())
				hasErrors = true
			} else if fault, found := constraintFault(p.Constraint); found {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
					fmt.Sprintf("property %q in type %q: %s", p.Name, t.name, fault)).
					WithDetail(diag.DetailKeyTypeName, t.name).
					WithDetail(diag.DetailKeyPropertyName, p.Name).Build())
				hasErrors = true
			}
		}

		for _, r := range t.relations {
			switch {
			case r.Name == "":
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("relation name cannot be empty in type %q", t.name)).
					WithDetail(diag.DetailKeyName, "").
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			case !builderRelationNameRE.MatchString(r.Name):
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("relation name %q in type %q is not a valid DSL relation name: relation names are UPPER_SNAKE — an uppercase letter, then uppercase letters, digits or underscores", r.Name, t.name)).
					WithDetail(diag.DetailKeyName, r.Name).
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			case parse.IsDatatypeKeyword(r.Name):
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("relation name %q in type %q is a built-in type name, which the DSL refuses as a relation name", r.Name, t.name)).
					WithDetail(diag.DetailKeyName, r.Name).
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			}
		}

		// An empty message and an absent expression are the completer's to
		// refuse, by the one rule the parse front door shares.
		for _, inv := range t.invariants {
			if bad, found := unsupportedLiteral(inv.Expr); found {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_INVARIANT,
					fmt.Sprintf("invariant %q in type %q holds a literal of Go type %T; an expression literal is nil, string, int64, float64, bool or *regexp.Regexp", inv.Name, t.name, bad)).
					WithDetail(diag.DetailKeyTypeName, t.name).
					WithDetail(diag.DetailKeyName, inv.Name).Build())
				hasErrors = true
			}
		}

		// A property-level annotation added via WithPropertyAnnotation must name
		// a property of the type. This is the one Builder-only check; all other
		// annotation validation flows through the shared completion phase.
		for _, pend := range t.pendingPropertyAnnotations {
			found := false
			for _, p := range t.properties {
				if p.Name == pend.propertyName {
					found = true
					break
				}
			}
			if !found {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_ANNOTATION_TARGET,
					fmt.Sprintf("annotation @%s names unknown property %q of type %q", pend.decl.Name, pend.propertyName, t.name)).
					WithDetail(diag.DetailKeyTypeName, t.name).
					WithDetail(diag.DetailKeyName, pend.propertyName).Build())
				hasErrors = true
			}
		}
	}

	for _, dt := range b.dataTypes {
		switch {
		case dt.Name == "":
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				"datatype name cannot be empty").
				WithDetail(diag.DetailKeyName, "").
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		case !builderTypeNameRE.MatchString(dt.Name):
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				fmt.Sprintf("datatype name %q is not a valid DSL type name: type names start with an uppercase letter, followed by letters, digits, or underscores", dt.Name)).
				WithDetail(diag.DetailKeyName, dt.Name).
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		case parse.IsDatatypeKeyword(dt.Name):
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				fmt.Sprintf("datatype name %q is a built-in type name, which the DSL refuses as a declared name", dt.Name)).
				WithDetail(diag.DetailKeyName, dt.Name).
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		}
		// The DSL's datatype declaration names a built-in type or a List after
		// its "=", never another datatype alone.
		bare, isBare := dt.Constraint.(AliasConstraint)
		switch {
		case dt.Constraint == nil:
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("datatype %q has nil constraint", dt.Name)).
				WithDetail(diag.DetailKeyName, dt.Name).Build())
			hasErrors = true
		case isBare:
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("datatype %q is declared as datatype %q alone; a datatype declares a built-in type or a List", dt.Name, bare.DataTypeName())).
				WithDetail(diag.DetailKeyName, dt.Name).Build())
			hasErrors = true
		default:
			if fault, found := constraintFault(dt.Constraint); found {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
					fmt.Sprintf("datatype %q: %s", dt.Name, fault)).
					WithDetail(diag.DetailKeyName, dt.Name).Build())
				hasErrors = true
			}
		}
	}

	return !hasErrors
}

// CheckConstraint returns why c cannot judge a value, or nil when it can. It
// refuses what [Builder.Build] refuses in a constraint: a nil constraint, an
// argument no .yammm source can state, a List with no element, more than two
// patterns or one regexp.Compile refuses, and a constraint of a Go type this
// package does not construct, such as a pointer to one. A raw constraint meets
// no completion, so it also refuses a DataType reference that resolves to no
// constraint, at any List depth. The Neo4j adapter's Coerce and CoerceParams
// call it before they coerce a value.
func CheckConstraint(c Constraint) error {
	if c == nil {
		return errors.New("schema: no constraint")
	}
	if fault, found := constraintFault(c); found {
		return fmt.Errorf("schema: %s", fault)
	}
	switch c := c.(type) {
	case ListConstraint:
		return CheckConstraint(c.Element())
	case AliasConstraint:
		terminal := ResolveAlias(c)
		if open, isOpen := terminal.(AliasConstraint); isOpen {
			return fmt.Errorf("schema: datatype %q resolves to no constraint", open.DataTypeName())
		}
		return CheckConstraint(terminal)
	}
	return nil
}

// constraintFault describes the first fault of c, or of a List element at any
// depth, that no .yammm source can state: an argument the DSL refuses, as
// E_INVALID_CONSTRAINT or E_SYNTAX, or a Go type this package does not construct.
// The Builder refuses it too, so a built schema stays expressible in the DSL. A
// DataType reference is judged by its name, which completion resolves.
func constraintFault(c Constraint) (string, bool) {
	switch c := c.(type) {
	case BooleanConstraint, TimestampConstraint, DateConstraint, UUIDConstraint, AliasConstraint:
		// Nothing to judge: the DSL states every Boolean, Date, UUID and Timestamp
		// format, and a reference is judged by the name completion resolves.
	case IntegerConstraint:
		lo, hasLo := c.Min()
		hi, hasHi := c.Max()
		if hasLo && hasHi && lo > hi {
			return fmt.Sprintf("integer bounds inverted: min %d > max %d", lo, hi), true
		}
	case FloatConstraint:
		lo, hasLo := c.Min()
		hi, hasHi := c.Max()
		for _, b := range [...]struct {
			v   float64
			has bool
		}{{lo, hasLo}, {hi, hasHi}} {
			if b.has && (math.IsInf(b.v, 0) || math.IsNaN(b.v)) {
				return fmt.Sprintf("non-finite float bound %v; a Float bound is a finite number", b.v), true
			}
		}
		if hasLo && hasHi && lo > hi {
			return fmt.Sprintf("float bounds inverted: min %v > max %v", lo, hi), true
		}
	case StringConstraint:
		return lengthFault("string", c.MinLen, c.MaxLen)
	case ListConstraint:
		if fault, found := lengthFault("list", c.MinLen, c.MaxLen); found {
			return fault, true
		}
		if c.Element() == nil {
			return "list has no element constraint", true
		}
		return constraintFault(c.Element())
	case EnumConstraint:
		values := c.Values()
		seen := make(map[string]bool, len(values))
		for _, v := range values {
			switch {
			case v == "":
				return "enum value cannot be empty", true
			case seen[v]:
				return fmt.Sprintf("duplicate enum value %q", v), true
			}
			seen[v] = true
		}
		if len(values) < 2 {
			return fmt.Sprintf("enum must have at least two values (got %d)", len(values)), true
		}
	case PatternConstraint:
		switch n := c.PatternCount(); {
		case n == 0:
			return "pattern constraint needs at least one pattern", true
		case n > parse.MaxPatterns:
			return fmt.Sprintf("pattern constraint exceeds maximum of %d patterns (got %d)", parse.MaxPatterns, n), true
		case c.invalid != "":
			return c.invalid, true
		}
	case VectorConstraint:
		if d := c.Dimension(); d < parse.MinVectorDimensions || d > parse.MaxVectorDimensions {
			return fmt.Sprintf("vector dimensions must be between %d and %d (got %d)",
				parse.MinVectorDimensions, parse.MaxVectorDimensions, d), true
		}
	default:
		return fmt.Sprintf("a constraint of Go type %T is none this package constructs", c), true
	}
	return "", false
}

// lengthFault reports a negative or inverted length pair; kind names the
// constraint as the DSL's diagnostics do.
func lengthFault(kind string, minLen, maxLen func() (int64, bool)) (string, bool) {
	lo, hasLo := minLen()
	hi, hasHi := maxLen()
	switch {
	case hasLo && lo < 0:
		return fmt.Sprintf("%s minimum length cannot be negative: %d", kind, lo), true
	case hasHi && hi < 0:
		return fmt.Sprintf("%s maximum length cannot be negative: %d", kind, hi), true
	case hasLo && hasHi && lo > hi:
		return fmt.Sprintf("%s length bounds inverted: min %d > max %d", kind, lo, hi), true
	}
	return "", false
}

// resolveImportPath resolves an import path to a SourceID.
//
// Resolution strategy:
//  1. If builder's SourceID is file-backed and path is relative (./foo, ../bar):
//     Resolve relative to schema's directory and look up by SourceID.
//  2. If builder's SourceID is synthetic and importResolver is set:
//     Use the resolver.
//  3. Otherwise (synthetic without resolver, or non-relative path):
//     Treat path as schema name and look up by name (backward compatible).
//
// A relative path holding a backslash resolves to nothing, since / is its only
// separator. Case 3 is unaffected: a schema name may hold any character.
func (b *Builder) resolveImportPath(importPath string) (location.SourceID, bool) {
	isRelative := strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../")
	// Only a relative import is read as a path, where / is the only separator.
	// Case 3 reads the string as a schema name, in which a backslash is an
	// ordinary character, so the refusal must not reach it.
	if isRelative && strings.ContainsRune(importPath, '\\') {
		return location.SourceID{}, false
	}

	// Case 1: File-backed SourceID with relative import
	if b.sourceID.IsFilePath() && isRelative {
		cp, ok := b.sourceID.CanonicalPath()
		if !ok {
			// Should not happen for IsFilePath() == true, but handle gracefully
			return location.SourceID{}, false
		}

		// Get schema's directory
		schemaDir := cp.Dir()

		// The extension is part of the name the import names, so it is added
		// before Join resolves the path: a directory beside the file may share
		// the import's name.
		fileName := importPath
		if !strings.HasSuffix(fileName, ".yammm") {
			fileName += ".yammm"
		}
		resolved, err := schemaDir.Join(fileName)
		if err != nil {
			return location.SourceID{}, false
		}

		resolvedID, err := location.SourceIDFromPath(resolved.String())
		if err != nil {
			return location.SourceID{}, false
		}

		// Look up by SourceID
		_, ok = b.registry.LookupBySourceID(resolvedID)
		if !ok {
			return location.SourceID{}, false
		}
		return resolvedID, true
	}

	// Case 2: Synthetic SourceID with resolver
	if !b.sourceID.IsFilePath() && b.importResolver != nil && isRelative {
		resolvedID, ok := b.importResolver(importPath)
		if !ok {
			return location.SourceID{}, false
		}
		// Look up by SourceID to verify it exists
		_, ok = b.registry.LookupBySourceID(resolvedID)
		if !ok {
			return location.SourceID{}, false
		}
		return resolvedID, true
	}

	// Case 2.5: Synthetic SourceID with relative path but no resolver - fail early
	// Relative paths require either file-backed SourceID or an import resolver.
	// Don't fall through to name lookup since "./common" is not a valid schema name.
	if !b.sourceID.IsFilePath() && isRelative && b.importResolver == nil {
		return location.SourceID{}, false
	}

	// Case 3: Fallback - treat path as schema name (non-relative paths only)
	s, ok := b.registry.LookupByName(importPath)
	if !ok {
		return location.SourceID{}, false
	}
	return s.SourceID(), true
}

// resolveImports resolves builder imports to SourceIDs via the registry.
// Returns nil if no imports, or if resolution fails (with diagnostics collected).
func (b *Builder) resolveImports(collector *diag.Collector) resolvedImportMap {
	if len(b.imports) == 0 {
		return nil
	}

	if b.registry == nil {
		// Imports declared but no registry provided
		// The Builder resolves through a registry, never through a root, so
		// its origin is "none" — the same shape the loader's sites carry.
		collector.Collect(importResolveIssue("", diag.ModuleRootNone,
			"imports declared but no registry provided; call WithRegistry() to enable import resolution", nil))
		return nil
	}

	resolved := make(resolvedImportMap, len(b.imports))
	// Track seen SourceIDs for duplicate detection
	seenSourceIDs := make(map[location.SourceID]*importDecl)
	hasErrors := false
	for _, imp := range b.imports {
		// Keep-first: once an alias has resolved, a later declaration of the
		// same alias is skipped (not re-resolved, not dedup-tracked), so the
		// kept Import is never rebound to a later declaration's schema. A
		// declaration whose path fails to resolve is not recorded here, so a
		// later same-alias declaration is still attempted — harmless, because
		// any resolution failure sets hasErrors and Build returns before
		// completeModel, discarding this map. The completer's duplicate-alias
		// check is the report either way.
		if _, bound := resolved[imp.Alias]; bound {
			continue
		}
		resolvedID, ok := b.resolveImportPath(imp.Path)
		if !ok {
			// Provide helpful error message based on path type
			isRelative := strings.HasPrefix(imp.Path, "./") || strings.HasPrefix(imp.Path, "../")
			var msg string
			switch {
			case isRelative && strings.ContainsRune(imp.Path, '\\'):
				msg = fmt.Sprintf("cannot resolve import %q: %s", imp.Path, errImportBackslash)
			case isRelative && !b.sourceID.IsFilePath() && b.importResolver == nil:
				msg = fmt.Sprintf("cannot resolve relative import %q: synthetic SourceID requires WithImportResolver() or use schema name instead", imp.Path)
			default:
				msg = fmt.Sprintf("cannot resolve import %q: schema not found in registry", imp.Path)
			}
			collector.Collect(importResolveIssue("", diag.ModuleRootNone, msg, imp))
			hasErrors = true
			continue
		}

		// Check for duplicate resolved SourceID
		if existing, found := seenSourceIDs[resolvedID]; found {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_IMPORT,
				fmt.Sprintf("schema %q imported multiple times", resolvedID.String())).
				WithSpan(imp.Span).
				WithDetail(diag.DetailKeyImportPath, resolvedID.String()).
				WithDetail(diag.DetailKeyFirstAlias, existing.Alias).
				WithDetail(diag.DetailKeyFirstLine, strconv.Itoa(existing.Span.Start.Line)).
				WithDetail(diag.DetailKeyDuplicateAlias, imp.Alias).
				WithDetail(diag.DetailKeyDuplicateLine, strconv.Itoa(imp.Span.Start.Line)).
				WithRelated(location.RelatedInfo{
					Span:    existing.Span,
					Message: fmt.Sprintf("first imported here as %q", existing.Alias),
				}).Build())
			hasErrors = true
			continue
		}
		seenSourceIDs[resolvedID] = imp
		resolved[imp.Alias] = importResolution{sourceID: resolvedID}
	}

	if hasErrors {
		return nil
	}
	return resolved
}

// wireImports wires schema pointers and seals imports after completion.
func (b *Builder) wireImports(s *Schema) {
	if len(b.imports) == 0 || b.registry == nil {
		return
	}

	for _, imp := range s.ImportsSlice() {
		// Look up by SourceID (which was set during completion)
		if !imp.ResolvedSourceID().IsZero() {
			resolved, ok := b.registry.LookupBySourceID(imp.ResolvedSourceID())
			if ok {
				imp.setSchema(resolved)
			}
		}
		imp.seal()
	}
}

// TypeBuilder provides a fluent API for building a type definition.
type TypeBuilder struct {
	parent *Builder
	state  *typeBuilderState
}

// WithProperty adds a property to the type.
func (t *TypeBuilder) WithProperty(name string, c Constraint) *TypeBuilder {
	t.state.properties = append(t.state.properties, &propertyDecl{
		Name:       name,
		Constraint: c,
		Optional:   false,
		Span:       location.Span{}, // Synthetic
	})
	return t
}

// WithOptionalProperty adds an optional property to the type.
func (t *TypeBuilder) WithOptionalProperty(name string, c Constraint) *TypeBuilder {
	t.state.properties = append(t.state.properties, &propertyDecl{
		Name:       name,
		Constraint: c,
		Optional:   true,
		Span:       location.Span{}, // Synthetic
	})
	return t
}

// WithPrimaryKey adds a primary key property to the type.
func (t *TypeBuilder) WithPrimaryKey(name string, c Constraint) *TypeBuilder {
	t.state.properties = append(t.state.properties, &propertyDecl{
		Name:         name,
		Constraint:   c,
		Optional:     false,
		IsPrimaryKey: true,
		Span:         location.Span{}, // Synthetic
	})
	return t
}

// WithRelation adds a relation to the type.
//
// By default, creates a required one-to-one association.
// Use optional and many parameters to modify:
//   - optional=false, many=false -> required one (default)
//   - optional=true, many=false -> optional one
//   - optional=false, many=true -> required many
//   - optional=true, many=true -> optional many
func (t *TypeBuilder) WithRelation(name string, target TypeRef, optional, many bool) *TypeBuilder {
	t.state.relations = append(t.state.relations, &relationDecl{
		Kind:     RelationAssociation,
		Name:     name,
		Target:   &astTypeRef{Qualifier: target.Qualifier(), Name: target.Name(), Span: target.Span()},
		Optional: optional,
		Many:     many,
		Span:     location.Span{}, // Synthetic
	})
	return t
}

// WithComposition adds a composition relation to the type.
//
// Compositions model parent-child ownership where the child's
// lifecycle is tied to the parent.
func (t *TypeBuilder) WithComposition(name string, target TypeRef, optional, many bool) *TypeBuilder {
	t.state.relations = append(t.state.relations, &relationDecl{
		Kind:     RelationComposition,
		Name:     name,
		Target:   &astTypeRef{Qualifier: target.Qualifier(), Name: target.Name(), Span: target.Span()},
		Optional: optional,
		Many:     many,
		Span:     location.Span{}, // Synthetic
	})
	return t
}

// Extends adds a type to inherit from.
func (t *TypeBuilder) Extends(ref TypeRef) *TypeBuilder {
	t.state.inherits = append(t.state.inherits, &astTypeRef{
		Qualifier: ref.Qualifier(),
		Name:      ref.Name(),
		Span:      ref.Span(),
	})
	return t
}

// AsPart marks this type as a part type.
func (t *TypeBuilder) AsPart() *TypeBuilder {
	t.state.isPart = true
	return t
}

// AsAbstract marks this type as abstract.
func (t *TypeBuilder) AsAbstract() *TypeBuilder {
	t.state.isAbstract = true
	return t
}

// WithTypeDocumentation sets documentation for this type.
func (t *TypeBuilder) WithTypeDocumentation(doc string) *TypeBuilder {
	t.state.documentation = doc
	return t
}

// WithInvariant adds an invariant constraint to the type.
//
// The name parameter is the user-facing message displayed when the invariant
// fails validation. The e parameter is the compiled expression to evaluate.
// The doc parameter is optional documentation for the invariant.
//
// Expressions are constructed with the expr package. Compiling one from source
// text is internal to schema loading and is not reachable from here:
//
//	ageExpr := expr.SExpr{expr.Op(">"), expr.SExpr{expr.Op("$"), expr.NewLiteral("age")}, expr.NewLiteral(int64(0))}
func (t *TypeBuilder) WithInvariant(name string, e expr.Expression, doc string) *TypeBuilder {
	t.state.invariants = append(t.state.invariants, &invariantDecl{
		Name:          name,
		Expr:          e,
		Documentation: doc,
		Span:          location.Span{}, // Synthetic - no source location
	})
	return t
}

// WithTypeAnnotation adds a type-level @@name(args) annotation. Arguments are
// carried as identifier tokens; a literal-argument surface can wait for an
// annotation that accepts one. Structure and eligibility are validated by the
// shared completion phase, so a bad annotation reports the same diagnostic as
// the parse front door.
//
// The parse front door additionally lets a type-level annotation carry a leading
// doc comment ([Annotation.Documentation]); a Builder-constructed one is always
// undocumented. Adding it here means an optional-doc entry point rather than a
// changed signature, and no consumer has needed it yet.
func (t *TypeBuilder) WithTypeAnnotation(name string, args ...string) *TypeBuilder {
	t.state.annotations = append(t.state.annotations, builderAnnotationDecl(name, args))
	return t
}

// WithPropertyAnnotation adds a @name(args) annotation to the named property of
// this type. The property need not exist yet; the binding is resolved at Build
// time. A name matching no property draws E_UNKNOWN_ANNOTATION_TARGET.
func (t *TypeBuilder) WithPropertyAnnotation(propertyName, name string, args ...string) *TypeBuilder {
	t.state.pendingPropertyAnnotations = append(t.state.pendingPropertyAnnotations,
		pendingPropertyAnnotation{propertyName: propertyName, decl: builderAnnotationDecl(name, args)})
	return t
}

// builderAnnotationDecl builds an annotationDecl from Builder inputs, tagging
// every argument as an identifier token (the only kind the Builder surface
// currently exposes).
func builderAnnotationDecl(name string, args []string) *annotationDecl {
	var argDecls []annotationArgDecl
	if len(args) > 0 {
		argDecls = make([]annotationArgDecl, len(args))
		for i, a := range args {
			argDecls[i] = annotationArgDecl{Text: a, Token: tokenIdentifier}
		}
	}
	return &annotationDecl{Name: name, Args: argDecls}
}

// Done completes the type definition and returns to the parent Builder.
func (t *TypeBuilder) Done() *Builder {
	return t.parent
}

// unresolve drops a caller-supplied alias resolution, at any List depth, so
// completion resolves every alias by its name as it does for a loaded schema.
func unresolve(c Constraint) Constraint {
	switch c := c.(type) {
	case AliasConstraint:
		return NewAliasConstraint(c.DataTypeName(), nil)
	case ListConstraint:
		elem := unresolve(c.Element())
		lo, hasLo := c.MinLen()
		hi, hasHi := c.MaxLen()
		switch {
		case hasLo && hasHi:
			return ListLenBetween(elem, lo, hi)
		case hasLo:
			return ListMinLen(elem, lo)
		case hasHi:
			return ListMaxLen(elem, hi)
		}
		return NewListConstraint(elem)
	}
	return c
}

func unresolveProps(ps []*propertyDecl) []*propertyDecl {
	out := make([]*propertyDecl, len(ps))
	for i, p := range ps {
		cp := *p
		cp.Constraint = unresolve(p.Constraint)
		out[i] = &cp
	}
	return out
}

// unresolveTypes covers the properties alone: the Builder gives a relation no
// properties, so a relationDecl it converts holds none to unresolve.
func unresolveTypes(ts []*typeDecl) []*typeDecl {
	for _, t := range ts {
		t.Properties = unresolveProps(t.Properties)
	}
	return ts
}

func unresolveDataTypes(ds []*dataTypeDecl) []*dataTypeDecl {
	out := make([]*dataTypeDecl, len(ds))
	for i, d := range ds {
		cd := *d
		cd.Constraint = unresolve(d.Constraint)
		out[i] = &cd
	}
	return out
}

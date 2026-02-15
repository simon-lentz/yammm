package build

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
	"github.com/simon-lentz/yammm/schema/internal/complete"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

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
// The Builder trusts that callers provide semantically valid constraints.
// Unlike the parser which validates constraint parameters (e.g., enum has
// >= 2 values, bounds are ordered correctly), the Builder accepts constraints
// as-is. Callers are responsible for constructing valid constraints using
// the schema.New*Constraint constructors.
//
// # Import Requirements
//
// AddImport() requires WithSourceID() to have been called first.
// If AddImport is called without a source ID, Build will return E_MISSING_SOURCE_ID.
type Builder struct {
	name           string
	sourceID       location.SourceID
	sourceIDSet    bool
	imports        []*parse.ImportDecl
	types          []*typeBuilderState
	dataTypes      []*parse.DataTypeDecl
	documentation  string
	registry       *schema.Registry
	issueLimit     int
	importResolver ImportResolver
}

// typeBuilderState holds the state for a type being built.
type typeBuilderState struct {
	name          string
	inherits      []*parse.TypeRef
	properties    []*parse.PropertyDecl
	relations     []*parse.RelationDecl
	invariants    []*parse.InvariantDecl
	isPart        bool
	isAbstract    bool
	documentation string
}

// NewBuilder creates a new schema builder.
//
// Pre-allocates empty slices for imports, types, and dataTypes to support the
// Add* builder pattern. Using make(..., 0) rather than nil avoids nil-check
// guards in builder methods. Memory overhead is minimal (~72 bytes per builder
// for 3 slice headers).
func NewBuilder() *Builder {
	return &Builder{
		imports:    make([]*parse.ImportDecl, 0),
		types:      make([]*typeBuilderState, 0),
		dataTypes:  make([]*parse.DataTypeDecl, 0),
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
func (b *Builder) WithRegistry(r *schema.Registry) *Builder {
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
//	s, result := build.NewBuilder().
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
// Requires WithSourceID() to have been called first.
// If not, Build() will return an E_MISSING_SOURCE_ID error.
func (b *Builder) AddImport(path, alias string) *Builder {
	b.imports = append(b.imports, &parse.ImportDecl{
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
		inherits:   make([]*parse.TypeRef, 0),
		properties: make([]*parse.PropertyDecl, 0),
		relations:  make([]*parse.RelationDecl, 0),
		invariants: make([]*parse.InvariantDecl, 0),
	}
	b.types = append(b.types, state)
	return &TypeBuilder{
		parent: b,
		state:  state,
	}
}

// AddDataType adds a named data type alias.
func (b *Builder) AddDataType(name string, constraint schema.Constraint) *Builder {
	b.dataTypes = append(b.dataTypes, &parse.DataTypeDecl{
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
func (b *Builder) Build() (*schema.Schema, diag.Result) {
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
				WithDetail(diag.DetailKeyId, sourceID.String()).Build())
			return nil, collector.Result()
		}
	}

	// Convert builder state to parse.Model
	model := &parse.Model{
		Name:          b.name,
		Imports:       b.imports,
		Types:         b.convertTypes(),
		DataTypes:     b.dataTypes,
		Documentation: b.documentation,
		Span:          location.Span{}, // Synthetic
	}

	// Create a registry adapter if one was provided
	var registry complete.Registry
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
	s := complete.Complete(model, sourceID, collector, registry, resolvedImports)
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
	s.Seal()

	return s, collector.Result()
}

// convertTypes converts the internal type state to parse.TypeDecl.
func (b *Builder) convertTypes() []*parse.TypeDecl {
	result := make([]*parse.TypeDecl, len(b.types))
	for i, state := range b.types {
		result[i] = &parse.TypeDecl{
			Name:          state.name,
			Inherits:      state.inherits,
			Properties:    state.properties,
			Relations:     state.relations,
			Invariants:    state.invariants,
			IsPart:        state.isPart,
			IsAbstract:    state.isAbstract,
			Documentation: state.documentation,
			Span:          location.Span{}, // Synthetic
		}
	}
	return result
}

// registryAdapter adapts *schema.Registry to the complete.Registry interface.
type registryAdapter struct {
	r *schema.Registry
}

// LookupBySourceID implements the complete.Registry interface.
func (a *registryAdapter) LookupBySourceID(id location.SourceID) (*schema.Schema, bool) {
	s, status := a.r.LookupBySourceID(id)
	return s, status.Found()
}

// validateInput performs shallow validation of builder input before completion.
// Returns true if validation passes, false otherwise (with diagnostics collected).
//
// Uses semantic diagnostic codes per:
//   - E_INVALID_NAME for empty/invalid identifiers
//   - E_INVALID_CONSTRAINT for nil constraints
//   - E_INVALID_INVARIANT for invalid invariant declarations
func (b *Builder) validateInput(collector *diag.Collector) bool {
	hasErrors := false

	for _, t := range b.types {
		if t.name == "" {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				"type name cannot be empty").
				WithDetail(diag.DetailKeyName, "").
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		}

		for _, p := range t.properties {
			if p.Name == "" {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("property name cannot be empty in type %q", t.name)).
					WithDetail(diag.DetailKeyName, "").
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			}
			if p.Constraint == nil {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
					fmt.Sprintf("property %q in type %q has nil constraint", p.Name, t.name)).
					WithDetail(diag.DetailKeyTypeName, t.name).
					WithDetail(diag.DetailKeyPropertyName, p.Name).Build())
				hasErrors = true
			}
		}

		for _, r := range t.relations {
			if r.Name == "" {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("relation name cannot be empty in type %q", t.name)).
					WithDetail(diag.DetailKeyName, "").
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			}
		}

		for _, inv := range t.invariants {
			if inv.Name == "" {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
					fmt.Sprintf("invariant name cannot be empty in type %q", t.name)).
					WithDetail(diag.DetailKeyName, "").
					WithDetail(diag.DetailKeyTypeName, t.name).Build())
				hasErrors = true
			}
			if inv.Expr == nil {
				collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_INVARIANT,
					fmt.Sprintf("invariant %q in type %q has nil expression", inv.Name, t.name)).
					WithDetail(diag.DetailKeyTypeName, t.name).
					WithDetail(diag.DetailKeyName, inv.Name).Build())
				hasErrors = true
			}
		}
	}

	for _, dt := range b.dataTypes {
		if dt.Name == "" {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_NAME,
				"datatype name cannot be empty").
				WithDetail(diag.DetailKeyName, "").
				WithDetail(diag.DetailKeyContext, "Builder").Build())
			hasErrors = true
		}
		if dt.Constraint == nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_CONSTRAINT,
				fmt.Sprintf("datatype %q has nil constraint", dt.Name)).
				WithDetail(diag.DetailKeyName, dt.Name).Build())
			hasErrors = true
		}
	}

	return !hasErrors
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
func (b *Builder) resolveImportPath(importPath string) (location.SourceID, bool) {
	isRelative := strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../")

	// Case 1: File-backed SourceID with relative import
	if b.sourceID.IsFilePath() && isRelative {
		cp, ok := b.sourceID.CanonicalPath()
		if !ok {
			// Should not happen for IsFilePath() == true, but handle gracefully
			return location.SourceID{}, false
		}

		// Get schema's directory
		schemaDir := cp.Dir()

		// Resolve the relative path
		resolved, err := schemaDir.Join(importPath)
		if err != nil {
			return location.SourceID{}, false
		}

		// Auto-append .yammm if missing
		resolvedPath := resolved.String()
		if !strings.HasSuffix(resolvedPath, ".yammm") {
			resolvedPath += ".yammm"
		}

		// Construct SourceID from resolved path
		resolvedID, err := location.SourceIDFromAbsolutePath(resolvedPath)
		if err != nil {
			return location.SourceID{}, false
		}

		// Look up by SourceID
		_, status := b.registry.LookupBySourceID(resolvedID)
		if !status.Found() {
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
		_, status := b.registry.LookupBySourceID(resolvedID)
		if !status.Found() {
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
	s, status := b.registry.LookupByName(importPath)
	if !status.Found() {
		return location.SourceID{}, false
	}
	return s.SourceID(), true
}

// resolveImports resolves builder imports to SourceIDs via the registry.
// Returns nil if no imports, or if resolution fails (with diagnostics collected).
func (b *Builder) resolveImports(collector *diag.Collector) complete.ResolvedImports {
	if len(b.imports) == 0 {
		return nil
	}

	if b.registry == nil {
		// Imports declared but no registry provided
		collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
			"imports declared but no registry provided; call WithRegistry() to enable import resolution").Build())
		return nil
	}

	resolved := make(complete.ResolvedImports, len(b.imports))
	// Track seen SourceIDs for duplicate detection
	seenSourceIDs := make(map[location.SourceID]*parse.ImportDecl)
	hasErrors := false
	for _, imp := range b.imports {
		resolvedID, ok := b.resolveImportPath(imp.Path)
		if !ok {
			// Provide helpful error message based on path type
			isRelative := strings.HasPrefix(imp.Path, "./") || strings.HasPrefix(imp.Path, "../")
			var msg string
			if isRelative && !b.sourceID.IsFilePath() && b.importResolver == nil {
				msg = fmt.Sprintf("cannot resolve relative import %q: synthetic SourceID requires WithImportResolver() or use schema name instead", imp.Path)
			} else {
				msg = fmt.Sprintf("cannot resolve import %q: schema not found in registry", imp.Path)
			}
			collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE, msg).
				WithSpan(imp.Span).
				WithDetail(diag.DetailKeyImportPath, imp.Path).
				WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
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
		resolved[imp.Alias] = resolvedID
	}

	if hasErrors {
		return nil
	}
	return resolved
}

// wireImports wires schema pointers and seals imports after completion.
func (b *Builder) wireImports(s *schema.Schema) {
	if len(b.imports) == 0 || b.registry == nil {
		return
	}

	for _, imp := range s.ImportsSlice() {
		// Look up by SourceID (which was set during completion)
		if !imp.ResolvedSourceID().IsZero() {
			resolved, status := b.registry.LookupBySourceID(imp.ResolvedSourceID())
			if status.Found() {
				imp.SetSchema(resolved)
			}
		}
		imp.Seal()
	}
}

// TypeBuilder provides a fluent API for building a type definition.
type TypeBuilder struct {
	parent *Builder
	state  *typeBuilderState
}

// WithProperty adds a property to the type.
func (t *TypeBuilder) WithProperty(name string, c schema.Constraint) *TypeBuilder {
	t.state.properties = append(t.state.properties, &parse.PropertyDecl{
		Name:       name,
		Constraint: c,
		Optional:   false,
		Span:       location.Span{}, // Synthetic
	})
	return t
}

// WithOptionalProperty adds an optional property to the type.
func (t *TypeBuilder) WithOptionalProperty(name string, c schema.Constraint) *TypeBuilder {
	t.state.properties = append(t.state.properties, &parse.PropertyDecl{
		Name:       name,
		Constraint: c,
		Optional:   true,
		Span:       location.Span{}, // Synthetic
	})
	return t
}

// WithPrimaryKey adds a primary key property to the type.
func (t *TypeBuilder) WithPrimaryKey(name string, c schema.Constraint) *TypeBuilder {
	t.state.properties = append(t.state.properties, &parse.PropertyDecl{
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
//   - optional=false, many=false → required one (default)
//   - optional=true, many=false → optional one
//   - optional=false, many=true → required many
//   - optional=true, many=true → optional many
func (t *TypeBuilder) WithRelation(name string, target schema.TypeRef, optional, many bool) *TypeBuilder {
	t.state.relations = append(t.state.relations, &parse.RelationDecl{
		Kind:     parse.RelationAssociation,
		Name:     name,
		Target:   &parse.TypeRef{Qualifier: target.Qualifier(), Name: target.Name(), Span: target.Span()},
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
func (t *TypeBuilder) WithComposition(name string, target schema.TypeRef, optional, many bool) *TypeBuilder {
	t.state.relations = append(t.state.relations, &parse.RelationDecl{
		Kind:     parse.RelationComposition,
		Name:     name,
		Target:   &parse.TypeRef{Qualifier: target.Qualifier(), Name: target.Name(), Span: target.Span()},
		Optional: optional,
		Many:     many,
		Span:     location.Span{}, // Synthetic
	})
	return t
}

// Extends adds a type to inherit from.
func (t *TypeBuilder) Extends(ref schema.TypeRef) *TypeBuilder {
	t.state.inherits = append(t.state.inherits, &parse.TypeRef{
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
// Expressions can be constructed programmatically using the expr package:
//
//	// Using CompileString (for testing):
//	collector := diag.NewCollector(10)
//	ageExpr := expr.CompileString("age > 0", collector, sourceID)
//
//	// Or construct directly:
//	ageExpr := expr.SExpr{expr.Op(">"), expr.SExpr{expr.Op("$"), expr.NewLiteral("age")}, expr.NewLiteral(int64(0))}
func (t *TypeBuilder) WithInvariant(name string, e expr.Expression, doc string) *TypeBuilder {
	t.state.invariants = append(t.state.invariants, &parse.InvariantDecl{
		Name:          name,
		Expr:          e,
		Documentation: doc,
		Span:          location.Span{}, // Synthetic - no source location
	})
	return t
}

// Done completes the type definition and returns to the parent Builder.
func (t *TypeBuilder) Done() *Builder {
	return t.parent
}

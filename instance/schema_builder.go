package instance

import (
	"errors"
	"fmt"
	"maps"

	"github.com/simon-lentz/yammm/schema"
)

// SchemaBuilder is a schema-aware raw-instance builder.
//
// Unlike a free-form map literal, SchemaBuilder is bound to a specific schema
// type at construction time and validates property names, relation names, and
// cardinality invariants as they are added — shifting the failure domain from
// "at ValidateOne time" to "at Build time." Shape errors (unknown property,
// unknown relation, cardinality mismatch) accumulate on the builder and surface from Build()
// with a call-site file:line locator captured via runtime.Callers.
//
// SchemaBuilder is constructed via [BuilderFor].
//
// # Scope of validation
//
// Build catches shape errors (schema-structural): property/relation names
// and cardinality.
// It does NOT catch value-level errors (wrong type, out-of-range, invalid PK);
// those remain in [Validator.ValidateOne]'s domain. A SchemaBuilder that
// returns a clean (RawInstance, nil) from Build is guaranteed to pass the
// shape portion of ValidateOne — property and relation names, and
// association cardinality. Compositions are not covered: the builder cannot
// produce one, so a type with a required composition builds clean here and
// fails ValidateOne with E_UNRESOLVED_REQUIRED_COMPOSITION.
//
// # Thread safety
//
// SchemaBuilder is NOT concurrent-safe; its internal state mutates as
// methods are called. Construct one builder per goroutine. A single
// [*schema.Schema] is immutable and may be shared freely across multiple
// builders on multiple goroutines.
//
// # One-shot on error
//
// After Build returns a non-nil error, the builder's internal state reflects
// partial accumulation. Subsequent method calls have undefined semantics
// (accumulated errors remain; new mutations may or may not be recorded
// cleanly). Do NOT reuse a builder after a failed Build; construct a fresh
// one via BuilderFor.
//
// # Performance
//
// Each builder method (Property, EdgeTo) captures the
// caller's program counter via runtime.Callers so Build-time shape errors can
// name the offending call site. Only the stack walk is eager; resolving the PC
// to file:line happens when an error is rendered, so a successful call
// allocates nothing for a locator it will never print. Measured per-method
// overhead is ~200–400 ns per call on M2-class hardware at zero allocations;
// at the typical I/O-bound pipeline scale (low-thousands records per batch,
// 3–4 builder-method calls per record) the aggregate per-batch overhead is
// under ~5 ms — genuinely noise against pipeline wall-clock. At a 100k+
// records-per-batch ceiling aggregate overhead reaches ~100–200 ms, still well
// below validation cost. See [BenchmarkSchemaBuilder_CallerCapture] for the
// in-tree pin and TestSchemaBuilder_SuccessPath_IsAllocationFree for the
// zero-allocation ratchet.
type SchemaBuilder struct {
	schema     *schema.Schema
	typeName   string // user-provided (may be qualified)
	typ        *schema.Type
	properties map[string]any
	edges      map[string]*edgeState
	errors     []*buildError
}

type edgeState struct {
	rel     *schema.Relation
	targets []map[string]any
}

// BuilderFor constructs a schema-aware builder for instances of the given
// type. Returns an error if s is nil, typeName is not in the schema, or the
// named type is abstract (abstract types cannot be directly instantiated).
// Part types are permitted so callers can construct composition children.
//
// typeName may be unqualified ("Person") or qualified with an import alias
// ("cross.Person"), matching the name-resolution rules [Validator] applies.
func BuilderFor(s *schema.Schema, typeName string) (*SchemaBuilder, error) {
	if s == nil {
		return nil, errors.New("instance.BuilderFor: nil schema")
	}
	typ, found := s.ResolveTypeName(typeName)
	if !found {
		return nil, fmt.Errorf("instance.BuilderFor: type %q not found", typeName)
	}
	if typ.IsAbstract() {
		return nil, fmt.Errorf("instance.BuilderFor: cannot build abstract type %q", typeName)
	}
	return &SchemaBuilder{
		schema:     s,
		typeName:   typeName,
		typ:        typ,
		properties: make(map[string]any),
		edges:      make(map[string]*edgeState),
	}, nil
}

// Property sets a property value on the instance under construction.
//
// If name is not a property of the bound type, Property records the error
// with the caller's file:line (captured via runtime.Callers); Build() returns
// the accumulated error including the locator. Property name matching is
// case-sensitive against the schema's canonical names — the programmatic API
// opts out of the case-insensitive fallback the validator performs on
// JSON inputs.
//
// Property(name, nil) passes through; the validator handles nil-value
// semantics (emitting E_MISSING_REQUIRED when the property is required).
func (b *SchemaBuilder) Property(name string, value any) *SchemaBuilder {
	callerPC := capturePC()
	prop, ok := b.typ.Property(name)
	if !ok {
		b.recordErr(&buildError{
			kind:     kindUnknownProperty,
			typ:      b.typeName,
			target:   name,
			callerPC: callerPC,
		})
		return b
	}
	b.properties[prop.Name()] = value
	return b
}

// EdgeTo adds one edge target to the named association relation for
// associations that do NOT declare edge properties. Variadic targetKey
// supports single-component and composite primary keys:
//
//	b.EdgeTo("in_region", regionCode)              // single-component PK
//	b.EdgeTo("part_of", publisherID, bookID)     // composite PK
//	b.EdgeTo("part_of", prebuiltKey...)        // pre-built slice
//
// name accepts either the schema's DSL form (e.g. "IN_REGION") or the
// lower-case FieldName form (e.g. "in_region"); both resolve to the same
// relation.
//
// For "many"-cardinality relations, call EdgeTo multiple times — once per
// target. Cardinality is enforced at Build time: a "one" relation with more
// than one EdgeTo call surfaces a cardinality error.
//
// The builder supports property-less association edges only: a relation that
// declares edge properties, and a composition, each record a shape error —
// construct such instances from raw data instead.
func (b *SchemaBuilder) EdgeTo(name string, targetKey ...any) *SchemaBuilder {
	callerPC := capturePC()
	b.addEdge(name, targetKey, callerPC)
	return b
}

// addEdge is EdgeTo's implementation.
func (b *SchemaBuilder) addEdge(name string, targetKey []any, callerPC uintptr) {
	rel, ok := b.resolveRelation(name)
	if !ok {
		b.recordErr(&buildError{
			kind:     kindUnknownRelation,
			typ:      b.typeName,
			target:   name,
			callerPC: callerPC,
		})
		return
	}
	if !rel.IsAssociation() {
		b.recordErr(&buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   rel.Name() + " is a composition; the builder does not support composed children — construct the instance from raw data",
			callerPC: callerPC,
		})
		return
	}
	if rel.HasProperties() {
		b.recordErr(&buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   "declares edge properties, which the builder does not support; construct the instance from raw data",
			callerPC: callerPC,
		})
		return
	}
	if len(targetKey) == 0 {
		b.recordErr(&buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   "EdgeTo requires at least one target-key component",
			callerPC: callerPC,
		})
		return
	}

	target, err := b.buildEdgeTarget(rel, targetKey, callerPC)
	if err != nil {
		// err is already shaped as *buildError; recordErr just appends.
		b.errors = append(b.errors, err)
		return
	}

	st := b.edgeStateFor(rel)
	st.targets = append(st.targets, target)
	// A (one) relation's second target is the call the caller has to delete,
	// so the error is recorded here, at that call, and Build reports errors
	// in call order.
	if !rel.IsMany() && len(st.targets) == 2 {
		b.recordErr(&buildError{
			kind:     kindCardinality,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   fmt.Sprintf("%q is single-valued, got %d targets", rel.Name(), len(st.targets)),
			callerPC: callerPC,
		})
	}
}

// buildEdgeTarget assembles one edge-target map, {_target_<pk>: value, ...},
// for a relation addEdge has already established declares no edge
// properties. Returns a shaped buildError on a missing target type, a keyless
// target, or a target-key arity mismatch; otherwise nil.
func (b *SchemaBuilder) buildEdgeTarget(
	rel *schema.Relation,
	targetKey []any,
	callerPC uintptr,
) (map[string]any, *buildError) {
	targetType, found := resolveRelationTarget(b.schema, rel)
	if !found {
		return nil, &buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   fmt.Sprintf("target type %q not found", rel.Target().String()),
			callerPC: callerPC,
		}
	}
	pks := targetType.PrimaryKeysSlice()
	if len(pks) == 0 {
		return nil, &buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   fmt.Sprintf("target type %q has no primary key", targetType.Name()),
			callerPC: callerPC,
		}
	}
	if len(targetKey) != len(pks) {
		return nil, &buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   fmt.Sprintf("target-key arity mismatch: expected %d component(s), got %d", len(pks), len(targetKey)),
			callerPC: callerPC,
		}
	}

	obj := make(map[string]any, len(pks))
	for i, pk := range pks {
		obj[fkPrefix+pk.Name()] = targetKey[i]
	}

	return obj, nil
}

// edgeStateFor returns the state for rel, creating it on first call.
func (b *SchemaBuilder) edgeStateFor(rel *schema.Relation) *edgeState {
	st, ok := b.edges[rel.Name()]
	if !ok {
		st = &edgeState{rel: rel}
		b.edges[rel.Name()] = st
	}
	return st
}

// Build produces the RawInstance.
//
// If any earlier call on this builder recorded an error, Build returns the
// first such error. When more than one error accumulated, the return wraps
// the first via %w with a trailing "(and N more build error(s))" suffix so
// callers retain access to the primary cause via errors.Is / errors.As.
//
// On success the returned RawInstance is guaranteed to pass the shape
// portion of [Validator.ValidateOne]: property names and relation names are
// schema-valid and association cardinality holds. Compositions are not
// covered — the builder cannot produce one — so a type with a required
// composition builds clean and fails ValidateOne with
// E_UNRESOLVED_REQUIRED_COMPOSITION. Value-level validation (constraint
// checks, PK type coercion, foreign-key shape and key-component checks) still
// happens at ValidateOne time; whether an association's target exists is
// [graph.Graph.Check]'s question, never the validator's.
//
// Error messages include:
//   - the offending call's target (property or relation name)
//   - the bound type's name (for disambiguation across builders)
//   - the caller's file:line (captured via runtime.Callers at the offending
//     Property or EdgeTo call)
func (b *SchemaBuilder) Build() (RawInstance, error) {
	if err := b.firstErrorWithCount(); err != nil {
		return RawInstance{}, err
	}

	out := make(map[string]any, len(b.properties)+len(b.edges))
	maps.Copy(out, b.properties)

	// Edges — iteration order on maps is nondeterministic; outputs go into
	// the RawInstance map under rel.FieldName() so no ordering-dependent
	// contract is exposed here, and every error was recorded at its call.
	for _, st := range b.edges {
		if st.rel.IsMany() {
			arr := make([]any, len(st.targets))
			for i, t := range st.targets {
				arr[i] = t
			}
			out[st.rel.FieldName()] = arr
		} else if len(st.targets) == 1 {
			out[st.rel.FieldName()] = st.targets[0]
		}
	}

	return RawInstance{Properties: out}, nil
}

// recordErr appends one shape-level error to the builder's accumulated list.
func (b *SchemaBuilder) recordErr(e *buildError) {
	b.errors = append(b.errors, e)
}

// firstErrorWithCount collapses the accumulated error list into a single
// returned error. When the list is empty, returns nil. When one error is
// present, returns it verbatim. When more than one, wraps the first via %w
// with a trailing "(and N more build error(s))" suffix so errors.Is /
// errors.As continue to reach the primary cause.
func (b *SchemaBuilder) firstErrorWithCount() error {
	if len(b.errors) == 0 {
		return nil
	}
	first := b.errors[0]
	if len(b.errors) == 1 {
		return first
	}
	return fmt.Errorf("%w (and %d more build error(s))", first, len(b.errors)-1)
}

// resolveRelation looks up a relation by name, accepting either the DSL form
// (e.g. "IN_DISTRICT") or the FieldName form (e.g. "in_district"), the two
// spellings every name-taking surface of this package accepts.
func (b *SchemaBuilder) resolveRelation(name string) (*schema.Relation, bool) {
	return relationByEitherName(b.typ, name)
}

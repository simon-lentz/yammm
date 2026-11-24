package instance

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// SchemaBuilder is a schema-aware raw-instance builder.
//
// Unlike a free-form map literal, SchemaBuilder is bound to a specific schema
// type at construction time and validates property names, relation names, and
// cardinality invariants as they are added — shifting the failure domain from
// "at ValidateOne time" to "at Build time." Shape errors (unknown property,
// unknown relation, cardinality mismatch, an edge the builder cannot write)
// accumulate on the builder and surface from Build()
// with a call-site file:line locator captured via runtime.Callers.
//
// SchemaBuilder is constructed via [BuilderFor].
//
// # Scope of validation
//
// Build catches shape errors (schema-structural): property and relation
// names, an excess target on a (one) relation, and the shape of each edge: a
// composition, which the builder does not support, an edge property the
// association does not declare, an empty target key, a target type that does
// not exist or has no primary key, and a key whose arity is not the target's.
// It does NOT catch value-level errors (wrong type, out-of-range, invalid PK, an
// absent required property or edge property, an invariant, a target key a
// (many) relation names twice, which two spellings of one key can do); those remain in
// [Validator.ValidateOne]'s domain. A SchemaBuilder that
// returns a clean (RawInstance, nil) from Build is guaranteed to pass the
// shape portion of ValidateOne — property, relation and edge-property names,
// and no excess target on a (one) relation. Compositions are not covered: the builder cannot
// produce one, so a type with a required composition builds clean here and
// fails ValidateOne with E_UNRESOLVED_REQUIRED_COMPOSITION.
//
// # Thread safety
//
// SchemaBuilder is NOT concurrent-safe; its internal state mutates as
// methods are called. Construct one builder per goroutine. A single
// [schema.Schema] is immutable and may be shared freely across multiple
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
// Property, EdgeTo and EdgeWith capture the caller's program counter with
// runtime.Callers so a Build-time shape error can name the call site; the PC
// resolves to file:line only when an error is rendered, so the capture
// allocates nothing. [BenchmarkSchemaBuilder_CallerCapture] times the per-call
// cost. Build copies each edge object, one map copy per edge target, so a
// change to an edge object of the instance it returns does not reach the
// builder; the copy is shallow, as Build's copy of the properties is.
type SchemaBuilder struct {
	schema     schema.Schema
	typeName   string // user-provided (may be qualified)
	typ        schema.Type
	properties map[string]any
	edges      map[string]*edgeState
	errors     []*buildError
}

type edgeState struct {
	rel     schema.Relation
	targets []map[string]any
}

// BuilderFor constructs a schema-aware builder for instances of the given
// type. Returns an error if s is zero, typeName is not in the schema, or the
// named type is abstract (abstract types cannot be directly instantiated).
// Part types are permitted so callers can construct composition children.
//
// typeName may be unqualified ("Person") or qualified with an import alias
// ("cross.Person"), matching the name-resolution rules [Validator] applies.
func BuilderFor(s schema.Schema, typeName string) (*SchemaBuilder, error) {
	if s.IsZero() {
		return nil, errors.New("instance.BuilderFor: zero Schema")
	}
	typ, found := s.ResolveTypeName(typeName)
	if !found {
		return nil, fmt.Errorf("instance.BuilderFor: type %s not found", diag.QuoteValue(typeName))
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
// the accumulated error including the locator. name matches a property by its
// declared spelling alone, as the validator matches an input key.
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

// EdgeTo adds one edge target, with no edge properties, to the named
// association. It is [SchemaBuilder.EdgeWith] with nil properties. Variadic
// targetKey supports single-component and composite primary keys:
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
// target. Cardinality is enforced at Build time: a "one" relation given more
// than one target with a valid key, by EdgeTo or EdgeWith, surfaces a
// cardinality error.
//
// A composition records a shape error: the builder does not construct
// composed children, so construct such instances from raw data instead.
func (b *SchemaBuilder) EdgeTo(name string, targetKey ...any) *SchemaBuilder {
	callerPC := capturePC()
	b.addEdge(name, nil, targetKey, callerPC)
	return b
}

// EdgeWith adds one edge target to the named association, with the edge's
// properties. Each key of props must name an edge property the association
// declares, by its declared spelling, as [SchemaBuilder.Property] matches a
// property; values pass through to [Validator.ValidateOne], which judges them
// and reports a required edge property left out. The builder copies props.
func (b *SchemaBuilder) EdgeWith(name string, props map[string]any, targetKey ...any) *SchemaBuilder {
	callerPC := capturePC()
	b.addEdge(name, props, targetKey, callerPC)
	return b
}

// addEdge is EdgeTo's and EdgeWith's implementation.
func (b *SchemaBuilder) addEdge(name string, props map[string]any, targetKey []any, callerPC uintptr) {
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
	// An undeclared edge property fails Build but does not refuse the target:
	// the key is still judged, and a valid key counts toward a (one) relation.
	b.checkEdgeProps(rel, props, callerPC)
	if len(targetKey) == 0 {
		b.recordErr(&buildError{
			kind:     kindEdgeShape,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   "an edge requires at least one target-key component",
			callerPC: callerPC,
		})
		return
	}

	target, err := b.buildEdgeTarget(rel, props, targetKey, callerPC)
	if err != nil {
		// err is already shaped as *buildError; recordErr just appends.
		b.errors = append(b.errors, err)
		return
	}
	maps.Copy(target, props)

	st := b.edgeStateFor(rel)
	st.targets = append(st.targets, target)
	// Every excess target on a (one) relation is the call the caller has to
	// delete, so each is an error recorded at its own call; Build reports the
	// first and counts the rest truly.
	if !rel.IsMany() && len(st.targets) > 1 {
		b.recordErr(&buildError{
			kind:     kindCardinality,
			typ:      b.typeName,
			target:   rel.Name(),
			detail:   fmt.Sprintf("%q is single-valued; this call adds target %d", rel.Name(), len(st.targets)),
			callerPC: callerPC,
		})
	}
}

// checkEdgeProps records one shape error for each key of props that names no
// edge property of rel, in key order so Build's first error does not move with
// map order.
func (b *SchemaBuilder) checkEdgeProps(rel schema.Relation, props map[string]any, callerPC uintptr) {
	keys := slices.AppendSeq(make([]string, 0, len(props)), maps.Keys(props))
	slices.Sort(keys)
	for _, key := range keys {
		if _, ok := rel.Property(key); !ok {
			b.recordErr(&buildError{
				kind:     kindEdgeShape,
				typ:      b.typeName,
				target:   rel.Name(),
				detail:   "unknown edge property " + diag.QuoteValue(key),
				callerPC: callerPC,
			})
		}
	}
}

// buildEdgeTarget assembles one edge-target map, {_target_<pk>: value, ...},
// sized for the edge properties addEdge copies in after it. Returns a shaped
// buildError on a missing target type, a keyless target, or a target-key arity
// mismatch; otherwise nil.
func (b *SchemaBuilder) buildEdgeTarget(
	rel schema.Relation,
	props map[string]any,
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
	pks := slices.Collect(targetType.PrimaryKeys())
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

	obj := make(map[string]any, len(pks)+len(props))
	for i, pk := range pks {
		obj[fkPrefix+pk.Name()] = targetKey[i]
	}

	return obj, nil
}

// edgeStateFor returns the state for rel, creating it on first call.
func (b *SchemaBuilder) edgeStateFor(rel schema.Relation) *edgeState {
	st, ok := b.edges[rel.Name()]
	if !ok {
		st = &edgeState{rel: rel}
		b.edges[rel.Name()] = st
	}
	return st
}

// Build produces the RawInstance. Its property map and each edge object are
// copies, so a caller may add, remove or replace their entries and build again;
// a value is shared, not copied.
//
// If any earlier call on this builder recorded an error, Build returns the
// first such error. When more than one error accumulated, the return wraps
// the first via %w with a trailing "(and N more build error(s))" suffix, so
// [errors.Unwrap] returns the first error.
//
// On success the returned RawInstance is guaranteed to pass the shape
// portion of [Validator.ValidateOne]: property, relation and edge-property
// names are schema-valid and no (one) relation holds an excess target. Compositions are not
// covered — the builder cannot produce one — so a type with a required
// composition builds clean and fails ValidateOne with
// E_UNRESOLVED_REQUIRED_COMPOSITION. Every check outside that shape portion
// still runs at ValidateOne time, value-level validation and a target key a
// (many) relation names twice among them. No
// validator asks whether an association's target exists: [graph.Graph.Check]
// answers for a required association, and [graph.Snapshot.Unresolved] holds an
// optional one.
//
// Error messages include:
//   - the offending call's target (property or relation name)
//   - the bound type's name (for disambiguation across builders)
//   - the caller's file:line (captured via runtime.Callers at the offending
//     Property, EdgeTo or EdgeWith call)
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
				arr[i] = maps.Clone(t)
			}
			out[st.rel.FieldName()] = arr
		} else if len(st.targets) == 1 {
			out[st.rel.FieldName()] = maps.Clone(st.targets[0])
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
func (b *SchemaBuilder) resolveRelation(name string) (schema.Relation, bool) {
	return relationByEitherName(b.typ, name)
}

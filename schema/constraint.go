package schema

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/internal/parse"
)

// ConstraintKind identifies the kind of constraint. The zero ConstraintKind
// names no kind.
type ConstraintKind uint8

const (
	KindString ConstraintKind = iota + 1
	KindInteger
	KindFloat
	KindBoolean
	KindTimestamp
	KindDate
	KindUUID
	KindEnum
	KindPattern
	KindVector
	KindList  // ordered collection with element constraint
	KindAlias // reference to DataType
)

// String returns the name of the constraint kind.
func (k ConstraintKind) String() string {
	//exhaustive:enforce
	switch k {
	case KindString:
		return "String"
	case KindInteger:
		return "Integer"
	case KindFloat:
		return "Float"
	case KindBoolean:
		return "Boolean"
	case KindTimestamp:
		return "Timestamp"
	case KindDate:
		return "Date"
	case KindUUID:
		return "UUID"
	case KindEnum:
		return "Enum"
	case KindPattern:
		return "Pattern"
	case KindVector:
		return "Vector"
	case KindList:
		return "List"
	case KindAlias:
		return "Alias"
	default:
		return fmt.Sprintf("ConstraintKind(%d)", k)
	}
}

// Constraint represents a type constraint for a property value.
// All constraints are immutable after construction.
type Constraint interface {
	// Kind returns the constraint kind.
	Kind() ConstraintKind

	// String returns a human-readable representation.
	String() string

	// equal reports whether two constraints are structurally equal.
	// For AliasConstraint, compares the resolved underlying constraint.
	equal(other Constraint) bool

	// narrowsTo reports whether the child constraint is a valid narrowing of
	// this (parent) constraint. It returns true only when every value accepted
	// by child would also be accepted by this constraint (child's valid set is
	// a subset of parent's valid set), and may return false for such a pair
	// the rules below do not read, as a child of another kind. That reading holds for constraints
	// [CheckConstraint] accepts; a value is never checked against one it
	// refuses, so such a constraint has no valid set to compare.
	//
	// For bounds-based constraints (String, Integer, Float): child min >= parent min
	// and child max <= parent max.
	// For EnumConstraint: child values must be a subset of parent values.
	// For ListConstraint: the child's element must narrow the parent's, and its
	// length bounds must narrow as above. A List with no element narrows to
	// nothing, and nothing narrows to it.
	// For parameterless or non-narrowable constraints (Boolean, Date, UUID,
	// Timestamp, Pattern, Vector): delegates to equal (no narrowing supported).
	// For AliasConstraint: resolves alias chain first, then delegates.
	narrowsTo(child Constraint) bool

	// isResolved reports whether the constraint references are fully resolved.
	// An AliasConstraint is resolved when its resolution ends in a resolved
	// constraint of another kind, and a ListConstraint when it holds an element
	// that is.
	// Every other kind holds no reference and returns true.
	isResolved() bool

	// constraint is an unexported marker method to prevent external implementations.
	constraint()
}

// StringConstraint constrains string values with optional min/max length.
type StringConstraint struct {
	minLen int64
	maxLen int64
	hasMin bool
	hasMax bool
}

// newStringConstraint creates a StringConstraint with no bounds.
func newStringConstraint() StringConstraint {
	return StringConstraint{}
}

// stringMinLen creates a StringConstraint with a minimum length bound.
func stringMinLen(lo int64) StringConstraint {
	return StringConstraint{minLen: lo, hasMin: true}
}

// stringMaxLen creates a StringConstraint with a maximum length bound.
func stringMaxLen(hi int64) StringConstraint {
	return StringConstraint{maxLen: hi, hasMax: true}
}

// stringLenBetween creates a StringConstraint with both min and max length bounds.
func stringLenBetween(lo, hi int64) StringConstraint {
	return StringConstraint{minLen: lo, maxLen: hi, hasMin: true, hasMax: true}
}

func (StringConstraint) Kind() ConstraintKind { return KindString }
func (StringConstraint) constraint()          {}

func (c StringConstraint) MinLen() (int64, bool) { return c.minLen, c.hasMin }
func (c StringConstraint) MaxLen() (int64, bool) { return c.maxLen, c.hasMax }

func (c StringConstraint) String() string {
	if !c.hasMin && !c.hasMax {
		return "String"
	}
	minStr := "_"
	maxStr := "_"
	if c.hasMin {
		minStr = strconv.FormatInt(c.minLen, 10)
	}
	if c.hasMax {
		maxStr = strconv.FormatInt(c.maxLen, 10)
	}
	return "String[" + minStr + ", " + maxStr + "]"
}

func (c StringConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(StringConstraint)
	return ok && c == o
}

func (c StringConstraint) narrowsTo(child Constraint) bool {
	o, ok := ResolveAlias(child).(StringConstraint)
	if !ok {
		return false
	}
	// Child min must be >= parent min (if parent has a min).
	if c.hasMin && (!o.hasMin || o.minLen < c.minLen) {
		return false
	}
	// Child max must be <= parent max (if parent has a max).
	if c.hasMax && (!o.hasMax || o.maxLen > c.maxLen) {
		return false
	}
	return true
}

func (StringConstraint) isResolved() bool { return true }

// IntegerConstraint constrains integer values with optional min/max bounds.
type IntegerConstraint struct {
	min    int64
	max    int64
	hasMin bool
	hasMax bool
}

// newIntegerConstraint creates an IntegerConstraint with no bounds.
func newIntegerConstraint() IntegerConstraint {
	return IntegerConstraint{}
}

// integerMin creates an IntegerConstraint with a minimum bound.
func integerMin(lo int64) IntegerConstraint {
	return IntegerConstraint{min: lo, hasMin: true}
}

// integerMax creates an IntegerConstraint with a maximum bound.
func integerMax(hi int64) IntegerConstraint {
	return IntegerConstraint{max: hi, hasMax: true}
}

// integerBetween creates an IntegerConstraint with both min and max bounds.
func integerBetween(lo, hi int64) IntegerConstraint {
	return IntegerConstraint{min: lo, max: hi, hasMin: true, hasMax: true}
}

func (IntegerConstraint) Kind() ConstraintKind { return KindInteger }
func (IntegerConstraint) constraint()          {}

func (c IntegerConstraint) Min() (int64, bool) { return c.min, c.hasMin }
func (c IntegerConstraint) Max() (int64, bool) { return c.max, c.hasMax }

func (c IntegerConstraint) String() string {
	if !c.hasMin && !c.hasMax {
		return "Integer"
	}
	minStr := "_"
	maxStr := "_"
	if c.hasMin {
		minStr = strconv.FormatInt(c.min, 10)
	}
	if c.hasMax {
		maxStr = strconv.FormatInt(c.max, 10)
	}
	return "Integer[" + minStr + ", " + maxStr + "]"
}

func (c IntegerConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(IntegerConstraint)
	return ok && c == o
}

func (c IntegerConstraint) narrowsTo(child Constraint) bool {
	o, ok := ResolveAlias(child).(IntegerConstraint)
	if !ok {
		return false
	}
	// Child min must be >= parent min (if parent has a min).
	if c.hasMin && (!o.hasMin || o.min < c.min) {
		return false
	}
	// Child max must be <= parent max (if parent has a max).
	if c.hasMax && (!o.hasMax || o.max > c.max) {
		return false
	}
	return true
}

func (IntegerConstraint) isResolved() bool { return true }

// FloatConstraint constrains float values with optional min/max bounds.
type FloatConstraint struct {
	min    float64
	max    float64
	hasMin bool
	hasMax bool
}

// newFloatConstraint creates a FloatConstraint with no bounds.
func newFloatConstraint() FloatConstraint {
	return FloatConstraint{}
}

// floatMin creates a FloatConstraint with a minimum bound.
func floatMin(lo float64) FloatConstraint {
	return FloatConstraint{min: lo, hasMin: true}
}

// floatMax creates a FloatConstraint with a maximum bound.
func floatMax(hi float64) FloatConstraint {
	return FloatConstraint{max: hi, hasMax: true}
}

// floatBetween creates a FloatConstraint with both min and max bounds.
func floatBetween(lo, hi float64) FloatConstraint {
	return FloatConstraint{min: lo, max: hi, hasMin: true, hasMax: true}
}

func (FloatConstraint) Kind() ConstraintKind { return KindFloat }
func (FloatConstraint) constraint()          {}

func (c FloatConstraint) Min() (float64, bool) { return c.min, c.hasMin }
func (c FloatConstraint) Max() (float64, bool) { return c.max, c.hasMax }

func (c FloatConstraint) String() string {
	if !c.hasMin && !c.hasMax {
		return "Float"
	}
	minStr := "_"
	maxStr := "_"
	if c.hasMin {
		minStr = strconv.FormatFloat(c.min, 'f', -1, 64)
	}
	if c.hasMax {
		maxStr = strconv.FormatFloat(c.max, 'f', -1, 64)
	}
	return "Float[" + minStr + ", " + maxStr + "]"
}

func (c FloatConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(FloatConstraint)
	return ok && c == o
}

func (c FloatConstraint) narrowsTo(child Constraint) bool {
	o, ok := ResolveAlias(child).(FloatConstraint)
	if !ok {
		return false
	}
	// Child min must be >= parent min (if parent has a min).
	if c.hasMin && (!o.hasMin || o.min < c.min) {
		return false
	}
	// Child max must be <= parent max (if parent has a max).
	if c.hasMax && (!o.hasMax || o.max > c.max) {
		return false
	}
	return true
}

func (FloatConstraint) isResolved() bool { return true }

// BooleanConstraint constrains boolean values. It has no parameters.
type BooleanConstraint struct{}

// newBooleanConstraint creates a BooleanConstraint.
func newBooleanConstraint() BooleanConstraint {
	return BooleanConstraint{}
}

func (BooleanConstraint) Kind() ConstraintKind { return KindBoolean }
func (BooleanConstraint) constraint()          {}
func (BooleanConstraint) String() string       { return "Boolean" }

func (c BooleanConstraint) equal(other Constraint) bool {
	_, ok := ResolveAlias(other).(BooleanConstraint)
	return ok
}

func (c BooleanConstraint) narrowsTo(child Constraint) bool { return c.equal(child) }

func (BooleanConstraint) isResolved() bool { return true }

// TimestampConstraint constrains timestamp values. It has no parameters.
type TimestampConstraint struct{}

// newTimestampConstraint creates a TimestampConstraint.
func newTimestampConstraint() TimestampConstraint {
	return TimestampConstraint{}
}

func (TimestampConstraint) Kind() ConstraintKind { return KindTimestamp }
func (TimestampConstraint) constraint()          {}

func (TimestampConstraint) String() string { return "Timestamp" }

func (TimestampConstraint) equal(other Constraint) bool {
	_, ok := ResolveAlias(other).(TimestampConstraint)
	return ok
}

func (c TimestampConstraint) narrowsTo(child Constraint) bool { return c.equal(child) }

func (TimestampConstraint) isResolved() bool { return true }

// DateConstraint constrains date values. It has no parameters.
type DateConstraint struct{}

// newDateConstraint creates a DateConstraint.
func newDateConstraint() DateConstraint {
	return DateConstraint{}
}

func (DateConstraint) Kind() ConstraintKind { return KindDate }
func (DateConstraint) constraint()          {}
func (DateConstraint) String() string       { return "Date" }

func (c DateConstraint) equal(other Constraint) bool {
	_, ok := ResolveAlias(other).(DateConstraint)
	return ok
}

func (c DateConstraint) narrowsTo(child Constraint) bool { return c.equal(child) }

func (DateConstraint) isResolved() bool { return true }

// UUIDConstraint constrains UUID values. It has no parameters.
type UUIDConstraint struct{}

// newUUIDConstraint creates a UUIDConstraint.
func newUUIDConstraint() UUIDConstraint {
	return UUIDConstraint{}
}

func (UUIDConstraint) Kind() ConstraintKind { return KindUUID }
func (UUIDConstraint) constraint()          {}
func (UUIDConstraint) String() string       { return "UUID" }

func (c UUIDConstraint) equal(other Constraint) bool {
	_, ok := ResolveAlias(other).(UUIDConstraint)
	return ok
}

func (c UUIDConstraint) narrowsTo(child Constraint) bool { return c.equal(child) }

func (UUIDConstraint) isResolved() bool { return true }

// EnumConstraint constrains string values to a fixed set of allowed values.
type EnumConstraint struct {
	values []string
}

// parsedEnumConstraint creates an EnumConstraint that keeps values, which
// internal/parse unquoted and kept as non-empty, distinct and valid UTF-8, so
// it records no fault. It takes the slice as its own.
func parsedEnumConstraint(values []string) EnumConstraint {
	return EnumConstraint{values: values}
}

func (EnumConstraint) Kind() ConstraintKind { return KindEnum }
func (EnumConstraint) constraint()          {}

// Values returns a defensive copy of the allowed enum values.
func (c EnumConstraint) Values() []string {
	return slices.Clone(c.values)
}

func (c EnumConstraint) String() string {
	quoted := make([]string, len(c.values))
	for i, v := range c.values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return fmt.Sprintf("Enum[%s]", strings.Join(quoted, ", "))
}

// equal compares the values as sets, so their order does not matter.
func (c EnumConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(EnumConstraint)
	if !ok || len(c.values) != len(o.values) {
		return false
	}
	// Set equality: same elements regardless of order
	set := make(map[string]struct{}, len(c.values))
	for _, v := range c.values {
		set[v] = struct{}{}
	}
	for _, v := range o.values {
		if _, exists := set[v]; !exists {
			return false
		}
	}
	return true
}

func (c EnumConstraint) narrowsTo(child Constraint) bool {
	o, ok := ResolveAlias(child).(EnumConstraint)
	if !ok {
		return false
	}
	// Child values must be a subset of parent values.
	parentSet := make(map[string]struct{}, len(c.values))
	for _, v := range c.values {
		parentSet[v] = struct{}{}
	}
	for _, v := range o.values {
		if _, exists := parentSet[v]; !exists {
			return false
		}
	}
	return true
}

func (EnumConstraint) isResolved() bool { return true }

// PatternConstraint constrains string values to those its one regex pattern
// matches. A match is a search: the pattern matches anywhere in the value
// unless it is anchored. The zero PatternConstraint holds no pattern, and
// [CheckConstraint] refuses it.
type PatternConstraint struct {
	pattern  string         // the source text
	compiled *regexp.Regexp // nil when the constraint holds no pattern
}

// parsedPatternConstraint creates a PatternConstraint that keeps compiled, the
// regexp internal/parse built with [regexp.Compile] from the source's pattern
// and judged there, or nil when the source's pattern was refused.
func parsedPatternConstraint(compiled *regexp.Regexp) PatternConstraint {
	if compiled == nil {
		return PatternConstraint{}
	}
	return PatternConstraint{pattern: compiled.String(), compiled: compiled}
}

func (PatternConstraint) Kind() ConstraintKind { return KindPattern }
func (PatternConstraint) constraint()          {}

// Pattern returns the regex pattern's source text, or "" when the constraint
// holds no pattern.
func (c PatternConstraint) Pattern() string { return c.pattern }

func (c PatternConstraint) String() string {
	if c.compiled == nil {
		return "Pattern[]"
	}
	return fmt.Sprintf("Pattern[%q]", c.pattern)
}

// equal reports whether other is a PatternConstraint with the same source
// text. A constraint that holds no pattern equals only another that holds none.
func (c PatternConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(PatternConstraint)
	return ok && (c.compiled == nil) == (o.compiled == nil) && c.pattern == o.pattern
}

func (c PatternConstraint) narrowsTo(child Constraint) bool { return c.equal(child) }

func (PatternConstraint) isResolved() bool { return true }

// VectorConstraint constrains vector values to a fixed dimension.
type VectorConstraint struct {
	dimension int
}

// newVectorConstraint creates a VectorConstraint with the given dimension.
func newVectorConstraint(dimension int) VectorConstraint {
	return VectorConstraint{dimension: dimension}
}

func (VectorConstraint) Kind() ConstraintKind { return KindVector }
func (VectorConstraint) constraint()          {}

// Dimension returns the required vector dimension.
func (c VectorConstraint) Dimension() int { return c.dimension }

func (c VectorConstraint) String() string {
	return fmt.Sprintf("Vector[%d]", c.dimension)
}

func (c VectorConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(VectorConstraint)
	return ok && c.dimension == o.dimension
}

func (c VectorConstraint) narrowsTo(child Constraint) bool { return c.equal(child) }

func (VectorConstraint) isResolved() bool { return true }

// ListConstraint constrains list values with an element type and optional min/max length.
type ListConstraint struct {
	element Constraint
	minLen  int64
	maxLen  int64
	hasMin  bool
	hasMax  bool
}

// newListConstraint creates a ListConstraint with no length bounds. The
// element must be non-nil: [CheckConstraint] refuses a List with no element,
// which is what the zero ListConstraint holds. A nil pointer
// is no element, whatever type it points to.
func newListConstraint(element Constraint) ListConstraint {
	return ListConstraint{element: held(element)}
}

// listMinLen creates a ListConstraint with a minimum length bound. The element
// must be non-nil, as for newListConstraint.
func listMinLen(element Constraint, lo int64) ListConstraint {
	return ListConstraint{element: held(element), minLen: lo, hasMin: true}
}

// listMaxLen creates a ListConstraint with a maximum length bound. The element
// must be non-nil, as for newListConstraint.
func listMaxLen(element Constraint, hi int64) ListConstraint {
	return ListConstraint{element: held(element), maxLen: hi, hasMax: true}
}

// listLenBetween creates a ListConstraint with both min and max length bounds.
// The element must be non-nil, as for newListConstraint.
func listLenBetween(element Constraint, lo, hi int64) ListConstraint {
	return ListConstraint{element: held(element), minLen: lo, maxLen: hi, hasMin: true, hasMax: true}
}

func (ListConstraint) Kind() ConstraintKind { return KindList }
func (ListConstraint) constraint()          {}

// Element returns the element constraint.
func (c ListConstraint) Element() Constraint { return c.element }

// MinLen returns the minimum list length and whether a minimum is set.
func (c ListConstraint) MinLen() (int64, bool) { return c.minLen, c.hasMin }

// MaxLen returns the maximum list length and whether a maximum is set.
func (c ListConstraint) MaxLen() (int64, bool) { return c.maxLen, c.hasMax }

func (c ListConstraint) String() string {
	var sb strings.Builder
	sb.WriteString("List<")
	if c.element != nil {
		sb.WriteString(c.element.String())
	}
	sb.WriteString(">")
	if c.hasMin || c.hasMax {
		sb.WriteString("[")
		if c.hasMin {
			sb.WriteString(strconv.FormatInt(c.minLen, 10))
		} else {
			sb.WriteString("_")
		}
		sb.WriteString(", ")
		if c.hasMax {
			sb.WriteString(strconv.FormatInt(c.maxLen, 10))
		} else {
			sb.WriteString("_")
		}
		sb.WriteString("]")
	}
	return sb.String()
}

func (c ListConstraint) equal(other Constraint) bool {
	o, ok := ResolveAlias(other).(ListConstraint)
	if !ok {
		return false
	}
	if c.hasMin != o.hasMin || c.hasMax != o.hasMax || c.minLen != o.minLen || c.maxLen != o.maxLen {
		return false
	}
	if c.element == nil || o.element == nil {
		return c.element == nil && o.element == nil
	}
	return c.element.equal(o.element)
}

func (c ListConstraint) narrowsTo(child Constraint) bool {
	o, ok := ResolveAlias(child).(ListConstraint)
	if !ok {
		return false
	}
	// Element must narrow; a List with no element judges nothing, so it narrows
	// to nothing.
	if c.element == nil || o.element == nil || !c.element.narrowsTo(o.element) {
		return false
	}
	// Child min must be >= parent min (if parent has a min).
	if c.hasMin && (!o.hasMin || o.minLen < c.minLen) {
		return false
	}
	// Child max must be <= parent max (if parent has a max).
	if c.hasMax && (!o.hasMax || o.maxLen > c.maxLen) {
		return false
	}
	return true
}

func (c ListConstraint) isResolved() bool {
	return c.element != nil && c.element.isResolved()
}

// AliasConstraint represents a reference to a named DataType.
// The underlying constraint is resolved for equality comparisons.
type AliasConstraint struct {
	dataTypeName string     // the DataType name this references
	resolved     Constraint // the resolved underlying constraint
}

// newAliasConstraint creates an AliasConstraint referencing a DataType. A
// raw-constraint caller with no schema, such as the neo4j package's
// CoerceParams, reads the resolved
// argument as the alias's meaning. Completion builds each resolved alias with
// this function, so in a loaded schema a DataType reference means its
// declaration and nothing else. [CheckConstraint] refuses a reference whose resolved argument is nil, and a
// nil pointer is nil here, whatever type it points to.
func newAliasConstraint(dataTypeName string, resolved Constraint) AliasConstraint {
	return AliasConstraint{dataTypeName: dataTypeName, resolved: held(resolved)}
}

// held returns c, or nil for a nil pointer. Every constraint is a value type,
// so a nil pointer holds no constraint, and a method called through one
// panics; a List or an alias that holds one would panic in its own methods.
func held(c Constraint) Constraint {
	if v := reflect.ValueOf(c); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	return c
}

func (AliasConstraint) Kind() ConstraintKind { return KindAlias }
func (AliasConstraint) constraint()          {}

// DataTypeName returns the name of the referenced DataType.
func (c AliasConstraint) DataTypeName() string { return c.dataTypeName }

// Resolved returns the underlying resolved constraint.
func (c AliasConstraint) Resolved() Constraint { return c.resolved }

func (c AliasConstraint) String() string {
	return c.dataTypeName
}

// equal compares the terminal constraints the alias chains resolve to, not the
// alias names. Two chains that resolve to no terminal constraint, unresolved or
// cyclic, are equal only when they name one datatype, which keeps equal
// reflexive for inheritance merging.
func (c AliasConstraint) equal(other Constraint) bool {
	if _, undecidable := ResolveAlias(c).(AliasConstraint); undecidable {
		o, ok := other.(AliasConstraint)
		if !ok {
			return false
		}
		if _, alsoUndecidable := ResolveAlias(o).(AliasConstraint); !alsoUndecidable {
			return false
		}
		return c.dataTypeName == o.dataTypeName
	}
	return ResolveAlias(c).equal(other)
}

// narrowsTo delegates to the terminal constraint the alias chain resolves to.
// A chain that resolves to none, unresolved or cyclic, narrows to nothing.
func (c AliasConstraint) narrowsTo(child Constraint) bool {
	if c.resolved == nil {
		return false
	}
	// Use ResolveAlias for cycle-safety before delegating.
	// ResolveAlias returns an AliasConstraint for a cycle or for a chain that
	// ends unresolved, and narrowing cannot be decided for either.
	term := ResolveAlias(c.resolved)
	if _, ok := term.(AliasConstraint); ok {
		return false // cycle or unresolved alias chain
	}
	return term.narrowsTo(child)
}

// isResolved reports whether the alias chain resolves to a terminal
// constraint; schema completion guarantees it for every constraint it keeps.
func (c AliasConstraint) isResolved() bool {
	if c.resolved == nil {
		return false
	}
	// Use ResolveAlias for cycle-safety before checking resolution.
	// If ResolveAlias returns an AliasConstraint, a cycle was detected
	// or the alias chain does not terminate - return false.
	term := ResolveAlias(c.resolved)
	if _, ok := term.(AliasConstraint); ok {
		return false // cycle or unresolved alias chain
	}
	return term.isResolved()
}

// ResolveAlias unwraps AliasConstraint chains to get the terminal constraint.
// Returns the input unchanged if not an alias.
//
// For alias chains (alias-of-alias), this iteratively unwraps until reaching
// a non-alias constraint or an unresolved alias. Includes cycle detection to
// prevent infinite loops in malformed schemas.
//
// Cycle detection uses datatype names (not constraint values) to avoid
// hashability issues with slice-containing constraint types like EnumConstraint.
func ResolveAlias(c Constraint) Constraint {
	// Fast path: not an alias or unresolved
	ac, ok := c.(AliasConstraint)
	if !ok || ac.resolved == nil {
		return c
	}

	// Track seen datatype names to detect cycles
	seen := map[string]struct{}{ac.dataTypeName: {}}
	c = ac.resolved

	for {
		next, ok := c.(AliasConstraint)
		if !ok || next.resolved == nil {
			return c
		}
		if _, dup := seen[next.dataTypeName]; dup {
			// Cycle detected - return current constraint
			return c
		}
		seen[next.dataTypeName] = struct{}{}
		c = next.resolved
	}
}

// CheckConstraint returns why c cannot judge a value, or nil when it can. It
// refuses nil, the zero value of a kind that needs an argument (an Enum, a
// Pattern, a Vector, a List, a DataType reference), and a Go type this package
// does not construct, such as a pointer to a constraint, at any List depth. It
// also refuses a DataType reference resolved to another reference alone or to
// no constraint, which completion refuses in a schema; the neo4j package's
// CoerceParams calls it before it reads a value.
func CheckConstraint(c Constraint) error {
	if c == nil {
		return errors.New("schema: no constraint")
	}
	if fault, found := constraintFault(c); found {
		return fmt.Errorf("schema: %s", fault)
	}
	// constraintFault judged every List level, so only the innermost element
	// is left to resolve: judging each level again costs the depth squared.
	for {
		list, isList := c.(ListConstraint)
		if !isList {
			break
		}
		c = list.Element()
	}
	held, isAlias := c.(AliasConstraint)
	if !isAlias {
		return nil
	}
	if inner, isAlias := held.resolved.(AliasConstraint); isAlias {
		return fmt.Errorf("schema: datatype %q resolves to datatype %q alone; a datatype declares a built-in type or a List", held.DataTypeName(), inner.DataTypeName())
	}
	terminal := ResolveAlias(c)
	if open, isOpen := terminal.(AliasConstraint); isOpen {
		return fmt.Errorf("schema: datatype %q resolves to no constraint", open.DataTypeName())
	}
	return CheckConstraint(terminal)
}

// constraintFault describes the first fault of c, or of a List element at any
// depth, that a caller can hold: the zero value of a kind that needs an
// argument, or a Go type this package does not construct. The parser refuses
// every argument the DSL cannot state, and only it sets one.
func constraintFault(c Constraint) (string, bool) {
	switch c := c.(type) {
	case BooleanConstraint, TimestampConstraint, DateConstraint, UUIDConstraint,
		IntegerConstraint, FloatConstraint, StringConstraint:
		// Nothing to judge: each value of these kinds is one the DSL states.
	case AliasConstraint:
		// The zero AliasConstraint meets no constructor, so its empty name is
		// read here; completion resolves every other name.
		if c.dataTypeName == "" {
			return fmt.Sprintf("datatype reference %q is not one a .yammm source can write; a reference is a datatype name, or an import alias, a dot and a datatype name", c.dataTypeName), true
		}
	case ListConstraint:
		if c.Element() == nil {
			return "list has no element constraint", true
		}
		return constraintFault(c.Element())
	case EnumConstraint:
		// The count is read here, not recorded: the zero EnumConstraint meets no
		// constructor and holds no values.
		if n := len(c.values); n < 2 {
			return fmt.Sprintf("enum must have at least two values (got %d)", n), true
		}
	case PatternConstraint:
		if c.compiled == nil {
			return "pattern constraint needs a pattern", true
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

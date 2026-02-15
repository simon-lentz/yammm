package schema

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ConstraintKind identifies the kind of constraint.
type ConstraintKind uint8

const (
	KindString ConstraintKind = iota
	KindInteger
	KindFloat
	KindBoolean
	KindTimestamp
	KindDate
	KindUUID
	KindEnum
	KindPattern
	KindVector
	KindAlias // reference to DataType
)

// String returns the name of the constraint kind.
func (k ConstraintKind) String() string {
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

	// Equal reports whether two constraints are structurally equal.
	// For AliasConstraint, compares the resolved underlying constraint.
	Equal(other Constraint) bool

	// IsResolved reports whether the constraint references are fully resolved.
	// This method is primarily meaningful for AliasConstraint, which starts
	// unresolved (referencing a DataType by name) and becomes resolved during
	// schema completion when the underlying constraint is wired.
	// All other constraint types always return true (they have no deferred
	// references to resolve).
	IsResolved() bool

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

// NewStringConstraint creates a StringConstraint with no bounds.
func NewStringConstraint() StringConstraint {
	return StringConstraint{}
}

// NewStringConstraintBounded creates a StringConstraint with the given bounds.
// Pass -1 for minLen or maxLen to indicate no bound.
func NewStringConstraintBounded(minLen, maxLen int64) StringConstraint {
	c := StringConstraint{}
	if minLen >= 0 {
		c.minLen = minLen
		c.hasMin = true
	}
	if maxLen >= 0 {
		c.maxLen = maxLen
		c.hasMax = true
	}
	return c
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

func (c StringConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(StringConstraint)
	return ok && c == o
}

func (StringConstraint) IsResolved() bool { return true }

// IntegerConstraint constrains integer values with optional min/max bounds.
type IntegerConstraint struct {
	min    int64
	max    int64
	hasMin bool
	hasMax bool
}

// NewIntegerConstraint creates an IntegerConstraint with no bounds.
func NewIntegerConstraint() IntegerConstraint {
	return IntegerConstraint{}
}

// NewIntegerConstraintBounded creates an IntegerConstraint with the given bounds.
// Use hasMin=false or hasMax=false to indicate no bound.
func NewIntegerConstraintBounded(min int64, hasMin bool, max int64, hasMax bool) IntegerConstraint {
	return IntegerConstraint{min: min, max: max, hasMin: hasMin, hasMax: hasMax}
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

func (c IntegerConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(IntegerConstraint)
	return ok && c == o
}

func (IntegerConstraint) IsResolved() bool { return true }

// FloatConstraint constrains float values with optional min/max bounds.
type FloatConstraint struct {
	min    float64
	max    float64
	hasMin bool
	hasMax bool
}

// NewFloatConstraint creates a FloatConstraint with no bounds.
func NewFloatConstraint() FloatConstraint {
	return FloatConstraint{}
}

// NewFloatConstraintBounded creates a FloatConstraint with the given bounds.
// Use hasMin=false or hasMax=false to indicate no bound.
func NewFloatConstraintBounded(min float64, hasMin bool, max float64, hasMax bool) FloatConstraint {
	return FloatConstraint{min: min, max: max, hasMin: hasMin, hasMax: hasMax}
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

func (c FloatConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(FloatConstraint)
	return ok && c == o
}

func (FloatConstraint) IsResolved() bool { return true }

// BooleanConstraint constrains boolean values. It has no parameters.
type BooleanConstraint struct{}

// NewBooleanConstraint creates a BooleanConstraint.
func NewBooleanConstraint() BooleanConstraint {
	return BooleanConstraint{}
}

func (BooleanConstraint) Kind() ConstraintKind { return KindBoolean }
func (BooleanConstraint) constraint()          {}
func (BooleanConstraint) String() string       { return "Boolean" }

func (c BooleanConstraint) Equal(other Constraint) bool {
	_, ok := resolveAlias(other).(BooleanConstraint)
	return ok
}

func (BooleanConstraint) IsResolved() bool { return true }

// TimestampConstraint constrains timestamp values with an optional format.
type TimestampConstraint struct {
	format string
}

// NewTimestampConstraint creates a TimestampConstraint with no format.
func NewTimestampConstraint() TimestampConstraint {
	return TimestampConstraint{}
}

// NewTimestampConstraintFormatted creates a TimestampConstraint with a Go time format.
func NewTimestampConstraintFormatted(format string) TimestampConstraint {
	return TimestampConstraint{format: format}
}

func (TimestampConstraint) Kind() ConstraintKind { return KindTimestamp }
func (TimestampConstraint) constraint()          {}

func (c TimestampConstraint) Format() string { return c.format }

func (c TimestampConstraint) String() string {
	if c.format == "" {
		return "Timestamp"
	}
	return fmt.Sprintf("Timestamp[%q]", c.format)
}

func (c TimestampConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(TimestampConstraint)
	return ok && c.format == o.format
}

func (TimestampConstraint) IsResolved() bool { return true }

// DateConstraint constrains date values. It has no parameters.
type DateConstraint struct{}

// NewDateConstraint creates a DateConstraint.
func NewDateConstraint() DateConstraint {
	return DateConstraint{}
}

func (DateConstraint) Kind() ConstraintKind { return KindDate }
func (DateConstraint) constraint()          {}
func (DateConstraint) String() string       { return "Date" }

func (c DateConstraint) Equal(other Constraint) bool {
	_, ok := resolveAlias(other).(DateConstraint)
	return ok
}

func (DateConstraint) IsResolved() bool { return true }

// UUIDConstraint constrains UUID values. It has no parameters.
type UUIDConstraint struct{}

// NewUUIDConstraint creates a UUIDConstraint.
func NewUUIDConstraint() UUIDConstraint {
	return UUIDConstraint{}
}

func (UUIDConstraint) Kind() ConstraintKind { return KindUUID }
func (UUIDConstraint) constraint()          {}
func (UUIDConstraint) String() string       { return "UUID" }

func (c UUIDConstraint) Equal(other Constraint) bool {
	_, ok := resolveAlias(other).(UUIDConstraint)
	return ok
}

func (UUIDConstraint) IsResolved() bool { return true }

// EnumConstraint constrains string values to a fixed set of allowed values.
type EnumConstraint struct {
	values []string
}

// NewEnumConstraint creates an EnumConstraint with the given allowed values.
// The values are stored in a defensive copy.
func NewEnumConstraint(values []string) EnumConstraint {
	return EnumConstraint{values: slices.Clone(values)}
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

// Equal compares using set equality (order-insensitive).
func (c EnumConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(EnumConstraint)
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

func (EnumConstraint) IsResolved() bool { return true }

// PatternConstraint constrains string values to match one or more regex patterns.
// All patterns must match (conjunction semantics). Maximum 2 patterns for performance.
type PatternConstraint struct {
	patterns []string         // source pattern strings (public API)
	compiled []*regexp.Regexp // compiled patterns (internal validation)
}

// NewPatternConstraint creates a PatternConstraint from compiled patterns.
// Maximum 2 patterns are allowed; extras are silently ignored.
func NewPatternConstraint(patterns []*regexp.Regexp) PatternConstraint {
	n := min(len(patterns), 2)
	strs := make([]string, n)
	compiled := make([]*regexp.Regexp, n)
	for i := range n {
		strs[i] = patterns[i].String()
		compiled[i] = patterns[i]
	}
	return PatternConstraint{patterns: strs, compiled: compiled}
}

func (PatternConstraint) Kind() ConstraintKind { return KindPattern }
func (PatternConstraint) constraint()          {}

// Patterns returns the regex pattern strings. Returns a defensive copy.
func (c PatternConstraint) Patterns() []string {
	return slices.Clone(c.patterns)
}

// PatternCount returns the number of patterns.
func (c PatternConstraint) PatternCount() int {
	return len(c.patterns)
}

// Pattern returns the first (primary) regex pattern.
// Returns empty string if no patterns exist.
// For constraints with two patterns, use Patterns() instead.
func (c PatternConstraint) Pattern() string {
	if len(c.patterns) == 0 {
		return ""
	}
	return c.patterns[0]
}

// CompiledPatterns returns the compiled regex patterns for internal validation use.
// This method is for internal use by the validation layer (checkers.go).
// External consumers should use Patterns() which returns strings per the public API spec.
func (c PatternConstraint) CompiledPatterns() []*regexp.Regexp {
	return slices.Clone(c.compiled)
}

func (c PatternConstraint) String() string {
	quoted := make([]string, len(c.patterns))
	for i, p := range c.patterns {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return fmt.Sprintf("Pattern[%s]", strings.Join(quoted, ", "))
}

// Equal compares using order-insensitive pattern string comparison.
func (c PatternConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(PatternConstraint)
	if !ok || len(c.patterns) != len(o.patterns) {
		return false
	}
	// Order-insensitive comparison (max 2 patterns)
	cp := slices.Clone(c.patterns)
	op := slices.Clone(o.patterns)
	sort.Strings(cp)
	sort.Strings(op)
	return slices.Equal(cp, op)
}

func (PatternConstraint) IsResolved() bool { return true }

// VectorConstraint constrains vector values to a fixed dimension.
type VectorConstraint struct {
	dimension int
}

// NewVectorConstraint creates a VectorConstraint with the given dimension.
func NewVectorConstraint(dimension int) VectorConstraint {
	return VectorConstraint{dimension: dimension}
}

func (VectorConstraint) Kind() ConstraintKind { return KindVector }
func (VectorConstraint) constraint()          {}

// Dimension returns the required vector dimension.
func (c VectorConstraint) Dimension() int { return c.dimension }

func (c VectorConstraint) String() string {
	return fmt.Sprintf("Vector[%d]", c.dimension)
}

func (c VectorConstraint) Equal(other Constraint) bool {
	o, ok := resolveAlias(other).(VectorConstraint)
	return ok && c.dimension == o.dimension
}

func (VectorConstraint) IsResolved() bool { return true }

// AliasConstraint represents a reference to a named DataType.
// The underlying constraint is resolved for equality comparisons.
type AliasConstraint struct {
	dataTypeName string     // the DataType name this references
	resolved     Constraint // the resolved underlying constraint
}

// NewAliasConstraint creates an AliasConstraint referencing a DataType.
func NewAliasConstraint(dataTypeName string, resolved Constraint) AliasConstraint {
	return AliasConstraint{dataTypeName: dataTypeName, resolved: resolved}
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

// Equal compares the resolved underlying constraints, not the alias names.
// This enables inheritance deduplication with different alias names but same constraint.
// For unresolved aliases, compares by datatype name.
//
// This method is cycle-safe: it uses resolveAlias() to unwrap alias chains before
// delegating to the terminal constraint's Equal method.
func (c AliasConstraint) Equal(other Constraint) bool {
	if c.resolved == nil {
		// Two unresolved aliases are equal if they reference the same datatype name
		if otherAlias, ok := other.(AliasConstraint); ok && otherAlias.resolved == nil {
			return c.dataTypeName == otherAlias.dataTypeName
		}
		return false // Unresolved alias is never equal to non-alias or resolved alias
	}
	// Use resolveAlias for cycle-safety before delegating.
	// If resolveAlias returns an AliasConstraint, a cycle was detected
	// and we cannot determine equality - return false.
	term := resolveAlias(c.resolved)
	if _, ok := term.(AliasConstraint); ok {
		return false // cycle or unresolved alias chain
	}
	return term.Equal(other)
}

// IsResolved reports whether the alias has been fully resolved to a terminal constraint.
//
// After successful schema completion, IsResolved() is guaranteed to return true
// for all constraints. An unresolved alias (IsResolved() == false) indicates
// either:
//   - Pre-completion state during schema loading
//   - A schema completion failure (alias target not found)
//
// Instance validation should only operate on fully completed schemas where
// IsResolved() is always true. Encountering an unresolved constraint during
// evaluation indicates a schema error.
//
// For alias chains (alias-of-alias), this method uses resolveAlias to unwrap
// the chain with cycle detection before checking the terminal constraint.
func (c AliasConstraint) IsResolved() bool {
	if c.resolved == nil {
		return false
	}
	// Use resolveAlias for cycle-safety before checking resolution.
	// If resolveAlias returns an AliasConstraint, a cycle was detected
	// or the alias chain does not terminate - return false.
	term := resolveAlias(c.resolved)
	if _, ok := term.(AliasConstraint); ok {
		return false // cycle or unresolved alias chain
	}
	return term.IsResolved()
}

// resolveAlias unwraps AliasConstraint chains to get the terminal constraint.
// Returns the input unchanged if not an alias.
//
// For alias chains (alias-of-alias), this iteratively unwraps until reaching
// a non-alias constraint or an unresolved alias. Includes cycle detection to
// prevent infinite loops in malformed schemas.
//
// Cycle detection uses datatype names (not constraint values) to avoid
// hashability issues with slice-containing constraint types like EnumConstraint
// and PatternConstraint.
func resolveAlias(c Constraint) Constraint {
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

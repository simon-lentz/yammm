package expr

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// defaultBuiltinNames returns the default set of builtin function names.
// These are the standard functions available in all yammm expression contexts.
func defaultBuiltinNames() map[string]struct{} {
	return map[string]struct{}{
		// Collection operations
		"Any":       {}, // Any(pred) - true if any element satisfies predicate
		"All":       {}, // All(pred) - true if all elements satisfy predicate (vacuous truth for empty)
		"AllOrNone": {}, // AllOrNone(pred) - true if all or none satisfy predicate
		"Filter":    {}, // Filter(pred) - returns elements satisfying predicate
		"Map":       {}, // Map(fn) - transforms each element
		"Reduce":    {}, // Reduce(init, fn) - reduces to single value
		"Count":     {}, // Count() or Count(pred) - counts elements
		"Sum":       {}, // Sum() - sums numeric elements
		"Min":       {}, // Min() - minimum value
		"Max":       {}, // Max() - maximum value
		"First":     {}, // First() - first element or nil
		"Last":      {}, // Last() - last element or nil
		"Unique":    {}, // Unique() - removes duplicates
		"Sort":      {}, // Sort() - sorts elements
		"Reverse":   {}, // Reverse() - reverses order
		"Flatten":   {}, // Flatten() - flattens nested lists
		"Contains":  {}, // Contains(val) - checks if value is in collection

		// String operations
		"Len":        {}, // Len() - string or collection length
		"Upper":      {}, // Upper() - uppercase string
		"Lower":      {}, // Lower() - lowercase string
		"Trim":       {}, // Trim() - trim whitespace
		"TrimPrefix": {}, // TrimPrefix(prefix) - removes prefix
		"TrimSuffix": {}, // TrimSuffix(suffix) - removes suffix
		"Split":      {}, // Split(sep) - splits string
		"Join":       {}, // Join(sep) - joins collection
		"Replace":    {}, // Replace(old, new) - replaces substring
		"StartsWith": {}, // StartsWith(prefix) - checks prefix
		"EndsWith":   {}, // EndsWith(suffix) - checks suffix
		"Substring":  {}, // Substring(start, end?) - extracts substring
		"Match":      {}, // Match(pattern) - regex match with captures

		// Type checking
		"TypeOf": {}, // TypeOf() - returns type name
		"IsNil":  {}, // IsNil() - checks for nil

		// Math
		"Abs":     {}, // Abs() - absolute value
		"Floor":   {}, // Floor() - floor of float
		"Ceil":    {}, // Ceil() - ceiling of float
		"Round":   {}, // Round() - round to nearest int
		"Compare": {}, // Compare(a, b) - three-way comparison

		// Control flow
		"Then": {}, // Then(fn) - execute body when non-nil
		"Lest": {}, // Lest(fn) - execute body when nil
		"With": {}, // With(fn) - bind value and execute

		// Utilities
		"Default":  {}, // Default(val) - returns val if nil
		"Coalesce": {}, // Coalesce(vals...) - first non-nil
		"Compact":  {}, // Compact() - remove nil entries
	}
}

// BuiltinRegistry provides an inventory of known builtin function names.
//
// This registry is used for:
//   - Documentation and tooling (listing available functions)
//   - IDE completions (suggesting function names)
//   - Future eval-time validation hooks (ValidateBuiltins)
//
// Note: Expression compilation does NOT validate function names against this
// registry. Unknown functions compile successfully into the AST; validation
// happens at evaluation time in instance/eval. This design allows schemas
// to be compiled without knowing all builtins, supporting runtime extension
// and custom builtin registration.
//
// The zero value is usable and contains all default builtins. This makes the
// following patterns equivalent:
//
//	var r expr.BuiltinRegistry  // zero value, contains defaults
//	r := expr.NewBuiltinRegistry()  // explicit construction
//
// Thread Safety: BuiltinRegistry is not safe for concurrent mutation. Build
// the registry during initialization, then share the result. Clone() can be
// used to create independent copies for different configurations.
type BuiltinRegistry struct {
	names map[string]struct{}
}

// NewBuiltinRegistry creates a registry with all default builtins.
func NewBuiltinRegistry() *BuiltinRegistry {
	return &BuiltinRegistry{names: defaultBuiltinNames()}
}

// ensureInit lazily initializes the registry to defaults if needed.
// This makes the zero value usable.
func (r *BuiltinRegistry) ensureInit() {
	if r.names != nil {
		return
	}
	r.names = defaultBuiltinNames()
}

// Register adds a custom builtin function name.
// Returns an error if name is empty or already registered.
//
// Use this when registering custom builtins for extended evaluators. The
// registered name will be recognized by Has() and returned by Names().
//
// Note: The registry does not validate that name matches the grammar's
// identifier form. Callers are responsible for ensuring registered names
// can appear in parsed expressions.
func (r *BuiltinRegistry) Register(name string) error {
	if r == nil {
		return errors.New("nil registry")
	}
	r.ensureInit()
	if name == "" {
		return errors.New("empty builtin name")
	}
	// Reject names with leading/trailing whitespace
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("builtin name has leading/trailing whitespace: %q", name)
	}
	// Reject names with embedded whitespace
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("builtin name contains whitespace: %q", name)
	}
	if _, exists := r.names[name]; exists {
		return fmt.Errorf("builtin already registered: %q", name)
	}
	r.names[name] = struct{}{}
	return nil
}

// MustRegister adds a custom builtin function name.
// Panics if name is empty or already registered.
func (r *BuiltinRegistry) MustRegister(name string) {
	if err := r.Register(name); err != nil {
		panic(err)
	}
}

// Has reports whether a function name is a known builtin.
func (r *BuiltinRegistry) Has(name string) bool {
	if r == nil {
		return false
	}
	r.ensureInit()
	_, exists := r.names[name]
	return exists
}

// Names returns all builtin function names in sorted order.
//
// The sorted order ensures deterministic output for tooling (completions,
// debug dumps) regardless of map iteration order.
func (r *BuiltinRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.ensureInit()
	names := make([]string, 0, len(r.names))
	for name := range r.names {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Len returns the number of registered builtins.
func (r *BuiltinRegistry) Len() int {
	if r == nil {
		return 0
	}
	r.ensureInit()
	return len(r.names)
}

// Clone returns a new registry with the same registered names.
// The clone is independent; modifications to one do not affect the other.
//
// If the receiver is nil, Clone returns a new registry with default builtins.
// This makes (*BuiltinRegistry)(nil).Clone() a valid "defaults factory".
func (r *BuiltinRegistry) Clone() *BuiltinRegistry {
	if r == nil {
		return NewBuiltinRegistry()
	}
	r.ensureInit()
	names := make(map[string]struct{}, len(r.names))
	for name := range r.names {
		names[name] = struct{}{}
	}
	return &BuiltinRegistry{names: names}
}

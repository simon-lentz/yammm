package schema_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/schema"
)

func TestConstraint_String(t *testing.T) {
	tests := []struct {
		name       string
		constraint schema.Constraint
		expected   string
	}{
		{"String", schema.TestNewStringConstraint(), "String"},
		{"String bounded", schema.TestStringLenBetween(1, 100), "String[1, 100]"},
		{"String min only", schema.TestStringMinLen(1), "String[1, _]"},
		{"String max only", schema.TestStringMaxLen(100), "String[_, 100]"},
		{"Integer", schema.TestNewIntegerConstraint(), "Integer"},
		{"Integer bounded", schema.TestIntegerBetween(0, 100), "Integer[0, 100]"},
		{"Float", schema.TestNewFloatConstraint(), "Float"},
		{"Boolean", schema.TestNewBooleanConstraint(), "Boolean"},
		{"Timestamp", schema.TestNewTimestampConstraint(), "Timestamp"},
		{"Date", schema.TestNewDateConstraint(), "Date"},
		{"UUID", schema.TestNewUUIDConstraint(), "UUID"},
		{"Enum", schema.TestParsedEnumConstraint([]string{"a", "b"}), `Enum["a", "b"]`},
		{"Vector", schema.TestNewVectorConstraint(128), "Vector[128]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.constraint.String())
		})
	}
}

func TestConstraint_Kind(t *testing.T) {
	assert.Equal(t, schema.KindString, schema.TestNewStringConstraint().Kind())
	assert.Equal(t, schema.KindInteger, schema.TestNewIntegerConstraint().Kind())
	assert.Equal(t, schema.KindFloat, schema.TestNewFloatConstraint().Kind())
	assert.Equal(t, schema.KindBoolean, schema.TestNewBooleanConstraint().Kind())
	assert.Equal(t, schema.KindTimestamp, schema.TestNewTimestampConstraint().Kind())
	assert.Equal(t, schema.KindDate, schema.TestNewDateConstraint().Kind())
	assert.Equal(t, schema.KindUUID, schema.TestNewUUIDConstraint().Kind())
	assert.Equal(t, schema.KindEnum, schema.TestParsedEnumConstraint([]string{"a"}).Kind())
	assert.Equal(t, schema.KindVector, schema.TestNewVectorConstraint(128).Kind())
}

// TestConstraint_Equal drives Equal through one table covering every
// concrete constraint type: same-value equality, per-field inequality,
// cross-kind inequality, and the set-based semantics for enums.
func TestConstraint_Equal(t *testing.T) {
	tests := []struct {
		name string
		a, b schema.Constraint
		want bool
	}{
		{"string same bounds", schema.TestStringLenBetween(1, 100), schema.TestStringLenBetween(1, 100), true},
		{"string different bounds", schema.TestStringLenBetween(1, 100), schema.TestStringLenBetween(1, 200), false},
		{"different kinds", schema.TestNewStringConstraint(), schema.TestNewIntegerConstraint(), false},
		{"enum set equality ignores order", schema.TestParsedEnumConstraint([]string{"a", "b", "c"}), schema.TestParsedEnumConstraint([]string{"c", "b", "a"}), true},
		{"enum different values", schema.TestParsedEnumConstraint([]string{"a", "b"}), schema.TestParsedEnumConstraint([]string{"a", "c"}), false},
		{"integer same", schema.TestIntegerBetween(10, 100), schema.TestIntegerBetween(10, 100), true},
		{"integer different min", schema.TestIntegerBetween(10, 100), schema.TestIntegerBetween(20, 100), false},
		{"integer different max", schema.TestIntegerBetween(10, 100), schema.TestIntegerBetween(10, 200), false},
		{"float same", schema.TestFloatBetween(1.5, 10.5), schema.TestFloatBetween(1.5, 10.5), true},
		{"float different min", schema.TestFloatBetween(1.5, 10.5), schema.TestFloatBetween(2.0, 10.5), false},
		{"boolean same", schema.TestNewBooleanConstraint(), schema.TestNewBooleanConstraint(), true},
		{"boolean vs string", schema.TestNewBooleanConstraint(), schema.TestNewStringConstraint(), false},
		{"timestamp same", schema.TestNewTimestampConstraint(), schema.TestNewTimestampConstraint(), true},
		{"timestamp and the zero value", schema.TestNewTimestampConstraint(), schema.TimestampConstraint{}, true},
		{"date same", schema.TestNewDateConstraint(), schema.TestNewDateConstraint(), true},
		{"date vs timestamp", schema.TestNewDateConstraint(), schema.TestNewTimestampConstraint(), false},
		{"uuid same", schema.TestNewUUIDConstraint(), schema.TestNewUUIDConstraint(), true},
		{"uuid vs string", schema.TestNewUUIDConstraint(), schema.TestNewStringConstraint(), false},
		{"pattern same", schema.TestParsedPatternConstraint(regexp.MustCompile("^test")), schema.TestParsedPatternConstraint(regexp.MustCompile("^test")), true},
		{"pattern different", schema.TestParsedPatternConstraint(regexp.MustCompile("^test")), schema.TestParsedPatternConstraint(regexp.MustCompile("^other")), false},
		{"pattern empty and none", schema.TestParsedPatternConstraint(regexp.MustCompile("")), schema.TestParsedPatternConstraint(nil), false},
		{"pattern none and the zero value", schema.TestParsedPatternConstraint(nil), schema.PatternConstraint{}, true},
		{"vector same dimension", schema.TestNewVectorConstraint(128), schema.TestNewVectorConstraint(128), true},
		{"vector different dimension", schema.TestNewVectorConstraint(128), schema.TestNewVectorConstraint(256), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schema.TestConstraintEqual(tt.a, tt.b))
		})
	}
}

func TestEnumConstraint_DefensiveCopy(t *testing.T) {
	c := schema.TestParsedEnumConstraint([]string{"a", "b", "c"})

	values := c.Values()

	// Modify returned values
	values[1] = "modified"

	// Constraint should still be unaffected
	values2 := c.Values()
	assert.Equal(t, "b", values2[1], "Values() should return defensive copy")
}

func TestAliasConstraint_ResolvesForEquality(t *testing.T) {
	underlying := schema.TestStringLenBetween(1, 100)
	alias := schema.TestNewAliasConstraint("Name", underlying)

	// Alias should equal its resolved constraint
	assert.True(t, schema.TestConstraintEqual(alias, underlying))

	// And vice versa
	assert.True(t, schema.TestConstraintEqual(underlying, alias))

	// Two aliases with same resolved constraint should be equal
	alias2 := schema.TestNewAliasConstraint("DifferentName", underlying)
	assert.True(t, schema.TestConstraintEqual(alias, alias2))
}

func TestAliasConstraint_UnresolvedEquality(t *testing.T) {
	t.Run("unresolved aliases with same datatype name are equal", func(t *testing.T) {
		// Create unresolved aliases (nil underlying)
		alias1 := schema.TestNewAliasConstraint("Name", nil)
		alias2 := schema.TestNewAliasConstraint("Name", nil)
		assert.True(t, schema.TestConstraintEqual(alias1, alias2), "unresolved aliases with same datatype name should be equal")
	})

	t.Run("unresolved aliases with different datatype names are not equal", func(t *testing.T) {
		alias1 := schema.TestNewAliasConstraint("Name", nil)
		alias2 := schema.TestNewAliasConstraint("Email", nil)
		assert.False(t, schema.TestConstraintEqual(alias1, alias2), "unresolved aliases with different datatype names should not be equal")
	})

	t.Run("unresolved alias is not equal to resolved alias", func(t *testing.T) {
		unresolved := schema.TestNewAliasConstraint("Name", nil)
		resolved := schema.TestNewAliasConstraint("Name", schema.TestNewStringConstraint())
		assert.False(t, schema.TestConstraintEqual(unresolved, resolved), "unresolved alias should not equal resolved alias")
	})

	t.Run("unresolved alias is not equal to non-alias constraint", func(t *testing.T) {
		unresolved := schema.TestNewAliasConstraint("Name", nil)
		assert.False(t, schema.TestConstraintEqual(unresolved, schema.TestNewStringConstraint()), "unresolved alias should not equal non-alias")
	})
}

func TestAliasConstraint_Kind(t *testing.T) {
	t.Run("alias always returns KindAlias", func(t *testing.T) {
		// AliasConstraint.Kind() returns what this constraint IS (an alias)
		// not what it resolves to
		underlying := schema.TestNewStringConstraint()
		alias := schema.TestNewAliasConstraint("Name", underlying)
		assert.Equal(t, schema.KindAlias, alias.Kind())

		// To get the resolved kind, use Resolved().Kind()
		assert.Equal(t, schema.KindString, alias.Resolved().Kind())
	})

	t.Run("unresolved alias returns KindAlias", func(t *testing.T) {
		alias := schema.TestNewAliasConstraint("Name", nil)
		assert.Equal(t, schema.KindAlias, alias.Kind())
	})
}

func TestStringConstraint_MinLen(t *testing.T) {
	c := schema.TestStringLenBetween(5, 100)
	minLen, hasMin := c.MinLen()
	assert.True(t, hasMin)
	assert.Equal(t, int64(5), minLen)
}

func TestStringConstraint_MaxLen(t *testing.T) {
	c := schema.TestStringLenBetween(5, 100)
	maxLen, hasMax := c.MaxLen()
	assert.True(t, hasMax)
	assert.Equal(t, int64(100), maxLen)
}

func TestStringConstraint_MinLen_Unbounded(t *testing.T) {
	c := schema.TestNewStringConstraint()
	_, hasMin := c.MinLen()
	assert.False(t, hasMin)
}

func TestStringConstraint_MaxLen_Unbounded(t *testing.T) {
	c := schema.TestNewStringConstraint()
	_, hasMax := c.MaxLen()
	assert.False(t, hasMax)
}

func TestIntegerConstraint_Min(t *testing.T) {
	c := schema.TestIntegerBetween(10, 100)
	minVal, hasMin := c.Min()
	assert.True(t, hasMin)
	assert.Equal(t, int64(10), minVal)
}

func TestIntegerConstraint_Max(t *testing.T) {
	c := schema.TestIntegerBetween(10, 100)
	maxVal, hasMax := c.Max()
	assert.True(t, hasMax)
	assert.Equal(t, int64(100), maxVal)
}

func TestIntegerConstraint_Min_Unbounded(t *testing.T) {
	c := schema.TestNewIntegerConstraint()
	_, hasMin := c.Min()
	assert.False(t, hasMin)
}

func TestIntegerConstraint_Max_Unbounded(t *testing.T) {
	c := schema.TestNewIntegerConstraint()
	_, hasMax := c.Max()
	assert.False(t, hasMax)
}

func TestFloatConstraint_Min(t *testing.T) {
	c := schema.TestFloatBetween(1.5, 10.5)
	minVal, hasMin := c.Min()
	assert.True(t, hasMin)
	assert.Equal(t, 1.5, minVal)
}

func TestFloatConstraint_Max(t *testing.T) {
	c := schema.TestFloatBetween(1.5, 10.5)
	maxVal, hasMax := c.Max()
	assert.True(t, hasMax)
	assert.Equal(t, 10.5, maxVal)
}

func TestFloatConstraint_Min_Unbounded(t *testing.T) {
	c := schema.TestNewFloatConstraint()
	_, hasMin := c.Min()
	assert.False(t, hasMin)
}

func TestFloatConstraint_Max_Unbounded(t *testing.T) {
	c := schema.TestNewFloatConstraint()
	_, hasMax := c.Max()
	assert.False(t, hasMax)
}

func TestPatternConstraint_Kind(t *testing.T) {
	p := regexp.MustCompile("^test")
	c := schema.TestParsedPatternConstraint(p)
	assert.Equal(t, schema.KindPattern, c.Kind())
}

func TestPatternConstraint_Pattern(t *testing.T) {
	p := regexp.MustCompile("^test")
	c := schema.TestParsedPatternConstraint(p)
	assert.Equal(t, "^test", c.Pattern())
}

func TestPatternConstraint_Pattern_Empty(t *testing.T) {
	c := schema.TestParsedPatternConstraint(nil)
	assert.Empty(t, c.Pattern())
}

func TestPatternRegexp_ReadsTheCompiledPattern(t *testing.T) {
	c := schema.TestParsedPatternConstraint(regexp.MustCompile("^a"))
	compiled := expr.PatternRegexp(c)
	assert.NotNil(t, compiled)
	assert.True(t, compiled.MatchString("abc"))
	assert.Nil(t, expr.PatternRegexp(schema.TestParsedPatternConstraint(nil)))
}

func TestPatternConstraint_String(t *testing.T) {
	p := regexp.MustCompile("^hello")
	c := schema.TestParsedPatternConstraint(p)
	assert.Equal(t, `Pattern["^hello"]`, c.String())
}

func TestPatternConstraint_String_None(t *testing.T) {
	assert.Equal(t, `Pattern[]`, schema.TestParsedPatternConstraint(nil).String())
}

// The empty pattern is a pattern, which matches every value: it is held and
// written, unlike a constraint that holds none.
func TestPatternConstraint_TheEmptyPatternIsHeld(t *testing.T) {
	c := schema.TestParsedPatternConstraint(regexp.MustCompile(""))
	assert.Equal(t, `Pattern[""]`, c.String())
	assert.NoError(t, schema.CheckConstraint(c))
}

func TestVectorConstraint_Dimension(t *testing.T) {
	c := schema.TestNewVectorConstraint(256)
	assert.Equal(t, 256, c.Dimension())
}

func TestAliasConstraint_DataTypeName(t *testing.T) {
	c := schema.TestNewAliasConstraint("Email", schema.TestNewStringConstraint())
	assert.Equal(t, "Email", c.DataTypeName())
}

func TestAliasConstraint_String(t *testing.T) {
	c := schema.TestNewAliasConstraint("Email", schema.TestNewStringConstraint())
	assert.Equal(t, "Email", c.String())
}

func TestAliasConstraint_String_Unresolved(t *testing.T) {
	c := schema.TestNewAliasConstraint("Email", nil)
	assert.Equal(t, "Email", c.String())
}

func TestFloatConstraint_StringStability(t *testing.T) {
	tests := []struct {
		name       string
		constraint schema.Constraint
		expected   string
	}{
		{"no bounds", schema.TestNewFloatConstraint(), "Float"},
		{"min only", schema.TestFloatMin(0), "Float[0, _]"},
		{"max only", schema.TestFloatMax(100.5), "Float[_, 100.5]"},
		{"both bounds", schema.TestFloatBetween(-10.25, 10.25), "Float[-10.25, 10.25]"},
		{"large values", schema.TestFloatBetween(1e10, 1e15), "Float[10000000000, 1000000000000000]"},
		{"small decimals", schema.TestFloatBetween(0.123456789, 0.987654321), "Float[0.123456789, 0.987654321]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.constraint.String())
		})
	}
}

func TestConstraint_IsResolved_NonAliasConstraints(t *testing.T) {
	// All non-alias constraints are always resolved (they have no references)
	tests := []struct {
		name       string
		constraint schema.Constraint
	}{
		{"String", schema.TestNewStringConstraint()},
		{"String bounded", schema.TestStringLenBetween(1, 100)},
		{"Integer", schema.TestNewIntegerConstraint()},
		{"Integer bounded", schema.TestIntegerBetween(0, 100)},
		{"Float", schema.TestNewFloatConstraint()},
		{"Float bounded", schema.TestFloatBetween(1.0, 10.0)},
		{"Boolean", schema.TestNewBooleanConstraint()},
		{"Timestamp", schema.TestNewTimestampConstraint()},
		{"Date", schema.TestNewDateConstraint()},
		{"UUID", schema.TestNewUUIDConstraint()},
		{"Enum", schema.TestParsedEnumConstraint([]string{"a", "b"})},
		{"Pattern", schema.TestParsedPatternConstraint(regexp.MustCompile("^test"))},
		{"Vector", schema.TestNewVectorConstraint(128)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, schema.TestConstraintIsResolved(tt.constraint), "%s constraint should always be resolved", tt.name)
		})
	}
}

func TestAliasConstraint_IsResolved_ResolvedAlias(t *testing.T) {
	underlying := schema.TestNewStringConstraint()
	alias := schema.TestNewAliasConstraint("Name", underlying)

	assert.True(t, schema.TestConstraintIsResolved(alias), "alias with resolved underlying constraint should return true")
}

func TestAliasConstraint_IsResolved_UnresolvedAlias(t *testing.T) {
	alias := schema.TestNewAliasConstraint("Name", nil)

	assert.False(t, schema.TestConstraintIsResolved(alias), "alias with nil underlying constraint should return false")
}

func TestAliasConstraint_IsResolved_NestedAlias(t *testing.T) {
	// An alias that resolves to another alias (should be resolved if inner alias is resolved)
	innerAlias := schema.TestNewAliasConstraint("Inner", schema.TestNewIntegerConstraint())
	outerAlias := schema.TestNewAliasConstraint("Outer", innerAlias)

	assert.True(t, schema.TestConstraintIsResolved(outerAlias), "nested alias should be resolved if underlying is resolved")
}

func TestAliasConstraint_IsResolved_NestedUnresolved(t *testing.T) {
	// Alias chain where the terminal alias is unresolved
	unresolvedInner := schema.TestNewAliasConstraint("Inner", nil)
	outerAlias := schema.TestNewAliasConstraint("Outer", unresolvedInner)

	assert.False(t, schema.TestConstraintIsResolved(outerAlias), "nested alias should be unresolved if terminal is unresolved")
}

func TestAliasConstraint_IsResolved_DeepChain(t *testing.T) {
	// Deep alias chain: A -> B -> C -> Integer
	terminal := schema.TestNewIntegerConstraint()
	aliasC := schema.TestNewAliasConstraint("C", terminal)
	aliasB := schema.TestNewAliasConstraint("B", aliasC)
	aliasA := schema.TestNewAliasConstraint("A", aliasB)

	assert.True(t, schema.TestConstraintIsResolved(aliasA), "deep alias chain should be resolved if terminal is resolved")
	assert.True(t, schema.TestConstraintIsResolved(aliasB))
	assert.True(t, schema.TestConstraintIsResolved(aliasC))
}

func TestConstraintKind_String_Unknown(t *testing.T) {
	// Unknown constraint kind should include numeric value
	unknown := schema.ConstraintKind(99)

	result := unknown.String()

	assert.Equal(t, "ConstraintKind(99)", result)
	assert.Equal(t, "ConstraintKind(0)", schema.ConstraintKind(0).String(), "the zero kind names no kind")
}

// These tests verify that alias chain resolution is safe with non-hashable constraint types
// (EnumConstraint, PatternConstraint) and handles cycles gracefully.

func TestAliasConstraint_Equal_WithEnumConstraint(t *testing.T) {
	// EnumConstraint contains a slice, which is non-hashable.
	// This test verifies that comparing aliases resolved to EnumConstraint
	// does not panic due to hashability issues.
	enum1 := schema.TestParsedEnumConstraint([]string{"a", "b", "c"})
	enum2 := schema.TestParsedEnumConstraint([]string{"a", "b", "c"})

	alias1 := schema.TestNewAliasConstraint("Status", enum1)
	alias2 := schema.TestNewAliasConstraint("OtherStatus", enum2)

	// These comparisons should not panic
	assert.True(t, schema.TestConstraintEqual(alias1, alias2))
	assert.True(t, schema.TestConstraintEqual(alias2, alias1))
	assert.True(t, schema.TestConstraintEqual(alias1, enum1))
	assert.True(t, schema.TestConstraintEqual(enum1, alias1))
}

func TestAliasConstraint_Equal_WithPatternConstraint(t *testing.T) {
	// PatternConstraint contains a slice, which is non-hashable.
	// This test verifies that comparing aliases resolved to PatternConstraint
	// does not panic due to hashability issues.
	p := regexp.MustCompile("^test")
	pattern1 := schema.TestParsedPatternConstraint(p)
	pattern2 := schema.TestParsedPatternConstraint(p)

	alias1 := schema.TestNewAliasConstraint("Code", pattern1)
	alias2 := schema.TestNewAliasConstraint("OtherCode", pattern2)

	// These comparisons should not panic
	assert.True(t, schema.TestConstraintEqual(alias1, alias2))
	assert.True(t, schema.TestConstraintEqual(alias2, alias1))
	assert.True(t, schema.TestConstraintEqual(alias1, pattern1))
	assert.True(t, schema.TestConstraintEqual(pattern1, alias1))
}

func TestAliasConstraint_Equal_DeepChainWithEnum(t *testing.T) {
	// Test deep alias chain with EnumConstraint at the terminal
	// A -> B -> C -> EnumConstraint
	enum := schema.TestParsedEnumConstraint([]string{"x", "y", "z"})
	aliasC := schema.TestNewAliasConstraint("C", enum)
	aliasB := schema.TestNewAliasConstraint("B", aliasC)
	aliasA := schema.TestNewAliasConstraint("A", aliasB)

	// All comparisons should work without panic
	assert.True(t, schema.TestConstraintEqual(aliasA, enum))
	assert.True(t, schema.TestConstraintEqual(aliasB, enum))
	assert.True(t, schema.TestConstraintEqual(aliasC, enum))
	assert.True(t, schema.TestConstraintEqual(aliasA, aliasB))
	assert.True(t, schema.TestConstraintEqual(aliasA, aliasC))
}

func TestAliasConstraint_Equal_DeepChainWithPattern(t *testing.T) {
	// Test deep alias chain with PatternConstraint at the terminal
	// A -> B -> C -> PatternConstraint
	p := regexp.MustCompile("^[a-z]+$")
	pattern := schema.TestParsedPatternConstraint(p)
	aliasC := schema.TestNewAliasConstraint("C", pattern)
	aliasB := schema.TestNewAliasConstraint("B", aliasC)
	aliasA := schema.TestNewAliasConstraint("A", aliasB)

	// All comparisons should work without panic
	assert.True(t, schema.TestConstraintEqual(aliasA, pattern))
	assert.True(t, schema.TestConstraintEqual(aliasB, pattern))
	assert.True(t, schema.TestConstraintEqual(aliasC, pattern))
	assert.True(t, schema.TestConstraintEqual(aliasA, aliasB))
	assert.True(t, schema.TestConstraintEqual(aliasA, aliasC))
}

// These tests verify that Equal and IsResolved terminate correctly
// when alias chains contain unresolved terminals (which triggers the
// AliasConstraint type check in the cycle-safety guards).

func TestAliasConstraint_Equal_UnresolvedChain(t *testing.T) {
	// Chain with unresolved terminal: outer -> inner (unresolved)
	// Equal should return false without hanging.
	unresolvedInner := schema.TestNewAliasConstraint("Inner", nil)
	outerAlias := schema.TestNewAliasConstraint("Outer", unresolvedInner)

	terminal := schema.TestNewStringConstraint()

	// These should terminate and return false (cycle-safety guard triggered)
	assert.False(t, schema.TestConstraintEqual(outerAlias, terminal))
	assert.False(t, schema.TestConstraintEqual(outerAlias, unresolvedInner))
}

func TestAliasConstraint_Equal_DeepUnresolvedChain(t *testing.T) {
	// Deep chain with unresolved terminal: A -> B -> C (unresolved)
	unresolvedC := schema.TestNewAliasConstraint("C", nil)
	aliasB := schema.TestNewAliasConstraint("B", unresolvedC)
	aliasA := schema.TestNewAliasConstraint("A", aliasB)

	terminal := schema.TestNewIntegerConstraint()

	// All should terminate and return false
	assert.False(t, schema.TestConstraintEqual(aliasA, terminal))
	assert.False(t, schema.TestConstraintEqual(aliasB, terminal))
	assert.False(t, schema.TestConstraintEqual(aliasA, aliasB))
}

func TestAliasConstraint_IsResolved_DeepUnresolvedChain(t *testing.T) {
	// Deep chain with unresolved terminal: A -> B -> C (unresolved)
	unresolvedC := schema.TestNewAliasConstraint("C", nil)
	aliasB := schema.TestNewAliasConstraint("B", unresolvedC)
	aliasA := schema.TestNewAliasConstraint("A", aliasB)

	// All should terminate and return false
	assert.False(t, schema.TestConstraintIsResolved(aliasA))
	assert.False(t, schema.TestConstraintIsResolved(aliasB))
	assert.False(t, schema.TestConstraintIsResolved(unresolvedC))
}

func TestStringConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical unbounded",
			parent: schema.TestNewStringConstraint(),
			child:  schema.TestNewStringConstraint(),
			want:   true,
		},
		{
			name:   "identical bounded",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestStringLenBetween(1, 100),
			want:   true,
		},
		{
			name:   "raise min",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestStringLenBetween(5, 100),
			want:   true,
		},
		{
			name:   "lower max",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestStringLenBetween(1, 50),
			want:   true,
		},
		{
			name:   "raise min and lower max",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestStringLenBetween(5, 50),
			want:   true,
		},
		{
			name:   "lower min rejected",
			parent: schema.TestStringLenBetween(5, 100),
			child:  schema.TestStringLenBetween(1, 100),
			want:   false,
		},
		{
			name:   "raise max rejected",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestStringLenBetween(1, 200),
			want:   false,
		},
		{
			name:   "add upper bound to unbounded parent",
			parent: schema.TestStringMinLen(1),
			child:  schema.TestStringLenBetween(1, 100),
			want:   true,
		},
		{
			name:   "remove upper bound rejected",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestStringMinLen(1),
			want:   false,
		},
		{
			name:   "unbounded parent to bounded child",
			parent: schema.TestNewStringConstraint(),
			child:  schema.TestStringLenBetween(5, 50),
			want:   true,
		},
		{
			name:   "bounded parent to unbounded child rejected",
			parent: schema.TestStringLenBetween(1, 100),
			child:  schema.TestNewStringConstraint(),
			want:   false,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.TestNewStringConstraint(),
			child:  schema.TestNewIntegerConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintNarrowsTo(tt.parent, tt.child))
		})
	}
}

func TestIntegerConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical unbounded",
			parent: schema.TestNewIntegerConstraint(),
			child:  schema.TestNewIntegerConstraint(),
			want:   true,
		},
		{
			name:   "identical bounded",
			parent: schema.TestIntegerBetween(0, 100),
			child:  schema.TestIntegerBetween(0, 100),
			want:   true,
		},
		{
			name:   "raise min",
			parent: schema.TestIntegerBetween(0, 100),
			child:  schema.TestIntegerBetween(10, 100),
			want:   true,
		},
		{
			name:   "lower max",
			parent: schema.TestIntegerBetween(0, 100),
			child:  schema.TestIntegerBetween(0, 50),
			want:   true,
		},
		{
			name:   "lower min rejected",
			parent: schema.TestIntegerBetween(10, 100),
			child:  schema.TestIntegerBetween(0, 100),
			want:   false,
		},
		{
			name:   "raise max rejected",
			parent: schema.TestIntegerBetween(0, 100),
			child:  schema.TestIntegerBetween(0, 200),
			want:   false,
		},
		{
			name:   "unbounded to bounded",
			parent: schema.TestNewIntegerConstraint(),
			child:  schema.TestIntegerBetween(0, 100),
			want:   true,
		},
		{
			name:   "add upper bound",
			parent: schema.TestIntegerMin(0),
			child:  schema.TestIntegerBetween(0, 100),
			want:   true,
		},
		{
			name:   "remove lower bound rejected",
			parent: schema.TestIntegerBetween(0, 100),
			child:  schema.TestIntegerMax(100),
			want:   false,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.TestNewIntegerConstraint(),
			child:  schema.TestNewFloatConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintNarrowsTo(tt.parent, tt.child))
		})
	}
}

func TestFloatConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical unbounded",
			parent: schema.TestNewFloatConstraint(),
			child:  schema.TestNewFloatConstraint(),
			want:   true,
		},
		{
			name:   "raise min",
			parent: schema.TestFloatBetween(0.0, 100.0),
			child:  schema.TestFloatBetween(10.0, 100.0),
			want:   true,
		},
		{
			name:   "lower max",
			parent: schema.TestFloatBetween(0.0, 100.0),
			child:  schema.TestFloatBetween(0.0, 50.0),
			want:   true,
		},
		{
			name:   "lower min rejected",
			parent: schema.TestFloatBetween(10.0, 100.0),
			child:  schema.TestFloatBetween(0.0, 100.0),
			want:   false,
		},
		{
			name:   "raise max rejected",
			parent: schema.TestFloatBetween(0.0, 100.0),
			child:  schema.TestFloatBetween(0.0, 200.0),
			want:   false,
		},
		{
			name:   "unbounded to bounded",
			parent: schema.TestNewFloatConstraint(),
			child:  schema.TestFloatBetween(0.0, 100.0),
			want:   true,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.TestNewFloatConstraint(),
			child:  schema.TestNewIntegerConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintNarrowsTo(tt.parent, tt.child))
		})
	}
}

func TestEnumConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical",
			parent: schema.TestParsedEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.TestParsedEnumConstraint([]string{"a", "b", "c"}),
			want:   true,
		},
		{
			name:   "subset",
			parent: schema.TestParsedEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.TestParsedEnumConstraint([]string{"a", "c"}),
			want:   true,
		},
		{
			name:   "single value",
			parent: schema.TestParsedEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.TestParsedEnumConstraint([]string{"b"}),
			want:   true,
		},
		{
			name:   "superset rejected",
			parent: schema.TestParsedEnumConstraint([]string{"a", "b"}),
			child:  schema.TestParsedEnumConstraint([]string{"a", "b", "c"}),
			want:   false,
		},
		{
			name:   "different values rejected",
			parent: schema.TestParsedEnumConstraint([]string{"a", "b"}),
			child:  schema.TestParsedEnumConstraint([]string{"x", "y"}),
			want:   false,
		},
		{
			name:   "empty child",
			parent: schema.TestParsedEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.TestParsedEnumConstraint([]string{}),
			want:   true,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.TestParsedEnumConstraint([]string{"a"}),
			child:  schema.TestNewStringConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintNarrowsTo(tt.parent, tt.child))
		})
	}
}

func TestNonNarrowableConstraints_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "boolean same",
			parent: schema.TestNewBooleanConstraint(),
			child:  schema.TestNewBooleanConstraint(),
			want:   true,
		},
		{
			name:   "boolean wrong kind",
			parent: schema.TestNewBooleanConstraint(),
			child:  schema.TestNewStringConstraint(),
			want:   false,
		},
		{
			name:   "date same",
			parent: schema.TestNewDateConstraint(),
			child:  schema.TestNewDateConstraint(),
			want:   true,
		},
		{
			name:   "date wrong kind",
			parent: schema.TestNewDateConstraint(),
			child:  schema.TestNewTimestampConstraint(),
			want:   false,
		},
		{
			name:   "uuid same",
			parent: schema.TestNewUUIDConstraint(),
			child:  schema.TestNewUUIDConstraint(),
			want:   true,
		},
		{
			name:   "uuid wrong kind",
			parent: schema.TestNewUUIDConstraint(),
			child:  schema.TestNewStringConstraint(),
			want:   false,
		},
		{
			name:   "timestamp same",
			parent: schema.TestNewTimestampConstraint(),
			child:  schema.TestNewTimestampConstraint(),
			want:   true,
		},
		{
			name:   "timestamp to date",
			parent: schema.TestNewTimestampConstraint(),
			child:  schema.TestNewDateConstraint(),
			want:   false,
		},
		{
			name:   "pattern same",
			parent: schema.TestParsedPatternConstraint(regexp.MustCompile("^test")),
			child:  schema.TestParsedPatternConstraint(regexp.MustCompile("^test")),
			want:   true,
		},
		{
			name:   "pattern different",
			parent: schema.TestParsedPatternConstraint(regexp.MustCompile("^a")),
			child:  schema.TestParsedPatternConstraint(regexp.MustCompile("^b")),
			want:   false,
		},
		{
			name:   "vector same",
			parent: schema.TestNewVectorConstraint(128),
			child:  schema.TestNewVectorConstraint(128),
			want:   true,
		},
		{
			name:   "vector different dims",
			parent: schema.TestNewVectorConstraint(128),
			child:  schema.TestNewVectorConstraint(256),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintNarrowsTo(tt.parent, tt.child))
		})
	}
}

// A kind that takes no parameter is equal to, and narrows to, itself and a
// datatype over itself, and no other kind.
func TestParameterlessConstraints_EqualAndNarrowThroughADatatype(t *testing.T) {
	t.Parallel()
	kinds := []schema.Constraint{
		schema.TestNewBooleanConstraint(),
		schema.TestNewTimestampConstraint(),
		schema.TestNewDateConstraint(),
		schema.TestNewUUIDConstraint(),
	}
	for _, a := range kinds {
		for _, b := range kinds {
			want := a.Kind() == b.Kind()
			for _, other := range []schema.Constraint{b, schema.TestNewAliasConstraint("Named", b)} {
				if got := schema.TestConstraintEqual(a, other); got != want {
					t.Errorf("%s.Equal(%s) = %t, want %t", a, other, got, want)
				}
				if got := schema.TestConstraintNarrowsTo(a, other); got != want {
					t.Errorf("%s.NarrowsTo(%s) = %t, want %t", a, other, got, want)
				}
			}
		}
	}
}

func TestAliasConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	t.Run("alias wrapping Integer narrows to narrower Integer", func(t *testing.T) {
		t.Parallel()
		parent := schema.TestNewAliasConstraint("Age",
			schema.TestIntegerBetween(0, 150))
		child := schema.TestIntegerBetween(18, 150)

		assert.True(t, schema.TestConstraintNarrowsTo(parent, child))
	})

	t.Run("alias wrapping Integer does not narrow to wider Integer", func(t *testing.T) {
		t.Parallel()
		parent := schema.TestNewAliasConstraint("AdultAge",
			schema.TestIntegerBetween(18, 150))
		child := schema.TestIntegerBetween(0, 150)

		assert.False(t, schema.TestConstraintNarrowsTo(parent, child))
	})

	t.Run("child is alias wrapping narrower Integer", func(t *testing.T) {
		t.Parallel()
		parent := schema.TestIntegerBetween(0, 150)
		child := schema.TestNewAliasConstraint("AdultAge",
			schema.TestIntegerBetween(18, 150))

		assert.True(t, schema.TestConstraintNarrowsTo(parent, child))
	})

	t.Run("both aliases wrapping compatible constraints", func(t *testing.T) {
		t.Parallel()
		parent := schema.TestNewAliasConstraint("Age",
			schema.TestIntegerBetween(0, 150))
		child := schema.TestNewAliasConstraint("AdultAge",
			schema.TestIntegerBetween(18, 150))

		assert.True(t, schema.TestConstraintNarrowsTo(parent, child))
	})

	t.Run("unresolved alias returns false", func(t *testing.T) {
		t.Parallel()
		unresolved := schema.TestNewAliasConstraint("Unknown", nil)
		child := schema.TestNewIntegerConstraint()

		assert.False(t, schema.TestConstraintNarrowsTo(unresolved, child))
	})

	t.Run("deep alias chain narrows correctly", func(t *testing.T) {
		t.Parallel()
		// A -> B -> Integer[0,150]
		terminal := schema.TestIntegerBetween(0, 150)
		aliasB := schema.TestNewAliasConstraint("B", terminal)
		aliasA := schema.TestNewAliasConstraint("A", aliasB)

		child := schema.TestIntegerBetween(18, 120)
		assert.True(t, schema.TestConstraintNarrowsTo(aliasA, child))
	})

	t.Run("deep alias chain rejects widening", func(t *testing.T) {
		t.Parallel()
		terminal := schema.TestIntegerBetween(18, 65)
		aliasB := schema.TestNewAliasConstraint("B", terminal)
		aliasA := schema.TestNewAliasConstraint("A", aliasB)

		child := schema.TestIntegerBetween(0, 150)
		assert.False(t, schema.TestConstraintNarrowsTo(aliasA, child))
	})

	t.Run("alias wrapping enum narrows to subset", func(t *testing.T) {
		t.Parallel()
		parent := schema.TestNewAliasConstraint("Status",
			schema.TestParsedEnumConstraint([]string{"active", "inactive", "pending"}))
		child := schema.TestParsedEnumConstraint([]string{"active", "inactive"})

		assert.True(t, schema.TestConstraintNarrowsTo(parent, child))
	})
}

func TestListConstraint_Kind(t *testing.T) {
	t.Parallel()
	c := schema.TestNewListConstraint(schema.TestNewStringConstraint())
	assert.Equal(t, schema.KindList, c.Kind())
}

func TestListConstraint_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		c    schema.ListConstraint
		want string
	}{
		{
			name: "bare",
			c:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			want: "List<String>",
		},
		{
			name: "element constrained",
			c:    schema.TestNewListConstraint(schema.TestStringMaxLen(6)),
			want: "List<String[_, 6]>",
		},
		{
			name: "list bounded",
			c:    schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 5),
			want: "List<String>[1, 5]",
		},
		{
			name: "both constrained",
			c:    schema.TestListLenBetween(schema.TestStringMaxLen(6), 1, 5),
			want: "List<String[_, 6]>[1, 5]",
		},
		{
			name: "nested",
			c:    schema.TestNewListConstraint(schema.TestNewListConstraint(schema.TestNewIntegerConstraint())),
			want: "List<List<Integer>>",
		},
		{
			name: "one-sided min",
			c:    schema.TestListMinLen(schema.TestNewStringConstraint(), 1),
			want: "List<String>[1, _]",
		},
		{
			name: "one-sided max",
			c:    schema.TestListMaxLen(schema.TestNewStringConstraint(), 10),
			want: "List<String>[_, 10]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.c.String())
		})
	}
}

func TestListConstraint_Equal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b schema.Constraint
		want bool
	}{
		{
			name: "same bare",
			a:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			b:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			want: true,
		},
		{
			name: "same bounded",
			a:    schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 5),
			b:    schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 5),
			want: true,
		},
		{
			name: "different element",
			a:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			b:    schema.TestNewListConstraint(schema.TestNewIntegerConstraint()),
			want: false,
		},
		{
			name: "different bounds",
			a:    schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 5),
			b:    schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 10),
			want: false,
		},
		{
			name: "bare vs bounded",
			a:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			b:    schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 5),
			want: false,
		},
		{
			name: "non-list",
			a:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			b:    schema.TestNewStringConstraint(),
			want: false,
		},
		{
			name: "alias wrapping list",
			a:    schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			b:    schema.TestNewAliasConstraint("Tags", schema.TestNewListConstraint(schema.TestNewStringConstraint())),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintEqual(tt.a, tt.b))
		})
	}
}

func TestListConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "bare to bare same element",
			parent: schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			child:  schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			want:   true,
		},
		{
			name:   "bare to bounded",
			parent: schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			child:  schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 10),
			want:   true,
		},
		{
			name:   "bounded to tighter bounds",
			parent: schema.TestListLenBetween(schema.TestNewStringConstraint(), 0, 100),
			child:  schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 50),
			want:   true,
		},
		{
			name:   "bounded to wider bounds fails",
			parent: schema.TestListLenBetween(schema.TestNewStringConstraint(), 5, 10),
			child:  schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 10),
			want:   false,
		},
		{
			name:   "element narrows",
			parent: schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			child:  schema.TestNewListConstraint(schema.TestStringLenBetween(1, 50)),
			want:   true,
		},
		{
			name:   "element widens fails",
			parent: schema.TestNewListConstraint(schema.TestStringLenBetween(1, 50)),
			child:  schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			want:   false,
		},
		{
			name:   "different element kind fails",
			parent: schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			child:  schema.TestNewListConstraint(schema.TestNewIntegerConstraint()),
			want:   false,
		},
		{
			name:   "non-list child fails",
			parent: schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			child:  schema.TestNewStringConstraint(),
			want:   false,
		},
		{
			name:   "child lacks max when parent has max fails",
			parent: schema.TestListMaxLen(schema.TestNewStringConstraint(), 10),
			child:  schema.TestNewListConstraint(schema.TestNewStringConstraint()),
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, schema.TestConstraintNarrowsTo(tt.parent, tt.child))
		})
	}
}

func TestListConstraint_IsResolved(t *testing.T) {
	t.Parallel()

	t.Run("resolved element", func(t *testing.T) {
		t.Parallel()
		c := schema.TestNewListConstraint(schema.TestNewStringConstraint())
		assert.True(t, schema.TestConstraintIsResolved(c))
	})

	t.Run("unresolved alias element", func(t *testing.T) {
		t.Parallel()
		c := schema.TestNewListConstraint(schema.TestNewAliasConstraint("Unknown", nil))
		assert.False(t, schema.TestConstraintIsResolved(c))
	})

	t.Run("resolved alias element", func(t *testing.T) {
		t.Parallel()
		c := schema.TestNewListConstraint(schema.TestNewAliasConstraint("MyStr", schema.TestNewStringConstraint()))
		assert.True(t, schema.TestConstraintIsResolved(c))
	})
}

func TestListConstraint_Accessors(t *testing.T) {
	t.Parallel()

	t.Run("element", func(t *testing.T) {
		t.Parallel()
		elem := schema.TestNewStringConstraint()
		c := schema.TestNewListConstraint(elem)
		assert.Equal(t, elem, c.Element())
	})

	t.Run("bounds present", func(t *testing.T) {
		t.Parallel()
		c := schema.TestListLenBetween(schema.TestNewStringConstraint(), 1, 5)
		minVal, hasMin := c.MinLen()
		maxVal, hasMax := c.MaxLen()
		assert.True(t, hasMin)
		assert.Equal(t, int64(1), minVal)
		assert.True(t, hasMax)
		assert.Equal(t, int64(5), maxVal)
	})

	t.Run("bounds absent", func(t *testing.T) {
		t.Parallel()
		c := schema.TestNewListConstraint(schema.TestNewStringConstraint())
		_, hasMin := c.MinLen()
		_, hasMax := c.MaxLen()
		assert.False(t, hasMin)
		assert.False(t, hasMax)
	})
}

// Equal is an equivalence relation, so it must be reflexive for every alias
// shape — including a chain that cannot be resolved to a terminal constraint.
// Callers rely on it: inheritance merging treats a property that is not equal
// to itself as conflicting with itself, and reports one declaration as both
// sides of its own conflict.
//
// Stdlib-only by design; this file's testify imports are grandfathered and new
// assertions must not add to them.
func TestAliasConstraint_Equal_ReflexiveWhenUndecidable(t *testing.T) {
	unresolved := schema.TestNewAliasConstraint("ext.Amount", nil)
	nested := schema.TestNewAliasConstraint("Money", unresolved)
	deep := schema.TestNewAliasConstraint("Wrapper", nested)
	cyclic := schema.TestNewAliasConstraint("Loop", schema.TestNewAliasConstraint("Loop", nil))

	for _, c := range []schema.Constraint{unresolved, nested, deep, cyclic} {
		if !schema.TestConstraintEqual(c, c) {
			t.Errorf("%T(%s) must equal itself", c, c.String())
		}
	}

	// Distinct datatypes stay unequal: nothing here can show two unresolvable
	// aliases mean the same thing, so undecidable must not collapse to "equal".
	if schema.TestConstraintEqual(nested, schema.TestNewAliasConstraint("Cash", unresolved)) {
		t.Error("aliases naming different datatypes must not compare equal merely because both are unresolvable")
	}
}

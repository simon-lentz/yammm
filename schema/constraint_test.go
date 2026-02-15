package schema_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/schema"
)

func TestConstraint_String(t *testing.T) {
	tests := []struct {
		name       string
		constraint schema.Constraint
		expected   string
	}{
		{"String", schema.NewStringConstraint(), "String"},
		{"String bounded", schema.NewStringConstraintBounded(1, 100), "String[1, 100]"},
		{"String min only", schema.NewStringConstraintBounded(1, -1), "String[1, _]"},
		{"String max only", schema.NewStringConstraintBounded(-1, 100), "String[_, 100]"},
		{"Integer", schema.NewIntegerConstraint(), "Integer"},
		{"Integer bounded", schema.NewIntegerConstraintBounded(0, true, 100, true), "Integer[0, 100]"},
		{"Float", schema.NewFloatConstraint(), "Float"},
		{"Boolean", schema.NewBooleanConstraint(), "Boolean"},
		{"Timestamp", schema.NewTimestampConstraint(), "Timestamp"},
		{"Timestamp formatted", schema.NewTimestampConstraintFormatted("2006-01-02"), `Timestamp["2006-01-02"]`},
		{"Date", schema.NewDateConstraint(), "Date"},
		{"UUID", schema.NewUUIDConstraint(), "UUID"},
		{"Enum", schema.NewEnumConstraint([]string{"a", "b"}), `Enum["a", "b"]`},
		{"Vector", schema.NewVectorConstraint(128), "Vector[128]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.constraint.String())
		})
	}
}

func TestConstraint_Kind(t *testing.T) {
	assert.Equal(t, schema.KindString, schema.NewStringConstraint().Kind())
	assert.Equal(t, schema.KindInteger, schema.NewIntegerConstraint().Kind())
	assert.Equal(t, schema.KindFloat, schema.NewFloatConstraint().Kind())
	assert.Equal(t, schema.KindBoolean, schema.NewBooleanConstraint().Kind())
	assert.Equal(t, schema.KindTimestamp, schema.NewTimestampConstraint().Kind())
	assert.Equal(t, schema.KindDate, schema.NewDateConstraint().Kind())
	assert.Equal(t, schema.KindUUID, schema.NewUUIDConstraint().Kind())
	assert.Equal(t, schema.KindEnum, schema.NewEnumConstraint([]string{"a"}).Kind())
	assert.Equal(t, schema.KindVector, schema.NewVectorConstraint(128).Kind())
}

func TestConstraint_Equal(t *testing.T) {
	t.Run("same constraints are equal", func(t *testing.T) {
		c1 := schema.NewStringConstraintBounded(1, 100)
		c2 := schema.NewStringConstraintBounded(1, 100)
		assert.True(t, c1.Equal(c2))
	})

	t.Run("different bounds are not equal", func(t *testing.T) {
		c1 := schema.NewStringConstraintBounded(1, 100)
		c2 := schema.NewStringConstraintBounded(1, 200)
		assert.False(t, c1.Equal(c2))
	})

	t.Run("different kinds are not equal", func(t *testing.T) {
		c1 := schema.NewStringConstraint()
		c2 := schema.NewIntegerConstraint()
		assert.False(t, c1.Equal(c2))
	})

	t.Run("enum set equality", func(t *testing.T) {
		c1 := schema.NewEnumConstraint([]string{"a", "b", "c"})
		c2 := schema.NewEnumConstraint([]string{"c", "b", "a"}) // different order
		assert.True(t, c1.Equal(c2), "Enum equality should be set-based")
	})

	t.Run("enum different values", func(t *testing.T) {
		c1 := schema.NewEnumConstraint([]string{"a", "b"})
		c2 := schema.NewEnumConstraint([]string{"a", "c"})
		assert.False(t, c1.Equal(c2))
	})
}

func TestEnumConstraint_DefensiveCopy(t *testing.T) {
	original := []string{"a", "b", "c"}
	c := schema.NewEnumConstraint(original)

	// Modify original
	original[0] = "modified"

	// Constraint should be unaffected
	values := c.Values()
	assert.Equal(t, "a", values[0], "EnumConstraint should make defensive copy")

	// Modify returned values
	values[1] = "modified"

	// Constraint should still be unaffected
	values2 := c.Values()
	assert.Equal(t, "b", values2[1], "Values() should return defensive copy")
}

func TestPatternConstraint_MaxTwoPatterns(t *testing.T) {
	p1 := regexp.MustCompile("^a")
	p2 := regexp.MustCompile("b$")
	p3 := regexp.MustCompile("c")

	c := schema.NewPatternConstraint([]*regexp.Regexp{p1, p2, p3})
	patterns := c.Patterns()

	assert.Len(t, patterns, 2, "PatternConstraint should limit to 2 patterns")
}

func TestAliasConstraint_ResolvesForEquality(t *testing.T) {
	underlying := schema.NewStringConstraintBounded(1, 100)
	alias := schema.NewAliasConstraint("Name", underlying)

	// Alias should equal its resolved constraint
	assert.True(t, alias.Equal(underlying))

	// And vice versa
	assert.True(t, underlying.Equal(alias))

	// Two aliases with same resolved constraint should be equal
	alias2 := schema.NewAliasConstraint("DifferentName", underlying)
	assert.True(t, alias.Equal(alias2))
}

func TestAliasConstraint_UnresolvedEquality(t *testing.T) {
	t.Run("unresolved aliases with same datatype name are equal", func(t *testing.T) {
		// Create unresolved aliases (nil underlying)
		alias1 := schema.NewAliasConstraint("Name", nil)
		alias2 := schema.NewAliasConstraint("Name", nil)
		assert.True(t, alias1.Equal(alias2), "unresolved aliases with same datatype name should be equal")
	})

	t.Run("unresolved aliases with different datatype names are not equal", func(t *testing.T) {
		alias1 := schema.NewAliasConstraint("Name", nil)
		alias2 := schema.NewAliasConstraint("Email", nil)
		assert.False(t, alias1.Equal(alias2), "unresolved aliases with different datatype names should not be equal")
	})

	t.Run("unresolved alias is not equal to resolved alias", func(t *testing.T) {
		unresolved := schema.NewAliasConstraint("Name", nil)
		resolved := schema.NewAliasConstraint("Name", schema.NewStringConstraint())
		assert.False(t, unresolved.Equal(resolved), "unresolved alias should not equal resolved alias")
	})

	t.Run("unresolved alias is not equal to non-alias constraint", func(t *testing.T) {
		unresolved := schema.NewAliasConstraint("Name", nil)
		assert.False(t, unresolved.Equal(schema.NewStringConstraint()), "unresolved alias should not equal non-alias")
	})
}

func TestAliasConstraint_Kind(t *testing.T) {
	t.Run("alias always returns KindAlias", func(t *testing.T) {
		// AliasConstraint.Kind() returns what this constraint IS (an alias)
		// not what it resolves to
		underlying := schema.NewStringConstraint()
		alias := schema.NewAliasConstraint("Name", underlying)
		assert.Equal(t, schema.KindAlias, alias.Kind())

		// To get the resolved kind, use Resolved().Kind()
		assert.Equal(t, schema.KindString, alias.Resolved().Kind())
	})

	t.Run("unresolved alias returns KindAlias", func(t *testing.T) {
		alias := schema.NewAliasConstraint("Name", nil)
		assert.Equal(t, schema.KindAlias, alias.Kind())
	})
}

// --- StringConstraint Accessor Tests ---

func TestStringConstraint_MinLen(t *testing.T) {
	c := schema.NewStringConstraintBounded(5, 100)
	minLen, hasMin := c.MinLen()
	assert.True(t, hasMin)
	assert.Equal(t, int64(5), minLen)
}

func TestStringConstraint_MaxLen(t *testing.T) {
	c := schema.NewStringConstraintBounded(5, 100)
	maxLen, hasMax := c.MaxLen()
	assert.True(t, hasMax)
	assert.Equal(t, int64(100), maxLen)
}

func TestStringConstraint_MinLen_Unbounded(t *testing.T) {
	c := schema.NewStringConstraint()
	_, hasMin := c.MinLen()
	assert.False(t, hasMin)
}

func TestStringConstraint_MaxLen_Unbounded(t *testing.T) {
	c := schema.NewStringConstraint()
	_, hasMax := c.MaxLen()
	assert.False(t, hasMax)
}

// --- IntegerConstraint Accessor Tests ---

func TestIntegerConstraint_Min(t *testing.T) {
	c := schema.NewIntegerConstraintBounded(10, true, 100, true)
	min, hasMin := c.Min()
	assert.True(t, hasMin)
	assert.Equal(t, int64(10), min)
}

func TestIntegerConstraint_Max(t *testing.T) {
	c := schema.NewIntegerConstraintBounded(10, true, 100, true)
	max, hasMax := c.Max()
	assert.True(t, hasMax)
	assert.Equal(t, int64(100), max)
}

func TestIntegerConstraint_Min_Unbounded(t *testing.T) {
	c := schema.NewIntegerConstraint()
	_, hasMin := c.Min()
	assert.False(t, hasMin)
}

func TestIntegerConstraint_Max_Unbounded(t *testing.T) {
	c := schema.NewIntegerConstraint()
	_, hasMax := c.Max()
	assert.False(t, hasMax)
}

func TestIntegerConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewIntegerConstraintBounded(10, true, 100, true)
	c2 := schema.NewIntegerConstraintBounded(10, true, 100, true)
	assert.True(t, c1.Equal(c2))
}

func TestIntegerConstraint_Equal_DifferentMin(t *testing.T) {
	c1 := schema.NewIntegerConstraintBounded(10, true, 100, true)
	c2 := schema.NewIntegerConstraintBounded(20, true, 100, true)
	assert.False(t, c1.Equal(c2))
}

func TestIntegerConstraint_Equal_DifferentMax(t *testing.T) {
	c1 := schema.NewIntegerConstraintBounded(10, true, 100, true)
	c2 := schema.NewIntegerConstraintBounded(10, true, 200, true)
	assert.False(t, c1.Equal(c2))
}

// --- FloatConstraint Accessor Tests ---

func TestFloatConstraint_Min(t *testing.T) {
	c := schema.NewFloatConstraintBounded(1.5, true, 10.5, true)
	min, hasMin := c.Min()
	assert.True(t, hasMin)
	assert.Equal(t, 1.5, min)
}

func TestFloatConstraint_Max(t *testing.T) {
	c := schema.NewFloatConstraintBounded(1.5, true, 10.5, true)
	max, hasMax := c.Max()
	assert.True(t, hasMax)
	assert.Equal(t, 10.5, max)
}

func TestFloatConstraint_Min_Unbounded(t *testing.T) {
	c := schema.NewFloatConstraint()
	_, hasMin := c.Min()
	assert.False(t, hasMin)
}

func TestFloatConstraint_Max_Unbounded(t *testing.T) {
	c := schema.NewFloatConstraint()
	_, hasMax := c.Max()
	assert.False(t, hasMax)
}

func TestFloatConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewFloatConstraintBounded(1.5, true, 10.5, true)
	c2 := schema.NewFloatConstraintBounded(1.5, true, 10.5, true)
	assert.True(t, c1.Equal(c2))
}

func TestFloatConstraint_Equal_DifferentMin(t *testing.T) {
	c1 := schema.NewFloatConstraintBounded(1.5, true, 10.5, true)
	c2 := schema.NewFloatConstraintBounded(2.0, true, 10.5, true)
	assert.False(t, c1.Equal(c2))
}

// --- BooleanConstraint Tests ---

func TestBooleanConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewBooleanConstraint()
	c2 := schema.NewBooleanConstraint()
	assert.True(t, c1.Equal(c2))
}

func TestBooleanConstraint_Equal_DifferentType(t *testing.T) {
	c1 := schema.NewBooleanConstraint()
	c2 := schema.NewStringConstraint()
	assert.False(t, c1.Equal(c2))
}

// --- TimestampConstraint Tests ---

func TestTimestampConstraint_Format(t *testing.T) {
	c := schema.NewTimestampConstraintFormatted("2006-01-02")
	format := c.Format()
	assert.Equal(t, "2006-01-02", format)
}

func TestTimestampConstraint_Format_NoFormat(t *testing.T) {
	c := schema.NewTimestampConstraint()
	format := c.Format()
	assert.Equal(t, "", format)
}

func TestTimestampConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewTimestampConstraintFormatted("2006-01-02")
	c2 := schema.NewTimestampConstraintFormatted("2006-01-02")
	assert.True(t, c1.Equal(c2))
}

func TestTimestampConstraint_Equal_DifferentFormat(t *testing.T) {
	c1 := schema.NewTimestampConstraintFormatted("2006-01-02")
	c2 := schema.NewTimestampConstraintFormatted("2006-01-02T15:04:05Z07:00")
	assert.False(t, c1.Equal(c2))
}

func TestTimestampConstraint_Equal_NoFormatBoth(t *testing.T) {
	c1 := schema.NewTimestampConstraint()
	c2 := schema.NewTimestampConstraint()
	assert.True(t, c1.Equal(c2))
}

// --- DateConstraint Tests ---

func TestDateConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewDateConstraint()
	c2 := schema.NewDateConstraint()
	assert.True(t, c1.Equal(c2))
}

func TestDateConstraint_Equal_DifferentType(t *testing.T) {
	c1 := schema.NewDateConstraint()
	c2 := schema.NewTimestampConstraint()
	assert.False(t, c1.Equal(c2))
}

// --- UUIDConstraint Tests ---

func TestUUIDConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewUUIDConstraint()
	c2 := schema.NewUUIDConstraint()
	assert.True(t, c1.Equal(c2))
}

func TestUUIDConstraint_Equal_DifferentType(t *testing.T) {
	c1 := schema.NewUUIDConstraint()
	c2 := schema.NewStringConstraint()
	assert.False(t, c1.Equal(c2))
}

// --- PatternConstraint Tests ---

func TestPatternConstraint_Kind(t *testing.T) {
	p := regexp.MustCompile("^test")
	c := schema.NewPatternConstraint([]*regexp.Regexp{p})
	assert.Equal(t, schema.KindPattern, c.Kind())
}

func TestPatternConstraint_Patterns(t *testing.T) {
	p1 := regexp.MustCompile("^test")
	p2 := regexp.MustCompile("end$")
	c := schema.NewPatternConstraint([]*regexp.Regexp{p1, p2})

	strs := c.Patterns()
	assert.Len(t, strs, 2)
	assert.Contains(t, strs, "^test")
	assert.Contains(t, strs, "end$")
}

func TestPatternConstraint_PatternCount(t *testing.T) {
	p1 := regexp.MustCompile("^a")
	p2 := regexp.MustCompile("b$")

	c1 := schema.NewPatternConstraint([]*regexp.Regexp{p1})
	assert.Equal(t, 1, c1.PatternCount())

	c2 := schema.NewPatternConstraint([]*regexp.Regexp{p1, p2})
	assert.Equal(t, 2, c2.PatternCount())
}

func TestPatternConstraint_Pattern(t *testing.T) {
	p := regexp.MustCompile("^test")
	c := schema.NewPatternConstraint([]*regexp.Regexp{p})
	assert.Equal(t, "^test", c.Pattern())
}

func TestPatternConstraint_Pattern_Empty(t *testing.T) {
	c := schema.NewPatternConstraint([]*regexp.Regexp{})
	assert.Equal(t, "", c.Pattern())
}

func TestPatternConstraint_CompiledPatterns(t *testing.T) {
	p1 := regexp.MustCompile("^a")
	p2 := regexp.MustCompile("b$")
	c := schema.NewPatternConstraint([]*regexp.Regexp{p1, p2})

	compiled := c.CompiledPatterns()
	assert.Len(t, compiled, 2)
	assert.True(t, compiled[0].MatchString("abc"))
	assert.True(t, compiled[1].MatchString("ab"))
}

func TestPatternConstraint_String(t *testing.T) {
	p := regexp.MustCompile("^hello")
	c := schema.NewPatternConstraint([]*regexp.Regexp{p})
	assert.Equal(t, `Pattern["^hello"]`, c.String())
}

func TestPatternConstraint_String_Multiple(t *testing.T) {
	p1 := regexp.MustCompile("^a")
	p2 := regexp.MustCompile("b$")
	c := schema.NewPatternConstraint([]*regexp.Regexp{p1, p2})
	assert.Equal(t, `Pattern["^a", "b$"]`, c.String())
}

func TestPatternConstraint_Equal_Same(t *testing.T) {
	p := regexp.MustCompile("^test")
	c1 := schema.NewPatternConstraint([]*regexp.Regexp{p})
	c2 := schema.NewPatternConstraint([]*regexp.Regexp{p})
	assert.True(t, c1.Equal(c2))
}

func TestPatternConstraint_Equal_DifferentPattern(t *testing.T) {
	p1 := regexp.MustCompile("^test")
	p2 := regexp.MustCompile("^other")
	c1 := schema.NewPatternConstraint([]*regexp.Regexp{p1})
	c2 := schema.NewPatternConstraint([]*regexp.Regexp{p2})
	assert.False(t, c1.Equal(c2))
}

func TestPatternConstraint_Equal_DifferentCount(t *testing.T) {
	p1 := regexp.MustCompile("^test")
	p2 := regexp.MustCompile("end$")
	c1 := schema.NewPatternConstraint([]*regexp.Regexp{p1})
	c2 := schema.NewPatternConstraint([]*regexp.Regexp{p1, p2})
	assert.False(t, c1.Equal(c2))
}

// --- VectorConstraint Tests ---

func TestVectorConstraint_Dimension(t *testing.T) {
	c := schema.NewVectorConstraint(256)
	assert.Equal(t, 256, c.Dimension())
}

func TestVectorConstraint_Equal_Same(t *testing.T) {
	c1 := schema.NewVectorConstraint(128)
	c2 := schema.NewVectorConstraint(128)
	assert.True(t, c1.Equal(c2))
}

func TestVectorConstraint_Equal_Different(t *testing.T) {
	c1 := schema.NewVectorConstraint(128)
	c2 := schema.NewVectorConstraint(256)
	assert.False(t, c1.Equal(c2))
}

// --- AliasConstraint Tests ---

func TestAliasConstraint_DataTypeName(t *testing.T) {
	c := schema.NewAliasConstraint("Email", schema.NewStringConstraint())
	assert.Equal(t, "Email", c.DataTypeName())
}

func TestAliasConstraint_String(t *testing.T) {
	c := schema.NewAliasConstraint("Email", schema.NewStringConstraint())
	assert.Equal(t, "Email", c.String())
}

func TestAliasConstraint_String_Unresolved(t *testing.T) {
	c := schema.NewAliasConstraint("Email", nil)
	assert.Equal(t, "Email", c.String())
}

func TestFloatConstraint_StringStability(t *testing.T) {
	tests := []struct {
		name     string
		min      float64
		hasMin   bool
		max      float64
		hasMax   bool
		expected string
	}{
		{"no bounds", 0, false, 0, false, "Float"},
		{"min only", 0, true, 0, false, "Float[0, _]"},
		{"max only", 0, false, 100.5, true, "Float[_, 100.5]"},
		{"both bounds", -10.25, true, 10.25, true, "Float[-10.25, 10.25]"},
		{"large values", 1e10, true, 1e15, true, "Float[10000000000, 1000000000000000]"},
		{"small decimals", 0.123456789, true, 0.987654321, true, "Float[0.123456789, 0.987654321]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c schema.Constraint
			switch {
			case tt.hasMin && tt.hasMax:
				c = schema.NewFloatConstraintBounded(tt.min, true, tt.max, true)
			case tt.hasMin:
				c = schema.NewFloatConstraintBounded(tt.min, true, 0, false)
			case tt.hasMax:
				c = schema.NewFloatConstraintBounded(0, false, tt.max, true)
			default:
				c = schema.NewFloatConstraint()
			}
			assert.Equal(t, tt.expected, c.String())
		})
	}
}

// --- IsResolved Tests ---

func TestConstraint_IsResolved_NonAliasConstraints(t *testing.T) {
	// All non-alias constraints are always resolved (they have no references)
	tests := []struct {
		name       string
		constraint schema.Constraint
	}{
		{"String", schema.NewStringConstraint()},
		{"String bounded", schema.NewStringConstraintBounded(1, 100)},
		{"Integer", schema.NewIntegerConstraint()},
		{"Integer bounded", schema.NewIntegerConstraintBounded(0, true, 100, true)},
		{"Float", schema.NewFloatConstraint()},
		{"Float bounded", schema.NewFloatConstraintBounded(1.0, true, 10.0, true)},
		{"Boolean", schema.NewBooleanConstraint()},
		{"Timestamp", schema.NewTimestampConstraint()},
		{"Timestamp formatted", schema.NewTimestampConstraintFormatted("2006-01-02")},
		{"Date", schema.NewDateConstraint()},
		{"UUID", schema.NewUUIDConstraint()},
		{"Enum", schema.NewEnumConstraint([]string{"a", "b"})},
		{"Pattern", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^test")})},
		{"Vector", schema.NewVectorConstraint(128)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.constraint.IsResolved(), "%s constraint should always be resolved", tt.name)
		})
	}
}

func TestAliasConstraint_IsResolved_ResolvedAlias(t *testing.T) {
	underlying := schema.NewStringConstraint()
	alias := schema.NewAliasConstraint("Name", underlying)

	assert.True(t, alias.IsResolved(), "alias with resolved underlying constraint should return true")
}

func TestAliasConstraint_IsResolved_UnresolvedAlias(t *testing.T) {
	alias := schema.NewAliasConstraint("Name", nil)

	assert.False(t, alias.IsResolved(), "alias with nil underlying constraint should return false")
}

func TestAliasConstraint_IsResolved_NestedAlias(t *testing.T) {
	// An alias that resolves to another alias (should be resolved if inner alias is resolved)
	innerAlias := schema.NewAliasConstraint("Inner", schema.NewIntegerConstraint())
	outerAlias := schema.NewAliasConstraint("Outer", innerAlias)

	assert.True(t, outerAlias.IsResolved(), "nested alias should be resolved if underlying is resolved")
}

func TestAliasConstraint_IsResolved_NestedUnresolved(t *testing.T) {
	// Alias chain where the terminal alias is unresolved
	unresolvedInner := schema.NewAliasConstraint("Inner", nil)
	outerAlias := schema.NewAliasConstraint("Outer", unresolvedInner)

	assert.False(t, outerAlias.IsResolved(), "nested alias should be unresolved if terminal is unresolved")
}

func TestAliasConstraint_IsResolved_DeepChain(t *testing.T) {
	// Deep alias chain: A -> B -> C -> Integer
	terminal := schema.NewIntegerConstraint()
	aliasC := schema.NewAliasConstraint("C", terminal)
	aliasB := schema.NewAliasConstraint("B", aliasC)
	aliasA := schema.NewAliasConstraint("A", aliasB)

	assert.True(t, aliasA.IsResolved(), "deep alias chain should be resolved if terminal is resolved")
	assert.True(t, aliasB.IsResolved())
	assert.True(t, aliasC.IsResolved())
}

func TestConstraintKind_String_Unknown(t *testing.T) {
	// Unknown constraint kind should include numeric value
	unknown := schema.ConstraintKind(99)

	result := unknown.String()

	assert.Equal(t, "ConstraintKind(99)", result)
}

// --- resolveAlias Cycle Detection Tests ---
// These tests verify that alias chain resolution is safe with non-hashable constraint types
// (EnumConstraint, PatternConstraint) and handles cycles gracefully.

func TestAliasConstraint_Equal_WithEnumConstraint(t *testing.T) {
	// EnumConstraint contains a slice, which is non-hashable.
	// This test verifies that comparing aliases resolved to EnumConstraint
	// does not panic due to hashability issues.
	enum1 := schema.NewEnumConstraint([]string{"a", "b", "c"})
	enum2 := schema.NewEnumConstraint([]string{"a", "b", "c"})

	alias1 := schema.NewAliasConstraint("Status", enum1)
	alias2 := schema.NewAliasConstraint("OtherStatus", enum2)

	// These comparisons should not panic
	assert.True(t, alias1.Equal(alias2))
	assert.True(t, alias2.Equal(alias1))
	assert.True(t, alias1.Equal(enum1))
	assert.True(t, enum1.Equal(alias1))
}

func TestAliasConstraint_Equal_WithPatternConstraint(t *testing.T) {
	// PatternConstraint contains a slice, which is non-hashable.
	// This test verifies that comparing aliases resolved to PatternConstraint
	// does not panic due to hashability issues.
	p := regexp.MustCompile("^test")
	pattern1 := schema.NewPatternConstraint([]*regexp.Regexp{p})
	pattern2 := schema.NewPatternConstraint([]*regexp.Regexp{p})

	alias1 := schema.NewAliasConstraint("Code", pattern1)
	alias2 := schema.NewAliasConstraint("OtherCode", pattern2)

	// These comparisons should not panic
	assert.True(t, alias1.Equal(alias2))
	assert.True(t, alias2.Equal(alias1))
	assert.True(t, alias1.Equal(pattern1))
	assert.True(t, pattern1.Equal(alias1))
}

func TestAliasConstraint_Equal_DeepChainWithEnum(t *testing.T) {
	// Test deep alias chain with EnumConstraint at the terminal
	// A -> B -> C -> EnumConstraint
	enum := schema.NewEnumConstraint([]string{"x", "y", "z"})
	aliasC := schema.NewAliasConstraint("C", enum)
	aliasB := schema.NewAliasConstraint("B", aliasC)
	aliasA := schema.NewAliasConstraint("A", aliasB)

	// All comparisons should work without panic
	assert.True(t, aliasA.Equal(enum))
	assert.True(t, aliasB.Equal(enum))
	assert.True(t, aliasC.Equal(enum))
	assert.True(t, aliasA.Equal(aliasB))
	assert.True(t, aliasA.Equal(aliasC))
}

func TestAliasConstraint_Equal_DeepChainWithPattern(t *testing.T) {
	// Test deep alias chain with PatternConstraint at the terminal
	// A -> B -> C -> PatternConstraint
	p := regexp.MustCompile("^[a-z]+$")
	pattern := schema.NewPatternConstraint([]*regexp.Regexp{p})
	aliasC := schema.NewAliasConstraint("C", pattern)
	aliasB := schema.NewAliasConstraint("B", aliasC)
	aliasA := schema.NewAliasConstraint("A", aliasB)

	// All comparisons should work without panic
	assert.True(t, aliasA.Equal(pattern))
	assert.True(t, aliasB.Equal(pattern))
	assert.True(t, aliasC.Equal(pattern))
	assert.True(t, aliasA.Equal(aliasB))
	assert.True(t, aliasA.Equal(aliasC))
}

// --- Cycle-Safety Tests ---
// These tests verify that Equal and IsResolved terminate correctly
// when alias chains contain unresolved terminals (which triggers the
// AliasConstraint type check in the cycle-safety guards).

func TestAliasConstraint_Equal_UnresolvedChain(t *testing.T) {
	// Chain with unresolved terminal: outer -> inner (unresolved)
	// Equal should return false without hanging.
	unresolvedInner := schema.NewAliasConstraint("Inner", nil)
	outerAlias := schema.NewAliasConstraint("Outer", unresolvedInner)

	terminal := schema.NewStringConstraint()

	// These should terminate and return false (cycle-safety guard triggered)
	assert.False(t, outerAlias.Equal(terminal))
	assert.False(t, outerAlias.Equal(unresolvedInner))
}

func TestAliasConstraint_Equal_DeepUnresolvedChain(t *testing.T) {
	// Deep chain with unresolved terminal: A -> B -> C (unresolved)
	unresolvedC := schema.NewAliasConstraint("C", nil)
	aliasB := schema.NewAliasConstraint("B", unresolvedC)
	aliasA := schema.NewAliasConstraint("A", aliasB)

	terminal := schema.NewIntegerConstraint()

	// All should terminate and return false
	assert.False(t, aliasA.Equal(terminal))
	assert.False(t, aliasB.Equal(terminal))
	assert.False(t, aliasA.Equal(aliasB))
}

func TestAliasConstraint_IsResolved_DeepUnresolvedChain(t *testing.T) {
	// Deep chain with unresolved terminal: A -> B -> C (unresolved)
	unresolvedC := schema.NewAliasConstraint("C", nil)
	aliasB := schema.NewAliasConstraint("B", unresolvedC)
	aliasA := schema.NewAliasConstraint("A", aliasB)

	// All should terminate and return false
	assert.False(t, aliasA.IsResolved())
	assert.False(t, aliasB.IsResolved())
	assert.False(t, unresolvedC.IsResolved())
}

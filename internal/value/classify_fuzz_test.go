package value_test

import (
	"encoding/json"
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
)

// FuzzClassify_IntReflexivity tests that Classify is reflexive for integers.
// Running the same integer through Classify multiple times should produce
// the same Kind.
func FuzzClassify_IntReflexivity(f *testing.F) {
	// Seed corpus with interesting values
	f.Add(int64(0))
	f.Add(int64(1))
	f.Add(int64(-1))
	f.Add(int64(42))
	f.Add(int64(9007199254740993)) // 2^53 + 1 (beyond float64 precision)
	f.Add(int64(-9007199254740993))
	f.Add(int64(1<<62 - 1)) // Large positive
	f.Add(int64(-1 << 62))  // Large negative

	f.Fuzz(func(t *testing.T, n int64) {
		kind1, _ := value.Classify(n)
		kind2, _ := value.Classify(n)

		if kind1 != kind2 {
			t.Errorf("Classify(%d) not reflexive: %v != %v", n, kind1, kind2)
		}

		// All int64 values should classify as IntKind
		if kind1 != value.IntKind {
			t.Errorf("Classify(%d) = %v, want IntKind", n, kind1)
		}
	})
}

// FuzzClassify_FloatReflexivity tests that Classify is reflexive for floats.
func FuzzClassify_FloatReflexivity(f *testing.F) {
	// Seed corpus
	f.Add(0.0)
	f.Add(1.0)
	f.Add(-1.0)
	f.Add(3.14159265358979)
	f.Add(1e308)  // Near max float64
	f.Add(1e-308) // Near min positive float64

	f.Fuzz(func(t *testing.T, n float64) {
		kind1, _ := value.Classify(n)
		kind2, _ := value.Classify(n)

		if kind1 != kind2 {
			t.Errorf("Classify(%f) not reflexive: %v != %v", n, kind1, kind2)
		}

		// All float64 values should classify as FloatKind
		if kind1 != value.FloatKind {
			t.Errorf("Classify(%f) = %v, want FloatKind", n, kind1)
		}
	})
}

// FuzzClassify_StringReflexivity tests that Classify is reflexive for strings.
func FuzzClassify_StringReflexivity(f *testing.F) {
	// Seed corpus with various strings
	f.Add("")
	f.Add("hello")
	f.Add("日本語")                        // Multi-byte characters
	f.Add("hello\x00world")             // Embedded null
	f.Add("line1\nline2")               // Newlines
	f.Add("emoji: 😀🔥")                  // Emoji
	f.Add("a" + string(rune(0x10FFFF))) // Max unicode

	f.Fuzz(func(t *testing.T, s string) {
		kind1, _ := value.Classify(s)
		kind2, _ := value.Classify(s)

		if kind1 != kind2 {
			t.Errorf("Classify(%q) not reflexive: %v != %v", s, kind1, kind2)
		}

		// All strings should classify as StringKind
		if kind1 != value.StringKind {
			t.Errorf("Classify(%q) = %v, want StringKind", s, kind1)
		}
	})
}

// FuzzClassify_JSONNumber tests json.Number classification.
// Integer-like json.Numbers should be IntKind, decimal ones should be FloatKind.
func FuzzClassify_JSONNumber(f *testing.F) {
	// Seed with valid JSON numbers
	f.Add("0")
	f.Add("42")
	f.Add("-42")
	f.Add("3.14")
	f.Add("-3.14")
	f.Add("1e10")
	f.Add("1.5e10")
	f.Add("9007199254740993") // 2^53 + 1

	f.Fuzz(func(t *testing.T, s string) {
		jn := json.Number(s)
		kind1, norm1 := value.Classify(jn)
		kind2, norm2 := value.Classify(jn)

		// Reflexivity
		if kind1 != kind2 {
			t.Errorf("Classify(json.Number(%q)) not reflexive: %v != %v", s, kind1, kind2)
		}

		// Normalized value consistency
		if norm1 != norm2 {
			t.Errorf("Classify(json.Number(%q)) normalized values differ: %v != %v", s, norm1, norm2)
		}

		// Valid numbers should be Int or Float, invalid should be Unspecified
		switch kind1 {
		case value.IntKind:
			// Should be parseable as int
			if _, err := jn.Int64(); err != nil {
				t.Errorf("IntKind json.Number(%q) not parseable as int64: %v", s, err)
			}
		case value.FloatKind:
			// Should be parseable as float
			if _, err := jn.Float64(); err != nil {
				t.Errorf("FloatKind json.Number(%q) not parseable as float64: %v", s, err)
			}
		case value.UnspecifiedKind:
			// Invalid json.Number - both int and float parsing should fail
			// (or string is empty/malformed)
		default:
			t.Errorf("Unexpected kind for json.Number(%q): %v", s, kind1)
		}
	})
}

// FuzzClassify_VectorIntConsistency tests that []int64 vectors produce consistent
// classification as VectorKind with Float element type (per v2 design).
func FuzzClassify_VectorIntConsistency(f *testing.F) {
	// Seed with various vector lengths (encoded as count of elements)
	f.Add(int64(0), int64(0), int64(0))      // zeros
	f.Add(int64(1), int64(2), int64(3))      // small positives
	f.Add(int64(-1), int64(-2), int64(-3))   // negatives
	f.Add(int64(1<<60), int64(0), int64(-1)) // large values

	f.Fuzz(func(t *testing.T, a, b, c int64) {
		// Build vector from fuzz inputs
		vec := []any{a, b, c}

		kind1, norm1 := value.Classify(vec)
		kind2, norm2 := value.Classify(vec)

		// Reflexivity
		if kind1 != kind2 {
			t.Errorf("Classify vector not reflexive: %v != %v", kind1, kind2)
		}

		// Should be VectorKind
		if kind1 != value.VectorKind {
			t.Errorf("Classify([]any{%d, %d, %d}) = %v, want VectorKind", a, b, c, kind1)
		}

		// Normalized values should be []float64 (per v2 design)
		norm1Slice, ok1 := norm1.([]float64)
		norm2Slice, ok2 := norm2.([]float64)
		if !ok1 || !ok2 {
			t.Errorf("Normalized vector should be []float64, got %T and %T", norm1, norm2)
			return
		}

		// Values should match
		if len(norm1Slice) != len(norm2Slice) {
			t.Errorf("Normalized slice lengths differ: %d != %d", len(norm1Slice), len(norm2Slice))
		}
		for i := range norm1Slice {
			if norm1Slice[i] != norm2Slice[i] {
				t.Errorf("Normalized values differ at index %d: %f != %f", i, norm1Slice[i], norm2Slice[i])
			}
		}
	})
}

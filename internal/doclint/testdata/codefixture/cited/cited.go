// Package cited cites diagnostic codes in its comments.
//
// It reports E_KNOWN and diag.W_KNOWN_WARNING, and every E_KNOWN_* code, all
// registered. It never reports E_VANISHED, and no code starts E_GONE_*.
package cited

// Example registers a code, as the placeholder E_EXAMPLE_CODE does.
func Example() string {
	return "E_IN_A_STRING" // a literal is a fixture; E_TRAILING_GONE is not
}

// Word holds NODE_E_EMBEDDED, XE_EMBEDDED and E_1ST, which are no citations.
const Word = 1

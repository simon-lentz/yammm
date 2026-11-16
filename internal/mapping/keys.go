package mapping

import (
	"slices"
	"strconv"
	"strings"
)

// Key encodes segments into a string no other segment list can produce, by
// writing each segment's byte length and a colon ahead of it.
//
// The segments include remote property names and labels, which are
// database-supplied and unvalidated — Neo4j accepts any property key when it is
// backticked — so a separator join is not injective: one property named "a,b"
// would encode identically to the two-property composite (a, b).
func Key(segments ...string) string {
	var b strings.Builder
	n := 0
	for _, s := range segments {
		n += len(s) + 3
	}
	b.Grow(n)
	for _, s := range segments {
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
	}
	return b.String()
}

// ConstraintKey is a constraint's semantic identity: its label, its remote
// type and its properties. A constraint's members are a set, so they are
// sorted.
func ConstraintKey(label, remoteType string, properties []string) string {
	props := slices.Sorted(slices.Values(properties))
	return Key(append([]string{label, remoteType}, props...)...)
}

// IndexKey is an index's semantic identity: its label, its remote type and its
// properties. Properties are NOT sorted: composite index order is significant,
// so a reordered composite is a distinct index.
func IndexKey(label, remoteType string, properties []string) string {
	return Key(append([]string{label, remoteType}, properties...)...)
}

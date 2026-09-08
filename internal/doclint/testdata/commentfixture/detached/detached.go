// Package detached holds a block a blank line cuts loose.
package detached

// Orphan is documented by this block, which a blank line separates from the
// declaration, so go/doc shows the declaration with no documentation at all.

func Orphan() {}

const (
	// OrphanConst is documented by this block, which a blank line separates
	// from the spec inside a parenthesised block — a position the scan reached
	// only at the declaration before it.

	OrphanConst = 1

	// KeptConst keeps its documentation because nothing separates the two.
	KeptConst = 2
)

// Holder carries a field whose doc a blank line cuts loose.
type Holder struct {
	// OrphanField is documented by this block, which a blank line separates
	// from the field.

	OrphanField int

	// KeptField keeps its documentation.
	KeptField int
}

// A trailing note about the section below. It names no declaration, so it is
// not documentation somebody lost — it is a note, and the gate leaves it.

// Noted has its own block.
func Noted() {}

// Stacked is documented by this paragraph, which sits above Carrier instead:
// the block abuts a declaration, so nothing is detached and go doc shows it
// under the wrong name.
func Carrier() {}

// Stacked is the declaration the paragraph above belongs to.
func Stacked() {}

// unexportedStacked names a declaration go doc -u renders, so a paragraph
// stacked onto its neighbour costs documentation there too.
func unexportedCarrier() {}

func unexportedStacked() {}

var _ = []any{Carrier, Stacked, unexportedCarrier, unexportedStacked}

// Package attached holds a block that abuts its declaration.
package attached

// Kept keeps its documentation because nothing separates the two.
func Kept() {}

// A note about the group below, followed by a blank line and then a
// declaration that carries its own doc comment.

// AlsoKept has its own block, so the note above documents nothing and is not
// this gate's business.
func AlsoKept() {}

// Elsewhere names a symbol this file does not declare, which is a reference
// rather than a stack, and the gate leaves it.
func Reference() {}

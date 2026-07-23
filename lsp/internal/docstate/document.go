package docstate

import "github.com/simon-lentz/yammm/location"

// LineState holds cached per-line analysis results for completion context detection.
// This enables O(1) lookup for isInsideTypeBody instead of O(n) scanning from line 0.
type LineState struct {
	Version        int    // document version this state was computed for
	BraceDepth     []int  // BraceDepth[i] = nesting depth at END of line i
	InBlockComment []bool // InBlockComment[i] = true if line i ends inside a block comment
}

// Document represents an open document in the workspace.
type Document struct {
	URI       string
	SourceID  location.SourceID
	Version   int
	Text      string
	OpenOrder int // Order in which document was opened (for deterministic URI selection)

	// LineState caches per-line brace depth for completion context.
	// Eagerly computed on open/change; invalidated when Version changes.
	LineState *LineState
}

// Snapshot is a point-in-time view of a document's state.
// Treat as immutable after creation — fields are value types or pointers
// for efficiency, but callers must not modify the underlying data.
type Snapshot struct {
	URI       string
	SourceID  location.SourceID
	Version   int
	Text      string
	LineState *LineState // Cached brace depth per line (may be nil)
}

// LineStartsInBlockComment reports whether the given 0-based line begins inside
// a block comment, read from the cached per-line state when it is present and
// current. Returns false when no line state is available (before the first
// analysis, or in a parse-independent test), which is the right answer for a
// comment opened and closed on the same line.
//
// It is the single owner of that question for the token scanners that must skip
// commented-out text — [InStringOrComment] can only see the line handed to it,
// so a scanner that answers this for itself sees a continuation line as clean
// code.
func (s *Snapshot) LineStartsInBlockComment(line int) bool {
	if s == nil || s.LineState == nil || s.LineState.Version != s.Version {
		return false
	}
	prev := line - 1
	if prev < 0 || prev >= len(s.LineState.InBlockComment) {
		return false
	}
	return s.LineState.InBlockComment[prev]
}

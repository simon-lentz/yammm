package lsputil

import (
	"bytes"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// fileSignature is the UTF-8 byte order mark. The server counts a file it reads
// as an editor that decodes one leading mark away holds it: from the byte
// after the mark.
var fileSignature = []byte("\xef\xbb\xbf")

// Texts holds the content of every source of one analysis and converts
// between a byte of that content and a position in the text the client holds.
//
// The two texts differ for one kind of source. A document the client sent is
// its text byte for byte, a leading byte order mark included. A file the
// server read is the client's text only after decoding, which drops one
// leading mark: line 1 of such a file is counted from the byte after the mark.
type Texts struct {
	registry *source.Registry
	// undecoded holds, for a file that starts with a mark, the mark's length.
	undecoded map[location.SourceID]int
}

// NewTexts returns an empty Texts.
func NewTexts() *Texts {
	return &Texts{
		registry:  source.NewRegistry(),
		undecoded: make(map[location.SourceID]int),
	}
}

// Register adds text the client sent. Content registered again for one source
// is accepted when it is the same and refused when it differs.
func (t *Texts) Register(id location.SourceID, content []byte) error {
	return t.registry.Register(id, content) //nolint:wrapcheck // the registry's error names the source
}

// RegisterFile adds content the server read from a file. A source already
// registered keeps the origin it was registered with.
func (t *Texts) RegisterFile(id location.SourceID, content []byte) error {
	known := t.registry.Has(id)
	if err := t.registry.Register(id, content); err != nil {
		return err //nolint:wrapcheck // the registry's error names the source
	}
	if !known && bytes.HasPrefix(content, fileSignature) {
		t.undecoded[id] = len(fileSignature)
	}
	return nil
}

// Has reports whether the source is registered.
func (t *Texts) Has(id location.SourceID) bool { return t.registry.Has(id) }

// Len returns the number of registered sources.
func (t *Texts) Len() int { return t.registry.Len() }

// ContentBySource returns the registered bytes of a source.
func (t *Texts) ContentBySource(id location.SourceID) ([]byte, bool) {
	return t.registry.ContentBySource(id)
}

// LineStartByte returns the byte offset at which a 1-based line of the
// registered content starts.
func (t *Texts) LineStartByte(id location.SourceID, line int) (int, bool) {
	return t.registry.LineStartByte(id, line)
}

// PositionAt returns the position of a byte offset of the registered content.
func (t *Texts) PositionAt(id location.SourceID, byteOffset int) location.Position {
	return t.registry.PositionAt(id, byteOffset)
}

// characterOrigin returns the byte offset the client counts the characters of
// a 1-based line from: the line's start, or on line 1 of a file the byte
// after its leading mark.
func (t *Texts) characterOrigin(id location.SourceID, line int) (int, bool) {
	start, ok := t.registry.LineStartByte(id, line)
	if !ok {
		return 0, false
	}
	if line == 1 {
		start += t.undecoded[id]
	}
	return start, true
}

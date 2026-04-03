package docstate

import (
	"log/slog"

	"github.com/simon-lentz/yammm/location"
)

// Overlay stores open .yammm document state.
//
// All fields are protected by Workspace.mu (external lock).
// Callers must hold Workspace.mu before calling any method.
type Overlay struct {
	// Open .yammm documents keyed by URI
	Open map[string]*Document

	// Counter for deterministic document ordering (symlink disambiguation)
	OpenCounter int
}

// NewOverlay creates an initialized Overlay. Prefer this over Overlay{} to
// avoid nil-map panics in OpenDocument.
func NewOverlay() *Overlay {
	return &Overlay{Open: make(map[string]*Document)}
}

// OpenDocument creates or replaces a document in the overlay.
// Normalizes line endings and eagerly computes LineState for completion context.
// The caller is responsible for resolving the canonical SourceID (symlinks, etc.).
//
// Must be called with Workspace.mu held.
func (d *Overlay) OpenDocument(uri string, sourceID location.SourceID, version int, text string) {
	if d.Open == nil {
		d.Open = make(map[string]*Document)
	}
	text = NormalizeLineEndings(text)
	depths, inComment := ComputeBraceDepths(text)

	d.OpenCounter++
	d.Open[uri] = &Document{
		URI:       uri,
		SourceID:  sourceID,
		Version:   version,
		Text:      text,
		OpenOrder: d.OpenCounter,
		LineState: &LineState{
			Version:        version,
			BraceDepth:     depths,
			InBlockComment: inComment,
		},
	}
}

// ChangeDocument updates an existing document's text and version.
// Ignores stale updates where version <= current version (unless version is 0/unknown).
//
// Must be called with Workspace.mu held.
func (d *Overlay) ChangeDocument(uri string, version int, text string, logger *slog.Logger) {
	doc, ok := d.Open[uri]
	if !ok {
		return
	}

	// Ignore stale updates (version <= current) unless version is 0 (unknown).
	// This prevents out-of-order updates from overwriting newer content.
	if version != 0 && doc.Version != 0 && version <= doc.Version {
		logger.Debug("ignoring stale document change",
			slog.String("uri", uri),
			slog.Int("incoming_version", version),
			slog.Int("current_version", doc.Version),
		)
		return
	}
	doc.Version = version
	// Normalize line endings (CRLF/CR -> LF) for consistent offset calculations.
	doc.Text = NormalizeLineEndings(text)
	// Eagerly recompute LineState on every change. For typical yammm files
	// (<1000 lines) this is sub-millisecond and eliminates lock juggling
	// in GetSnapshot.
	depths, inComment := ComputeBraceDepths(doc.Text)
	doc.LineState = &LineState{
		Version:        doc.Version,
		BraceDepth:     depths,
		InBlockComment: inComment,
	}
}

// RemoveDocument removes a document from the overlay.
//
// Must be called with Workspace.mu held.
func (d *Overlay) RemoveDocument(uri string) {
	delete(d.Open, uri)
}

// GetSnapshot returns an immutable snapshot of the document for a URI.
// The snapshot contains a copy of the document state at the time of the call,
// allowing safe access outside of locks without racing with ChangeDocument.
// LineState slices are copied to prevent callers from accidentally mutating
// the live document's state through the shared backing array.
// Returns nil if the document is not open.
//
// Must be called with Workspace.mu held (at least RLock).
func (d *Overlay) GetSnapshot(uri string) *Snapshot {
	doc := d.Open[uri]
	if doc == nil {
		return nil
	}

	// Copy LineState to prevent shared-backing-array mutation.
	var ls *LineState
	if doc.LineState != nil {
		ls = &LineState{
			Version:        doc.LineState.Version,
			BraceDepth:     append([]int(nil), doc.LineState.BraceDepth...),
			InBlockComment: append([]bool(nil), doc.LineState.InBlockComment...),
		}
	}

	return &Snapshot{
		URI:       doc.URI,
		SourceID:  doc.SourceID,
		Version:   doc.Version,
		Text:      doc.Text,
		LineState: ls,
	}
}

// CollectOverlays builds an overlay map from all open documents.
// Uses canonical SourceID as key to ensure symlinks and path variations
// map to the same entry that the loader will use.
//
// Must be called with Workspace.mu held (at least RLock).
func (d *Overlay) CollectOverlays() map[string][]byte {
	overlays := make(map[string][]byte, len(d.Open))
	for _, doc := range d.Open {
		overlays[doc.SourceID.String()] = []byte(doc.Text)
	}
	return overlays
}

// AllOpenURIs returns a list of all open .yammm document URIs.
//
// Must be called with Workspace.mu held (at least RLock).
func (d *Overlay) AllOpenURIs() []string {
	uris := make([]string, 0, len(d.Open))
	for uri := range d.Open {
		uris = append(uris, uri)
	}
	return uris
}

package definition

import (
	"log/slog"

	"github.com/simon-lentz/yammm/internal/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/internal/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/internal/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/internal/lsp/internal/protocol"
)

// AtPosition returns the definition location for the symbol at the given position.
// The char parameter counts UTF-16 code units.
// Returns nil when no definition is found or when snapshot is nil (pre-first-analysis).
func AtPosition(
	snapshot *analysis.Snapshot,
	doc *docstate.Snapshot,
	line, char int,
	mapURI func(string) string,
	logger *slog.Logger,
) *protocol.Location {
	if snapshot == nil {
		return nil
	}
	if snapshot.EntryVersion != doc.Version {
		logger.Debug(
			"serving stale snapshot for definition",
			"uri", doc.URI,
			"snapshot_version", snapshot.EntryVersion,
			"doc_version", doc.Version,
		)
	}

	idx := snapshot.SymbolIndexAt(doc.SourceID)
	if idx == nil {
		logger.Debug("no symbol index for source", "source", doc.SourceID)
		return nil
	}

	internalPos, ok := lsputil.PositionFromLSP(
		snapshot.Sources,
		doc.SourceID,
		line,
		char,
	)
	if !ok {
		return nil
	}

	ref := idx.ReferenceAtPosition(internalPos)
	if ref != nil {
		loc := ResolveReference(snapshot, ref, doc.SourceID, mapURI)
		if loc == nil {
			logger.Debug(
				"could not resolve reference",
				"target", ref.TargetName,
				"qualifier", ref.Qualifier,
			)
		}
		return loc
	}

	sym := idx.SymbolAtPosition(internalPos)
	if sym != nil {
		return ResolveSymbol(snapshot, sym, mapURI)
	}

	logger.Debug(
		"no symbol or reference at position",
		"uri", doc.URI,
		"position", internalPos,
	)
	return nil
}

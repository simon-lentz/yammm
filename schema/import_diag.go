package schema

import (
	"fmt"
	"path/filepath"

	"github.com/simon-lentz/yammm/diag"
)

// The import-resolution family — E_IMPORT_RESOLVE, E_PATH_ESCAPE and
// E_IMPORT_CYCLE — is built through one builder per code, and every site that
// raises one of the three calls its builder.
//
// The reason is shape, not convenience. Before these builders E_IMPORT_RESOLVE
// was raised at six sites in three different detail shapes, and E_PATH_ESCAPE
// omitted the alias its declaration had in hand. A caller keying on a code
// cannot rely on a detail that only some sites attach, so one code carries one
// shape: the module root and its origin always, and the span, path and alias
// whenever a declaration exists.

// moduleRootClause renders a load's root and its origin as one message clause.
//
// It is appended to the message rather than left to the details because the
// text renderer prints no details — only the JSON renderer does — and the
// plugin hook reads the text output. Provenance carried in details alone would
// be invisible to the reader who most needs it.
func moduleRootClause(root, origin string) string {
	switch origin {
	case diag.ModuleRootDiscovered:
		return fmt.Sprintf("module root %s, discovered from %s", root, filepath.Join(root, ModuleRootMarker))
	case diag.ModuleRootDefault:
		return fmt.Sprintf("module root %s, defaulted to the entry schema's directory; a %s in an ancestor directory would widen resolution", root, ModuleRootMarker)
	case diag.ModuleRootExplicit:
		return fmt.Sprintf("module root %s, given explicitly", root)
	case diag.ModuleRootSynthetic:
		return "synthetic root " + root
	default:
		return "no module root in play"
	}
}

// resolutionIssue is the one shape every import-resolution diagnostic takes.
// imp may be nil for a site that has no declaration in hand.
func resolutionIssue(code diag.Code, root, origin, message string, imp *importDecl) diag.Issue {
	b := diag.NewIssue(diag.Error, code, message+"; "+moduleRootClause(root, origin)).
		WithDetail(diag.DetailKeyModuleRoot, root).
		WithDetail(diag.DetailKeyModuleRootOrigin, origin)
	if imp != nil {
		b = b.WithSpan(imp.Span).
			WithDetail(diag.DetailKeyImportPath, imp.Path).
			WithDetail(diag.DetailKeyAlias, imp.Alias)
	}
	return b.Build()
}

// importResolveIssue builds E_IMPORT_RESOLVE: an import path that does not
// resolve to a readable schema.
func importResolveIssue(root, origin, message string, imp *importDecl) diag.Issue {
	return resolutionIssue(diag.E_IMPORT_RESOLVE, root, origin, message, imp)
}

// pathEscapeIssue builds E_PATH_ESCAPE: an import path that resolves outside
// the module root, refused at the kernel level by the sandbox.
func pathEscapeIssue(root, origin string, imp *importDecl) diag.Issue {
	return resolutionIssue(diag.E_PATH_ESCAPE, root, origin,
		fmt.Sprintf("import %q escapes module root", imp.Path), imp)
}

// importCycleIssue builds E_IMPORT_CYCLE: a cycle in the import graph. The
// cycle is detected on the source being entered, not on a declaration, so
// there is no import declaration to anchor it to.
func importCycleIssue(root, origin string, sourceID fmt.Stringer) diag.Issue {
	return resolutionIssue(diag.E_IMPORT_CYCLE, root, origin,
		fmt.Sprintf("import cycle detected involving %s", sourceID), nil)
}

// loaderRoot returns the root a loader's import diagnostics report and where
// it came from. A synthetic root stands in for the module root, and the
// loader's own moduleRoot is empty in that case.
func (l *loader) loaderRoot() (root, origin string) {
	if l.syntheticRoot != "" {
		return l.syntheticRoot, l.rootOrigin
	}
	return l.moduleRoot, l.rootOrigin
}

package main

import (
	"path/filepath"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// bindSchemaSource points the sink at a loaded schema, so every diagnostic the
// invocation renders resolves excerpts and relativizes paths the way the loader
// itself would.
//
// It is called as soon as the load returns rather than at the render, because a
// command that fails in a later phase renders through the wrapper's deferred
// call and never reaches its own code again.
//
// explicitRoot is the command's module-root flag where it has one, and "" where
// it does not; it selects the root together with the schema path (see
// [diagRootFor]).
func bindSchemaSource(sink *cli.DiagnosticSink, s schema.Schema, explicitRoot, absSchemaPath string) {
	sink.SetSource(sink.CapturedSources(), diagRootFor(s, explicitRoot, absSchemaPath))
}

// reportSchemaLoad hands a schema load's diagnostics to the sink and reports
// whether the load failed — an unreadable schema file is an I/O failure, not a
// validation one.
//
// It returns no residual result. A successful load's warnings are already the
// sink's, and handing them back invited a caller to render them beside its own,
// which is the two-document shape this replaces. A load warning matters — it
// exists because the loader chose not to reject, and W_ANNOTATION_SHADOWED is
// the only signal that a subtype silently dropped an inherited annotation — and
// it now reaches the operator on every path, including the ones that return
// before the command's own phase runs.
func reportSchemaLoad(sink *cli.DiagnosticSink, s schema.Schema, explicitRoot, absSchemaPath string, result diag.Result) error {
	bindSchemaSource(sink, s, explicitRoot, absSchemaPath)
	sink.Add(result)
	if result.HasErrors() {
		return cli.Exit(cli.ExitForResult(result))
	}
	return nil
}

// diagRootFor returns the host path rendered locations are relativized
// against, which the renderer turns into an identity: a completed load's
// ModuleRoot unless it is a synthetic scheme string, and for a failed load the
// root the loader would have used — the explicit root, then the nearest
// yammm.mod, then the directory of the host path the schema file resolves to.
func diagRootFor(s schema.Schema, explicitRoot, absSchemaPath string) string {
	if !s.IsZero() {
		if root := s.ModuleRoot(); root != "" && filepath.IsAbs(root) {
			return root
		}
	}
	base := explicitRoot
	if base == "" {
		file := absSchemaPath
		if resolved, err := location.ResolveHostPath(absSchemaPath); err == nil {
			file = resolved
		}
		base = filepath.Dir(file)
		// A discovery error is deliberately ignored: the load already
		// reported it, and the renderer's job is to relativize what it can.
		if root, found, err := schema.FindModuleRoot(base); err == nil && found {
			return root
		}
	}
	if resolved, err := location.ResolveHostPath(base); err == nil {
		return resolved
	}
	return filepath.Clean(base)
}

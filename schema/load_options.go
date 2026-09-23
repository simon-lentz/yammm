package schema

import (
	"log/slog"

	"github.com/simon-lentz/yammm/internal/source"
)

// LoadOption configures the behavior of Load functions.
type LoadOption func(*loadConfig)

// loadConfig holds configuration for schema loading.
type loadConfig struct {
	registry        *Registry
	moduleRoot      string
	syntheticRoot   string
	issueLimit      int
	sourceRegistry  *source.Registry
	logger          *slog.Logger
	disallowImports bool
	sourcesOnly     bool
	sourcesOut      **Sources
	// syntheticRootSet separates "WithSyntheticRoot not passed" from
	// "WithSyntheticRoot passed an empty root", which is an error rather than
	// a no-op.
	syntheticRootSet bool
}

// defaultLoadConfig returns a loadConfig with sensible defaults.
func defaultLoadConfig() *loadConfig {
	return &loadConfig{
		issueLimit: 100,
	}
}

// WithRegistry provides a schema registry for cross-schema type resolution.
// Schemas loaded via imports will be registered automatically. If nil, a
// fresh Registry is created for the Load operation (the default, safe for
// any usage pattern).
//
// Shared-Registry semantics (post-v0.3.0). Passing the same *Registry to
// multiple Load calls is safe and efficient:
//
//   - Overlapping transitive imports short-circuit via the registry cache:
//     when loadImport encounters a SourceID already registered in r, or held
//     inside the import closure of a schema r holds, the existing *Schema
//     pointer is reused and the import is NOT re-parsed, whatever order the
//     load meets its imports in. A source two schemas in r compiled from
//     different bytes is not reused: an import of it fails with
//     E_IMPORT_RESOLVE rather than reading it again.
//     This is where cross-Load schema caching pays off.
//   - Re-registering the same source — the same bytes for every source both
//     carry, the same structural hash — is a no-op (see Registry.Register); a
//     source whose bytes or hash changed fails the load with
//     E_LOAD_SOURCE_CHANGED naming what differed.
//   - A root is compiled on every Load call; when r already holds its
//     SourceID with identical content, Register keeps the first object and
//     the Load returns that object, so every reader of r sees one *Schema per
//     SourceID. Divergent content is the error Registry.Register documents.
//   - A shared registry assumes the files it holds do not change while it
//     lives: it hands every load the object it first compiled, with that
//     object's module root, sources, documentation and annotations. Do not
//     share one across an edit; use a fresh Registry per load instead.
//   - Cross-Load sharing fires whenever an import resolves to a SourceID
//     already in r — including across loads with different WithModuleRoot
//     values whose root+key combinations land on the same canonical path
//     (nested roots reaching one shared file, for example). A cache-reused
//     import keeps the ModuleRoot of the load that compiled it; see
//     [Schema.ModuleRoot]. For LoadString, the synthetic
//     "string://<sourceName>" SourceID scheme means two LoadString calls
//     sharing a Registry must use distinct sourceName values unless
//     re-registering byte-identical content.
func WithRegistry(r *Registry) LoadOption {
	return func(c *loadConfig) {
		c.registry = r
	}
}

// applyLoadOptions applies all options to the loadConfig.
func applyLoadOptions(cfg *loadConfig, opts []LoadOption) {
	for _, opt := range opts {
		opt(cfg)
	}
}

// WithModuleRoot sets the root directory for module-style imports.
// This option is only meaningful for Load(), which operates on filesystem paths.
// LoadString() has no module root, and LoadSourcesWithEntry() takes one as an
// explicit argument rather than through this option.
//
// It is the first rung of the ladder [Load] resolves a root by: this option,
// then the directory of the nearest ancestor holding a [ModuleRootMarker]
// file, then the entry schema's own directory. The editor inserts its
// workspace folder between the second and the third.
//
// Under this option no marker is read at all — not even a malformed one, which
// would otherwise fail the load. That is what "explicit wins" means: a caller
// can always override a marker it did not put there.
//
// The root is the import sandbox's boundary. A discovered root therefore
// widens what a load may read: committing a marker at a repository root grants
// the loader read access to that whole subtree for imports. That widening is
// the mechanism working — it is what makes a repository-relative import
// resolve — but it is not something a marker's author should discover later.
//
// Discovery walks the canonical (symlink-resolved) ancestor chain, so a marker
// reachable only through a symlinked spelling of the entry path is invisible
// to it. Pass this option for that case.
func WithModuleRoot(root string) LoadOption {
	return func(c *loadConfig) {
		c.moduleRoot = root
	}
}

// WithIssueLimit sets the maximum number of diagnostic issues to collect.
// When the limit is reached, loading continues and the collector retains the
// most severe issues seen: a more severe arrival evicts the least severe stored
// issue rather than being dropped itself. Set to 0 for unlimited. Default is 100.
func WithIssueLimit(limit int) LoadOption {
	return func(c *loadConfig) {
		c.issueLimit = limit
	}
}

// withSourceRegistry provides a source registry for position tracking.
// If not provided, a new source registry is created for the load operation.
//
// Unexported: the parameter type lives in internal/source, so no caller
// outside the module can construct a meaningful argument — consumers read
// the loaded schema's source closure via [Schema.Sources] instead. Tests
// reach this seam through the package's test-only exports.
func withSourceRegistry(reg *source.Registry) LoadOption {
	return func(c *loadConfig) {
		c.sourceRegistry = reg
	}
}

// WithSourcesOnly selects whether import resolution is restricted to the
// pre-registered in-memory sources: with true a miss fails with E_IMPORT_RESOLVE
// and the module-root sandbox is never opened, the hermetic load
// [WithSyntheticRoot] requires; with false, the default, a miss falls back to the
// module root on disk. It decides where an import is read from, never a source's
// identity: without a synthetic root a key is resolved on the host under the
// module root, or the working directory when the root is empty, as [Load]
// resolves a path, through symlinks and in the disk's spelling. Only [LoadSourcesWithEntry] takes it; [Load] and [LoadString] refuse
// it with true.
func WithSourcesOnly(only bool) LoadOption {
	return func(c *loadConfig) {
		c.sourcesOnly = only
	}
}

// WithSyntheticRoot makes in-memory source identities synthetic rather than
// filesystem-derived: each source key is joined to root, so a type's SchemaPath
// reads "embedded://app/a/b/x.yammm" instead of a path that moves with the
// working directory, the checkout, or the container mount point. It is the way
// to load embedded sources whose identities are persisted — a snapshot records
// them, and a filesystem-derived one re-keys every record when the process
// moves. The root has the form scheme://authority, optionally followed by a
// path, such as "embedded://app". It is normalized once: the scheme in lower
// case, the path cleaned as path.Clean cleans it, and the whole in NFC; the
// authority keeps its case and is not cleaned. A path climbing above it, and a
// backslash, are refused.
//
// The root also stands in for the module root, so module-style imports resolve
// under it. The load is rejected outright, rather than silently degraded, in
// four cases: a root not of that form, or one
// [github.com/simon-lentz/yammm/location.ValidateSyntheticSourceID] refuses; a
// root without [WithSourcesOnly], where an import miss would fall back to disk
// and mix a file-backed identity into the closure; a root together with a
// non-empty moduleRoot argument, which names the same concept twice; and the
// option passed to [Load] or [LoadString], where it can only ever be a no-op.
//
// A key is read by [github.com/simon-lentz/yammm/location.NormalizeSyntheticKey]:
// it holds no backslash, and once cleaned and in NFC it is relative and names a
// file, not the root or a directory above it. A key that escapes the root is
// permitted and yields a ".."-bearing identity, which stays stable and
// distinct. A relative import ("./x", "../x") resolves
// against the importing source's key, as text, and one climbing above the root
// keeps its ".."; [SyntheticImportKey] states the rule. [Schema.ModuleRoot] reports the
// synthetic root, because the root is the one this load resolved imports
// against; a schema loaded this way is a supported input to
// [github.com/simon-lentz/yammm/adapter/gogen.Marshal], whose embedded keys
// are then relative to that root.
//
// Do not share a [Registry] between a synthetic-root load and a disk load of
// the same schema: the two mint different SourceIDs for one schema name, and
// [Registry.Register] reports DuplicateName, which the loader surfaces as
// E_DUPLICATE_SCHEMA.
func WithSyntheticRoot(root string) LoadOption {
	return func(c *loadConfig) {
		c.syntheticRoot = root
		c.syntheticRootSet = true
	}
}

// WithImportsAllowed selects whether import declarations are processed. With
// false, any import statement in the source produces a single
// E_IMPORT_NOT_ALLOWED diagnostic at the first declaration. The rejection
// does not suppress the source's other diagnostics: analysis continues
// with the rejected aliases deferred, and the rejected imports are never
// probed or resolved. The LSP markdown analysis path passes false for
// isolated blocks. [LoadString] always rejects imports, whatever the caller
// passes. With true — the default — imports resolve normally.
func WithImportsAllowed(allowed bool) LoadOption {
	return func(c *loadConfig) {
		c.disallowImports = !allowed
	}
}

// WithLogger provides a structured logger for load operation diagnostics.
// If not provided, logging is disabled.
func WithLogger(logger *slog.Logger) LoadOption {
	return func(c *loadConfig) {
		c.logger = logger
	}
}

// CaptureSources stores the load's source registry in *dst when the load
// starts, before it reads anything, so a load that fails early leaves *dst
// holding that load's empty registry, never an earlier load's. A load that
// fails returns a nil Schema, and Schema.Sources with it, so a caller that
// renders excerpts for a failed load takes the sources from here. A nil dst
// captures nothing.
func CaptureSources(dst **Sources) LoadOption {
	return func(c *loadConfig) {
		c.sourcesOut = dst
	}
}

// startCapture gives the load its source registry at the load's entry and, when
// [CaptureSources] asked for it, stores the registry in the caller's dst.
func (c *loadConfig) startCapture() {
	if c.sourcesOut == nil {
		return
	}
	if c.sourceRegistry == nil {
		c.sourceRegistry = source.NewRegistry()
	}
	*c.sourcesOut = NewSources(c.sourceRegistry)
}

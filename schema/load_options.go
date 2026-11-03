package schema

import (
	"github.com/simon-lentz/yammm/internal/source"
)

// LoadOption configures the behavior of Load functions.
type LoadOption func(*loadConfig)

// loadConfig holds configuration for schema loading.
type loadConfig struct {
	moduleRoot      string
	syntheticRoot   string
	sourceRegistry  *source.Registry
	disallowImports bool
	sourcesOnly     bool
	sourcesOut      *Sources
	// syntheticRootSet separates "WithSyntheticRoot not passed" from
	// "WithSyntheticRoot passed an empty root", which is an error rather than
	// a no-op.
	syntheticRootSet bool
}

// loadIssueLimit is the most diagnostic issues one load stores.
const loadIssueLimit = 100

// defaultLoadConfig returns a loadConfig with sensible defaults.
func defaultLoadConfig() *loadConfig {
	return &loadConfig{}
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
// Under a non-empty root no marker is read at all — not even a malformed one, which
// would otherwise fail the load. That is what "explicit wins" means: a caller
// can always override a marker it did not put there. [Load] refuses a root that
// is not a directory it can open with a Fatal E_LOAD_IO_FAILURE, whether or not
// the schema imports anything.
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

// WithSourcesOnly selects whether import resolution is restricted to the
// pre-registered in-memory sources: with true a miss fails with E_IMPORT_RESOLVE
// and the module-root sandbox is never opened; with false, the default, a miss
// falls back to the module root on disk. A load under [WithSyntheticRoot] is
// restricted to the in-memory sources whatever this option says. It decides where an import is read from, never a source's
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
// to load embedded sources that import one another and whose identities must
// stay fixed: a SourceID
// from the filesystem, and every TypeID built on it, changes as the process
// moves. The root has the form scheme://authority, optionally followed by a
// path, such as "embedded://app", and is judged whole. Its scheme is an RFC
// 3986 scheme of two characters or more, and not "string" in any case, which [LoadString]
// mints. The text after the scheme is judged in NFC, where it holds letters,
// digits, the marks - . _ ~ ! $ & ' ( ) * + , ; = and characters past ASCII
// that print, as strconv.IsPrint reports; "/" separates the authority from
// the path and the path's segments, and ":" and "@" are taken in the path
// alone. So a query, a fragment, a percent escape, a space, a character that does not print
// and a backslash are each refused, and so are text that is not valid UTF-8,
// an authority that is "." or ".." and a path climbing above the authority. It
// is normalized once: the scheme in lower case, the text in NFC, and the path
// cleaned as path.Clean cleans it; the authority keeps its case and is not
// cleaned.
//
// The root also stands in for the module root, so module-style imports resolve
// under it. A root names every identity the load mints, so the given sources
// are every source the load reads: the load resolves imports as under
// [WithSourcesOnly] with true, whether or not that option is passed, and a miss
// fails with E_IMPORT_RESOLVE and reads no disk. The load is rejected outright,
// rather than silently degraded, in three cases of the root: a root not of
// that form; a root together with a non-empty moduleRoot argument, which names
// the same concept twice; and the option passed to [Load] or [LoadString],
// where it can only ever be a no-op. A key the key rule below refuses is
// rejected outright too.
//
// A key is read by [github.com/simon-lentz/yammm/location.NormalizeSyntheticKey]:
// it holds no backslash and no character that does not print, and once cleaned
// and in NFC it is relative and names a file, not the root or a directory above it. A key that escapes the root is
// permitted and yields a ".."-bearing identity, which stays stable and
// distinct. A relative import ("./x", "../x") resolves
// against the importing source's key, as text, and one climbing above the root
// keeps its ".."; [SyntheticImportKey] states the rule. [Schema.ModuleRoot] reports the
// synthetic root, because the root is the one this load resolved imports
// against; a schema loaded this way is a supported input to
// [github.com/simon-lentz/yammm/schema/gogen.Marshal], whose embedded keys
// are then relative to that root.
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

// CaptureSources stores the load's source registry in *dst when the load
// starts, before it reads anything, so a load that fails early leaves *dst
// holding that load's empty registry, never an earlier load's. A load that
// fails returns a zero Schema, on which Schema.Sources panics, so a caller that
// renders excerpts for a failed load takes the sources from here. A nil dst
// captures nothing.
func CaptureSources(dst *Sources) LoadOption {
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

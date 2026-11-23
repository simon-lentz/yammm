package schema

import (
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
)

// Option configures a load by [Load], [LoadString] or [LoadSourcesWithEntry].
// A load refuses an option it does not take with an error, with no diagnostic
// code, that names the option and the load.
type Option func(*loadConfig)

// optionKind names the constructor that built an [Option], so a load can refuse
// one it does not take.
type optionKind uint8

const (
	optModuleRoot optionKind = iota
	optSourcesOnly
	optSyntheticRoot
	optImportsAllowed
	optCaptureSources
	optWarnings
)

var optionNames = [...]string{
	optModuleRoot:     "WithModuleRoot",
	optSourcesOnly:    "WithSourcesOnly",
	optSyntheticRoot:  "WithSyntheticRoot",
	optImportsAllowed: "WithImportsAllowed",
	optCaptureSources: "CaptureSources",
	optWarnings:       "WithWarnings",
}

// loadConfig holds configuration for schema loading.
type loadConfig struct {
	given           []optionKind // every option passed, in argument order
	moduleRoot      string
	syntheticRoot   string
	sourceRegistry  *source.Registry
	disallowImports bool
	sourcesOnly     bool
	sourcesOut      *Sources
	warnings        func(diag.Issue)
	// syntheticRootSet separates "WithSyntheticRoot not passed" from
	// "WithSyntheticRoot passed an empty root", which is an error rather than
	// a no-op.
	syntheticRootSet bool
}

// loadIssueLimit is the most failures one load collects. A Fatal that ends the
// load joins them past it.
const loadIssueLimit = 100

// newLoadConfig applies opts to a new loadConfig.
func newLoadConfig(opts []Option) *loadConfig {
	cfg := &loadConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// gave reports whether the load was passed an option of kind k.
func (c *loadConfig) gave(k optionKind) bool {
	return slices.Contains(c.given, k)
}

// refuse returns an error naming the first option, in argument order, that the
// load call does not take, or nil when it takes every option it was given.
func (c *loadConfig) refuse(call string, takes ...optionKind) error {
	for _, g := range c.given {
		taken := false
		for _, t := range takes {
			taken = taken || g == t
		}
		if !taken {
			return fmt.Errorf("schema.%s does not take the %s option", call, optionNames[g])
		}
	}
	return nil
}

// WithModuleRoot sets the root directory for module-style imports. [Load] and
// [LoadSourcesWithEntry] take it, and there an empty root sets no root.
// [LoadString], which has no root, refuses it whatever root it names, and so
// does a load given [WithSyntheticRoot], whose root stands in for this one. Under
// [LoadSourcesWithEntry] a relative source key resolves under the root.
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
func WithModuleRoot(root string) Option {
	return func(c *loadConfig) {
		c.given = append(c.given, optModuleRoot)
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
// it.
func WithSourcesOnly(only bool) Option {
	return func(c *loadConfig) {
		c.given = append(c.given, optSourcesOnly)
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
// rather than silently degraded, in three cases: a root not of that form; the
// option beside [WithModuleRoot], which names the same concept twice; and the
// option passed to [Load] or [LoadString], where it can only ever be a no-op.
// The last two are refusals of an option the load does not take. A key the key
// rule below refuses is rejected outright too.
//
// A key is read by one rule:
// it is valid UTF-8, holds no backslash and no character that does not print, and once cleaned
// and in NFC it is relative and names a file, not the root or a directory above it. A key that escapes the root is
// permitted and yields a ".."-bearing identity, which stays stable and
// distinct. A relative import ("./x", "../x") resolves
// against the importing source's key, as text, and one climbing above the root
// keeps its "..". [Schema.ModuleRoot] reports the
// synthetic root, because the root is the one this load resolved imports
// against; a schema loaded this way is a supported input to
// [github.com/simon-lentz/yammm/schema/gogen.Marshal], whose embedded keys
// are then relative to that root.
func WithSyntheticRoot(root string) Option {
	return func(c *loadConfig) {
		c.given = append(c.given, optSyntheticRoot)
		c.syntheticRoot = root
		c.syntheticRootSet = true
	}
}

// WithImportsAllowed selects whether import declarations are processed. With
// false, the import declarations the parser keeps draw a single
// E_IMPORT_NOT_ALLOWED diagnostic at the first of them; the parser drops one
// whose path derives a reserved alias, with E_INVALID_ALIAS. The rejection
// does not end the analysis: it continues with the rejected aliases
// deferred as a failed import's is, and the rejected imports are never
// probed or resolved. The LSP markdown analysis path passes false for
// isolated blocks. [LoadString] refuses the option: it rejects every import
// declaration. With true — the default — imports resolve normally.
func WithImportsAllowed(allowed bool) Option {
	return func(c *loadConfig) {
		c.given = append(c.given, optImportsAllowed)
		c.disallowImports = !allowed
	}
}

// CaptureSources stores the load's source registry in *dst when the load
// starts, before it reads anything, so a load that fails early leaves *dst
// holding that load's empty registry, never an earlier load's. A load that
// fails returns a zero Schema, on which Schema.Sources panics, so a caller that
// renders excerpts for a failed load takes the sources from here. A nil dst
// captures nothing.
func CaptureSources(dst *Sources) Option {
	return func(c *loadConfig) {
		c.given = append(c.given, optCaptureSources)
		c.sourcesOut = dst
	}
}

// WithWarnings sets fn to receive every issue below failure severity the load
// draws: a Warning, Info or Hint. fn is called as each is found, in that
// order, on the goroutine that called the load and before it returns; no
// such issue is ever in the load's error. Without the option, and with a nil
// fn, such issues are discarded. Every load takes it.
func WithWarnings(fn func(diag.Issue)) Option {
	return func(c *loadConfig) {
		c.given = append(c.given, optWarnings)
		c.warnings = fn
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
	*c.sourcesOut = newSources(c.sourceRegistry)
}

package snapshot

import (
	"maps"
	"time"

	"github.com/simon-lentz/yammm/diag"
)

// Option configures snapshot serialization behavior.
type Option func(*config)

type config struct {
	indent    string
	createdAt time.Time
	metadata  map[string]string
}

func applyOptions(opts []Option) config {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithIndent sets the indentation string for the serialized output.
// Use "" for compact output (default), "\t" for tab indentation.
// Compact output is preferred for storage; indented output for inspection.
//
// Both compact and indented output include a valid integrity_hash in the
// header. The canonical-form technique works identically regardless of
// indentation.
func WithIndent(indent string) Option {
	return func(c *config) {
		c.indent = indent
	}
}

// WithCreatedAt sets the created_at timestamp in the snapshot header.
// If not set, created_at is omitted from the header (omitempty), preserving
// byte-level determinism. Breaking determinism is always opt-in: callers
// that need a timestamp must explicitly provide one.
func WithCreatedAt(t time.Time) Option {
	return func(c *config) {
		c.createdAt = t
	}
}

// WithMetadata sets user-provided key-value annotations in the snapshot header.
// If not set, the metadata field is omitted from the header (omitempty).
// Metadata is informational and does not affect schema compatibility. The
// integrity hash covers the full document including metadata; changing
// metadata requires re-marshaling, which recomputes the integrity hash.
// The map is shallow-copied at call time; subsequent mutations to the
// caller's map do not affect the serialized output.
func WithMetadata(m map[string]string) Option {
	return func(c *config) {
		if len(m) == 0 {
			c.metadata = nil
			return
		}
		c.metadata = make(map[string]string, len(m))
		maps.Copy(c.metadata, m)
	}
}

// LoadOption configures snapshot deserialization and validation behavior.
// Both [Load] and [Verify] accept LoadOption values — they share the same
// streamDecoder infrastructure and validation logic.
type LoadOption func(*loadConfig)

type loadConfig struct {
	skipIntegrityCheck bool
	valueConformance   bool

	// headerOnly marks the surfaces that read a header without trusting a
	// body. They keep the hash-algorithm mismatch non-fatal, so dispatch
	// can classify a stale document instead of receiving nil.
	headerOnly bool

	revalidate         bool
	revalidateSeverity diag.Severity

	issueLimit int
}

// defaultIssueLimit matches schema.Load's default.
const defaultIssueLimit = 100

func applyLoadOptions(opts []LoadOption) loadConfig {
	cfg := loadConfig{issueLimit: defaultIssueLimit}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithIssueLimit sets the maximum number of issues [Load] and [Verify] store.
// The walk always completes: past the limit the collector keeps the most
// severe issues seen, and [github.com/simon-lentz/yammm/diag.Result.DroppedCount],
// [github.com/simon-lentz/yammm/diag.Result.LimitReached] and
// [github.com/simon-lentz/yammm/diag.Result.TruncationNote] report the rest
// exactly. Set to 0 for unlimited. Default is 100, matching schema.Load.
func WithIssueLimit(limit int) LoadOption {
	return func(c *loadConfig) {
		c.issueLimit = limit
	}
}

// WithValueConformance selects whether [Load] and [Verify] report a stored
// value that does not conform to its schema constraint, as a Warning carrying
// [github.com/simon-lentz/yammm/diag.W_SNAPSHOT_VALUE_NONCONFORMING]. Off by
// default, so nothing changes and the walk costs nothing when nobody asks.
//
// It reports values of the three kinds that have a canonical stored form —
// Timestamp, Date and UUID — and nothing else. Bounds, enums, patterns and
// invariants stay unchecked. **This is not re-validation**, and a document it
// reports nothing for is not thereby valid; [WithRevalidation] is the full
// check.
//
// Warning severity is deliberate: Load still returns the snapshot together
// with the findings. Reporting a document is not refusing it, and the values
// themselves are rendered rather than rejected — [Load] never re-validates.
func WithValueConformance(report bool) LoadOption {
	return func(c *loadConfig) {
		c.valueConformance = report
	}
}

// WithRevalidation makes [Load] and [Verify] run every root instance — its
// composed children, association edges and invariants included — back
// through the real validator ([github.com/simon-lentz/yammm/instance.Validator])
// and report each finding at the given severity, typically
// [github.com/simon-lentz/yammm/diag.Warning] or
// [github.com/simon-lentz/yammm/diag.Error]. At Error severity a document
// that fails re-validation refuses to load.
//
// This is the full check [WithValueConformance] is not: bounds, enums,
// patterns, invariants, edge shapes and compositions are all evaluated. A
// loaded document can fail it, because the graph accepts what
// [github.com/simon-lentz/yammm/graph.RebuildSnapshot] and the bypass
// constructors assert without validation.
//
// An unresolved record for a Required association is reported as
// [github.com/simon-lentz/yammm/diag.W_SNAPSHOT_UNRESOLVED_REQUIRED]. An
// edge or composed child stored under a relation name its type does not
// declare is reported rather than silently skipped — such a document is
// exactly what the option exists to find. Off by default: without the
// option, Load returns what was written.
//
// A systematic defect on a large document produces one finding per instance;
// [WithIssueLimit] bounds what is stored (default 100), and the counts stay
// exact past it.
func WithRevalidation(severity diag.Severity) LoadOption {
	return func(c *loadConfig) {
		c.revalidate = true
		c.revalidateSeverity = severity
	}
}

// WithIntegrityCheck selects whether [Load] and [Verify] verify the
// integrity hash. With false, the hash is not checked; all other structural
// validation still runs and produces diagnostics with the standard snapshot
// codes. This follows etcd's SkipHashCheck pattern and supports debugging
// workflows where .ys files have been hand-edited for inspection. With true —
// the default — a mismatch draws E_SNAPSHOT_INTEGRITY_MISMATCH.
func WithIntegrityCheck(check bool) LoadOption {
	return func(c *loadConfig) {
		c.skipIntegrityCheck = !check
	}
}

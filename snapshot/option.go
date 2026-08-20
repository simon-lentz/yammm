package snapshot

import (
	"maps"
	"time"
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
}

func applyLoadOptions(opts []LoadOption) loadConfig {
	var cfg loadConfig
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithValueConformance makes [Load] and [Verify] report a stored value that
// does not conform to its schema constraint, as a Warning carrying
// [github.com/simon-lentz/yammm/diag.W_SNAPSHOT_VALUE_NONCONFORMING]. Off by
// default, so nothing changes and the walk costs nothing when nobody asks.
//
// It reports values of the three kinds that have a canonical stored form —
// Timestamp, Date and UUID — and nothing else. Bounds, enums, patterns and
// invariants stay unchecked. **This is not re-validation**, and a document it
// reports nothing for is not thereby valid.
//
// Warning severity is deliberate: Load still returns the snapshot together
// with the findings. Reporting a document is not refusing it, and the values
// themselves are rendered rather than rejected — [Load] never re-validates.
func WithValueConformance() LoadOption {
	return func(c *loadConfig) {
		c.valueConformance = true
	}
}

// WithSkipIntegrityCheck disables integrity hash verification on load
// and verify. All other structural validation still runs and produces
// diagnostics with the standard snapshot codes. This follows etcd's
// SkipHashCheck pattern and supports debugging workflows where .ys files
// have been hand-edited for inspection.
func WithSkipIntegrityCheck() LoadOption {
	return func(c *loadConfig) {
		c.skipIntegrityCheck = true
	}
}

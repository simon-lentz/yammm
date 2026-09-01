package neo4j

// Edition represents a Neo4j deployment edition.
type Edition int

const (
	// Enterprise supports all constraint types.
	Enterprise Edition = iota
	// Community supports UNIQUE constraints only.
	// EXISTS (NOT NULL), KEY, and PROPERTY_TYPE constraints are Enterprise-only.
	Community
)

type adapterConfig struct {
	labelSeparator              string
	labelPrefix                 string
	scalarTypeConstraints       bool
	requiredOnlyTypeConstraints bool
	nodeKeyConstraints          bool
	namedConstraints            bool
	edition                     Edition
}

// Option configures Adapter behavior.
type Option func(*adapterConfig)

// Adapter generates Neo4j constraint statements and label mappings from yammm schemas.
//
// Thread Safety: Adapter is safe for concurrent use after construction.
// Configuration is immutable after New() returns.
type Adapter struct {
	config adapterConfig
}

func defaultConfig() adapterConfig {
	return adapterConfig{
		labelSeparator:              "__",
		labelPrefix:                 "",
		scalarTypeConstraints:       true,
		requiredOnlyTypeConstraints: false,
		nodeKeyConstraints:          false,
		namedConstraints:            true,
		edition:                     Enterprise,
	}
}

// New creates a new Neo4j adapter with the given options.
func New(opts ...Option) *Adapter {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Adapter{config: cfg}
}

// WithLabelSeparator sets the separator between schema name and type name in labels.
// Default: "__" (double underscore), producing labels like "book_catalog__Publisher".
func WithLabelSeparator(sep string) Option {
	return func(c *adapterConfig) {
		c.labelSeparator = sep
	}
}

// WithLabelPrefix adds a global prefix to all generated labels.
// Default: "" (no prefix). Example: WithLabelPrefix("app_") produces "app_book_catalog__Publisher".
//
// The prefix is only applied when a schema name is provided to [Adapter.Label].
// When schemaName is empty (legacy/unscoped usage), the label is the sanitized
// type name alone, without any prefix.
func WithLabelPrefix(prefix string) Option {
	return func(c *adapterConfig) {
		c.labelPrefix = prefix
	}
}

// WithScalarTypeConstraints controls whether PROPERTY-TYPE constraints —
// `REQUIRE n.prop IS :: T` — are generated. Default: true.
//
// It covers list-shaped types as well as scalars, because they are one kind of
// statement: a `Vector[4]` and a `List<Float>` emit the same expression, and
// gating them apart suppressed the constraint for one declaration and not for
// an identical one.
//
// **Property-type constraints require Neo4j 5.9**, which is older than the 5.0
// floor this adapter otherwise supports. On a server between 5.0 and 5.8 this
// option is what turns them off; nothing else does, and the adapter has no
// version input with which to decide for itself.
//
// When enabled alongside WithRequiredOnlyTypeConstraints(true), only required
// properties receive type constraints. See WithRequiredOnlyTypeConstraints
// for rationale.
func WithScalarTypeConstraints(enabled bool) Option {
	return func(c *adapterConfig) {
		c.scalarTypeConstraints = enabled
	}
}

// WithRequiredOnlyTypeConstraints restricts scalar and list type constraints
// to required properties only. Optional properties are skipped.
//
// This reduces constraint volume significantly. For book_catalog (~83 properties,
// ~24 required), this cuts TYPE constraints from ~83 to ~24 -- a 3.5x reduction.
//
// Rationale: optional properties are often absent from nodes entirely, and
// Neo4j's IS :: TYPE constraint passes for absent properties (it only fires
// when the property exists). The primary value of type constraints is catching
// writes that supply a present-but-wrong-typed value, which is most dangerous
// for required properties that are always present.
//
// Has no effect if WithScalarTypeConstraints(false) is set.
// Default: false (type constraints for all properties).
func WithRequiredOnlyTypeConstraints(enabled bool) Option {
	return func(c *adapterConfig) {
		c.requiredOnlyTypeConstraints = enabled
	}
}

// WithNodeKeyConstraints controls whether NODE KEY constraints are used instead
// of separate UNIQUE + NOT NULL for primary keys. NODE KEY is semantically
// equivalent but expressed as a single constraint. Requires Neo4j 5.7+.
// Requires Enterprise edition. Under [WithEdition]([Community]) the request
// cannot be honored — a Community server holds no NODE KEY — so primary keys
// fall back to UNIQUE, the strongest kind that edition affords, and
// [W_NEO4J_NODE_KEY_UNSUPPORTED] reports the substitution. Community output is
// therefore identical whether or not this option is set.
//
// Default: false (generates separate UNIQUE + NOT NULL for broader compatibility).
func WithNodeKeyConstraints(enabled bool) Option {
	return func(c *adapterConfig) {
		c.nodeKeyConstraints = enabled
	}
}

// WithNamedConstraints controls whether generated constraints include explicit names.
// Named constraints use a deterministic convention: {label}_{property}_{kind}
// (e.g., "book_catalog__Publisher_publisher_id_unique").
//
// Named constraints are recommended for production use because:
//   - Anonymous constraint names are opaque hashes that differ across environments.
//   - DROP CONSTRAINT requires a name; anonymous constraints cannot be dropped by name portably.
//   - Diff/plan tooling requires stable names to compare desired vs actual state.
//
// Default: true.
func WithNamedConstraints(enabled bool) Option {
	return func(c *adapterConfig) {
		c.namedConstraints = enabled
	}
}

// WithEdition sets the target Neo4j edition. Constraint types that require
// Enterprise edition are omitted when Community is selected — they have no
// Community equivalent to fall back to — and
// [W_NEO4J_EDITION_CONSTRAINT_OMITTED] reports the omission once per call with
// a count per kind. NODE KEY is the exception:
// it stands in for UNIQUE + NOT NULL rather than expressing something of its
// own, so under Community it degrades to its UNIQUE half instead of vanishing,
// and [W_NEO4J_NODE_KEY_UNSUPPORTED] reports that it did. See
// [WithNodeKeyConstraints].
//
// Community edition supports: UNIQUE constraints only.
// Enterprise edition supports: UNIQUE, NOT NULL, NODE KEY, PROPERTY_TYPE.
//
// Default: Enterprise.
func WithEdition(edition Edition) Option {
	return func(c *adapterConfig) {
		c.edition = edition
	}
}

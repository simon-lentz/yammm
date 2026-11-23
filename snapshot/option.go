package snapshot

import (
	"fmt"
	"maps"
	"time"

	"github.com/simon-lentz/yammm/diag"
)

// Option configures a call of this package. Each call's doc names the options
// it takes, and a call refuses any other with an error, with no diagnostic
// code, that names the option and the call.
type Option func(*config)

// optionKind names the constructor that built an [Option], so a call can refuse
// one it does not take.
type optionKind uint8

const (
	optIndent optionKind = iota
	optCreatedAt
	optMetadata
	optIssueLimit
	optRevalidation
	optIntegrityCheck
	optWarnings
)

var optionNames = [...]string{
	optIndent:         "WithIndent",
	optCreatedAt:      "WithCreatedAt",
	optMetadata:       "WithMetadata",
	optIssueLimit:     "WithIssueLimit",
	optRevalidation:   "WithRevalidation",
	optIntegrityCheck: "WithIntegrityCheck",
	optWarnings:       "WithWarnings",
}

type config struct {
	given []optionKind // every option passed, in argument order

	indent    string
	createdAt time.Time
	metadata  map[string]string

	skipIntegrityCheck bool

	// headerOnly marks the surfaces that read a header without trusting a
	// body. They accept an unrecognized hash algorithm, so dispatch can
	// classify a stale document by its header instead of receiving nil.
	headerOnly bool

	revalidate         bool
	revalidateSeverity diag.Severity

	issueLimit int

	// warnings receives each issue below failure severity a read draws; nil
	// discards them.
	warnings func(diag.Issue)
}

// defaultIssueLimit equals the cap schema.Load holds.
const defaultIssueLimit = 100

// newConfig applies opts to the state every call starts from. A surface that
// takes no option starts from it too, so its error holds at most
// defaultIssueLimit failures.
func newConfig(opts []Option) config {
	cfg := config{issueLimit: defaultIssueLimit}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// refuse returns an error naming the first option, in argument order, that
// call does not take, or nil when it takes every option it was given.
func (c *config) refuse(call string, takes ...optionKind) error {
	for _, g := range c.given {
		taken := false
		for _, t := range takes {
			taken = taken || g == t
		}
		if !taken {
			return fmt.Errorf("snapshot.%s does not take the %s option", call, optionNames[g])
		}
	}
	return nil
}

// WithIndent sets the indentation string for the serialized output.
// Use "" for compact output (default), "\t" for tab indentation.
// Compact output is preferred for storage; indented output for inspection.
//
// Both compact and indented output include a valid integrity_hash in the
// header. The canonical-form technique works identically regardless of
// indentation.
//
// The indent must be JSON whitespace: space, tab, line feed or carriage
// return. [Marshal] refuses anything else with an
// Error-severity [diag.E_SNAPSHOT_MALFORMED] rather than emitting it: the
// string is written between JSON tokens, so a non-whitespace one produces a
// document that is not JSON at all — which this package's own [Unmarshal] refuses
// and [WriteFile] would put on disk unreadable. Only [Marshal] takes it.
func WithIndent(indent string) Option {
	return func(c *config) {
		c.given = append(c.given, optIndent)
		c.indent = indent
	}
}

// WithCreatedAt sets the header's created_at to t, which [Marshal] writes as
// t.Format(time.RFC3339Nano) in t's own offset, as encoding/json writes a
// [time.Time]. A zero t writes no created_at, so output is deterministic unless
// a caller opts in. Marshal refuses a t that has no RFC 3339 form, a year past
// 9999 and an offset that is no whole number of minutes among them, with Error
// [diag.E_SNAPSHOT_MALFORMED]. Only [Marshal] takes it.
func WithCreatedAt(t time.Time) Option {
	return func(c *config) {
		c.given = append(c.given, optCreatedAt)
		c.createdAt = t
	}
}

// WithMetadata sets user-provided key-value annotations in the snapshot header.
// If not set, the metadata field is omitted from the header (omitempty).
// Metadata is informational and does not affect schema compatibility. The
// integrity hash covers the full document including metadata; changing
// metadata means a new hash, which re-marshaling and [UpdateMetadata] each
// compute.
// The map is shallow-copied at call time; subsequent mutations to the
// caller's map do not affect the serialized output. Each key and value must be
// valid UTF-8: [Marshal] refuses one that is not, since encoding/json would
// write each byte that is not as U+FFFD. Only [Marshal] takes it.
func WithMetadata(m map[string]string) Option {
	return func(c *config) {
		c.given = append(c.given, optMetadata)
		if len(m) == 0 {
			c.metadata = nil
			return
		}
		c.metadata = make(map[string]string, len(m))
		maps.Copy(c.metadata, m)
	}
}

// WithIssueLimit sets the maximum number of failures the error of [Unmarshal]
// and [Verify] holds. The walk completes unless the context is cancelled: past
// the limit the error keeps the most severe failures seen, and its
// [github.com/simon-lentz/yammm/diag.Result.DroppedCount],
// [github.com/simon-lentz/yammm/diag.Result.LimitReached] and
// [github.com/simon-lentz/yammm/diag.Result.TruncationNote] report the rest
// exactly. Set to 0 for unlimited; the default is 100, the cap schema.Load
// holds. A warning takes no cap, since [WithWarnings] receives every one.
func WithIssueLimit(limit int) Option {
	return func(c *config) {
		c.given = append(c.given, optIssueLimit)
		c.issueLimit = limit
	}
}

// WithRevalidation makes [Unmarshal] and [Verify] run root instances, their
// composed children, edges, target_missing records and invariants included,
// back through the real validator
// ([github.com/simon-lentz/yammm/instance.Validator]) and report its findings
// about the data at the given severity, after every structural issue (a
// cancellation or an E_INTERNAL keeps its own); at Error a document that fails
// refuses to load, and below failure severity each finding goes to
// [WithWarnings]. A root is run only when the structural checks left it
// whole: no check refused its type, a unit of its tree, one of its edges (an
// edge that names no root is the exception below), one of its unresolved
// records, or one of its required or (one) associations, no record whose
// types-table row the reader refused could be its, and no other root the
// reader walks stands at its address. The reader walks no root of an instances
// entry that repeats a types-table row or names none. An edge that names no root
// does not stop it, since the validator does not judge whether a target exists, unless it stands beside
// another record under a (one) association. The validator judges no edge's
// target key, which the root at that address, the refusal of a unit there or
// the edge's reference issue judges, nor the target key of a target_missing
// record whose target a refused unit stands at or states. A listing that repeats a target no sound root holds
// alone is left out of the root's run, so the target's own issue stands for the
// repeat and the root's later passes still run.
// No duplicate record's instance is run. It judges values — bounds,
// enums, patterns, invariants — which the structural checks every read runs
// leave alone, and reports each unresolved record no check refused under a
// required association as
// [github.com/simon-lentz/yammm/diag.W_SNAPSHOT_UNRESOLVED_REQUIRED].
// The read holds one row's findings at a time: a row's findings are reported
// when its run ends, so a row that draws many costs memory in proportion until
// then. At Fatal or Error [WithIssueLimit] bounds what a systematic defect
// stores (default 100), and the counts stay exact past it. It panics when
// severity is not one of Fatal to Hint (programming error).
func WithRevalidation(severity diag.Severity) Option {
	if severity < diag.Fatal || severity > diag.Hint {
		panic(fmt.Sprintf("snapshot.WithRevalidation: invalid severity %d", severity))
	}
	return func(c *config) {
		c.given = append(c.given, optRevalidation)
		c.revalidate = true
		c.revalidateSeverity = severity
	}
}

// WithIntegrityCheck selects whether [Unmarshal], [Verify] and
// [UpdateMetadata] verify the integrity hash. With false, the hash is not
// checked; all other structural validation still runs and produces diagnostics
// with the standard snapshot codes. This follows etcd's SkipHashCheck pattern and
// supports debugging workflows where .ys files have been hand-edited for
// inspection; [UpdateMetadata] then seals under a new hash any body whose shape
// and text it accepts. With true — the default — a mismatch, or a header that
// holds no hash (an empty, null or absent integrity_hash), draws
// E_SNAPSHOT_INTEGRITY_MISMATCH.
func WithIntegrityCheck(check bool) Option {
	return func(c *config) {
		c.given = append(c.given, optIntegrityCheck)
		c.skipIntegrityCheck = !check
	}
}

// WithWarnings sets fn to receive every issue below failure severity that
// [Unmarshal] or [Verify] draws: a Warning, Info or Hint, which only
// [WithRevalidation] below failure severity produces. fn is called for each in
// the order found, a row's findings when its run ends, on the goroutine that
// made the call, never concurrently and before the call returns; no such issue
// is ever in the call's error, and none is capped. Without the option, and with
// a nil fn, such issues are discarded.
func WithWarnings(fn func(diag.Issue)) Option {
	return func(c *config) {
		c.given = append(c.given, optWarnings)
		c.warnings = fn
	}
}

package format_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/schema"
)

// knownBroken maps a round-trip fixture to the damage the formatter does to it
// today. Every entry is asserted to still hold, so a repair turns this test red
// and the entry moves rather than lapsing unnoticed.
var knownBroken = map[string]roundTripDamage{
	"g1_single_quoted_value_with_comma.yammm":    {kind: damageRefused, want: "formatting a single-quoted value must not split it at an interior comma"},
	"g2_extends_header_trailing_comment.yammm":   {kind: damageRefused, want: "a trailing comment on an extends header must not absorb the parent list"},
	"g3_comment_on_enum_value_line.yammm":        {kind: damageRefused, want: "a comment on an enum value line must not absorb the values after it"},
	"g4_logical_op_in_trailing_comment.yammm":    {kind: damageRefused, want: "a wrap point must not be chosen inside a trailing comment"},
	"g5_bracket_in_string_on_closing_line.yammm": {kind: damageAltered, want: "a bracket inside a string literal must not be read as the construct's terminator"},
	"g11_annotated_enum_that_wraps.yammm":        {kind: damageRefused, want: "a wrapped property must not leave its annotation attached to nothing"},
}

type damageKind int

const (
	// damageRefused: the formatted text no longer parses or no longer loads.
	damageRefused damageKind = iota
	// damageAltered: the formatted text loads and means something else.
	damageAltered
)

type roundTripDamage struct {
	kind damageKind
	want string
}

func (d damageKind) String() string {
	if d == damageAltered {
		return "loads but the structural hash moved"
	}
	return "no longer loads"
}

// TestTokenStream_CorpusRoundTrip pins what formatting does to a schema that
// loads clean. A golden is the wrong instrument here: it records whatever the
// formatter emits, so it passes on corrupt output and locks the corruption in.
// The oracle is the structural hash, which catches a value the formatter
// changed without breaking the parse.
func TestTokenStream_CorpusRoundTrip(t *testing.T) {
	t.Parallel()

	fixtures := discoverRoundTrip(t)
	t.Logf("discovered %d round-trip fixtures, %d recorded broken", len(fixtures), len(knownBroken))

	for _, path := range fixtures {
		name := strings.TrimPrefix(path, roundTripDir+"/")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			before, ok := loadHash(t, string(src))
			if !ok {
				t.Fatalf("fixture does not load; a round-trip claim needs a clean input")
			}

			out, err := format.TokenStream(string(src))
			if err != nil {
				t.Fatalf("TokenStream returned an error: %v", err)
			}
			after, loaded := loadHash(t, out)

			damage, recorded := knownBroken[name]
			switch {
			case !recorded:
				if !loaded {
					t.Errorf("formatting turned a loading schema into one that does not load:\n%s", out)
					return
				}
				if after != before {
					t.Errorf("formatting changed the schema's meaning: hash %s -> %s\n%s", before, after, out)
				}
			case damage.kind == damageRefused:
				if loaded {
					t.Errorf("recorded as %q but the output loads now — remove the knownBroken entry; the repair is %s",
						damage.kind, damage.want)
				}
			case damage.kind == damageAltered:
				if !loaded {
					t.Errorf("recorded as %q but the output no longer loads at all", damage.kind)
					return
				}
				if after == before {
					t.Errorf("recorded as %q but the hash is unchanged now — remove the knownBroken entry; the repair is %s",
						damage.kind, damage.want)
				}
			}
		})
	}
}

// TestKnownBrokenNamesLiveFixtures fails on an entry naming no fixture, which
// would silently assert nothing about anything.
func TestKnownBrokenNamesLiveFixtures(t *testing.T) {
	t.Parallel()

	live := make(map[string]bool, len(knownBroken))
	for _, p := range discoverRoundTrip(t) {
		live[strings.TrimPrefix(p, roundTripDir+"/")] = true
	}
	for name := range knownBroken {
		if !live[name] {
			t.Errorf("knownBroken names %q, which is not a fixture under %s", name, roundTripDir)
		}
	}
}

// loadHash loads src and returns its structural hash, reporting whether the
// load produced no error.
func loadHash(t *testing.T, src string) (string, bool) {
	t.Helper()
	s, result := schema.LoadString(context.Background(), src, "roundtrip.yammm")
	if result.Err() != nil || s == nil {
		return "", false
	}
	return schema.StructuralHash(s), true
}

// discoverRoundTrip returns every fixture under the round-trip directory. It
// fails on an empty result, which would pass over nothing.
func discoverRoundTrip(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, in := range discoverInputs(t) {
		if strings.HasPrefix(in, roundTripDir+"/") {
			found = append(found, in)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no round-trip fixtures discovered under %s", roundTripDir)
	}
	return found
}

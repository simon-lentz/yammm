package value_test

import (
	"encoding/binary"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// Canonical returns the text a Timestamp, UUID or Date stores unchanged, and
// reaches that text from every spelling it reads in one pass. The snapshot
// reader refuses a key component Canonical moves, so a key a writer stored must
// be one it does not move.
func TestCanonical_StoredTextIsAFixedPoint(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed makes the values reproducible
	check := func(in any, c schema.Constraint) {
		t.Helper()
		once, err := value.Canonical(in, c)
		if err != nil {
			t.Fatalf("Canonical(%#v): %v", in, err)
		}
		text, ok := once.(string)
		if !ok {
			t.Fatalf("Canonical(%#v) = %#v, want text", in, once)
		}
		twice, err := value.Canonical(text, c)
		if err != nil || twice != text {
			t.Fatalf("Canonical(%q) = %#v, %v; want it unchanged", text, twice, err)
		}
	}
	ts, ud, dt := schema.NewTimestampConstraint(), schema.NewUUIDConstraint(), schema.NewDateConstraint()
	for range 20000 {
		// An offset under 24 hours, in whole minutes, and an instant in years 1
		// to 9998 at every offset.
		offset := (r.IntN(2*24*60-1) - (24*60 - 1)) * 60
		at := time.Unix(r.Int64N(315_000_000_000)-62_000_000_000, r.Int64N(1_000_000_000)).
			In(time.FixedZone("", offset))
		check(at, ts)
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000000000Z07:00"} {
			check(at.Format(layout), ts)
		}
		check(at, dt)
		check(at.Format(time.DateOnly), dt)

		var u uuid.UUID
		binary.LittleEndian.PutUint64(u[:8], r.Uint64())
		binary.LittleEndian.PutUint64(u[8:], r.Uint64())
		check(u, ud)
		for _, spelled := range []string{
			u.String(),
			strings.ToUpper(u.String()),
			"{" + u.String() + "}",
			"urn:uuid:" + u.String(),
			strings.ReplaceAll(u.String(), "-", ""),
		} {
			check(spelled, ud)
		}
	}
}

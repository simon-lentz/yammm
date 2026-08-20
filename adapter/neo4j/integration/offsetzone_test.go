//go:build neo4j_integration

package integration

import (
	"context"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	n4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// TestCoerce_OffsetBearingTimestampReachesTheServer is the measurement behind
// the offset repair, taken against a real server rather than argued from the
// driver's source.
//
// A Timestamp value canonicalizes to RFC 3339 text, and text carrying a numeric
// offset parses into a time.Time whose location has no name. The driver picks
// its wire encoding by the zone abbreviation: it sends an offset only when
// Zone() reports "Offset", and otherwise sends Location().String() as a
// time-zone identifier — empty, for an unnamed location, which the server
// rejects. Coerce now re-expresses such an instant in the driver's own offset
// form.
//
// The unrepaired shape is written alongside as the control. Without it the test
// cannot tell the repair from a server that accepts anything. The control takes
// its own connection pool: see isolatedDriver.
func TestCoerce_OffsetBearingTimestampReachesTheServer(t *testing.T) {
	ctx := context.Background()
	driver(t)

	const text = "2026-08-19T12:00:00+02:00"
	want, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}

	// The control: what Coerce produced before the repair. It runs on its own
	// pool, because the server rejects it and a rejected query leaves its
	// connection needing a RESET — which the assertion below must not inherit.
	if _, err := neo4jdriver.ExecuteQuery(ctx, isolatedDriver(t), "RETURN $v AS v",
		map[string]any{"v": want}, neo4jdriver.EagerResultTransformer); err == nil {
		t.Log("an unnamed-zone instant was accepted; the driver or server changed, " +
			"so the repair is now belt-and-braces rather than load-bearing")
	} else {
		t.Logf("unrepaired shape rejected as expected: %v", err)
	}

	coerced, err := n4j.Coerce(schema.NewTimestampConstraint(), text)
	if err != nil {
		t.Fatalf("Coerce(%q): %v", text, err)
	}

	rec := singleWithParams(t, ctx, "RETURN $v AS v", map[string]any{"v": coerced})
	got, ok := rec["v"].(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", rec["v"])
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if _, offset := got.Zone(); offset != 2*60*60 {
		t.Errorf("round trip lost the offset: got %d seconds, want %d", offset, 2*60*60)
	}
}

// singleWithParams runs a parameterised query expected to return one record.
func singleWithParams(t *testing.T, ctx context.Context, cypher string, params map[string]any) map[string]any {
	t.Helper()
	res, err := neo4jdriver.ExecuteQuery(ctx, driver(t), cypher, params,
		neo4jdriver.EagerResultTransformer)
	if err != nil {
		t.Fatalf("executing %q: %v", cypher, err)
	}
	if len(res.Records) != 1 {
		t.Fatalf("expected 1 record from %q, got %d", cypher, len(res.Records))
	}
	return res.Records[0].AsMap()
}

// TestCoerce_TextDerivedInstantIsAcceptedWhateverTheHostZone is the server-side
// half of the host-dependence fix.
//
// time.Parse resolves text whose offset matches the local zone to time.Local,
// whose name the driver sends as a time-zone identifier — a name no tz database
// holds, which the server refuses with "Illegal zone identifier". The unrepaired
// shape is written first as the control, on its own pool, because that rejection
// is the kind that leaves a connection needing a RESET.
//
// It sets time.Local rather than reading it: a host already in UTC cannot
// reproduce the case, and every CI runner is one.
func TestCoerce_TextDerivedInstantIsAcceptedWhateverTheHostZone(t *testing.T) {
	ctx := context.Background()
	driver(t)

	restore := time.Local
	t.Cleanup(func() { time.Local = restore })
	time.Local = time.FixedZone("XYZ", -4*60*60)

	want := time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local)
	text := want.Format(time.RFC3339Nano)

	// The control: what the string arm produced before the repair.
	unrepaired, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	if unrepaired.Location() != time.Local {
		t.Fatalf("fixture no longer resolves to time.Local")
	}
	if _, err := neo4jdriver.ExecuteQuery(ctx, isolatedDriver(t), "RETURN $v AS v",
		map[string]any{"v": unrepaired}, neo4jdriver.EagerResultTransformer); err == nil {
		t.Log("the host-zoned shape was accepted; the driver or server changed")
	} else {
		t.Logf("host-zoned shape rejected as expected: %v", err)
	}

	coerced, err := n4j.Coerce(schema.NewTimestampConstraint(), text)
	if err != nil {
		t.Fatalf("Coerce(%q): %v", text, err)
	}

	rec := singleWithParams(t, ctx, "RETURN $v AS v", map[string]any{"v": coerced})
	got, ok := rec["v"].(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", rec["v"])
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if _, offset := got.Zone(); offset != -4*60*60 {
		t.Errorf("round trip returned offset %d, want %d", offset, -4*60*60)
	}
}

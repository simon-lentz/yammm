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

	echoed := echoParam(t, ctx, coerced)
	got, ok := echoed.(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", echoed)
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if _, offset := got.Zone(); offset != 2*60*60 {
		t.Errorf("round trip lost the offset: got %d seconds, want %d", offset, 2*60*60)
	}
}

// echoParam sends v to the server as a parameter and returns it as the
// server echoes it back.
func echoParam(t *testing.T, ctx context.Context, v any) any {
	t.Helper()
	const cypher = "RETURN $v AS v"
	res, err := neo4jdriver.ExecuteQuery(ctx, driver(t), cypher, map[string]any{"v": v},
		neo4jdriver.EagerResultTransformer)
	if err != nil {
		t.Fatalf("executing %q: %v", cypher, err)
	}
	if len(res.Records) != 1 {
		t.Fatalf("expected 1 record from %q, got %d", cypher, len(res.Records))
	}
	return res.Records[0].AsMap()["v"]
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

	echoed := echoParam(t, ctx, coerced)
	got, ok := echoed.(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", echoed)
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if _, offset := got.Zone(); offset != -4*60*60 {
		t.Errorf("round trip returned offset %d, want %d", offset, -4*60*60)
	}
}

// withLocal substitutes time.Local for the test's duration; the caller must
// not run in parallel.
func withLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	restore := time.Local
	t.Cleanup(func() { time.Local = restore })
	time.Local = loc
}

// TestCoerce_HostLocalTimeValueIsAcceptedWhateverTheHostZone is the
// server-side half of the time.Time-arm rule: a value built in time.Local —
// what time.Now produces — reaches the server whatever name the host's
// location carries. The unrepaired shape is the control, on its own pool.
func TestCoerce_HostLocalTimeValueIsAcceptedWhateverTheHostZone(t *testing.T) {
	ctx := context.Background()
	driver(t)
	withLocal(t, time.FixedZone("XYZ", -4*60*60))

	want := time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local)
	if _, err := neo4jdriver.ExecuteQuery(ctx, isolatedDriver(t), "RETURN $v AS v",
		map[string]any{"v": want}, neo4jdriver.EagerResultTransformer); err == nil {
		t.Log("the host-zoned shape was accepted; the driver or server changed")
	} else {
		t.Logf("host-zoned shape rejected as expected: %v", err)
	}

	coerced, err := n4j.Coerce(schema.NewTimestampConstraint(), want)
	if err != nil {
		t.Fatalf("Coerce: %v", err)
	}
	echoed := echoParam(t, ctx, coerced)
	got, ok := echoed.(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", echoed)
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if _, offset := got.Zone(); offset != -4*60*60 {
		t.Errorf("round trip returned offset %d, want %d", offset, -4*60*60)
	}
}

// TestCoerce_UnresolvableZoneNameIsAcceptedAsOffset covers a caller-built
// location whose name no tz database holds: sent by name the server refuses
// it, so Coerce sends the offset.
func TestCoerce_UnresolvableZoneNameIsAcceptedAsOffset(t *testing.T) {
	ctx := context.Background()
	driver(t)

	want := time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("Mars/Olympus", 3600))
	if _, err := neo4jdriver.ExecuteQuery(ctx, isolatedDriver(t), "RETURN $v AS v",
		map[string]any{"v": want}, neo4jdriver.EagerResultTransformer); err == nil {
		t.Log("an unresolvable zone name was accepted; the driver or server changed")
	} else {
		t.Logf("unresolvable name rejected as expected: %v", err)
	}

	coerced, err := n4j.Coerce(schema.NewTimestampConstraint(), want)
	if err != nil {
		t.Fatalf("Coerce: %v", err)
	}
	echoed := echoParam(t, ctx, coerced)
	got, ok := echoed.(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", echoed)
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if _, offset := got.Zone(); offset != 3600 {
		t.Errorf("round trip returned offset %d, want 3600", offset)
	}
}

// TestCoerce_ResolvableZoneNameRoundTripsByName is the kept-name control: an
// IANA location is a zone identifier the server resolves, and it comes back
// by name rather than as an offset.
func TestCoerce_ResolvableZoneNameRoundTripsByName(t *testing.T) {
	ctx := context.Background()
	driver(t)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}

	want := time.Date(2026, 8, 19, 12, 0, 0, 0, berlin)
	coerced, err := n4j.Coerce(schema.NewTimestampConstraint(), want)
	if err != nil {
		t.Fatalf("Coerce: %v", err)
	}
	echoed := echoParam(t, ctx, coerced)
	got, ok := echoed.(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", echoed)
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if got.Location().String() != "Europe/Berlin" {
		t.Errorf("round trip returned location %q, want Europe/Berlin", got.Location())
	}
}

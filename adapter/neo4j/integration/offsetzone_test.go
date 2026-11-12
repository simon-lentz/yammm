//go:build neo4j_integration

package integration

import (
	"context"
	"errors"
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
// rejects. CoerceParams re-expresses such an instant in the driver's own offset
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

	// The control: an instant in an unnamed zone, unrepaired. It runs on its own
	// pool, because the server rejects it and a rejected query leaves its
	// connection needing a RESET — which the assertion below must not inherit.
	if _, err := neo4jdriver.ExecuteQuery(ctx, isolatedDriver(t), "RETURN $v AS v",
		map[string]any{"v": want}, neo4jdriver.EagerResultTransformer); err == nil {
		t.Log("an unnamed-zone instant was accepted; the driver or server changed, " +
			"so the repair is now belt-and-braces rather than load-bearing")
	} else {
		t.Logf("unrepaired shape rejected as expected: %v", err)
	}

	coerced := coerceTimestamp(t, text)

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

	coerced := coerceTimestamp(t, text)

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

// A value built in time.Local, as time.Now builds one, reaches the server
// whatever name the host's location carries: CoerceParams sends the offset its
// stored text carries. The uncoerced value is the control, on its own pool.
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

	coerced := coerceTimestamp(t, want)
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

// A location whose name no tz database holds reaches the server as its offset.
// Sent by name the server refuses it, which the uncoerced control shows.
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

	coerced := coerceTimestamp(t, want)
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

// A time.Time in an IANA location reaches the server as its offset, never by the
// location's name: CoerceParams sends what the stored text holds.
func TestCoerce_ACallersZoneNameReachesTheServerAsItsOffset(t *testing.T) {
	ctx := context.Background()
	driver(t)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}

	want := time.Date(2026, 8, 19, 12, 0, 0, 0, berlin)
	echoed := echoParam(t, ctx, coerceTimestamp(t, want))
	got, ok := echoed.(time.Time)
	if !ok {
		t.Fatalf("server returned %T, want time.Time", echoed)
	}
	if !got.Equal(want) {
		t.Errorf("round trip returned %s, want the same instant as %s", got, want)
	}
	if name, offset := got.Zone(); name != "Offset" || offset != 2*60*60 {
		t.Errorf("round trip returned zone %q at %d seconds, want Offset at %d", name, offset, 2*60*60)
	}
}

// The server reads one instant under a zone name and under that zone's offset
// as two values: they are not equal, and a MERGE on each makes two nodes. So a
// time.Time and the text the validator stores for it must reach the server in
// one form, or a hand-built MERGE misses the node the write path made.
func TestZoneNameAgainstOffset_ServerResponse(t *testing.T) {
	ctx := context.Background()
	driver(t)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	const label = "ZoneNameAgainstOffset"
	clear := func() { run(t, ctx, "MATCH (n:"+label+") DETACH DELETE n") }
	clear()
	t.Cleanup(clear)

	named := time.Date(2026, 8, 19, 12, 0, 0, 0, berlin)
	byOffset := named.In(time.FixedZone("Offset", 2*60*60))
	res, err := neo4jdriver.ExecuteQuery(ctx, driver(t), "RETURN $a = $b AS equal",
		map[string]any{"a": named, "b": byOffset}, neo4jdriver.EagerResultTransformer)
	if err != nil {
		t.Fatalf("comparing the two forms: %v", err)
	}
	if equal := res.Records[0].AsMap()["equal"]; equal != false {
		t.Errorf("the server compares a named-zone instant and its offset form as %v, want false", equal)
	}
	for _, v := range []any{named, byOffset} {
		execWithParams(t, ctx, "MERGE (n:"+label+" {at: $v})", map[string]any{"v": v})
	}
	if n := countNodes(t, ctx, label); n != 2 {
		t.Errorf("a MERGE by name and a MERGE by offset left %d nodes, want 2", n)
	}

	clear()
	for _, raw := range []any{named, "2026-08-19T12:00:00+02:00"} {
		execWithParams(t, ctx, "MERGE (n:"+label+" {at: $v})", map[string]any{"v": coerceTimestamp(t, raw)})
	}
	if n := countNodes(t, ctx, label); n != 1 {
		t.Errorf("the coerced time.Time and the coerced stored text left %d nodes, want 1", n)
	}
}

// An instant reaches the server as the value Cypher's datetime() reads from its
// RFC 3339 text, a UTC instant included: the two are equal, the zone the server
// holds is the offset, and a MERGE on each finds one node. The same instant
// under the zone name UTC is the control, which the server holds unequal.
func TestCoerce_AnInstantIsTheValueCypherReadsFromItsText(t *testing.T) {
	ctx := context.Background()
	driver(t)
	const label = "InstantAgainstCypherText"
	clear := func() { run(t, ctx, "MATCH (n:"+label+") DETACH DELETE n") }
	clear()
	t.Cleanup(clear)

	utc := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	const utcText = "2026-08-19T12:00:00Z"
	equalsText := func(v any, text string) (equal, zone any) {
		res, err := neo4jdriver.ExecuteQuery(ctx, driver(t), "RETURN $v = datetime($text) AS equal, $v.timezone AS zone",
			map[string]any{"v": v, "text": text}, neo4jdriver.EagerResultTransformer)
		if err != nil {
			t.Fatalf("comparing %v with datetime(%q): %v", v, text, err)
		}
		row := res.Records[0].AsMap()
		return row["equal"], row["zone"]
	}
	for _, row := range []struct {
		raw        any
		text, zone string
	}{
		{utcText, utcText, "Z"},
		{"2026-08-19T12:00:00+00:00", utcText, "Z"},
		{utc, utcText, "Z"},
		{"2026-08-19T12:00:00+02:00", "2026-08-19T12:00:00+02:00", "+02:00"},
		{"2026-08-19T12:00:00.123456789-05:30", "2026-08-19T12:00:00.123456789-05:30", "-05:30"},
	} {
		coerced := coerceTimestamp(t, row.raw)
		if equal, zone := equalsText(coerced, row.text); equal != true || zone != row.zone {
			t.Errorf("%v: equal to datetime(%q) = %v in zone %v, want true in zone %q", row.raw, row.text, equal, zone, row.zone)
		}
		clear()
		execWithParams(t, ctx, "MERGE (n:"+label+" {at: $v})", map[string]any{"v": coerced})
		execWithParams(t, ctx, "MERGE (n:"+label+" {at: datetime($text)})", map[string]any{"text": row.text})
		if n := countNodes(t, ctx, label); n != 1 {
			t.Errorf("%v: a MERGE by the coerced value and one by datetime(%q) left %d nodes, want 1", row.raw, row.text, n)
		}
	}
	if equal, zone := equalsText(utc, utcText); equal != false || zone != "UTC" {
		t.Errorf("control: the instant under the zone name UTC is equal to datetime(%q) = %v in zone %v, want false in zone UTC", utcText, equal, zone)
	}
}

// The server holds an instant at a UTC offset of 18 hours at most. One at the
// bound is stored at its offset. One a second or a minute past it is refused
// by the server's own error in the form the adapter would send it, and
// CoerceParams refuses it first.
func TestCoerce_TheServerHoldsAnOffsetOfEighteenHoursAtMost(t *testing.T) {
	ctx := context.Background()
	driver(t)

	const bound = 18 * 60 * 60
	for text, offset := range map[string]int{"2026-08-19T12:00:00+18:00": bound, "2026-08-19T12:00:00-18:00": -bound} {
		want, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatalf("parse %q: %v", text, err)
		}
		got, ok := echoParam(t, ctx, coerceTimestamp(t, text)).(time.Time)
		if !ok || !got.Equal(want) {
			t.Errorf("%s: the round trip returned %v, want the same instant", text, got)
		}
		if _, held := got.Zone(); held != offset {
			t.Errorf("%s: the round trip returned offset %d, want %d", text, held, offset)
		}
	}

	// The server refuses an offset past the bound, by a second too.
	for _, offset := range []int{bound + 60, -bound - 60, bound + 1, -bound - 1} {
		at := time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("Offset", offset))
		_, err := neo4jdriver.ExecuteQuery(ctx, isolatedDriver(t), "RETURN $v AS v",
			map[string]any{"v": at}, neo4jdriver.EagerResultTransformer)
		if !neo4jdriver.IsNeo4jError(err) {
			t.Errorf("an offset of %d seconds: the server answered %v; want its own refusal, or the adapter's bound of 18 hours is no longer the server's", offset, err)
		}
	}
	for _, text := range []string{"2026-08-19T12:00:00+18:01", "2026-08-19T12:00:00-18:01"} {
		if _, err := n4j.CoerceParams(map[string]any{"v": text}, n4j.ParamTypes{"v": schema.NewTimestampConstraint()}); !errors.Is(err, n4j.ErrUnrepresentable) {
			t.Errorf("%s: CoerceParams = %v, want a refusal that matches ErrUnrepresentable", text, err)
		}
	}
}

// coerceTimestamp is the value a hand-built parameter map sends for raw at a
// Timestamp.
func coerceTimestamp(t *testing.T, raw any) any {
	t.Helper()
	params, err := n4j.CoerceParams(map[string]any{"v": raw}, n4j.ParamTypes{"v": schema.NewTimestampConstraint()})
	if err != nil {
		t.Fatalf("CoerceParams(%v): %v", raw, err)
	}
	return params["v"]
}

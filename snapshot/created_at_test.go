package snapshot_test

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// stampedDoc returns a signed document whose header states member as its
// created_at.
func stampedDoc(ctx context.Context, t *testing.T, member string) []byte {
	t.Helper()
	data := makeSnapshotBytes(ctx, t)
	stamped := bytes.Replace(data, []byte(`"features":[]`), []byte(`"features":[],"created_at":`+member), 1)
	if bytes.Equal(stamped, data) {
		t.Fatal("fixture shape changed; features not found")
	}
	return rehashDocument(t, stamped)
}

// Every reader refuses a created_at that RFC 3339's date-time production does
// not accept, a leap second and the zero instant, none of which a reader can
// return as a time Marshal writes, and reads a null one as none. time.Parse
// accepts the one-digit hour, the comma and the offsets of 24 hours and of 60
// minutes.
func TestRead_RefusesACreatedAtThatIsNotRFC3339(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	for name, member := range map[string]string{
		"words":                  `"yesterday"`,
		"empty text":             `""`,
		"a 13th month":           `"2026-13-01T00:00:00Z"`,
		"a 30th of February":     `"2026-02-30T00:00:00Z"`,
		"no offset":              `"2026-01-01T00:00:00"`,
		"a date alone":           `"2026-01-01"`,
		"a number":               `20260101`,
		"an object":              `{}`,
		"a one-digit hour":       `"2024-01-02T3:04:05Z"`,
		"an hour of 24":          `"2024-01-02T24:00:00Z"`,
		"a comma fraction":       `"2024-01-02T03:04:05,5Z"`,
		"a dot and no digit":     `"2024-01-02T03:04:05.Z"`,
		"an offset of 24 hours":  `"2024-01-02T03:04:05+24:00"`,
		"an offset of 60 minute": `"2024-01-02T03:04:05+23:60"`,
		"a space for T":          `"2024-01-02 03:04:05Z"`,
		"a leap second":          `"2016-12-31T23:59:60Z"`,
		"a 60th second at noon":  `"2016-12-31T12:34:60Z"`,
		"the zero instant":       `"0001-01-01T00:00:00Z"`,
		"the zero instant east":  `"0001-01-01T01:00:00+01:00"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := stampedDoc(ctx, t, member)
			_, load := snapshot.Unmarshal(ctx, doc, s)
			_, stat := snapshot.Stat(ctx, doc)
			_, header := snapshot.ReadHeader(ctx, bytes.NewReader(doc))
			out, update := snapshot.UpdateMetadata(ctx, doc, map[string]string{"k": "v"})
			want := map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1}
			for surface, res := range map[string]diag.Result{
				"Unmarshal": load, "Verify": snapshot.Verify(ctx, doc, s), "Stat": stat,
				"ReadHeader": header, "UpdateMetadata": update,
			} {
				if !maps.Equal(errorCodes(res), want) {
					t.Errorf("%s: %v, want %v:\n%s", surface, errorCodes(res), want, res)
				}
			}
			if out != nil {
				t.Errorf("UpdateMetadata returned %d bytes", len(out))
			}
		})
	}

	// time.Parse refuses a second of 60 too, but names no reason. A 60th second
	// at 23:59 UTC is a leap second in any offset; at another time it is none.
	for member, leap := range map[string]bool{
		`"2016-12-31T23:59:60Z"`:      true,
		`"2017-01-01T00:59:60+01:00"`: true,
		`"2016-12-31T12:34:60Z"`:      false,
	} {
		_, res := snapshot.ReadHeader(ctx, bytes.NewReader(stampedDoc(ctx, t, member)))
		if strings.Contains(res.String(), "states a leap second") != leap {
			t.Errorf("created_at %s: %s, want a leap second named: %v", member, res, leap)
		}
	}

	for _, member := range []string{`20260101`, `{}`} {
		_, res := snapshot.ReadHeader(ctx, bytes.NewReader(stampedDoc(ctx, t, member)))
		if !strings.Contains(res.String(), "created_at "+member+" is not a string") {
			t.Errorf("created_at %s: %s, want the refusal to name it no string", member, res)
		}
	}

	doc := stampedDoc(ctx, t, `null`)
	header, res := snapshot.ReadHeader(ctx, bytes.NewReader(doc))
	if res.HasErrors() || !header.CreatedAt.IsZero() {
		t.Errorf("a null created_at reads as %v beside %s, want none", header.CreatedAt, res)
	}
	out, res := snapshot.UpdateMetadata(ctx, doc, nil)
	if res.HasErrors() || bytes.Contains(out, []byte("created_at")) {
		t.Errorf("UpdateMetadata of a null created_at: %s, want a document that states none:\n%s", res, out)
	}
}

// The production allows a lower-case "t" and "z", any number of fraction
// digits and the offset "-00:00", and every reader returns the time each names.
func TestRead_AcceptsEveryRFC3339FormOfATime(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	for member, want := range map[string]time.Time{
		`"2024-01-02t03:04:05z"`:               time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		`"2024-01-02T03:04:05.1234567891234Z"`: time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC),
		`"2024-01-02T03:04:05-00:00"`:          time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		`"2024-01-02T03:04:05+00:00"`:          time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		`"0000-01-01T00:00:00Z"`:               time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		doc := stampedDoc(ctx, t, member)
		if _, res := snapshot.Unmarshal(ctx, doc, s); res.HasErrors() {
			t.Errorf("Unmarshal of %s: %s", member, res)
		}
		info, res := snapshot.Stat(ctx, doc)
		if res.HasErrors() || !info.CreatedAt.Equal(want) {
			t.Errorf("Stat of %s: %v beside %s, want %v", member, info, res, want)
		}
		header, res := snapshot.ReadHeader(ctx, bytes.NewReader(doc))
		if res.HasErrors() || !header.CreatedAt.Equal(want) || header.CreatedAt.Location() != want.Location() {
			t.Errorf("ReadHeader of %s: %v beside %s, want %v", member, header, res, want)
		}
	}
}

// A version no reader accepts is refused for its version alone: the created_at
// rule is the readable version's, so it never judges another version's header.
func TestRead_RefusesAnUnknownVersionBeforeItsCreatedAt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	doc := stampedDoc(ctx, t, `"yesterday"`)
	doc = rehashDocument(t, bytes.Replace(doc, []byte(`"version":4`), []byte(`"version":5`), 1))
	_, load := snapshot.Unmarshal(ctx, doc, s)
	_, stat := snapshot.Stat(ctx, doc)
	_, header := snapshot.ReadHeader(ctx, bytes.NewReader(doc))
	_, update := snapshot.UpdateMetadata(ctx, doc, nil)
	want := map[diag.Code]int{diag.E_SNAPSHOT_UNSUPPORTED_VERSION: 1}
	for surface, res := range map[string]diag.Result{
		"Unmarshal": load, "Verify": snapshot.Verify(ctx, doc, s), "Stat": stat,
		"ReadHeader": header, "UpdateMetadata": update,
	} {
		if !maps.Equal(errorCodes(res), want) {
			t.Errorf("%s: %v, want %v:\n%s", surface, errorCodes(res), want, res)
		}
	}
}

// Marshal writes WithCreatedAt's time as t.Format(time.RFC3339Nano), in the
// time's own offset and at its own precision, and a reader returns that time.
func TestMarshal_WritesCreatedAtAsGiven(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	east := time.FixedZone("", 2*60*60)
	for want, at := range map[string]time.Time{
		"2026-01-01T12:00:00.123456789+02:00": time.Date(2026, 1, 1, 12, 0, 0, 123456789, east),
		"2026-01-01T12:00:00.12+02:00":        time.Date(2026, 1, 1, 12, 0, 0, 120000000, east),
		"2026-01-01T10:00:00Z":                time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	} {
		data := makeSnapshotBytes(ctx, t, snapshot.WithCreatedAt(at))
		if !bytes.Contains(data, []byte(`"created_at":"`+want+`"`)) {
			t.Errorf("Marshal(%s) does not write created_at %s", at, want)
		}
		header, res := snapshot.ReadHeader(ctx, bytes.NewReader(data))
		if res.HasErrors() {
			t.Fatalf("ReadHeader: %s", res)
		}
		if !header.CreatedAt.Equal(at) || header.CreatedAt.Format(time.RFC3339Nano) != want {
			t.Errorf("created_at reads back as %s, want %s", header.CreatedAt.Format(time.RFC3339Nano), want)
		}
	}
}

// A time with no RFC 3339 form would make a document no reader accepts, so
// Marshal refuses it.
func TestMarshal_RefusesACreatedAtWithNoRFC3339Form(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	snap := buildSnapshot(t, s)
	for name, at := range map[string]time.Time{
		"a five-digit year":     time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		"a year before 0":       time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC),
		"an offset of 24 hours": time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", 24*60*60)),
		"an offset of 30 s":     time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", 30)),
		"an offset of -3645 s":  time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", -3645)),
		"a local mean time":     time.Date(1900, 1, 1, 12, 0, 0, 0, time.FixedZone("LMT", 1172)),
	} {
		data, res := snapshot.Marshal(ctx, snap, snapshot.WithCreatedAt(at))
		if data != nil || !maps.Equal(errorCodes(res), map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1}) {
			t.Errorf("%s: Marshal returned %d bytes and %s, want none and one %s", name, len(data), res, diag.E_SNAPSHOT_MALFORMED)
		}
	}
}

// UpdateMetadata keeps the bytes of the header's created_at, which a parse and
// a Format would rewrite: here an escape and a trailing zero.
func TestUpdateMetadata_KeepsTheCreatedAtBytes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const written = `"2026-01-01T12:00:00.120\u002b02:00"`
	out, res := snapshot.UpdateMetadata(ctx, stampedDoc(ctx, t, written), map[string]string{"k": "v"})
	if res.HasErrors() {
		t.Fatalf("UpdateMetadata: %s", res)
	}
	if !bytes.Contains(out, []byte(`"created_at":`+written)) {
		t.Errorf("the update does not keep created_at %s:\n%s", written, out)
	}
	header, res := snapshot.ReadHeader(ctx, bytes.NewReader(out))
	if res.HasErrors() {
		t.Fatalf("ReadHeader: %s", res)
	}
	if want := time.Date(2026, 1, 1, 10, 0, 0, 120000000, time.UTC); !header.CreatedAt.Equal(want) {
		t.Errorf("created_at reads as %s, want %s", header.CreatedAt, want)
	}
}

// A created_at fault is one fault of the header: the others are reported
// beside it.
func TestRead_ReportsACreatedAtFaultBesideTheHeadersOthers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	doc := stampedDoc(ctx, t, `"nope"`)
	doc = rehashDocument(t, bytes.Replace(doc, []byte(`"features":[]`), []byte(`"features":["x"]`), 1))
	_, load := snapshot.Unmarshal(ctx, doc, s)
	_, stat := snapshot.Stat(ctx, doc)
	_, header := snapshot.ReadHeader(ctx, bytes.NewReader(doc))
	_, update := snapshot.UpdateMetadata(ctx, doc, nil)
	want := map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1, diag.E_SNAPSHOT_UNSUPPORTED_FEATURE: 1}
	for surface, res := range map[string]diag.Result{
		"Unmarshal": load, "Verify": snapshot.Verify(ctx, doc, s), "Stat": stat,
		"ReadHeader": header, "UpdateMetadata": update,
	} {
		if !maps.Equal(errorCodes(res), want) {
			t.Errorf("%s: %v, want %v:\n%s", surface, errorCodes(res), want, res)
		}
	}
}

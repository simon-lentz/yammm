package snapshot_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/snapshot"
)

// headerSpan returns the bytes of data before and after the header value.
func headerSpan(t *testing.T, data []byte) (before, after []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	for range 2 {
		if _, err := dec.Token(); err != nil {
			t.Fatalf("headerSpan: %v", err)
		}
	}
	var header json.RawMessage
	if err := dec.Decode(&header); err != nil {
		t.Fatalf("headerSpan: %v", err)
	}
	end := dec.InputOffset()
	return data[:end-int64(len(header))], data[end:]
}

// UpdateMetadata updates every document the readers accept, whatever
// whitespace stands around the header: the bytes before and after the header
// value are kept as they were, and the header is written again.
func TestUpdateMetadata_UpdatesEveryShapeTheReadersAccept(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"old": "x"}))
	for name, edit := range map[string]func(string) string{
		"whitespace before the document": func(d string) string { return " \n\t" + d },
		"whitespace after the document":  func(d string) string { return d + "\n " },
		"whitespace around the header": func(d string) string {
			d = strings.Replace(d, `{"yammm_snapshot":{`, "{ \"yammm_snapshot\" :\n {", 1)
			return strings.Replace(d, `},"types":`, "} ,\"types\":", 1)
		},
		"whitespace inside the header": func(d string) string {
			return strings.Replace(d, `"version":4,`, `"version" : 4 , `, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := edit(string(data))
			if edited == string(data) {
				t.Fatal("the edit changed nothing")
			}
			doc := rehashDocument(t, []byte(edited))
			if _, err := snapshot.Unmarshal(ctx, doc, s); err != nil {
				t.Fatalf("the readers refuse the fixture: %s", err)
			}

			out, err := snapshot.UpdateMetadata(ctx, doc, map[string]string{"stage": "done"})
			if err != nil {
				t.Fatalf("UpdateMetadata: %s", err)
			}
			if _, err := snapshot.Unmarshal(ctx, out, s); err != nil {
				t.Errorf("Unmarshal of the update: %s", err)
			}
			header, err := snapshot.ReadHeader(ctx, bytes.NewReader(out))
			if err != nil {
				t.Fatalf("ReadHeader of the update: %s", err)
			}
			if want := map[string]string{"stage": "done"}; !maps.Equal(header.Metadata, want) {
				t.Errorf("metadata = %v, want %v", header.Metadata, want)
			}
			before, after := headerSpan(t, doc)
			outBefore, outAfter := headerSpan(t, out)
			if !bytes.Equal(before, outBefore) || !bytes.Equal(after, outAfter) {
				t.Errorf("the bytes around the header moved:\n in:  %q … %.40q\n out: %q … %.40q", before, after, outBefore, outAfter)
			}
		})
	}
}

// UpdateMetadata reads the document whole and through no capped reader, so a
// header past [snapshot.MaxHeaderSize], which ReadHeader refuses, is updated and
// keeps its created_at.
// The update writes the header in the indent Marshal wrote, whatever stands
// before the opening brace and whatever whitespace the indent holds: an
// indent may hold a line feed of its own.
func TestUpdateMetadata_IndentsAsMarshalWrote(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	for _, indent := range []string{"\t", "  ", "\n", "\n\t", "\t\n", "\r\n", " \n "} {
		data := makeSnapshotBytes(ctx, t, snapshot.WithIndent(indent))
		loaded, err := snapshot.Unmarshal(ctx, data, s)
		if err != nil {
			t.Fatalf("indent %q: Unmarshal: %s", indent, err)
		}
		want, err := snapshot.Marshal(ctx, loaded, snapshot.WithIndent(indent))
		if err != nil {
			t.Fatalf("indent %q: Marshal: %s", indent, err)
		}
		for _, lead := range []string{"", " ", "\n"} {
			got, err := snapshot.UpdateMetadata(ctx, rehashDocument(t, append([]byte(lead), data...)), nil)
			if err != nil {
				t.Fatalf("indent %q, lead %q: UpdateMetadata: %s", indent, lead, err)
			}
			if lead == "" && !bytes.Equal(got, want) {
				t.Errorf("indent %q: UpdateMetadata differs from Marshal(Unmarshal(x))", indent)
			}
			// A lead byte is hashed, so the hash alone may differ from Marshal's.
			header := func(doc []byte) []byte {
				header, _, _ := bytes.Cut(doc, []byte(`"types"`))
				return integrityHashValue.ReplaceAll(header, nil)
			}
			wantHeader, gotHeader := header(want), header(got[len(lead):])
			if !bytes.Equal(gotHeader, wantHeader) {
				t.Errorf("indent %q, lead %q: header\n%q\nwant Marshal's\n%q", indent, lead, gotHeader, wantHeader)
			}
		}
	}
}

func TestUpdateMetadata_UpdatesAHeaderPastMaxHeaderSize(t *testing.T) {
	t.Parallel()
	// The 16 MiB header is scanned on one goroutine, and the detector multiplies each byte.
	raceskip.Skip(t)
	ctx := t.Context()
	stamped := time.Date(2026, 8, 17, 14, 0, 0, 123456789, time.FixedZone("", 2*60*60))
	big := strings.Repeat("x", snapshot.MaxHeaderSize)
	data := makeSnapshotBytes(ctx, t, snapshot.WithCreatedAt(stamped), snapshot.WithMetadata(map[string]string{"big": big}))
	spaced := bytes.Replace(data, []byte(`},"types":`), []byte(`} ,"types":`), 1)
	if bytes.Equal(spaced, data) {
		t.Fatal("fixture shape changed; header boundary not found")
	}
	spaced = rehashDocument(t, spaced)
	if _, err := snapshot.ReadHeader(ctx, bytes.NewReader(spaced)); err == nil {
		t.Fatal("ReadHeader accepted a header past MaxHeaderSize; the fixture tests nothing")
	}

	out, err := snapshot.UpdateMetadata(ctx, spaced, map[string]string{"big": big, "stage": "done"})
	if err != nil {
		t.Fatalf("UpdateMetadata: %s", err)
	}
	info, err := snapshot.Stat(ctx, out)
	if err != nil {
		t.Fatalf("Stat of the update: %s", err)
	}
	if !info.CreatedAt.Equal(stamped) {
		t.Errorf("created_at = %s, want %s", info.CreatedAt, stamped)
	}
	if got := slices.Sorted(maps.Keys(info.Metadata)); !slices.Equal(got, []string{"big", "stage"}) || info.Metadata["big"] != big {
		t.Errorf("metadata keys = %v, want big, whole, and stage", got)
	}
	if !bytes.Contains(out, []byte(`"created_at":"2026-08-17T14:00:00.123456789+02:00"`)) {
		t.Error("the update did not keep the created_at bytes")
	}
}

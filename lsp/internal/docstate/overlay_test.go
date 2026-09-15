package docstate

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
)

// TestCollectOverlays_KeysADocumentByItsHostPath holds the overlay map to the
// key the loader reads as a path. A decomposed name makes the host path and
// the NFC identity two strings, and only the host path names the file on every
// filesystem.
func TestCollectOverlays_KeysADocumentByItsHostPath(t *testing.T) {
	t.Parallel()

	host := yammmtest.HostAbs("/work/cafe\u0301/main.yammm")
	id, err := location.SourceIDFromPath(host)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() == host {
		t.Fatalf("the identity %q equals the host path, so the key is not tested", id)
	}
	const text = "schema \"main\"\n"

	d := NewOverlay()
	d.OpenDocument(yammmtest.FileURI("/work/cafe%CC%81/main.yammm"), id, host, 1, text)

	want := map[string][]byte{host: []byte(text)}
	if diff := cmp.Diff(want, d.CollectOverlays()); diff != "" {
		t.Errorf("CollectOverlays() mismatch (-want +got):\n%s", diff)
	}
}

// TestCollectOverlays_OneKeyPerSourceTheFirstOpenedWinning holds the overlay map
// to one entry per source, because the loader refuses two keys that name one
// source. Two host paths can name one source when the identity folds them, as
// NFC and NFD spellings of one name, and one host path can be open under two
// URIs. The first opened document's text wins, whatever order the map yields.
func TestCollectOverlays_OneKeyPerSourceTheFirstOpenedWinning(t *testing.T) {
	t.Parallel()

	nfcHost := yammmtest.HostAbs("/work/café.yammm")
	nfdHost := yammmtest.HostAbs("/work/café.yammm")
	id := mustSourceID(t, nfcHost)
	if other := mustSourceID(t, nfdHost); other != id {
		t.Fatalf("the two spellings give two identities %q and %q, so they do not name one source", id, other)
	}
	plain := yammmtest.HostAbs("/work/plain.yammm")
	plainID := mustSourceID(t, plain)

	type opened struct {
		uri  string
		id   location.SourceID
		host string
		text string
	}
	rows := []struct {
		name   string
		opened []opened
		want   map[string][]byte
	}{
		{
			name: "an NFC name opened before its NFD twin",
			opened: []opened{
				{yammmtest.FileURI("/work/caf%C3%A9.yammm"), id, nfcHost, "nfc"},
				{yammmtest.FileURI("/work/cafe%CC%81.yammm"), id, nfdHost, "nfd"},
			},
			want: map[string][]byte{nfcHost: []byte("nfc")},
		},
		{
			name: "an NFD name opened before its NFC twin",
			opened: []opened{
				{yammmtest.FileURI("/work/cafe%CC%81.yammm"), id, nfdHost, "nfd"},
				{yammmtest.FileURI("/work/caf%C3%A9.yammm"), id, nfcHost, "nfc"},
			},
			want: map[string][]byte{nfdHost: []byte("nfd")},
		},
		{
			name: "one host path open under two URIs",
			opened: []opened{
				{yammmtest.FileURI("/work/plain.yammm"), plainID, plain, "first"},
				{yammmtest.FileURI("/link/plain.yammm"), plainID, plain, "second"},
			},
			want: map[string][]byte{plain: []byte("first")},
		},
		{
			name: "a document with no host path is left out",
			opened: []opened{
				{yammmtest.FileURI("/work/plain.yammm"), plainID, plain, "kept"},
				{yammmtest.FileURI("/refused/\xff.yammm"), location.SourceID{}, "", "refused"},
			},
			want: map[string][]byte{plain: []byte("kept")},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			d := NewOverlay()
			for _, o := range row.opened {
				d.OpenDocument(o.uri, o.id, o.host, 1, o.text)
			}
			for range 20 {
				if diff := cmp.Diff(row.want, d.CollectOverlays()); diff != "" {
					t.Fatalf("CollectOverlays() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func mustSourceID(t *testing.T, host string) location.SourceID {
	t.Helper()
	id, err := location.SourceIDFromPath(host)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

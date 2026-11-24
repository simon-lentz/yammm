package snapshot_test

import (
	"bytes"
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/snapshot"
)

// UpdateMetadata seals the document under a new hash, so it first checks the
// stored one. A document whose hash does not match its bytes, or whose header
// holds none, is refused with the one issue Verify reports for it.
func TestUpdateMetadata_RefusesADocumentWhoseHashDoesNotVerify(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t)
	member := regexp.MustCompile(`"integrity_hash":"sha256:[0-9a-f]{64}"`)
	if len(member.FindAll(data, -1)) != 1 {
		t.Fatalf("the fixture holds no one signed integrity_hash: %s", data)
	}
	for name, doc := range map[string][]byte{
		"a body edited after it was sealed": bytes.Replace(data, []byte(`"Alice"`), []byte(`"Alicf"`), 1),
		"an empty hash":                     member.ReplaceAll(data, []byte(`"integrity_hash":""`)),
		"a null hash":                       member.ReplaceAll(data, []byte(`"integrity_hash":null`)),
		"no hash":                           bytes.Replace(member.ReplaceAll(data, nil), []byte(",,"), []byte(","), 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if bytes.Equal(doc, data) {
				t.Fatal("the edit changed nothing")
			}
			want := map[diag.Code]int{diag.E_SNAPSHOT_INTEGRITY_MISMATCH: 1}
			vres := diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
				return snapshot.Verify(ctx, doc, s, snapshot.WithWarnings(warn))
			})
			if !maps.Equal(errorCodes(vres), want) {
				t.Fatalf("Verify: %v, want %v; the fixture holds another fault", errorCodes(vres), want)
			}

			out, res := diagtest.Issues(t, func() ([]byte, error) {
				return snapshot.UpdateMetadata(ctx, doc, map[string]string{"k": "v"})
			})
			if out != nil {
				t.Errorf("UpdateMetadata returned %d bytes for a document whose hash does not verify", len(out))
			}
			if res.Len() != 1 || !maps.Equal(errorCodes(res), want) {
				t.Fatalf("UpdateMetadata: %s, want one %s", res, diag.E_SNAPSHOT_INTEGRITY_MISMATCH)
			}
			got, verified := slices.Collect(res.Issues())[0], slices.Collect(vres.Errors())[0]
			if got.Message() != verified.Message() || !slices.Equal(got.Details(), verified.Details()) {
				t.Errorf("UpdateMetadata reports %q %v, Verify %q %v", got.Message(), got.Details(), verified.Message(), verified.Details())
			}
		})
	}
}

// UpdateMetadata decodes no instance, so a sealed body that Unmarshal refuses
// for an instance is updated, and the update is refused as the input was.
func TestUpdateMetadata_UpdatesASealedBodyItDoesNotDecode(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t)
	edited := bytes.Replace(data, []byte(`"name":"Alice"`), []byte(`"nickname":"Alice"`), 1)
	if bytes.Equal(edited, data) {
		t.Fatal("fixture shape changed; Alice's name not found")
	}
	sealed := rehashDocument(t, edited)
	_, before := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(ctx, sealed, s, snapshot.WithWarnings(warn))
	})
	if diagtest.Err(before) == nil {
		t.Fatal("Unmarshal accepted the fixture; it tests nothing")
	}

	out, err := snapshot.UpdateMetadata(ctx, sealed, map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("UpdateMetadata: %s", err)
	}
	if _, after := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(ctx, out, s, snapshot.WithWarnings(warn))
	}); !maps.Equal(errorCodes(after), errorCodes(before)) {
		t.Errorf("Unmarshal of the update: %v, want %v", errorCodes(after), errorCodes(before))
	}
}

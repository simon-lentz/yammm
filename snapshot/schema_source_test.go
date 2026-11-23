package snapshot_test

import (
	"bytes"
	"testing"

	"github.com/simon-lentz/yammm/snapshot"
)

// Marshal writes no schema_source: a source path does not travel between
// machines, and the schema's name and structural hash identify the schema.
func TestMarshal_WritesNoSchemaSource(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for _, opts := range [][]snapshot.Option{nil, {snapshot.WithIndent("  ")}} {
		if data := makeSnapshotBytes(ctx, t, opts...); bytes.Contains(data, []byte("schema_source")) {
			t.Errorf("Marshal wrote a schema_source:\n%s", data)
		}
	}
}

// A document an earlier writer wrote carries a schema_source. Every reader
// accepts it at the same version, and UpdateMetadata writes the header as
// Marshal does, without it.
func TestRead_AcceptsTheSchemaSourceAnEarlierWriterWrote(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t)
	legacy := bytes.Replace(data, []byte(`"schema_name":"test",`), []byte(`"schema_name":"test","schema_source":"string://test.yammm",`), 1)
	if bytes.Equal(legacy, data) {
		t.Fatal("fixture shape changed; schema_name not found")
	}
	legacy = rehashDocument(t, legacy)

	if _, err := snapshot.Unmarshal(ctx, legacy, s); err != nil {
		t.Errorf("Unmarshal: %s", err)
	}
	if err := snapshot.Verify(ctx, legacy, s); err != nil {
		t.Errorf("Verify: %s", err)
	}
	if _, err := snapshot.Stat(ctx, legacy); err != nil {
		t.Errorf("Stat: %s", err)
	}
	if _, err := snapshot.ReadHeader(ctx, bytes.NewReader(legacy)); err != nil {
		t.Errorf("ReadHeader: %s", err)
	}
	out, err := snapshot.UpdateMetadata(ctx, legacy, nil)
	if err != nil {
		t.Fatalf("UpdateMetadata: %s", err)
	}
	if !bytes.Equal(out, data) {
		t.Errorf("UpdateMetadata wrote the header other than Marshal does:\n got:  %s\n want: %s", out, data)
	}
}

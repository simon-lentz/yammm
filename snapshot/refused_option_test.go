package snapshot_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestCalls_RefuseAnOptionTheyDoNotTake holds each call to the options it takes:
// any other is refused before anything is read, whatever its value, by an error
// that names the first such option and the call and carries no diagnostic code.
func TestCalls_RefuseAnOptionTheyDoNotTake(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t)
	snap := buildSnapshot(t, s, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}))
	warnings := snapshot.WithWarnings(func(diag.Issue) {})

	type call func(opts ...snapshot.Option) error
	marshal := func(opts ...snapshot.Option) error {
		out, err := snapshot.Marshal(ctx, snap, opts...)
		if out != nil && err != nil {
			t.Error("Marshal returned bytes beside an error")
		}
		return err
	}
	unmarshal := func(opts ...snapshot.Option) error {
		got, err := snapshot.Unmarshal(ctx, data, s, opts...)
		if !got.IsZero() && err != nil {
			t.Error("Unmarshal returned a snapshot beside an error")
		}
		return err
	}
	verify := func(opts ...snapshot.Option) error { return snapshot.Verify(ctx, data, s, opts...) }
	update := func(opts ...snapshot.Option) error {
		out, err := snapshot.UpdateMetadata(ctx, data, map[string]string{"k": "v"}, opts...)
		if out != nil && err != nil {
			t.Error("UpdateMetadata returned bytes beside an error")
		}
		return err
	}

	refused := []struct {
		name string
		call call
		opts []snapshot.Option
		want string
	}{
		{"Marshal WithIssueLimit", marshal, []snapshot.Option{snapshot.WithIssueLimit(0)}, "snapshot.Marshal does not take the WithIssueLimit option"},
		{"Marshal WithRevalidation", marshal, []snapshot.Option{snapshot.WithRevalidation(diag.Error)}, "snapshot.Marshal does not take the WithRevalidation option"},
		{"Marshal WithIntegrityCheck(true)", marshal, []snapshot.Option{snapshot.WithIntegrityCheck(true)}, "snapshot.Marshal does not take the WithIntegrityCheck option"},
		{"Marshal WithWarnings", marshal, []snapshot.Option{warnings}, "snapshot.Marshal does not take the WithWarnings option"},
		{"Unmarshal WithIndent", unmarshal, []snapshot.Option{snapshot.WithIndent("")}, "snapshot.Unmarshal does not take the WithIndent option"},
		{"Unmarshal WithCreatedAt", unmarshal, []snapshot.Option{snapshot.WithCreatedAt(time.Time{})}, "snapshot.Unmarshal does not take the WithCreatedAt option"},
		{"Unmarshal WithMetadata", unmarshal, []snapshot.Option{snapshot.WithMetadata(nil)}, "snapshot.Unmarshal does not take the WithMetadata option"},
		{"Verify WithIndent", verify, []snapshot.Option{snapshot.WithIndent("  ")}, "snapshot.Verify does not take the WithIndent option"},
		{"Verify WithMetadata", verify, []snapshot.Option{snapshot.WithMetadata(map[string]string{"k": "v"})}, "snapshot.Verify does not take the WithMetadata option"},
		{"Verify WithCreatedAt", verify, []snapshot.Option{snapshot.WithCreatedAt(time.Time{})}, "snapshot.Verify does not take the WithCreatedAt option"},
		{"UpdateMetadata WithMetadata", update, []snapshot.Option{snapshot.WithMetadata(nil)}, "snapshot.UpdateMetadata does not take the WithMetadata option"},
		{"UpdateMetadata WithCreatedAt", update, []snapshot.Option{snapshot.WithCreatedAt(time.Time{})}, "snapshot.UpdateMetadata does not take the WithCreatedAt option"},
		{"UpdateMetadata WithIssueLimit", update, []snapshot.Option{snapshot.WithIssueLimit(10)}, "snapshot.UpdateMetadata does not take the WithIssueLimit option"},
		{"UpdateMetadata WithWarnings", update, []snapshot.Option{warnings}, "snapshot.UpdateMetadata does not take the WithWarnings option"},
		{"UpdateMetadata WithRevalidation", update, []snapshot.Option{snapshot.WithRevalidation(diag.Warning)}, "snapshot.UpdateMetadata does not take the WithRevalidation option"},
		{"UpdateMetadata WithIndent", update, []snapshot.Option{snapshot.WithIndent("")}, "snapshot.UpdateMetadata does not take the WithIndent option"},
		{
			"the first refused option is named", unmarshal,
			[]snapshot.Option{warnings, snapshot.WithCreatedAt(time.Now()), snapshot.WithIndent("")},
			"snapshot.Unmarshal does not take the WithCreatedAt option",
		},
	}
	for _, row := range refused {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			err := row.call(row.opts...)
			if err == nil || err.Error() != row.want {
				t.Fatalf("err = %v; want %q", err, row.want)
			}
			if _, ok := errors.AsType[*diag.ResultError](err); ok {
				t.Errorf("err = %#v; want an error with no diagnostics", err)
			}
		})
	}

	taken := []struct {
		name string
		call call
		opts []snapshot.Option
	}{
		{"Marshal", marshal, []snapshot.Option{snapshot.WithIndent("  "), snapshot.WithCreatedAt(time.Unix(0, 0)), snapshot.WithMetadata(map[string]string{"k": "v"})}},
		{"Unmarshal", unmarshal, []snapshot.Option{snapshot.WithIssueLimit(1), snapshot.WithRevalidation(diag.Warning), snapshot.WithIntegrityCheck(true), warnings}},
		{"Verify", verify, []snapshot.Option{snapshot.WithIssueLimit(1), snapshot.WithRevalidation(diag.Info), snapshot.WithIntegrityCheck(false), warnings}},
		{"UpdateMetadata", update, []snapshot.Option{snapshot.WithIntegrityCheck(true)}},
	}
	for _, row := range taken {
		t.Run("takes/"+row.name, func(t *testing.T) {
			t.Parallel()
			if err := row.call(row.opts...); err != nil {
				t.Fatalf("err = %v; want nil", err)
			}
		})
	}
}

// TestCalls_RefuseAnOptionBeforeTheyReadTheContext pins that a refused option
// is refused under a cancelled context too: the refusal comes before the call
// reads anything, its context included.
func TestCalls_RefuseAnOptionBeforeTheyReadTheContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := testSchema(t)
	data := makeSnapshotBytes(t.Context(), t)
	snap := buildSnapshot(t, s, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}))
	refused := snapshot.WithIssueLimit(1)
	indent := snapshot.WithIndent("")
	calls := []struct {
		name string
		err  error
		want string
	}{
		{"Marshal", func() error { _, err := snapshot.Marshal(ctx, snap, refused); return err }(), "snapshot.Marshal does not take the WithIssueLimit option"},
		{"Unmarshal", func() error { _, err := snapshot.Unmarshal(ctx, data, s, indent); return err }(), "snapshot.Unmarshal does not take the WithIndent option"},
		{"Verify", snapshot.Verify(ctx, data, s, indent), "snapshot.Verify does not take the WithIndent option"},
		{"UpdateMetadata", func() error { _, err := snapshot.UpdateMetadata(ctx, data, nil, refused); return err }(), "snapshot.UpdateMetadata does not take the WithIssueLimit option"},
	}
	for _, c := range calls {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("%s under a cancelled context: %v; want %q", c.name, c.err, c.want)
		}
	}
}

// UpdateMetadata under WithIntegrityCheck(false) updates a document whose hash
// does not verify, and seals the edited body under the new hash.
func TestUpdateMetadata_WithIntegrityCheckFalseSkipsTheHashCheck(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testSchema(t)
	edited := bytes.Replace(makeSnapshotBytes(ctx, t), []byte(`"Alice"`), []byte(`"Alicf"`), 1)
	if err := snapshot.Verify(ctx, edited, s); !errors.Is(err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
		t.Fatalf("Verify of the edited document: %v, want %s", err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH)
	}
	if _, err := snapshot.UpdateMetadata(ctx, edited, map[string]string{"k": "v"}); !errors.Is(err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
		t.Fatalf("UpdateMetadata checking the hash: %v, want %s", err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH)
	}

	out, err := snapshot.UpdateMetadata(ctx, edited, map[string]string{"k": "v"}, snapshot.WithIntegrityCheck(false))
	if err != nil {
		t.Fatalf("UpdateMetadata skipping the hash check: %v", err)
	}
	if err := snapshot.Verify(ctx, out, s); err != nil {
		t.Errorf("Verify of the update: %v, want the new hash to seal the edited body", err)
	}
}

// Stat returns its Info beside the error for a document whose body breaks a
// check, and nil beside the error for one whose header it refuses.
func TestStat_ReturnsAnInfoBesideABodyFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	data := makeSnapshotBytes(ctx, t)

	edited := bytes.Replace(data, []byte(`"Alice"`), []byte(`"Alicf"`), 1)
	info, err := snapshot.Stat(ctx, edited)
	if !errors.Is(err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
		t.Fatalf("Stat of an edited body: %v, want %s", err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH)
	}
	if info == nil || info.IntegrityStatus != snapshot.IntegrityMismatch || info.TotalInstances != 2 {
		t.Errorf("Stat of an edited body: Info %+v, want one that records the mismatch and counts both instances", info)
	}

	info, err = snapshot.Stat(ctx, bytes.Replace(data, []byte(`"version":`), []byte(`"versiom":`), 1))
	if err == nil || info != nil {
		t.Errorf("Stat of a refused header: (%+v, %v), want (nil, an error)", info, err)
	}
}

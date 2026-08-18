package snapshot_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/snapshot"
)

func TestCreatedAtTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		createdAt string
		wantOK    bool
		want      time.Time
	}{
		{
			name:      "utc second precision, the form yammm writes",
			createdAt: "2026-08-18T12:34:56Z",
			wantOK:    true,
			want:      time.Date(2026, 8, 18, 12, 34, 56, 0, time.UTC),
		},
		{
			name:      "fractional seconds parse under the same layout",
			createdAt: "2026-08-18T12:34:56.789Z",
			wantOK:    true,
			want:      time.Date(2026, 8, 18, 12, 34, 56, 789000000, time.UTC),
		},
		{
			name:      "non-utc offset keeps its offset",
			createdAt: "2026-08-18T12:34:56+02:00",
			wantOK:    true,
			want:      time.Date(2026, 8, 18, 12, 34, 56, 0, time.FixedZone("", 2*60*60)),
		},
		{name: "empty is not usable", createdAt: "", wantOK: false},
		{name: "garbage is not usable", createdAt: "not a timestamp", wantOK: false},
		{name: "date only is not RFC 3339", createdAt: "2026-08-18", wantOK: false},
		{name: "missing zone is not RFC 3339", createdAt: "2026-08-18T12:34:56", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			header := &snapshot.HeaderInfo{CreatedAt: tt.createdAt}
			got, ok := header.CreatedAtTime()
			if ok != tt.wantOK {
				t.Fatalf("HeaderInfo.CreatedAtTime() ok = %v, want %v", ok, tt.wantOK)
			}
			info := snapshot.SnapshotInfo{CreatedAt: tt.createdAt}
			gotInfo, okInfo := info.CreatedAtTime()
			if okInfo != tt.wantOK {
				t.Fatalf("SnapshotInfo.CreatedAtTime() ok = %v, want %v", okInfo, tt.wantOK)
			}
			if !tt.wantOK {
				if !got.IsZero() || !gotInfo.IsZero() {
					t.Errorf("an unusable value returned %v / %v, want the zero time", got, gotInfo)
				}
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("HeaderInfo.CreatedAtTime() = %v, want %v", got, tt.want)
			}
			if !gotInfo.Equal(tt.want) {
				t.Errorf("SnapshotInfo.CreatedAtTime() = %v, want %v", gotInfo, tt.want)
			}
			// The offset survives, so a caller rendering the value back sees
			// what the header said rather than a UTC translation of it.
			if _, wantOffset := tt.want.Zone(); true {
				if _, gotOffset := got.Zone(); gotOffset != wantOffset {
					t.Errorf("zone offset = %d, want %d", gotOffset, wantOffset)
				}
			}
		})
	}
}

// A caller that must tell absent from corrupt has one documented way to do it,
// and it has to keep working.
func TestCreatedAtTime_EmptyIsDistinguishableFromMalformed(t *testing.T) {
	t.Parallel()
	absent := &snapshot.HeaderInfo{CreatedAt: ""}
	corrupt := &snapshot.HeaderInfo{CreatedAt: "garbage"}
	if _, ok := absent.CreatedAtTime(); ok {
		t.Error("an empty CreatedAt reported a usable time")
	}
	if _, ok := corrupt.CreatedAtTime(); ok {
		t.Error("a malformed CreatedAt reported a usable time")
	}
	if absent.CreatedAt != "" {
		t.Error("the documented absent test does not hold")
	}
	if corrupt.CreatedAt == "" {
		t.Error("the documented corrupt test does not hold")
	}
}

// A zero WithUpdateCreatedAt preserves the existing header rather than stamping
// 0001-01-01T00:00:00Z over it — the failure mode the old doc recipe produced
// on every parse error.
func TestUpdateMetadata_ZeroCreatedAtPreserves(t *testing.T) {
	original := seedStampedYS(t)
	before, res := snapshot.HeaderOnly(t.Context(), original)
	if err := res.Err(); err != nil {
		t.Fatalf("HeaderOnly: %v", err)
	}
	if before.CreatedAt == "" {
		t.Fatal("the fixture carries no created_at, so this asserts nothing")
	}

	out, res := snapshot.UpdateMetadata(
		t.Context(), original,
		map[string]string{"pipeline_completed": "true"},
		snapshot.WithUpdateCreatedAt(time.Time{}),
	)
	if err := res.Err(); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	after, res := snapshot.HeaderOnly(t.Context(), out)
	if err := res.Err(); err != nil {
		t.Fatalf("HeaderOnly after update: %v", err)
	}
	if after.CreatedAt != before.CreatedAt {
		t.Errorf("created_at = %q after a zero WithUpdateCreatedAt, want the original %q",
			after.CreatedAt, before.CreatedAt)
	}
}

// A non-zero override still applies, so the guard did not turn the option off.
func TestUpdateMetadata_NonZeroCreatedAtStillOverrides(t *testing.T) {
	original := seedStampedYS(t)
	stamp := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	out, res := snapshot.UpdateMetadata(
		t.Context(), original,
		map[string]string{"pipeline_completed": "true"},
		snapshot.WithUpdateCreatedAt(stamp),
	)
	if err := res.Err(); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	after, res := snapshot.HeaderOnly(t.Context(), out)
	if err := res.Err(); err != nil {
		t.Fatalf("HeaderOnly after update: %v", err)
	}
	got, ok := after.CreatedAtTime()
	if !ok {
		t.Fatalf("created_at %q is not parseable after an override", after.CreatedAt)
	}
	if !got.Equal(stamp) {
		t.Errorf("created_at = %v, want %v", got, stamp)
	}
}

// seedStampedYS returns a marshaled document carrying a created_at, which
// seedValidYS deliberately omits — Marshal only stamps one when asked, to keep
// its output byte-stable.
func seedStampedYS(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ys")
	seedValidYS(t, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	stamped, res := snapshot.UpdateMetadata(t.Context(), data, nil,
		snapshot.WithUpdateCreatedAt(time.Date(2019, 6, 5, 4, 3, 2, 0, time.UTC)))
	if err := res.Err(); err != nil {
		t.Fatalf("stamping created_at: %v", err)
	}
	return stamped
}

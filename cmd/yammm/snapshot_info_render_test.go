package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestSnapshotInfo_FullTextStatesTheAttestation pins B55. Deleting the
// attestation line from the full report left every test green: the header-only
// renderer's copy was covered and this one was not, so the report could stop
// stating the writer's validity claim without anything noticing.
func TestSnapshotInfo_FullTextStatesTheAttestation(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "a.ys")
	saveFixture(t, file)

	code, out, errOut := executeCmdOutput(t, "snapshot", "info", file)
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	if !strings.Contains(out, "Attested:") {
		t.Errorf("the full report does not state the attestation:\n%s", out)
	}
	if !strings.Contains(out, "values=true associations=true") {
		t.Errorf("the attestation line does not carry the claim:\n%s", out)
	}
}

// TestScanEntryToDTO_ZeroModTimeRendersEmpty pins B56. A file that could not be
// stat'd carries the zero Time, and rendering it would publish
// "0001-01-01T00:00:00.000000000Z" — a value that parses, sorts FIRST, and
// silently corrupts any chronological ordering built on the key.
func TestScanEntryToDTO_ZeroModTimeRendersEmpty(t *testing.T) {
	t.Parallel()

	dto := scanEntryToDTO(snapshot.ScanEntry{Name: "unstattable.ys"})
	if dto.ModTime != "" {
		t.Errorf("mod_time = %q, want empty — an unknown time is absent, not the zero time", dto.ModTime)
	}

	// The other arm, so the guard is pinned in both directions.
	known := time.Date(2026, 3, 4, 5, 6, 7, 8, time.UTC)
	if got := scanEntryToDTO(snapshot.ScanEntry{ModTime: known}).ModTime; got == "" {
		t.Error("mod_time is empty for a known time — the guard rejects what it should render")
	}
}

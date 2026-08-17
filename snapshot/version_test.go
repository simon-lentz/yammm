package snapshot

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
)

// detailValue looks up a detail by key on an Issue. Issue.Details()
// returns a defensive copy of all details; this helper wraps a linear
// scan for the version-test call sites.
func detailValue(iss diag.Issue, key string) (string, bool) {
	for _, d := range iss.Details() {
		if d.Key == key {
			return d.Value, true
		}
	}
	return "", false
}

// TestVersionConstants_Pinned catches any future accidental bump of
// currentVersion or MinReadableVersion that is not paired with a
// corresponding enumeration in docs/VERSIONING.md. The documented values at
// v0.12.0 are currentVersion == 3 (the types-table bump) and
// MinReadableVersion == 3 (v3 is the only readable version).
func TestVersionConstants_Pinned(t *testing.T) {
	require.Equal(t, 3, currentVersion, "currentVersion should be 3 at yammm v0.12.0")
	require.Equal(t, 3, MinReadableVersion, "MinReadableVersion should be 3 at yammm v0.12.0")
}

// TestAcceptVersion_OnlyV3Accepted pins the live accept range: v3 is the only
// readable version, and every other version is refused with the supported
// version named and the observed version carried as a structured detail.
func TestAcceptVersion_OnlyV3Accepted(t *testing.T) {
	iss, ok := acceptVersion(3, MinReadableVersion, currentVersion)
	require.True(t, ok, "the reader must accept a v3 document")
	require.Equal(t, diag.Issue{}, iss)

	for _, v := range []int{0, 1, 2, 99} {
		t.Run(strconv.Itoa(v), func(t *testing.T) {
			iss, ok := acceptVersion(v, MinReadableVersion, currentVersion)
			require.False(t, ok, "version %d must be rejected", v)
			require.Equal(t, diag.E_SNAPSHOT_UNSUPPORTED_VERSION, iss.Code())
			require.Contains(t, iss.Message(), "supported: 3",
				"reject message names the supported version")
			got, found := detailValue(iss, diag.DetailKeyVersion)
			require.True(t, found, "version detail should be present")
			require.Equal(t, strconv.Itoa(v), got)
		})
	}
}

// TestAcceptVersion_OlderReaderRejectsNewerFile pins the "older reader rejects
// newer file cleanly" half of the version-bump contract. An older reader
// (simulated via bounds [2, 2]) rejects a v3 document with
// E_SNAPSHOT_UNSUPPORTED_VERSION — operators running a pre-v0.12.0 binary
// against a v0.12.0-written .ys see a structured diagnostic rather than a
// misread types section.
func TestAcceptVersion_OlderReaderRejectsNewerFile(t *testing.T) {
	iss, ok := acceptVersion(3, 2, 2)
	require.False(t, ok, "an older reader must reject a v3 document")
	require.Equal(t, diag.E_SNAPSHOT_UNSUPPORTED_VERSION, iss.Code())
	require.Equal(t, diag.Error, iss.Severity())

	// Message names both the observed version and the supported bound.
	require.Contains(t, iss.Message(), "3", "message should name observed version")
	require.Contains(t, iss.Message(), "supported: 2",
		"singular-bound message should read 'supported: 2'")

	// Observed version is carried as a structured detail.
	got, found := detailValue(iss, diag.DetailKeyVersion)
	require.True(t, found, "version detail should be present")
	require.Equal(t, "3", got)
}

// TestAcceptVersion_RangeMessageFormats pins both message forms through the
// explicit-bounds seam: a multi-version range renders in brackets, and a
// single-version range renders as "supported: N" with no brackets.
func TestAcceptVersion_RangeMessageFormats(t *testing.T) {
	iss, ok := acceptVersion(99, 2, 3)
	require.False(t, ok)
	require.Contains(t, iss.Message(), "[2, 3]",
		"multi-bound message should name the range")

	iss, ok = acceptVersion(5, 2, 2)
	require.False(t, ok)
	require.Contains(t, iss.Message(), "supported: 2")
	require.NotContains(t, iss.Message(), "[2, 2]",
		"singular-bound message should not use brackets")
}

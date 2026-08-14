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
// MinReadableVersion == 2 (v1 documents are no longer read).
func TestVersionConstants_Pinned(t *testing.T) {
	require.Equal(t, 3, currentVersion, "currentVersion should be 3 at yammm v0.12.0")
	require.Equal(t, 2, MinReadableVersion, "MinReadableVersion should be 2 at yammm v0.12.0")
}

// TestAcceptVersion_V3ReaderAcceptsBothV2AndV3 pins the "newer reader
// accepts older file" half of the asymmetric-bump contract. A v3 reader
// (yammm v0.12.0+, bounds [2, 3]) accepts both v2 and v3 documents.
func TestAcceptVersion_V3ReaderAcceptsBothV2AndV3(t *testing.T) {
	iss, ok := acceptVersion(2, 2, 3)
	require.True(t, ok, "v3 reader must accept v2 document")
	require.Equal(t, diag.Issue{}, iss)

	iss, ok = acceptVersion(3, 2, 3)
	require.True(t, ok, "v3 reader must accept v3 document")
	require.Equal(t, diag.Issue{}, iss)
}

// TestAcceptVersion_V2ReaderRejectsV3 pins the "older reader rejects
// newer file cleanly" half of the asymmetric-bump contract. A v2-only
// reader (simulated via bounds [2, 2]) rejects v3 documents with
// E_SNAPSHOT_UNSUPPORTED_VERSION — operators running a pre-v0.12.0
// binary against a v0.12.0-written .ys see a structured diagnostic
// rather than a misread types section.
func TestAcceptVersion_V2ReaderRejectsV3(t *testing.T) {
	iss, ok := acceptVersion(3, 2, 2)
	require.False(t, ok, "v2-only reader must reject v3 document")
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

// TestAcceptVersion_UnknownVersionsRejected pins the rejection path for
// versions outside the accept range on either side: versions 0 and 1 (below
// min) and 99 (above max). Both must surface the range in the message
// so operators know the reader's supported bounds.
func TestAcceptVersion_UnknownVersionsRejected(t *testing.T) {
	cases := []struct{ v int }{{0}, {1}, {99}}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.v), func(t *testing.T) {
			iss, ok := acceptVersion(tc.v, 2, 3)
			require.False(t, ok, "version %d must be rejected", tc.v)
			require.Equal(t, diag.E_SNAPSHOT_UNSUPPORTED_VERSION, iss.Code())
			require.Contains(t, iss.Message(), "[2, 3]",
				"multi-bound message should name the range")
			got, found := detailValue(iss, diag.DetailKeyVersion)
			require.True(t, found)
			require.Equal(t, strconv.Itoa(tc.v), got)
		})
	}
}

// TestAcceptVersion_SingularRangeMessage pins the message format when
// minV == maxV: the helper reads "supported: N" (no brackets) so the
// single-version case is grammatically clean. Separate from the
// multi-bound case for clarity.
func TestAcceptVersion_SingularRangeMessage(t *testing.T) {
	iss, ok := acceptVersion(5, 2, 2)
	require.False(t, ok)
	require.Contains(t, iss.Message(), "supported: 2")
	require.NotContains(t, iss.Message(), "[2, 2]",
		"singular-bound message should not use brackets")
}

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
// corresponding plan-doc update. The documented values at v0.3.0 are
// currentVersion == 2 (the "Properties on UnresolvedEdge" bump) and
// MinReadableVersion == 1 (v0.3.0 readers still accept v1 documents
// produced before the bump).
func TestVersionConstants_Pinned(t *testing.T) {
	require.Equal(t, 2, currentVersion, "currentVersion should be 2 at yammm v0.3.0")
	require.Equal(t, 1, MinReadableVersion, "MinReadableVersion should be 1 at yammm v0.3.0")
}

// TestAcceptVersion_V2ReaderAcceptsBothV1AndV2 pins the "newer reader
// accepts older file" half of the asymmetric-bump contract. A v2 reader
// (yammm v0.3.0+, bounds [1, 2]) accepts both v1 and v2 documents.
func TestAcceptVersion_V2ReaderAcceptsBothV1AndV2(t *testing.T) {
	iss, ok := acceptVersion(1, 1, 2)
	require.True(t, ok, "v2 reader must accept v1 document")
	require.Equal(t, diag.Issue{}, iss)

	iss, ok = acceptVersion(2, 1, 2)
	require.True(t, ok, "v2 reader must accept v2 document")
	require.Equal(t, diag.Issue{}, iss)
}

// TestAcceptVersion_V1ReaderRejectsV2 pins the "older reader rejects
// newer file cleanly" half of the asymmetric-bump contract. A v1-only
// reader (simulated via bounds [1, 1]) rejects v2 documents with
// E_SNAPSHOT_UNSUPPORTED_VERSION — operators running a pre-v0.3.0
// binary against a v0.3.0-written .ys see a structured diagnostic
// rather than silently-missing edge properties.
func TestAcceptVersion_V1ReaderRejectsV2(t *testing.T) {
	iss, ok := acceptVersion(2, 1, 1)
	require.False(t, ok, "v1-only reader must reject v2 document")
	require.Equal(t, diag.E_SNAPSHOT_UNSUPPORTED_VERSION, iss.Code())
	require.Equal(t, diag.Error, iss.Severity())

	// Message names both the observed version and the supported bound.
	require.Contains(t, iss.Message(), "2", "message should name observed version")
	require.Contains(t, iss.Message(), "supported: 1",
		"singular-bound message should read 'supported: 1'")

	// Observed version is carried as a structured detail.
	got, found := detailValue(iss, diag.DetailKeyVersion)
	require.True(t, found, "version detail should be present")
	require.Equal(t, "2", got)
}

// TestAcceptVersion_UnknownVersionsRejected pins the rejection path for
// versions outside the accept range on either side: version 0 (below min)
// and version 3+ (above max). Both must surface the range in the message
// so operators know the reader's supported bounds.
func TestAcceptVersion_UnknownVersionsRejected(t *testing.T) {
	cases := []struct{ v int }{{0}, {3}, {99}}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.v), func(t *testing.T) {
			iss, ok := acceptVersion(tc.v, 1, 2)
			require.False(t, ok, "version %d must be rejected", tc.v)
			require.Equal(t, diag.E_SNAPSHOT_UNSUPPORTED_VERSION, iss.Code())
			require.Contains(t, iss.Message(), "[1, 2]",
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

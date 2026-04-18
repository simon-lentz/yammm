package snapshot

import (
	"fmt"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
)

// acceptVersion validates a .ys wire-format version against the closed
// range [minV, maxV]. Returns (zero Issue, true) when v is inside the
// range, and (Fatal E_SNAPSHOT_UNSUPPORTED_VERSION issue, false) otherwise.
//
// The returned issue's message names the observed version and the
// supported range, and carries the observed version as a DetailKeyVersion
// structured detail so operators inspecting diagnostics programmatically
// can recover it without parsing the message text.
//
// The helper is package-internal but kept factored out of decodeHeader
// so the "v1 reader rejects v2" asymmetric-bump contract can be pinned
// at the unit-test layer by passing explicit bounds — tests do not need
// to fork the decoder to simulate an older reader.
func acceptVersion(v, minV, maxV int) (diag.Issue, bool) {
	if v >= minV && v <= maxV {
		return diag.Issue{}, true
	}
	var supported string
	if minV == maxV {
		supported = strconv.Itoa(minV)
	} else {
		supported = fmt.Sprintf("[%d, %d]", minV, maxV)
	}
	iss := diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNSUPPORTED_VERSION,
		fmt.Sprintf("unsupported snapshot format version %d (supported: %s)", v, supported)).
		WithDetail(diag.DetailKeyVersion, strconv.Itoa(v)).
		Build()
	return iss, false
}

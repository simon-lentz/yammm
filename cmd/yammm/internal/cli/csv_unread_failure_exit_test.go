package cli

import (
	"errors"
	"io"
	"testing"

	"github.com/simon-lentz/yammm/adapter/csv"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// headerThenFailure returns a refused header and its failure in one Read.
type headerThenFailure struct {
	header string
	err    error
}

func (r *headerThenFailure) Read(p []byte) (int, error) {
	if r.header == "" {
		return 0, io.EOF
	}
	n := copy(p, r.header)
	r.header = r.header[n:]
	if r.header != "" {
		return n, nil
	}
	return n, r.err
}

// A data file whose read fails is exit 3 whatever else the parse found: the
// exit rule ranks a failure outside the input over a refusal of it. A header
// the parser refuses ends the parse, and the failure its reader returned beside
// that header is still the result's to report.
func TestExitForResult_ARefusedCSVHeaderWhoseReaderFailedIsRuntime(t *testing.T) {
	t.Parallel()
	failure := errors.New("input/output error")
	for _, header := range []string{"id,id\n", "id,\n", "i\"d,name\n", "\"id\"x,name"} {
		_, res := csv.New().ParseTyped(t.Context(), location.MustNewSourceID("test://x.csv"), "P",
			&headerThenFailure{header: header, err: failure}, nil)
		if !res.HasCode(diag.E_ADAPTER_PARSE) {
			t.Fatalf("%q is not refused, so the case asserts nothing: %s", header, res)
		}
		if got := ExitForResult(res); got != ExitRuntime {
			t.Errorf("%q: exit %d, want %d: %s", header, got, ExitRuntime, res)
		}
	}
}

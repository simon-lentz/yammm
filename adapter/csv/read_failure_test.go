package csv

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// failsOnce serves data a byte per Read and fails the at'th Read once, as a
// transient fault a caller's reader does not repeat.
type failsOnce struct {
	data  string
	at    int
	calls int
}

var errTransient = errors.New("transient fault")

func (r *failsOnce) Read(p []byte) (int, error) {
	r.calls++
	if r.calls == r.at {
		return 0, errTransient
	}
	if r.data == "" {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

// A reader's failure is never lost, whenever it comes: while the byte order
// mark is looked for, or behind a record the reader refuses, which
// encoding/csv reads past. Each stops the parse at a Fatal E_ADAPTER_IO
// carrying the reader's error.
func TestParse_AReadFailureIsNeverLost(t *testing.T) {
	t.Parallel()
	inputs := map[string]string{
		"plain":       "id,name\np1,Ann\np2,Bob\n",
		"byte order":  "\xef\xbb\xbfid,name\np1,Ann\n",
		"quote fault": "id,name\np1,\"Ann\"x\np2,Bob\n",
	}
	for name, data := range inputs {
		for at := 1; at <= len(data); at++ {
			r := &failsOnce{data: data, at: at}
			_, res := New().ParseTyped(t.Context(), location.MustNewSourceID("test://x.csv"), "P", r, nil)
			fatal := 0
			for issue := range res.Issues() {
				if issue.Severity() == diag.Fatal && issue.Code() == diag.E_ADAPTER_IO &&
					strings.Contains(issue.Message(), errTransient.Error()) {
					fatal++
				}
			}
			if fatal != 1 {
				t.Errorf("%s, failing at read %d: %d Fatal E_ADAPTER_IO carrying the fault, want 1: %s", name, at, fatal, res)
			}
		}
	}
}

// scripted serves each step's bytes, returning the step's error with its last
// byte, in order, then io.EOF.
type scripted struct {
	steps []struct {
		data string
		err  error
	}
}

func (r *scripted) Read(p []byte) (int, error) {
	if len(r.steps) == 0 {
		return 0, io.EOF
	}
	step := &r.steps[0]
	n := copy(p, step.data)
	step.data = step.data[n:]
	if step.data != "" {
		return n, nil
	}
	err := step.err
	r.steps = r.steps[1:]
	return n, err
}

// The data a Read returns beside its error is read before the error stops the
// parse, and the end of the input is its end: a reader that returns data after
// io.EOF is not read again, in the head or after it.
func TestParse_TheFirstErrorEndsTheInputAndItsDataIsKept(t *testing.T) {
	t.Parallel()
	type step = struct {
		data string
		err  error
	}
	for _, c := range []struct {
		name    string
		steps   []step
		records int
		fatal   bool
	}{
		{"data and a failure in one Read", []step{{"id,name\np1,Ann\np2,Bob\n", errTransient}}, 2, true},
		{"the end in the head, then data", []step{{"i", io.EOF}, {"d,name\np1,Ann\n", nil}}, 0, false},
		{"the end after a record, then data", []step{{"id,name\np1,Ann\n", io.EOF}, {"p2,Bob\n", nil}}, 1, false},
	} {
		raws, res := New().ParseTyped(t.Context(), location.MustNewSourceID("test://x.csv"), "P", &scripted{steps: c.steps}, nil)
		if len(raws) != c.records || res.HasFatal() != c.fatal {
			t.Errorf("%s: %d records, Fatal=%v, want %d and %v: %s", c.name, len(raws), res.HasFatal(), c.records, c.fatal, res)
		}
	}
}

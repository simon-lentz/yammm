package csv

import "testing"

func TestErrZeroSnapshot_NamesTheAdapterAndTheCondition(t *testing.T) {
	t.Parallel()
	const want = "csv adapter: zero graph snapshot"
	if got := ErrZeroSnapshot.Error(); got != want {
		t.Errorf("ErrZeroSnapshot = %q, want %q", got, want)
	}
}

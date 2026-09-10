package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestNeo4jDiff_LateWarningReachesTheOperator drives the index read through a
// sink that has already flushed, as a command flushes before its payload, and
// closes it as the wrapper does. The warning must reach the stream in both
// formats.
func TestNeo4jDiff_LateWarningReachesTheOperator(t *testing.T) {
	t.Parallel()

	for _, format := range []cli.OutputFormat{cli.FormatText, cli.FormatJSON} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			sink := cli.NewDiagnosticSink(&out, format, true, false)
			log := &queryLog{replies: []queryReply{
				{records: recordedConstraints()},
				{err: errors.New("unsupported in this edition")},
			}}
			sink.Flush()
			state, err := fetchRemoteState(t.Context(), log.run, "", sink)
			if err != nil || !state.indexFailed {
				t.Fatalf("fetchRemoteState: err=%v indexFailed=%v", err, state.indexFailed)
			}
			_ = sink.Close(nil)
			var failure string
			if !strings.Contains(out.String(), "W_NEO4J_INDEXES_UNREADABLE") {
				failure = "the warning added after the first render reached no stream; got " + out.String()
			}
			checkRepairState(t, "neo4j diff: a warning added after the pre-payload render, "+string(format), failure)
		})
	}
}

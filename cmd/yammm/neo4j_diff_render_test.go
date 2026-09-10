package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestNeo4jDiff_LateWarningReachesTheOperator mirrors runNeo4jDiff's render
// order: the load's diagnostics render before the connection, the index read
// then adds its warning, and the wrapper renders again at the end. The warning
// must reach the stream in both formats.
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
			sink.Render()
			state, err := fetchRemoteState(t.Context(), log.run, "", sink)
			if err != nil || !state.indexFailed {
				t.Fatalf("fetchRemoteState: err=%v indexFailed=%v", err, state.indexFailed)
			}
			sink.Render()
			var failure string
			if !strings.Contains(out.String(), "W_NEO4J_INDEXES_UNREADABLE") {
				failure = "the warning added after the first render reached no stream; got " + out.String()
			}
			checkRepairState(t, "neo4j diff: a warning added after the pre-payload render, "+string(format), failure)
		})
	}
}

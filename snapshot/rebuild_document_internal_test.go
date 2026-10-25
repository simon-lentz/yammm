package snapshot

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// RebuildDocument runs graph.RebuildSnapshot over the parts the reader builds
// from data without the reader's own refusals, so a test can count what the
// rebuild reports for a document the reader refuses. A document whose parts
// cannot be built, as one holding a null row, reports one Fatal issue.
func RebuildDocument(data []byte, s *schema.Schema) (res diag.Result) {
	fail := func(msg string) diag.Result {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, msg).Build())
		return c.Result()
	}
	sd := newStreamDecoder(data, s, loadConfig{})
	if err := sd.decodeHeader(); err != nil {
		return fail(err.Error())
	}
	groups, diags, err := sd.decodeSections()
	if err != nil {
		return fail(err.Error())
	}
	parts, built := func() (parts graph.SnapshotParts, built bool) {
		// Only the parts' building is recovered: a panic in RebuildSnapshot
		// reaches the test.
		defer func() {
			if r := recover(); r != nil {
				res = fail(fmt.Sprintf("the document's parts cannot be built: %v", r))
			}
		}()
		return sd.loadDocument(groups, diags), true
	}()
	if !built {
		return res
	}
	_, res = graph.RebuildSnapshot(s, parts)
	return res
}

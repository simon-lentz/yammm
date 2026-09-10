package format_test

import "testing"

// pendingRepairs names each instrument case that fails on the current tree
// and the defect it observes. A listed case must keep failing, so a repair
// turns it red until its entry is deleted; an unlisted failure is a regression.
var pendingRepairs = map[string]string{
	"testdata/ensure_blank_after_import_block.yammm#comment-ending-brace":          "a commented schema or import line loses its required blank line",
	"testdata/ensure_blank_after_import_block.yammm#trailing-comment":              "a commented schema or import line loses its required blank line",
	"testdata/ensure_blank_after_schema.yammm#comment-ending-brace":                "a commented schema or import line loses its required blank line",
	"testdata/ensure_blank_after_schema.yammm#trailing-comment":                    "a commented schema or import line loses its required blank line",
	"testdata/golden/wrapping.yammm#comment-ending-brace":                          "a comment ending in a brace is read as the extends body brace",
	"testdata/golden/wrapping.yammm#tight-operators":                               "an unspaced logical operator is cut one byte late and severs the next variable",
	"testdata/roundtrip/enum_value_on_opening_line.yammm#blank-in-block":           "the value sharing the opening line with Enum[ is dropped",
	"testdata/roundtrip/enum_value_on_opening_line.yammm#original":                 "the value sharing the opening line with Enum[ is dropped",
	"testdata/roundtrip/enum_value_on_opening_line.yammm#tight-operators":          "the value sharing the opening line with Enum[ is dropped",
	"testdata/roundtrip/g12_single_quoted_invariant_message.yammm#tight-operators": "an unspaced logical operator is cut one byte late and severs the next variable",
	"testdata/roundtrip/g4_logical_op_in_trailing_comment.yammm#tight-operators":   "an unspaced logical operator is cut one byte late and severs the next variable",
	"testdata/roundtrip/enum_value_on_opening_line.yammm":                          "the value sharing the opening line with Enum[ is dropped",
	"testdata/enum_closing_line_comment.yammm":                                     "a comment on the closing bracket line refuses a collapse that loses nothing",
}

// checkRepairState reports a case against pendingRepairs. failure is empty
// when the case passed.
func checkRepairState(t *testing.T, key, failure string) {
	t.Helper()
	defect, pinned := pendingRepairs[key]
	switch {
	case pinned && failure == "":
		t.Errorf("%s: the pinned defect (%s) no longer reproduces; delete its pendingRepairs entry", key, defect)
	case pinned:
		t.Logf("%s: pinned defect reproduces (%s): %s", key, defect, failure)
	case failure != "":
		t.Error(failure)
	}
}

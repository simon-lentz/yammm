package format_test

import "testing"

// pendingRepairs names each instrument case that fails on the current tree
// and the defect it observes. A listed case must keep failing, so a repair
// turns it red until its entry is deleted; an unlisted failure is a regression.
var pendingRepairs = map[string]string{
	"testdata/brace_inside_block_comment.yammm#blank-in-block":                                 "a blank line inside a block comment is dropped",
	"testdata/ensure_blank_after_import_block.yammm#comment-ending-brace":                      "a commented schema or import line loses its required blank line",
	"testdata/ensure_blank_after_import_block.yammm#trailing-comment":                          "a commented schema or import line loses its required blank line",
	"testdata/ensure_blank_after_schema.yammm#comment-ending-brace":                            "a commented schema or import line loses its required blank line",
	"testdata/ensure_blank_after_schema.yammm#trailing-comment":                                "a commented schema or import line loses its required blank line",
	"testdata/golden/comprehensive.yammm#blank-in-block":                                       "a blank line inside a block comment is dropped",
	"testdata/golden/comprehensive.yammm#comment-ending-brace":                                 "formatting is not a fixed point once a trailing comment carries a bracket or a brace",
	"testdata/golden/comprehensive.yammm#trailing-comment":                                     "formatting is not a fixed point once a trailing comment carries a bracket or a brace",
	"testdata/golden/edge_cases.yammm#blank-in-block":                                          "a blank line inside a block comment is dropped",
	"testdata/golden/wrapping.yammm#comment-ending-brace":                                      "a trailing comment is folded into the construct it followed",
	"testdata/golden/wrapping.yammm#tight-operators":                                           "a comment ending in a brace is read as the extends body brace",
	"testdata/golden/wrapping.yammm#trailing-comment":                                          "formatting is not a fixed point once a trailing comment carries a bracket or a brace",
	"testdata/roundtrip/block_comment_blank_lines.yammm#blank-in-block":                        "a blank line inside a block comment is dropped",
	"testdata/roundtrip/block_comment_blank_lines.yammm#comment-ending-brace":                  "a blank line inside a block comment is dropped",
	"testdata/roundtrip/block_comment_blank_lines.yammm#original":                              "a blank line inside a block comment is dropped",
	"testdata/roundtrip/block_comment_blank_lines.yammm#tight-operators":                       "a blank line inside a block comment is dropped",
	"testdata/roundtrip/block_comment_blank_lines.yammm#trailing-comment":                      "a blank line inside a block comment is dropped",
	"testdata/roundtrip/control_clean.yammm#comment-ending-brace":                              "formatting is not a fixed point once a trailing comment carries a bracket or a brace",
	"testdata/roundtrip/control_clean.yammm#trailing-comment":                                  "formatting is not a fixed point once a trailing comment carries a bracket or a brace",
	"testdata/roundtrip/enum_value_on_opening_line.yammm#blank-in-block":                       "the value sharing the opening line with Enum[ is dropped",
	"testdata/roundtrip/enum_value_on_opening_line.yammm#original":                             "the value sharing the opening line with Enum[ is dropped",
	"testdata/roundtrip/enum_value_on_opening_line.yammm#tight-operators":                      "the value sharing the opening line with Enum[ is dropped",
	"testdata/roundtrip/g12_single_quoted_invariant_message.yammm#tight-operators":             "an unspaced logical operator is cut one byte late and severs the next variable",
	"testdata/roundtrip/g4_logical_op_in_trailing_comment.yammm#tight-operators":               "an unspaced logical operator is cut one byte late and severs the next variable",
	"testdata/roundtrip/layout_alignment_after_commented_construct.yammm#blank-in-block":       "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_after_commented_construct.yammm#comment-ending-brace": "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_after_commented_construct.yammm#original":             "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_after_commented_construct.yammm#tight-operators":      "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_after_commented_construct.yammm#trailing-comment":     "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_bracket_in_enum_value.yammm#blank-in-block":           "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_bracket_in_enum_value.yammm#comment-ending-brace":     "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_bracket_in_enum_value.yammm#original":                 "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_bracket_in_enum_value.yammm#tight-operators":          "a property group loses its alignment after a construct or a wrapped line is rebuilt without its lexical record",
	"testdata/roundtrip/layout_alignment_bracket_in_enum_value.yammm#trailing-comment":         "formatting is not a fixed point once a trailing comment carries a bracket or a brace",
	"testdata/roundtrip/enum_value_on_opening_line.yammm":                                      "the value sharing the opening line with Enum[ is dropped",
	"testdata/enum_closing_line_comment.yammm":                                                 "a comment on the closing bracket line refuses a collapse that loses nothing",
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

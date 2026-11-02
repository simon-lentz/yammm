package format

import "github.com/simon-lentz/yammm/internal/parse"

// The instruments in package format_test read the postcondition and the
// layout checker through these names.
var (
	Preserves        = preserves
	LayoutViolations = layoutViolations
	Finalize         = finalizeFormattedText
)

// Collapsed returns src after phases 1 and 2.
func Collapsed(src string) (string, error) {
	ls, _, err := lexicalLines(src)
	if err != nil {
		return "", err
	}
	return joinLines(collapseBlankLines(ls)), nil
}

// Unchecked returns src after phases 1 to 5, without the postcondition, so a
// test can compare a rewrite the formatter would refuse.
func Unchecked(src string) (string, error) {
	ls, _, err := lexicalLines(src)
	if err != nil {
		return "", err
	}
	return rewrite(ls), nil
}

// preserves is [preservesTokens] over a source not yet lexed.
func preserves(in, out string) error {
	return preservesTokens(parse.Lex(lineEndingReplacer.Replace(in)), out)
}

// WrapLongLines runs the wrapping phase over text it lexes itself.
func WrapLongLines(text string) string {
	if text == "" {
		return ""
	}
	return joinLines(wrapLongLines(classifyLexed(text)))
}

// AlignColumns runs the alignment phase over text it lexes itself.
func AlignColumns(text string) string {
	if text == "" {
		return ""
	}
	return joinLines(alignColumns(classifyLexed(text)))
}

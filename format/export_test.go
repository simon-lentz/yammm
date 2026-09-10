package format

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

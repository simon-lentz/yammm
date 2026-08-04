package parse

import "fmt"

// InvalidNumberMessage returns the diagnostic text for a literal the lexer
// classified as a malformed number. The wording is part of the language's
// user-facing contract, so it lives in one place and every position that can
// hold a numeric literal reports it identically.
func InvalidNumberMessage(text string) string {
	return fmt.Sprintf(
		"malformed numeric literal %q: numeric literals are decimal "+
			"(hexadecimal and suffixed forms are not supported)", text,
	)
}

package docstate

import "strings"

// lineEndingReplacer performs CRLF/CR → LF normalization in a single pass.
var lineEndingReplacer = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// NormalizeLineEndings converts CRLF and CR line endings to LF.
// This ensures consistent line ending handling across platforms.
// Windows clients may send CRLF (\r\n), which would cause incorrect
// byte offset calculations in position conversion.
func NormalizeLineEndings(text string) string {
	return lineEndingReplacer.Replace(text)
}

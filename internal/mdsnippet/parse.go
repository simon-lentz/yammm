package mdsnippet

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/internal/grammar"
)

// ParseErrors runs the lexer and parser over source and returns the syntax
// errors it reported. An empty slice means source parsed cleanly.
func ParseErrors(source string) []string {
	input := antlr.NewInputStream(source)
	lexer := grammar.NewYammmGrammarLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar.NewYammmGrammarParser(stream)

	el := &collectingErrorListener{}
	parser.RemoveErrorListeners()
	parser.AddErrorListener(el)

	parser.Schema() // root rule

	return el.errors
}

// collectingErrorListener collects ANTLR syntax errors as strings.
type collectingErrorListener struct {
	antlr.DefaultErrorListener
	errors []string
}

func (l *collectingErrorListener) SyntaxError(
	_ antlr.Recognizer,
	_ any,
	line, column int,
	msg string,
	_ antlr.RecognitionException,
) {
	l.errors = append(l.errors, fmt.Sprintf("line %d:%d %s", line, column, msg))
}

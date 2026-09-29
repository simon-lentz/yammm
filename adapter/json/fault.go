package json

import (
	"bytes"

	"github.com/simon-lentz/yammm/adapter/internal/typetag"
)

// nestingLimit is the depth both implementations of encoding/json refuse to
// nest past; nestingFromRoot says where the running one counts it from.
const nestingLimit = 10000

// A faultState is what a container of the document accepts next.
type faultState uint8

const (
	wantFirst faultState = iota // after the opening delimiter
	wantColon                   // after a member name
	wantValue                   // after a colon, or after a comma in an array
	wantName                    // after a comma in an object
	wantComma                   // after a value
)

// faultContainer is one open object or array of the document faultOffset
// reads, with its nesting level as the running implementation counts it.
type faultContainer struct {
	object    bool
	state     faultState
	level     int
	instances bool // an array of instances, whose elements are decoded one by one
	refused   bool // the root object's member being read has a refused name
}

type faultOutcome uint8

const (
	readOn    faultOutcome = iota // the text read so far can go on
	faulted                       // no continuation repairs the byte named
	exhausted                     // the input ends where a continuation can go on
)

// faultOffset returns the offset of the first byte of src from which no
// continuation gives a document ParseObject reads without a decoder fault, or
// false when src ends, or its root closes, before such a byte. A root array is
// refused by shape once its "[" reads, so it is not read; the package doc's
// "Where a fault is placed" states the grammar.
func faultOffset(src []byte) (int, bool) {
	var stack []faultContainer
	i := 0
	for {
		next, out := skipBlank(src, i)
		if out != readOn {
			return next, out == faulted
		}
		i = next
		if len(stack) == 0 {
			switch src[i] {
			case '{':
				stack = append(stack, faultContainer{object: true, level: nestingLevel(nil)})
				i++
				continue
			case '[':
				return 0, false
			}
			// A scalar root: the parse reads it whole with one Token call.
			next, out = openOrScalar(src, i, &stack)
			return next, out == faulted
		}
		top := &stack[len(stack)-1]
		c := src[i]
		switch {
		case isCloser(c) && acceptsCloser(top):
			if closes(top, c) != readOn {
				return i, true
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return 0, false
			}
			stack[len(stack)-1].state = wantComma
			i++
		case c == ',':
			next, out = comma(src, i, &stack)
			if out != readOn {
				return next, out == faulted
			}
			i = next
		case top.object && (top.state == wantFirst || top.state == wantName) && c == '"':
			end, out := scanString(src, i)
			if out != readOn {
				return end, out == faulted
			}
			if len(stack) == 1 {
				top.refused = typetag.Validate(string(decodeName(src[i:end]))) != nil
			}
			top.state = wantColon
			i = end
		case top.object && top.state == wantColon && c == ':':
			top.state = wantValue
			i++
		case top.state == wantValue || !top.object && top.state == wantFirst:
			next, out = openOrScalar(src, i, &stack)
			if out != readOn {
				return next, out == faulted
			}
			i = next
		default:
			return i, true
		}
	}
}

// acceptsCloser reports whether a closing delimiter is judged by top's rules at
// all: after a comma it is, because jsonc blanks that comma.
func acceptsCloser(top *faultContainer) bool {
	return top.state == wantFirst || top.state == wantComma ||
		top.state == wantName || !top.object && top.state == wantValue
}

func isCloser(c byte) bool { return c == '}' || c == ']' }

// closes reports whether c is the delimiter that closes top.
func closes(top *faultContainer, c byte) faultOutcome {
	if top.object && c == '}' || !top.object && c == ']' {
		return readOn
	}
	return faulted
}

// comma reads a comma at src[i] against the container it stands in. jsonc
// blanks a comma that a closing delimiter follows, and the delimiter is then
// judged by the state before the comma.
func comma(src []byte, i int, stack *[]faultContainer) (int, faultOutcome) {
	top := &(*stack)[len(*stack)-1]
	switch top.state {
	case wantComma:
		if top.object {
			top.state = wantName
		} else {
			top.state = wantValue
		}
		return i + 1, readOn
	case wantFirst:
		next, out := skipBlank(src, i+1)
		if out != readOn {
			return next, out
		}
		if closes(top, src[next]) == readOn {
			*stack = (*stack)[:len(*stack)-1]
			if len(*stack) == 0 {
				return 0, exhausted
			}
			(*stack)[len(*stack)-1].state = wantComma
			return next + 1, readOn
		}
		return next, faulted
	}
	return i, faulted
}

// openOrScalar reads the value that starts at src[i]: it opens a container,
// checking the nesting limit, or reads a string, a number or a literal whole.
func openOrScalar(src []byte, i int, stack *[]faultContainer) (int, faultOutcome) {
	c := src[i]
	if c == '{' || c == '[' {
		child := faultContainer{object: c == '{'}
		child.level = nestingLevel(*stack)
		if child.level > nestingLimit {
			return i, faulted
		}
		if n := len(*stack); n == 1 && !child.object && !(*stack)[0].refused {
			child.instances = true
		}
		*stack = append(*stack, child)
		return i + 1, readOn
	}
	var end int
	var out faultOutcome
	switch {
	case c == '"':
		end, out = scanString(src, i)
	case c == '-' || '0' <= c && c <= '9':
		end, out = scanNumber(src, i)
	case c == 't':
		end, out = scanWord(src, i, "true")
	case c == 'f':
		end, out = scanWord(src, i, "false")
	case c == 'n':
		end, out = scanWord(src, i, "null")
	default:
		return i, faulted
	}
	if out == readOn && len(*stack) > 0 {
		(*stack)[len(*stack)-1].state = wantComma
	}
	return end, out
}

// nestingLevel returns the level a container opened inside stack takes. Where
// encoding/json runs its own implementation, only a value read whole by Decode
// counts, from its own opening delimiter: an element of an array of instances,
// or the value of a refused name. Its v2 implementation counts from the root.
func nestingLevel(stack []faultContainer) int {
	if nestingFromRoot {
		return len(stack) + 1
	}
	if len(stack) == 0 {
		return 0
	}
	parent := stack[len(stack)-1]
	switch {
	case parent.level > 0:
		return parent.level + 1
	case parent.instances, len(stack) == 1 && parent.refused:
		return 1
	}
	return 0
}

// skipBlank returns the offset of the next byte past white space and jsonc's
// comments. A "/" that no "/" or "*" follows faults at the byte after it,
// since a "/" at the end of the input may yet start a comment.
func skipBlank(src []byte, i int) (int, faultOutcome) {
	for i < len(src) {
		switch src[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '/':
			if i+1 == len(src) {
				return 0, exhausted
			}
			var end int
			switch src[i+1] {
			case '/':
				end = bytes.IndexByte(src[i+2:], '\n')
			case '*':
				if end = bytes.Index(src[i+2:], []byte("*/")); end >= 0 {
					end++
				}
			default:
				return i + 1, faulted
			}
			if end < 0 {
				return 0, exhausted
			}
			i += 2 + end + 1
		default:
			return i, readOn
		}
	}
	return 0, exhausted
}

// scanString reads the string that opens at src[i] and returns the offset past
// its closing quote. A byte of an invalid UTF-8 sequence is read, as both
// implementations of encoding/json read it; a control character is not.
func scanString(src []byte, i int) (int, faultOutcome) {
	for j := i + 1; j < len(src); j++ {
		switch c := src[j]; {
		case c == '"':
			return j + 1, readOn
		case c < 0x20:
			return j, faulted
		case c == '\\':
			j++
			if j == len(src) {
				return 0, exhausted
			}
			switch src[j] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				for k := j + 1; k <= j+4; k++ {
					if k == len(src) {
						return 0, exhausted
					}
					if !isHex(src[k]) {
						return k, faulted
					}
				}
				j += 4
			default:
				return j, faulted
			}
		}
	}
	return 0, exhausted
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// scanNumber reads the number that starts at src[i] as RFC 8259 spells one,
// and returns the offset of the first byte past it for the container to judge.
func scanNumber(src []byte, i int) (int, faultOutcome) {
	j := i
	if src[j] == '-' {
		j++
	}
	digits := func(required bool) faultOutcome {
		if j == len(src) {
			return exhausted
		}
		if required && !isDigit(src[j]) {
			return faulted
		}
		for j < len(src) && isDigit(src[j]) {
			j++
		}
		if j == len(src) {
			return exhausted
		}
		return readOn
	}
	switch {
	case j == len(src):
		return 0, exhausted
	case src[j] == '0':
		j++
		if j == len(src) {
			return 0, exhausted
		}
	case isDigit(src[j]):
		if out := digits(true); out != readOn {
			return j, out
		}
	default:
		return j, faulted
	}
	if src[j] == '.' {
		j++
		if out := digits(true); out != readOn {
			return j, out
		}
	}
	if src[j] == 'e' || src[j] == 'E' {
		j++
		if j < len(src) && (src[j] == '+' || src[j] == '-') {
			j++
		}
		if out := digits(true); out != readOn {
			return j, out
		}
	}
	return j, readOn
}

// scanWord reads the literal word that starts at src[i].
func scanWord(src []byte, i int, word string) (int, faultOutcome) {
	for k := range len(word) {
		if i+k == len(src) {
			return 0, exhausted
		}
		if src[i+k] != word[k] {
			return i + k, faulted
		}
	}
	return i + len(word), readOn
}

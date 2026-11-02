package ident

import (
	"strings"
	"unicode"
)

// ToUpperCamelInitialisms tokenizes s on runes and returns an UpperCamelCase
// identifier: segments split on separators, digit/letter transitions and case
// breaks, acronym runs stay grouped, and adjacent numeric segments are separated
// by a single underscore. A name segment whose lower-case form is a key in
// initialisms is upper-cased wholesale, so with {"id": true, "url": true},
// "user_id" -> "UserID" and "base_url" -> "BaseURL".
//
// The caller owns the initialism set; this package holds no vocabulary of its
// own, so domain-specific acronyms stay with the consumer.
func ToUpperCamelInitialisms(s string, initialisms map[string]bool) string {
	tokens := splitIdentifier(s)
	for i := range tokens {
		t := &tokens[i]
		if !t.isAcronym && initialisms[strings.ToLower(t.text)] {
			t.isAcronym = true
		}
	}
	return joinCamelTokens(tokens)
}

type token struct {
	text      string
	isNumber  bool
	isAcronym bool
}

type runeClass int

const (
	classOther runeClass = iota
	classLower
	classUpper
	classDigit
)

func splitIdentifier(s string) []token {
	var tokens []token
	var current []rune
	var lastClass runeClass
	runes := []rune(s)
	for i, r := range runes {
		class := classifyRune(r)
		if class == classOther {
			tokens = appendToken(tokens, current)
			current = current[:0]
			continue
		}

		nextClass := classOther
		if i+1 < len(runes) {
			nextClass = classifyRune(runes[i+1])
		}

		if len(current) > 0 {
			if boundary(lastClass, class, nextClass) {
				tokens = appendToken(tokens, current)
				current = current[:0]
			}
		}

		current = append(current, r)
		lastClass = class
	}

	return appendToken(tokens, current)
}

func appendToken(tokens []token, current []rune) []token {
	if len(current) == 0 {
		return tokens
	}
	tokens = append(tokens, buildToken(current))
	return tokens
}

func boundary(prev, current, next runeClass) bool {
	switch {
	case prev == classDigit && current != classDigit:
		return true
	case prev != classDigit && current == classDigit:
		return true
	case prev == classLower && current == classUpper:
		return true
	case prev == classUpper && current == classUpper && next == classLower:
		return true
	default:
		return false
	}
}

// classifyRune determines the character class of a rune for identifier splitting.
// Unicode title case letters (e.g., ǅ, ǲ) fall through to the IsLetter check
// and are classified as lower case. This is intentional: title case is rare in
// identifiers, and treating it as lower case produces reasonable behavior for
// the tokenization algorithm.
func classifyRune(r rune) runeClass {
	switch {
	case unicode.IsDigit(r):
		return classDigit
	case unicode.IsUpper(r):
		return classUpper
	case unicode.IsLower(r):
		return classLower
	case unicode.IsLetter(r):
		// Title case and other Unicode letter categories treated as lower
		return classLower
	default:
		return classOther
	}
}

func buildToken(runes []rune) token {
	isNumber := true
	allUpper := true
	for _, r := range runes {
		switch {
		case unicode.IsDigit(r):
			allUpper = false
		case unicode.IsLetter(r):
			isNumber = false
			if !unicode.IsUpper(r) {
				allUpper = false
			}
		default:
			isNumber = false
			allUpper = false
		}
	}
	return token{
		text:      string(runes),
		isNumber:  isNumber,
		isAcronym: !isNumber && allUpper,
	}
}

func joinCamelTokens(tokens []token) string {
	var b strings.Builder
	for i, tok := range tokens {
		if i > 0 && tok.isNumber && tokens[i-1].isNumber {
			b.WriteRune('_')
		}

		b.WriteString(upperToken(tok))
	}
	result := b.String()
	// Prefix with underscore if result starts with a digit to ensure valid Go identifier
	if len(result) > 0 && unicode.IsDigit(rune(result[0])) {
		return "_" + result
	}
	return result
}

func upperToken(tok token) string {
	if tok.isNumber {
		return tok.text
	}
	if tok.isAcronym {
		return strings.ToUpper(tok.text)
	}
	return capitalizeFirst(tok.text)
}

func capitalizeFirst(s string) string {
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

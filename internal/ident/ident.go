package ident

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ToLowerSnake converts an identifier to lower_snake_case.
//
// This implements the canonical lower_snake algorithm for relation name
// normalization (schema relation names to JSON field names).
//
// Separator characters (underscores, spaces, punctuation) are treated as token
// boundaries and removed from output. Empty or separator-only inputs return an
// empty string; callers should validate that relation names are non-empty before
// normalization.
//
// Examples:
//
//	ToLowerSnake("WORKS_AT")   = "works_at"
//	ToLowerSnake("HTTPProxy")  = "http_proxy"
//	ToLowerSnake("CreatedBy")  = "created_by"
//	ToLowerSnake("UserID")     = "user_id"
//	ToLowerSnake("___")        = ""           // separator-only
func ToLowerSnake(s string) string {
	tokens := splitIdentifier(s)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, len(tokens))
	for i, tok := range tokens {
		parts[i] = tok.text
	}
	return strings.ToLower(strings.Join(parts, "_"))
}

// Capitalize converts an identifier to UpperCamelCase using rune-aware tokenization.
// Acronym runs remain upper-case and separators are removed.
//
// Capitalize is semantically equivalent to [ToUpperCamel] and is provided for
// code clarity when the intent is deriving Go export names.
//
// Examples:
//
//	Capitalize("http_server") = "HttpServer"
//	Capitalize("user_id")     = "UserID"
func Capitalize(s string) string {
	return ToUpperCamel(s)
}

// ToUpperCamel tokenizes s on runes and returns an UpperCamelCase identifier.
// Segments are split on separators, digit/letter transitions, and case breaks;
// acronym runs stay grouped; adjacent numeric segments are separated by a
// single underscore in the output.
func ToUpperCamel(s string) string {
	return joinCamelTokens(splitIdentifier(s), false)
}

// ToLowerCamel tokenizes s on runes and returns a lowerCamelCase identifier.
// Segments are split on separators, digit/letter transitions, and case breaks;
// acronym runs stay grouped; adjacent numeric segments are separated by a
// single underscore in the output.
func ToLowerCamel(s string) string {
	return joinCamelTokens(splitIdentifier(s), true)
}

// --- Internal tokenization types and functions ---

type token struct {
	text      string
	isNumber  bool
	isAcronym bool
	runeLen   int
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

	tokens = appendToken(tokens, current)
	return mergeAcronymRuns(tokens)
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
	runeLen := len(runes)
	isAcronym := !isNumber && allUpper
	return token{
		text:      string(runes),
		isNumber:  isNumber,
		isAcronym: isAcronym,
		runeLen:   runeLen,
	}
}

func mergeAcronymRuns(tokens []token) []token {
	if len(tokens) == 0 {
		return tokens
	}
	merged := make([]token, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.isNumber || !tok.isAcronym || tok.runeLen > 1 {
			merged = append(merged, tok)
			continue
		}

		builder := strings.Builder{}
		builder.WriteString(tok.text)
		j := i
		for j+1 < len(tokens) && tokens[j+1].isAcronym && !tokens[j+1].isNumber && tokens[j+1].runeLen == 1 {
			builder.WriteString(tokens[j+1].text)
			j++
		}
		if j == i {
			merged = append(merged, tok)
			continue
		}
		merged = append(merged, token{
			text:      builder.String(),
			isNumber:  false,
			isAcronym: true,
			runeLen:   utf8.RuneCountInString(builder.String()),
		})
		i = j
	}
	return merged
}

func joinCamelTokens(tokens []token, lowerFirst bool) string {
	if len(tokens) == 0 {
		return ""
	}

	var b strings.Builder
	for i, tok := range tokens {
		if i > 0 && tok.isNumber && tokens[i-1].isNumber {
			b.WriteRune('_')
		}

		switch {
		case lowerFirst && i == 0:
			b.WriteString(lowerToken(tok))
		default:
			b.WriteString(upperToken(tok))
		}
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

func lowerToken(tok token) string {
	if tok.isNumber {
		return tok.text
	}
	return strings.ToLower(tok.text)
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

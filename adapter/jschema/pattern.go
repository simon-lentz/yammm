package jschema

import (
	"fmt"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// sharedPattern rewrites a yammm pattern, which Go's regexp compiles as RE2,
// into a pattern written only in syntax that RE2 and ECMA-262 (with the "u"
// flag) read the same way. JSON Schema names ECMA-262 as its dialect, and
// validators built on Go's regexp read the emitted text as RE2, so the source
// text reaches neither faithfully: "(?i)" and "[[:alpha:]]" do not compile
// under ECMA-262, and "." and "\s" match different characters there.
//
// The rewrite parses the pattern as regexp.Compile does and renders the tree:
// case folding becomes explicit classes, every class becomes explicit ranges
// (\d and \w too, which some validators read as Unicode classes), "." becomes
// "[^\n]", and flags vanish. A string of code points matches the result
// exactly when it matches the source; a JSON string holding a lone surrogate
// is not one (see the Fidelity Caveats). It reports false for a pattern holding a line
// anchor — "^" or "$" under the "m" flag — which neither dialect can state
// without the flag or a lookaround the other lacks.
func sharedPattern(pattern string) (string, bool) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return "", false
	}
	var sb strings.Builder
	if !writeShared(&sb, re) {
		return "", false
	}
	return sb.String(), true
}

// writeShared renders re into sb, reporting false when re holds a node no
// shared syntax states.
func writeShared(sb *strings.Builder, re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpNoMatch:
		sb.WriteString(`[^\s\S]`)
	case syntax.OpEmptyMatch:
		sb.WriteString(`(?:)`)
	case syntax.OpLiteral:
		for _, r := range re.Rune {
			if re.Flags&syntax.FoldCase != 0 {
				writeClass(sb, foldOrbit(r))
			} else {
				writeLiteral(sb, r)
			}
		}
	case syntax.OpCharClass:
		writeClass(sb, re.Rune)
	case syntax.OpAnyCharNotNL:
		sb.WriteString(`[^\n]`)
	case syntax.OpAnyChar:
		sb.WriteString(`[\s\S]`)
	case syntax.OpBeginText:
		sb.WriteByte('^')
	case syntax.OpEndText:
		sb.WriteByte('$')
	case syntax.OpWordBoundary:
		sb.WriteString(`\b`)
	case syntax.OpNoWordBoundary:
		sb.WriteString(`\B`)
	case syntax.OpCapture:
		// A capture groups nothing a match depends on except an alternation.
		if re.Sub[0].Op == syntax.OpAlternate {
			return writeGroup(sb, re.Sub[0])
		}
		return writeShared(sb, re.Sub[0])
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if !writeAtom(sb, re.Sub[0]) {
			return false
		}
		switch re.Op {
		case syntax.OpStar:
			sb.WriteByte('*')
		case syntax.OpPlus:
			sb.WriteByte('+')
		case syntax.OpQuest:
			sb.WriteByte('?')
		default:
			sb.WriteByte('{')
			sb.WriteString(strconv.Itoa(re.Min))
			if re.Max != re.Min {
				sb.WriteByte(',')
				if re.Max >= 0 {
					sb.WriteString(strconv.Itoa(re.Max))
				}
			}
			sb.WriteByte('}')
		}
		if re.Flags&syntax.NonGreedy != 0 {
			sb.WriteByte('?')
		}
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpAlternate {
				if !writeGroup(sb, sub) {
					return false
				}
				continue
			}
			if !writeShared(sb, sub) {
				return false
			}
		}
	case syntax.OpAlternate:
		for i, sub := range re.Sub {
			if i > 0 {
				sb.WriteByte('|')
			}
			if !writeShared(sb, sub) {
				return false
			}
		}
	default:
		// OpBeginLine and OpEndLine: the "m" flag's anchors.
		return false
	}
	return true
}

// writeAtom renders a repetition operand, grouping it unless it renders as a
// single character or class.
func writeAtom(sb *strings.Builder, re *syntax.Regexp) bool {
	for re.Op == syntax.OpCapture {
		re = re.Sub[0]
	}
	switch {
	case re.Op == syntax.OpLiteral && len(re.Rune) == 1,
		re.Op == syntax.OpCharClass, re.Op == syntax.OpAnyChar, re.Op == syntax.OpAnyCharNotNL:
		return writeShared(sb, re)
	}
	return writeGroup(sb, re)
}

func writeGroup(sb *strings.Builder, re *syntax.Regexp) bool {
	sb.WriteString("(?:")
	if !writeShared(sb, re) {
		return false
	}
	sb.WriteByte(')')
	return true
}

// foldOrbit returns the ranges of every rune simple case folding maps r to,
// r included, sorted as an OpCharClass carries them.
func foldOrbit(r rune) []rune {
	orbit := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		orbit = append(orbit, f)
	}
	slices.Sort(orbit)
	ranges := make([]rune, 0, 2*len(orbit))
	for _, f := range orbit {
		ranges = append(ranges, f, f)
	}
	return ranges
}

// writeClass renders an OpCharClass's range pairs. A class holding U+0000 is
// written as the negation of its complement, which is how such classes are
// usually spelled. The surrogate block is dropped from every range: no Go
// string holds a surrogate, so the source can never match one.
func writeClass(sb *strings.Builder, ranges []rune) {
	negated := len(ranges) > 0 && ranges[0] == 0
	if negated {
		ranges = complementRanges(ranges)
	}
	var parts [][2]rune
	for i := 0; i < len(ranges); i += 2 {
		parts = append(parts, splitSurrogates(ranges[i], ranges[i+1])...)
	}
	switch {
	case len(parts) == 0 && negated:
		sb.WriteString(`[\s\S]`)
		return
	case len(parts) == 0:
		sb.WriteString(`[^\s\S]`)
		return
	case len(parts) == 1 && !negated && parts[0][0] == parts[0][1]:
		writeLiteral(sb, parts[0][0])
		return
	}
	sb.WriteByte('[')
	if negated {
		sb.WriteByte('^')
	}
	for _, part := range parts {
		writeClassRune(sb, part[0])
		if part[1] != part[0] {
			if part[1] > part[0]+1 {
				sb.WriteByte('-')
			}
			writeClassRune(sb, part[1])
		}
	}
	sb.WriteByte(']')
}

// complementRanges returns the ranges of every rune the sorted, disjoint
// ranges exclude.
func complementRanges(ranges []rune) []rune {
	var out []rune
	next := rune(0)
	for i := 0; i < len(ranges); i += 2 {
		if ranges[i] > next {
			out = append(out, next, ranges[i]-1)
		}
		next = ranges[i+1] + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, next, unicode.MaxRune)
	}
	return out
}

// splitSurrogates returns lo-hi with U+D800 to U+DFFF removed.
func splitSurrogates(lo, hi rune) [][2]rune {
	const surrLo, surrHi = 0xD800, 0xDFFF
	var out [][2]rune
	if lo < surrLo {
		out = append(out, [2]rune{lo, min(hi, surrLo-1)})
	}
	if hi > surrHi {
		out = append(out, [2]rune{max(lo, surrHi+1), hi})
	}
	return out
}

// writeLiteral renders r outside a class. Every ASCII punctuation character
// the two dialects treat as syntax is escaped; both read "\" before one as
// that character. A rune up to U+00FF that unicode.IsPrint rejects — a
// control character, U+00A0 or U+00AD — is written as \xHH, which both read
// the same; any other rune is written as itself, because the dialects share
// no escape above U+00FF.
func writeLiteral(sb *strings.Builder, r rune) {
	switch {
	case strings.ContainsRune(`\^$.|?*+()[]{}/`, r):
		sb.WriteByte('\\')
		sb.WriteRune(r)
	default:
		writePrintable(sb, r)
	}
}

// writeClassRune renders r inside a class, where "\", "]", "[", "^" and "-"
// are the characters either dialect can read as syntax.
func writeClassRune(sb *strings.Builder, r rune) {
	if strings.ContainsRune(`\]-[^`, r) {
		sb.WriteByte('\\')
		sb.WriteRune(r)
		return
	}
	writePrintable(sb, r)
}

func writePrintable(sb *strings.Builder, r rune) {
	if r <= 0xFF && !unicode.IsPrint(r) {
		fmt.Fprintf(sb, `\x%02X`, r)
		return
	}
	sb.WriteRune(r)
}

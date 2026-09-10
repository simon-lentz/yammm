package format

import (
	"strings"

	"github.com/simon-lentz/yammm/internal/parse"
)

// lineTokens groups the significant tokens of out by line, from a lex of out
// alone, so the checks below share nothing with the phases they check.
func lineTokens(out string) ([]string, [][]parse.Token) {
	lines := strings.Split(out, "\n")
	starts := make([]int, len(lines))
	off := 0
	for i, l := range lines {
		starts[i] = off
		off += len(l) + 1
	}
	byLine := make([][]parse.Token, len(lines))
	li := 0
	for _, tok := range parse.Lex(out) {
		if tok.Kind == kindWS || tok.Kind == kindSLComment || tok.Kind == kindDocComment {
			continue
		}
		for li+1 < len(lines) && starts[li+1] <= tok.Start {
			li++
		}
		byLine[li] = append(byLine[li], tok)
	}
	return lines, byLine
}

// declarationHeads are the lower-case words that open a declaration rather
// than name a property.
var declarationHeads = map[string]bool{
	"schema": true, "import": true, "type": true, "abstract": true, "part": true, "extends": true,
}

// layoutViolations checks three documented layout rules from a lex of the
// output alone, sharing no code with the phases that produce them: a blank
// line after schema and the last import, one type column per property group,
// and a long invariant wrapped at a top-level logical operator.
func layoutViolations(out string) []string {
	lines, toks := lineTokens(out)
	var v []string

	lastImport := -1
	for i, ts := range toks {
		if len(ts) >= 2 && ts[0].Kind == "LC_WORD" && ts[1].Kind == kindSTRING {
			switch ts[0].Value {
			case "schema":
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					v = append(v, "no blank line after schema")
				}
			case "import":
				lastImport = i
			}
		}
	}
	if lastImport >= 0 && lastImport+1 < len(lines) && strings.TrimSpace(lines[lastImport+1]) != "" {
		v = append(v, "no blank line after the last import")
	}

	depth := 0
	var run []int
	flush := func() {
		if len(run) > 1 {
			col := run[0]
			for _, c := range run[1:] {
				if c != col {
					v = append(v, "a property group is not aligned")
					break
				}
			}
		}
		run = nil
	}
	lineStart := 0
	for i, ts := range toks {
		lineDepth := 0
		for _, t := range ts {
			switch t.Kind {
			case kindLBRACK:
				lineDepth++
			case kindRBRACK:
				lineDepth--
			}
		}
		isProp := depth == 0 && lineDepth == 0 && len(ts) >= 2 &&
			ts[0].Kind == "LC_WORD" && !declarationHeads[ts[0].Value] && ts[1].Kind == "UC_WORD"
		if isProp {
			run = append(run, DisplayWidth(lines[i][:ts[1].Start-lineStart]))
		} else {
			flush()
		}
		depth += lineDepth
		lineStart += len(lines[i]) + 1
	}
	flush()

	for i, ts := range toks {
		if len(ts) < 3 || ts[0].Kind != "EXCLAMATION" || ts[1].Kind != kindSTRING {
			continue
		}
		if DisplayWidth(lines[i]) <= LineWidthThreshold {
			continue
		}
		nesting := 0
		for _, t := range ts[2:] {
			switch t.Kind {
			case kindLPAR, kindLBRACK, kindLBRACE:
				nesting++
			case kindRPAR, kindRBRACK, kindRBRACE:
				nesting--
			case "AND", "OR":
				if nesting == 0 {
					v = append(v, "a long invariant is not wrapped")
				}
			}
		}
	}
	return v
}

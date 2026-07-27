// Code generated from YammmGrammar.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // YammmGrammar
import (
	"fmt"
	"strconv"
	"sync"

	"github.com/antlr4-go/antlr/v4"
)

// Suppress unused import errors
var (
	_ = fmt.Printf
	_ = strconv.Itoa
	_ = sync.Once{}
)

type YammmGrammarParser struct {
	*antlr.BaseParser
}

var YammmGrammarParserStaticData struct {
	once                   sync.Once
	serializedATN          []int32
	LiteralNames           []string
	SymbolicNames          []string
	RuleNames              []string
	PredictionContextCache *antlr.PredictionContextCache
	atn                    *antlr.ATN
	decisionToDFA          []*antlr.DFA
}

func yammmgrammarParserInit() {
	staticData := &YammmGrammarParserStaticData
	staticData.LiteralNames = []string{
		"", "'schema'", "'import'", "'as'", "'abstract'", "'part'", "'type'",
		"'extends'", "'primary'", "'required'", "'one'", "'many'", "'Integer'",
		"'Float'", "'Boolean'", "'String'", "'Enum'", "'Pattern'", "'Timestamp'",
		"'Vector'", "'Date'", "'UUID'", "'List'", "'in'", "'nil'", "'datatype'",
		"'includes'", "'{'", "'}'", "'['", "']'", "'('", "')'", "':'", "','",
		"'='", "'-->'", "'*->'", "'->'", "'/'", "'_'", "'*'", "'@@'", "'@'",
		"'!'", "'+'", "'-'", "'||'", "'&&'", "'=='", "'!='", "'=~'", "'!~'",
		"'?'", "'>'", "'>='", "'<'", "'<='", "'$'", "'|'", "'.'", "'%'", "'^'",
	}
	staticData.SymbolicNames = []string{
		"", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "",
		"", "", "", "", "", "", "", "", "", "", "LBRACE", "RBRACE", "LBRACK",
		"RBRACK", "LPAR", "RPAR", "COLON", "COMMA", "EQUALS", "ASSOC", "COMP",
		"ARROW", "SLASH", "USCORE", "STAR", "ATAT", "AT", "EXCLAMATION", "PLUS",
		"MINUS", "OR", "AND", "EQUAL", "NOTEQUAL", "MATCH", "NOTMATCH", "QMARK",
		"GT", "GTE", "LT", "LTE", "DOLLAR", "PIPE", "PERIOD", "PERCENT", "HAT",
		"STRING", "DOC_COMMENT", "SL_COMMENT", "REGEXP", "WS", "VARIABLE", "INTEGER",
		"FLOAT", "INVALID_NUMBER", "BOOLEAN", "UC_WORD", "LC_WORD", "ANY_OTHER",
	}
	staticData.RuleNames = []string{
		"schema", "schema_name", "import_decl", "type", "datatype", "type_name",
		"alias_name", "type_ref", "extends_types", "type_body", "property",
		"rel_property", "property_name", "annotation", "type_annotation", "annotation_args",
		"annotation_arg", "data_type_ref", "qualified_alias", "association",
		"composition", "any_name", "multiplicity", "relation_body", "built_in",
		"integerT", "floatT", "boolT", "stringT", "enumT", "patternT", "timestampT",
		"vectorT", "dateT", "uuidT", "listT", "datatypeKeyword", "invariant",
		"expr", "arguments", "parameters", "literal", "lc_keyword",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 1, 75, 552, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2, 4, 7,
		4, 2, 5, 7, 5, 2, 6, 7, 6, 2, 7, 7, 7, 2, 8, 7, 8, 2, 9, 7, 9, 2, 10, 7,
		10, 2, 11, 7, 11, 2, 12, 7, 12, 2, 13, 7, 13, 2, 14, 7, 14, 2, 15, 7, 15,
		2, 16, 7, 16, 2, 17, 7, 17, 2, 18, 7, 18, 2, 19, 7, 19, 2, 20, 7, 20, 2,
		21, 7, 21, 2, 22, 7, 22, 2, 23, 7, 23, 2, 24, 7, 24, 2, 25, 7, 25, 2, 26,
		7, 26, 2, 27, 7, 27, 2, 28, 7, 28, 2, 29, 7, 29, 2, 30, 7, 30, 2, 31, 7,
		31, 2, 32, 7, 32, 2, 33, 7, 33, 2, 34, 7, 34, 2, 35, 7, 35, 2, 36, 7, 36,
		2, 37, 7, 37, 2, 38, 7, 38, 2, 39, 7, 39, 2, 40, 7, 40, 2, 41, 7, 41, 2,
		42, 7, 42, 1, 0, 1, 0, 5, 0, 89, 8, 0, 10, 0, 12, 0, 92, 9, 0, 1, 0, 1,
		0, 5, 0, 96, 8, 0, 10, 0, 12, 0, 99, 9, 0, 1, 0, 1, 0, 1, 1, 3, 1, 104,
		8, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1, 2, 1, 2, 1, 2, 3, 2, 113, 8, 2, 1, 3,
		3, 3, 116, 8, 3, 1, 3, 1, 3, 3, 3, 120, 8, 3, 1, 3, 1, 3, 1, 3, 3, 3, 125,
		8, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 4, 3, 4, 132, 8, 4, 1, 4, 1, 4, 1, 4,
		1, 4, 1, 4, 1, 5, 1, 5, 1, 6, 1, 6, 1, 7, 1, 7, 1, 7, 3, 7, 146, 8, 7,
		1, 7, 1, 7, 1, 8, 1, 8, 1, 8, 1, 8, 5, 8, 154, 8, 8, 10, 8, 12, 8, 157,
		9, 8, 1, 8, 3, 8, 160, 8, 8, 1, 9, 1, 9, 1, 9, 1, 9, 1, 9, 5, 9, 167, 8,
		9, 10, 9, 12, 9, 170, 9, 9, 1, 10, 3, 10, 173, 8, 10, 1, 10, 1, 10, 1,
		10, 1, 10, 3, 10, 179, 8, 10, 1, 10, 5, 10, 182, 8, 10, 10, 10, 12, 10,
		185, 9, 10, 1, 11, 3, 11, 188, 8, 11, 1, 11, 1, 11, 1, 11, 3, 11, 193,
		8, 11, 1, 12, 1, 12, 3, 12, 197, 8, 12, 1, 13, 1, 13, 1, 13, 3, 13, 202,
		8, 13, 1, 14, 3, 14, 205, 8, 14, 1, 14, 1, 14, 1, 14, 3, 14, 210, 8, 14,
		1, 15, 1, 15, 1, 15, 1, 15, 5, 15, 216, 8, 15, 10, 15, 12, 15, 219, 9,
		15, 1, 15, 3, 15, 222, 8, 15, 1, 15, 1, 15, 1, 16, 1, 16, 1, 16, 3, 16,
		229, 8, 16, 1, 17, 1, 17, 3, 17, 233, 8, 17, 1, 18, 1, 18, 1, 18, 3, 18,
		238, 8, 18, 1, 18, 1, 18, 1, 19, 3, 19, 243, 8, 19, 1, 19, 1, 19, 1, 19,
		3, 19, 248, 8, 19, 1, 19, 1, 19, 1, 19, 1, 19, 3, 19, 254, 8, 19, 3, 19,
		256, 8, 19, 1, 19, 1, 19, 3, 19, 260, 8, 19, 1, 19, 3, 19, 263, 8, 19,
		1, 20, 3, 20, 266, 8, 20, 1, 20, 1, 20, 1, 20, 3, 20, 271, 8, 20, 1, 20,
		1, 20, 1, 20, 1, 20, 3, 20, 277, 8, 20, 3, 20, 279, 8, 20, 1, 21, 1, 21,
		1, 22, 1, 22, 1, 22, 1, 22, 3, 22, 287, 8, 22, 1, 22, 1, 22, 1, 22, 3,
		22, 292, 8, 22, 1, 22, 3, 22, 295, 8, 22, 1, 22, 1, 22, 1, 23, 4, 23, 300,
		8, 23, 11, 23, 12, 23, 301, 1, 24, 1, 24, 1, 24, 1, 24, 1, 24, 1, 24, 1,
		24, 1, 24, 1, 24, 1, 24, 1, 24, 3, 24, 315, 8, 24, 1, 25, 1, 25, 1, 25,
		3, 25, 320, 8, 25, 1, 25, 1, 25, 1, 25, 3, 25, 325, 8, 25, 1, 25, 1, 25,
		3, 25, 329, 8, 25, 1, 26, 1, 26, 1, 26, 3, 26, 334, 8, 26, 1, 26, 1, 26,
		1, 26, 3, 26, 339, 8, 26, 1, 26, 1, 26, 3, 26, 343, 8, 26, 1, 27, 1, 27,
		1, 28, 1, 28, 1, 28, 1, 28, 1, 28, 1, 28, 3, 28, 353, 8, 28, 1, 29, 1,
		29, 1, 29, 1, 29, 1, 29, 4, 29, 360, 8, 29, 11, 29, 12, 29, 361, 1, 29,
		3, 29, 365, 8, 29, 1, 29, 1, 29, 1, 30, 1, 30, 1, 30, 1, 30, 1, 30, 3,
		30, 374, 8, 30, 1, 30, 1, 30, 1, 31, 1, 31, 1, 31, 1, 31, 3, 31, 382, 8,
		31, 1, 32, 1, 32, 1, 32, 1, 32, 1, 32, 1, 33, 1, 33, 1, 34, 1, 34, 1, 35,
		1, 35, 1, 35, 1, 35, 1, 35, 1, 35, 1, 35, 1, 35, 1, 35, 3, 35, 402, 8,
		35, 1, 36, 1, 36, 1, 37, 3, 37, 407, 8, 37, 1, 37, 1, 37, 1, 37, 1, 37,
		1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 5, 38, 419, 8, 38, 10, 38, 12,
		38, 422, 9, 38, 1, 38, 3, 38, 425, 8, 38, 3, 38, 427, 8, 38, 1, 38, 1,
		38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38,
		1, 38, 1, 38, 3, 38, 443, 8, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1,
		38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38,
		1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1,
		38, 1, 38, 1, 38, 1, 38, 1, 38, 1, 38, 5, 38, 477, 8, 38, 10, 38, 12, 38,
		480, 9, 38, 1, 38, 3, 38, 483, 8, 38, 3, 38, 485, 8, 38, 1, 38, 1, 38,
		1, 38, 1, 38, 1, 38, 3, 38, 492, 8, 38, 1, 38, 3, 38, 495, 8, 38, 1, 38,
		1, 38, 1, 38, 1, 38, 3, 38, 501, 8, 38, 1, 38, 1, 38, 1, 38, 1, 38, 1,
		38, 1, 38, 3, 38, 509, 8, 38, 1, 38, 1, 38, 5, 38, 513, 8, 38, 10, 38,
		12, 38, 516, 9, 38, 1, 39, 1, 39, 1, 39, 1, 39, 5, 39, 522, 8, 39, 10,
		39, 12, 39, 525, 9, 39, 3, 39, 527, 8, 39, 1, 39, 3, 39, 530, 8, 39, 1,
		39, 1, 39, 1, 40, 1, 40, 1, 40, 1, 40, 5, 40, 538, 8, 40, 10, 40, 12, 40,
		541, 9, 40, 1, 40, 3, 40, 544, 8, 40, 1, 40, 1, 40, 1, 41, 1, 41, 1, 42,
		1, 42, 1, 42, 0, 1, 76, 43, 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22,
		24, 26, 28, 30, 32, 34, 36, 38, 40, 42, 44, 46, 48, 50, 52, 54, 56, 58,
		60, 62, 64, 66, 68, 70, 72, 74, 76, 78, 80, 82, 84, 0, 14, 1, 0, 73, 74,
		1, 0, 10, 11, 2, 0, 40, 40, 69, 69, 2, 0, 40, 40, 69, 70, 1, 0, 12, 22,
		2, 0, 24, 24, 40, 40, 3, 0, 39, 39, 41, 41, 61, 61, 1, 0, 45, 46, 1, 0,
		54, 57, 1, 0, 51, 52, 1, 0, 49, 50, 2, 0, 47, 47, 62, 62, 4, 0, 63, 63,
		66, 66, 69, 70, 72, 72, 4, 0, 1, 2, 4, 4, 6, 11, 25, 26, 616, 0, 86, 1,
		0, 0, 0, 2, 103, 1, 0, 0, 0, 4, 108, 1, 0, 0, 0, 6, 115, 1, 0, 0, 0, 8,
		131, 1, 0, 0, 0, 10, 138, 1, 0, 0, 0, 12, 140, 1, 0, 0, 0, 14, 145, 1,
		0, 0, 0, 16, 149, 1, 0, 0, 0, 18, 168, 1, 0, 0, 0, 20, 172, 1, 0, 0, 0,
		22, 187, 1, 0, 0, 0, 24, 196, 1, 0, 0, 0, 26, 198, 1, 0, 0, 0, 28, 204,
		1, 0, 0, 0, 30, 211, 1, 0, 0, 0, 32, 228, 1, 0, 0, 0, 34, 232, 1, 0, 0,
		0, 36, 237, 1, 0, 0, 0, 38, 242, 1, 0, 0, 0, 40, 265, 1, 0, 0, 0, 42, 280,
		1, 0, 0, 0, 44, 282, 1, 0, 0, 0, 46, 299, 1, 0, 0, 0, 48, 314, 1, 0, 0,
		0, 50, 316, 1, 0, 0, 0, 52, 330, 1, 0, 0, 0, 54, 344, 1, 0, 0, 0, 56, 346,
		1, 0, 0, 0, 58, 354, 1, 0, 0, 0, 60, 368, 1, 0, 0, 0, 62, 377, 1, 0, 0,
		0, 64, 383, 1, 0, 0, 0, 66, 388, 1, 0, 0, 0, 68, 390, 1, 0, 0, 0, 70, 392,
		1, 0, 0, 0, 72, 403, 1, 0, 0, 0, 74, 406, 1, 0, 0, 0, 76, 442, 1, 0, 0,
		0, 78, 517, 1, 0, 0, 0, 80, 533, 1, 0, 0, 0, 82, 547, 1, 0, 0, 0, 84, 549,
		1, 0, 0, 0, 86, 90, 3, 2, 1, 0, 87, 89, 3, 4, 2, 0, 88, 87, 1, 0, 0, 0,
		89, 92, 1, 0, 0, 0, 90, 88, 1, 0, 0, 0, 90, 91, 1, 0, 0, 0, 91, 97, 1,
		0, 0, 0, 92, 90, 1, 0, 0, 0, 93, 96, 3, 6, 3, 0, 94, 96, 3, 8, 4, 0, 95,
		93, 1, 0, 0, 0, 95, 94, 1, 0, 0, 0, 96, 99, 1, 0, 0, 0, 97, 95, 1, 0, 0,
		0, 97, 98, 1, 0, 0, 0, 98, 100, 1, 0, 0, 0, 99, 97, 1, 0, 0, 0, 100, 101,
		5, 0, 0, 1, 101, 1, 1, 0, 0, 0, 102, 104, 5, 64, 0, 0, 103, 102, 1, 0,
		0, 0, 103, 104, 1, 0, 0, 0, 104, 105, 1, 0, 0, 0, 105, 106, 5, 1, 0, 0,
		106, 107, 5, 63, 0, 0, 107, 3, 1, 0, 0, 0, 108, 109, 5, 2, 0, 0, 109, 112,
		5, 63, 0, 0, 110, 111, 5, 3, 0, 0, 111, 113, 3, 12, 6, 0, 112, 110, 1,
		0, 0, 0, 112, 113, 1, 0, 0, 0, 113, 5, 1, 0, 0, 0, 114, 116, 5, 64, 0,
		0, 115, 114, 1, 0, 0, 0, 115, 116, 1, 0, 0, 0, 116, 119, 1, 0, 0, 0, 117,
		120, 5, 4, 0, 0, 118, 120, 5, 5, 0, 0, 119, 117, 1, 0, 0, 0, 119, 118,
		1, 0, 0, 0, 119, 120, 1, 0, 0, 0, 120, 121, 1, 0, 0, 0, 121, 122, 5, 6,
		0, 0, 122, 124, 3, 10, 5, 0, 123, 125, 3, 16, 8, 0, 124, 123, 1, 0, 0,
		0, 124, 125, 1, 0, 0, 0, 125, 126, 1, 0, 0, 0, 126, 127, 5, 27, 0, 0, 127,
		128, 3, 18, 9, 0, 128, 129, 5, 28, 0, 0, 129, 7, 1, 0, 0, 0, 130, 132,
		5, 64, 0, 0, 131, 130, 1, 0, 0, 0, 131, 132, 1, 0, 0, 0, 132, 133, 1, 0,
		0, 0, 133, 134, 5, 6, 0, 0, 134, 135, 3, 10, 5, 0, 135, 136, 5, 35, 0,
		0, 136, 137, 3, 48, 24, 0, 137, 9, 1, 0, 0, 0, 138, 139, 5, 73, 0, 0, 139,
		11, 1, 0, 0, 0, 140, 141, 7, 0, 0, 0, 141, 13, 1, 0, 0, 0, 142, 143, 3,
		12, 6, 0, 143, 144, 5, 60, 0, 0, 144, 146, 1, 0, 0, 0, 145, 142, 1, 0,
		0, 0, 145, 146, 1, 0, 0, 0, 146, 147, 1, 0, 0, 0, 147, 148, 3, 10, 5, 0,
		148, 15, 1, 0, 0, 0, 149, 150, 5, 7, 0, 0, 150, 155, 3, 14, 7, 0, 151,
		152, 5, 34, 0, 0, 152, 154, 3, 14, 7, 0, 153, 151, 1, 0, 0, 0, 154, 157,
		1, 0, 0, 0, 155, 153, 1, 0, 0, 0, 155, 156, 1, 0, 0, 0, 156, 159, 1, 0,
		0, 0, 157, 155, 1, 0, 0, 0, 158, 160, 5, 34, 0, 0, 159, 158, 1, 0, 0, 0,
		159, 160, 1, 0, 0, 0, 160, 17, 1, 0, 0, 0, 161, 167, 3, 20, 10, 0, 162,
		167, 3, 38, 19, 0, 163, 167, 3, 40, 20, 0, 164, 167, 3, 74, 37, 0, 165,
		167, 3, 28, 14, 0, 166, 161, 1, 0, 0, 0, 166, 162, 1, 0, 0, 0, 166, 163,
		1, 0, 0, 0, 166, 164, 1, 0, 0, 0, 166, 165, 1, 0, 0, 0, 167, 170, 1, 0,
		0, 0, 168, 166, 1, 0, 0, 0, 168, 169, 1, 0, 0, 0, 169, 19, 1, 0, 0, 0,
		170, 168, 1, 0, 0, 0, 171, 173, 5, 64, 0, 0, 172, 171, 1, 0, 0, 0, 172,
		173, 1, 0, 0, 0, 173, 174, 1, 0, 0, 0, 174, 175, 3, 24, 12, 0, 175, 178,
		3, 34, 17, 0, 176, 179, 5, 8, 0, 0, 177, 179, 5, 9, 0, 0, 178, 176, 1,
		0, 0, 0, 178, 177, 1, 0, 0, 0, 178, 179, 1, 0, 0, 0, 179, 183, 1, 0, 0,
		0, 180, 182, 3, 26, 13, 0, 181, 180, 1, 0, 0, 0, 182, 185, 1, 0, 0, 0,
		183, 181, 1, 0, 0, 0, 183, 184, 1, 0, 0, 0, 184, 21, 1, 0, 0, 0, 185, 183,
		1, 0, 0, 0, 186, 188, 5, 64, 0, 0, 187, 186, 1, 0, 0, 0, 187, 188, 1, 0,
		0, 0, 188, 189, 1, 0, 0, 0, 189, 190, 3, 24, 12, 0, 190, 192, 3, 34, 17,
		0, 191, 193, 5, 9, 0, 0, 192, 191, 1, 0, 0, 0, 192, 193, 1, 0, 0, 0, 193,
		23, 1, 0, 0, 0, 194, 197, 5, 74, 0, 0, 195, 197, 3, 84, 42, 0, 196, 194,
		1, 0, 0, 0, 196, 195, 1, 0, 0, 0, 197, 25, 1, 0, 0, 0, 198, 199, 5, 43,
		0, 0, 199, 201, 3, 24, 12, 0, 200, 202, 3, 30, 15, 0, 201, 200, 1, 0, 0,
		0, 201, 202, 1, 0, 0, 0, 202, 27, 1, 0, 0, 0, 203, 205, 5, 64, 0, 0, 204,
		203, 1, 0, 0, 0, 204, 205, 1, 0, 0, 0, 205, 206, 1, 0, 0, 0, 206, 207,
		5, 42, 0, 0, 207, 209, 3, 24, 12, 0, 208, 210, 3, 30, 15, 0, 209, 208,
		1, 0, 0, 0, 209, 210, 1, 0, 0, 0, 210, 29, 1, 0, 0, 0, 211, 212, 5, 31,
		0, 0, 212, 217, 3, 32, 16, 0, 213, 214, 5, 34, 0, 0, 214, 216, 3, 32, 16,
		0, 215, 213, 1, 0, 0, 0, 216, 219, 1, 0, 0, 0, 217, 215, 1, 0, 0, 0, 217,
		218, 1, 0, 0, 0, 218, 221, 1, 0, 0, 0, 219, 217, 1, 0, 0, 0, 220, 222,
		5, 34, 0, 0, 221, 220, 1, 0, 0, 0, 221, 222, 1, 0, 0, 0, 222, 223, 1, 0,
		0, 0, 223, 224, 5, 32, 0, 0, 224, 31, 1, 0, 0, 0, 225, 229, 3, 24, 12,
		0, 226, 229, 5, 73, 0, 0, 227, 229, 3, 82, 41, 0, 228, 225, 1, 0, 0, 0,
		228, 226, 1, 0, 0, 0, 228, 227, 1, 0, 0, 0, 229, 33, 1, 0, 0, 0, 230, 233,
		3, 48, 24, 0, 231, 233, 3, 36, 18, 0, 232, 230, 1, 0, 0, 0, 232, 231, 1,
		0, 0, 0, 233, 35, 1, 0, 0, 0, 234, 235, 3, 12, 6, 0, 235, 236, 5, 60, 0,
		0, 236, 238, 1, 0, 0, 0, 237, 234, 1, 0, 0, 0, 237, 238, 1, 0, 0, 0, 238,
		239, 1, 0, 0, 0, 239, 240, 5, 73, 0, 0, 240, 37, 1, 0, 0, 0, 241, 243,
		5, 64, 0, 0, 242, 241, 1, 0, 0, 0, 242, 243, 1, 0, 0, 0, 243, 244, 1, 0,
		0, 0, 244, 245, 5, 36, 0, 0, 245, 247, 3, 42, 21, 0, 246, 248, 3, 44, 22,
		0, 247, 246, 1, 0, 0, 0, 247, 248, 1, 0, 0, 0, 248, 249, 1, 0, 0, 0, 249,
		255, 3, 14, 7, 0, 250, 251, 5, 39, 0, 0, 251, 253, 3, 42, 21, 0, 252, 254,
		3, 44, 22, 0, 253, 252, 1, 0, 0, 0, 253, 254, 1, 0, 0, 0, 254, 256, 1,
		0, 0, 0, 255, 250, 1, 0, 0, 0, 255, 256, 1, 0, 0, 0, 256, 262, 1, 0, 0,
		0, 257, 259, 5, 27, 0, 0, 258, 260, 3, 46, 23, 0, 259, 258, 1, 0, 0, 0,
		259, 260, 1, 0, 0, 0, 260, 261, 1, 0, 0, 0, 261, 263, 5, 28, 0, 0, 262,
		257, 1, 0, 0, 0, 262, 263, 1, 0, 0, 0, 263, 39, 1, 0, 0, 0, 264, 266, 5,
		64, 0, 0, 265, 264, 1, 0, 0, 0, 265, 266, 1, 0, 0, 0, 266, 267, 1, 0, 0,
		0, 267, 268, 5, 37, 0, 0, 268, 270, 3, 42, 21, 0, 269, 271, 3, 44, 22,
		0, 270, 269, 1, 0, 0, 0, 270, 271, 1, 0, 0, 0, 271, 272, 1, 0, 0, 0, 272,
		278, 3, 14, 7, 0, 273, 274, 5, 39, 0, 0, 274, 276, 3, 42, 21, 0, 275, 277,
		3, 44, 22, 0, 276, 275, 1, 0, 0, 0, 276, 277, 1, 0, 0, 0, 277, 279, 1,
		0, 0, 0, 278, 273, 1, 0, 0, 0, 278, 279, 1, 0, 0, 0, 279, 41, 1, 0, 0,
		0, 280, 281, 7, 0, 0, 0, 281, 43, 1, 0, 0, 0, 282, 294, 5, 31, 0, 0, 283,
		286, 5, 40, 0, 0, 284, 285, 5, 33, 0, 0, 285, 287, 7, 1, 0, 0, 286, 284,
		1, 0, 0, 0, 286, 287, 1, 0, 0, 0, 287, 295, 1, 0, 0, 0, 288, 291, 5, 10,
		0, 0, 289, 290, 5, 33, 0, 0, 290, 292, 7, 1, 0, 0, 291, 289, 1, 0, 0, 0,
		291, 292, 1, 0, 0, 0, 292, 295, 1, 0, 0, 0, 293, 295, 5, 11, 0, 0, 294,
		283, 1, 0, 0, 0, 294, 288, 1, 0, 0, 0, 294, 293, 1, 0, 0, 0, 295, 296,
		1, 0, 0, 0, 296, 297, 5, 32, 0, 0, 297, 45, 1, 0, 0, 0, 298, 300, 3, 22,
		11, 0, 299, 298, 1, 0, 0, 0, 300, 301, 1, 0, 0, 0, 301, 299, 1, 0, 0, 0,
		301, 302, 1, 0, 0, 0, 302, 47, 1, 0, 0, 0, 303, 315, 3, 50, 25, 0, 304,
		315, 3, 52, 26, 0, 305, 315, 3, 54, 27, 0, 306, 315, 3, 56, 28, 0, 307,
		315, 3, 58, 29, 0, 308, 315, 3, 60, 30, 0, 309, 315, 3, 62, 31, 0, 310,
		315, 3, 66, 33, 0, 311, 315, 3, 68, 34, 0, 312, 315, 3, 64, 32, 0, 313,
		315, 3, 70, 35, 0, 314, 303, 1, 0, 0, 0, 314, 304, 1, 0, 0, 0, 314, 305,
		1, 0, 0, 0, 314, 306, 1, 0, 0, 0, 314, 307, 1, 0, 0, 0, 314, 308, 1, 0,
		0, 0, 314, 309, 1, 0, 0, 0, 314, 310, 1, 0, 0, 0, 314, 311, 1, 0, 0, 0,
		314, 312, 1, 0, 0, 0, 314, 313, 1, 0, 0, 0, 315, 49, 1, 0, 0, 0, 316, 328,
		5, 12, 0, 0, 317, 319, 5, 29, 0, 0, 318, 320, 5, 46, 0, 0, 319, 318, 1,
		0, 0, 0, 319, 320, 1, 0, 0, 0, 320, 321, 1, 0, 0, 0, 321, 322, 7, 2, 0,
		0, 322, 324, 5, 34, 0, 0, 323, 325, 5, 46, 0, 0, 324, 323, 1, 0, 0, 0,
		324, 325, 1, 0, 0, 0, 325, 326, 1, 0, 0, 0, 326, 327, 7, 2, 0, 0, 327,
		329, 5, 30, 0, 0, 328, 317, 1, 0, 0, 0, 328, 329, 1, 0, 0, 0, 329, 51,
		1, 0, 0, 0, 330, 342, 5, 13, 0, 0, 331, 333, 5, 29, 0, 0, 332, 334, 5,
		46, 0, 0, 333, 332, 1, 0, 0, 0, 333, 334, 1, 0, 0, 0, 334, 335, 1, 0, 0,
		0, 335, 336, 7, 3, 0, 0, 336, 338, 5, 34, 0, 0, 337, 339, 5, 46, 0, 0,
		338, 337, 1, 0, 0, 0, 338, 339, 1, 0, 0, 0, 339, 340, 1, 0, 0, 0, 340,
		341, 7, 3, 0, 0, 341, 343, 5, 30, 0, 0, 342, 331, 1, 0, 0, 0, 342, 343,
		1, 0, 0, 0, 343, 53, 1, 0, 0, 0, 344, 345, 5, 14, 0, 0, 345, 55, 1, 0,
		0, 0, 346, 352, 5, 15, 0, 0, 347, 348, 5, 29, 0, 0, 348, 349, 7, 2, 0,
		0, 349, 350, 5, 34, 0, 0, 350, 351, 7, 2, 0, 0, 351, 353, 5, 30, 0, 0,
		352, 347, 1, 0, 0, 0, 352, 353, 1, 0, 0, 0, 353, 57, 1, 0, 0, 0, 354, 355,
		5, 16, 0, 0, 355, 356, 5, 29, 0, 0, 356, 359, 5, 63, 0, 0, 357, 358, 5,
		34, 0, 0, 358, 360, 5, 63, 0, 0, 359, 357, 1, 0, 0, 0, 360, 361, 1, 0,
		0, 0, 361, 359, 1, 0, 0, 0, 361, 362, 1, 0, 0, 0, 362, 364, 1, 0, 0, 0,
		363, 365, 5, 34, 0, 0, 364, 363, 1, 0, 0, 0, 364, 365, 1, 0, 0, 0, 365,
		366, 1, 0, 0, 0, 366, 367, 5, 30, 0, 0, 367, 59, 1, 0, 0, 0, 368, 369,
		5, 17, 0, 0, 369, 370, 5, 29, 0, 0, 370, 373, 5, 63, 0, 0, 371, 372, 5,
		34, 0, 0, 372, 374, 5, 63, 0, 0, 373, 371, 1, 0, 0, 0, 373, 374, 1, 0,
		0, 0, 374, 375, 1, 0, 0, 0, 375, 376, 5, 30, 0, 0, 376, 61, 1, 0, 0, 0,
		377, 381, 5, 18, 0, 0, 378, 379, 5, 29, 0, 0, 379, 380, 5, 63, 0, 0, 380,
		382, 5, 30, 0, 0, 381, 378, 1, 0, 0, 0, 381, 382, 1, 0, 0, 0, 382, 63,
		1, 0, 0, 0, 383, 384, 5, 19, 0, 0, 384, 385, 5, 29, 0, 0, 385, 386, 5,
		69, 0, 0, 386, 387, 5, 30, 0, 0, 387, 65, 1, 0, 0, 0, 388, 389, 5, 20,
		0, 0, 389, 67, 1, 0, 0, 0, 390, 391, 5, 21, 0, 0, 391, 69, 1, 0, 0, 0,
		392, 393, 5, 22, 0, 0, 393, 394, 5, 56, 0, 0, 394, 395, 3, 34, 17, 0, 395,
		401, 5, 54, 0, 0, 396, 397, 5, 29, 0, 0, 397, 398, 7, 2, 0, 0, 398, 399,
		5, 34, 0, 0, 399, 400, 7, 2, 0, 0, 400, 402, 5, 30, 0, 0, 401, 396, 1,
		0, 0, 0, 401, 402, 1, 0, 0, 0, 402, 71, 1, 0, 0, 0, 403, 404, 7, 4, 0,
		0, 404, 73, 1, 0, 0, 0, 405, 407, 5, 64, 0, 0, 406, 405, 1, 0, 0, 0, 406,
		407, 1, 0, 0, 0, 407, 408, 1, 0, 0, 0, 408, 409, 5, 44, 0, 0, 409, 410,
		5, 63, 0, 0, 410, 411, 3, 76, 38, 0, 411, 75, 1, 0, 0, 0, 412, 413, 6,
		38, -1, 0, 413, 443, 3, 82, 41, 0, 414, 426, 5, 29, 0, 0, 415, 420, 3,
		76, 38, 0, 416, 417, 5, 34, 0, 0, 417, 419, 3, 76, 38, 0, 418, 416, 1,
		0, 0, 0, 419, 422, 1, 0, 0, 0, 420, 418, 1, 0, 0, 0, 420, 421, 1, 0, 0,
		0, 421, 424, 1, 0, 0, 0, 422, 420, 1, 0, 0, 0, 423, 425, 5, 34, 0, 0, 424,
		423, 1, 0, 0, 0, 424, 425, 1, 0, 0, 0, 425, 427, 1, 0, 0, 0, 426, 415,
		1, 0, 0, 0, 426, 427, 1, 0, 0, 0, 427, 428, 1, 0, 0, 0, 428, 443, 5, 30,
		0, 0, 429, 430, 5, 46, 0, 0, 430, 443, 3, 76, 38, 20, 431, 432, 5, 44,
		0, 0, 432, 443, 3, 76, 38, 16, 433, 434, 5, 31, 0, 0, 434, 435, 3, 76,
		38, 0, 435, 436, 5, 32, 0, 0, 436, 443, 1, 0, 0, 0, 437, 443, 5, 68, 0,
		0, 438, 443, 3, 24, 12, 0, 439, 443, 3, 72, 36, 0, 440, 443, 5, 73, 0,
		0, 441, 443, 7, 5, 0, 0, 442, 412, 1, 0, 0, 0, 442, 414, 1, 0, 0, 0, 442,
		429, 1, 0, 0, 0, 442, 431, 1, 0, 0, 0, 442, 433, 1, 0, 0, 0, 442, 437,
		1, 0, 0, 0, 442, 438, 1, 0, 0, 0, 442, 439, 1, 0, 0, 0, 442, 440, 1, 0,
		0, 0, 442, 441, 1, 0, 0, 0, 443, 514, 1, 0, 0, 0, 444, 445, 10, 17, 0,
		0, 445, 446, 5, 60, 0, 0, 446, 513, 3, 76, 38, 18, 447, 448, 10, 15, 0,
		0, 448, 449, 7, 6, 0, 0, 449, 513, 3, 76, 38, 16, 450, 451, 10, 14, 0,
		0, 451, 452, 7, 7, 0, 0, 452, 513, 3, 76, 38, 15, 453, 454, 10, 13, 0,
		0, 454, 455, 7, 8, 0, 0, 455, 513, 3, 76, 38, 14, 456, 457, 10, 12, 0,
		0, 457, 458, 5, 23, 0, 0, 458, 513, 3, 76, 38, 13, 459, 460, 10, 11, 0,
		0, 460, 461, 7, 9, 0, 0, 461, 513, 3, 76, 38, 12, 462, 463, 10, 10, 0,
		0, 463, 464, 7, 10, 0, 0, 464, 513, 3, 76, 38, 11, 465, 466, 10, 9, 0,
		0, 466, 467, 5, 48, 0, 0, 467, 513, 3, 76, 38, 10, 468, 469, 10, 8, 0,
		0, 469, 470, 7, 11, 0, 0, 470, 513, 3, 76, 38, 9, 471, 472, 10, 19, 0,
		0, 472, 484, 5, 29, 0, 0, 473, 478, 3, 76, 38, 0, 474, 475, 5, 34, 0, 0,
		475, 477, 3, 76, 38, 0, 476, 474, 1, 0, 0, 0, 477, 480, 1, 0, 0, 0, 478,
		476, 1, 0, 0, 0, 478, 479, 1, 0, 0, 0, 479, 482, 1, 0, 0, 0, 480, 478,
		1, 0, 0, 0, 481, 483, 5, 34, 0, 0, 482, 481, 1, 0, 0, 0, 482, 483, 1, 0,
		0, 0, 483, 485, 1, 0, 0, 0, 484, 473, 1, 0, 0, 0, 484, 485, 1, 0, 0, 0,
		485, 486, 1, 0, 0, 0, 486, 513, 5, 30, 0, 0, 487, 488, 10, 18, 0, 0, 488,
		489, 5, 38, 0, 0, 489, 491, 7, 0, 0, 0, 490, 492, 3, 78, 39, 0, 491, 490,
		1, 0, 0, 0, 491, 492, 1, 0, 0, 0, 492, 494, 1, 0, 0, 0, 493, 495, 3, 80,
		40, 0, 494, 493, 1, 0, 0, 0, 494, 495, 1, 0, 0, 0, 495, 500, 1, 0, 0, 0,
		496, 497, 5, 27, 0, 0, 497, 498, 3, 76, 38, 0, 498, 499, 5, 28, 0, 0, 499,
		501, 1, 0, 0, 0, 500, 496, 1, 0, 0, 0, 500, 501, 1, 0, 0, 0, 501, 513,
		1, 0, 0, 0, 502, 503, 10, 7, 0, 0, 503, 504, 5, 53, 0, 0, 504, 505, 5,
		27, 0, 0, 505, 508, 3, 76, 38, 0, 506, 507, 5, 33, 0, 0, 507, 509, 3, 76,
		38, 0, 508, 506, 1, 0, 0, 0, 508, 509, 1, 0, 0, 0, 509, 510, 1, 0, 0, 0,
		510, 511, 5, 28, 0, 0, 511, 513, 1, 0, 0, 0, 512, 444, 1, 0, 0, 0, 512,
		447, 1, 0, 0, 0, 512, 450, 1, 0, 0, 0, 512, 453, 1, 0, 0, 0, 512, 456,
		1, 0, 0, 0, 512, 459, 1, 0, 0, 0, 512, 462, 1, 0, 0, 0, 512, 465, 1, 0,
		0, 0, 512, 468, 1, 0, 0, 0, 512, 471, 1, 0, 0, 0, 512, 487, 1, 0, 0, 0,
		512, 502, 1, 0, 0, 0, 513, 516, 1, 0, 0, 0, 514, 512, 1, 0, 0, 0, 514,
		515, 1, 0, 0, 0, 515, 77, 1, 0, 0, 0, 516, 514, 1, 0, 0, 0, 517, 526, 5,
		31, 0, 0, 518, 523, 3, 76, 38, 0, 519, 520, 5, 34, 0, 0, 520, 522, 3, 76,
		38, 0, 521, 519, 1, 0, 0, 0, 522, 525, 1, 0, 0, 0, 523, 521, 1, 0, 0, 0,
		523, 524, 1, 0, 0, 0, 524, 527, 1, 0, 0, 0, 525, 523, 1, 0, 0, 0, 526,
		518, 1, 0, 0, 0, 526, 527, 1, 0, 0, 0, 527, 529, 1, 0, 0, 0, 528, 530,
		5, 34, 0, 0, 529, 528, 1, 0, 0, 0, 529, 530, 1, 0, 0, 0, 530, 531, 1, 0,
		0, 0, 531, 532, 5, 32, 0, 0, 532, 79, 1, 0, 0, 0, 533, 534, 5, 59, 0, 0,
		534, 539, 5, 68, 0, 0, 535, 536, 5, 34, 0, 0, 536, 538, 5, 68, 0, 0, 537,
		535, 1, 0, 0, 0, 538, 541, 1, 0, 0, 0, 539, 537, 1, 0, 0, 0, 539, 540,
		1, 0, 0, 0, 540, 543, 1, 0, 0, 0, 541, 539, 1, 0, 0, 0, 542, 544, 5, 34,
		0, 0, 543, 542, 1, 0, 0, 0, 543, 544, 1, 0, 0, 0, 544, 545, 1, 0, 0, 0,
		545, 546, 5, 59, 0, 0, 546, 81, 1, 0, 0, 0, 547, 548, 7, 12, 0, 0, 548,
		83, 1, 0, 0, 0, 549, 550, 7, 13, 0, 0, 550, 85, 1, 0, 0, 0, 74, 90, 95,
		97, 103, 112, 115, 119, 124, 131, 145, 155, 159, 166, 168, 172, 178, 183,
		187, 192, 196, 201, 204, 209, 217, 221, 228, 232, 237, 242, 247, 253, 255,
		259, 262, 265, 270, 276, 278, 286, 291, 294, 301, 314, 319, 324, 328, 333,
		338, 342, 352, 361, 364, 373, 381, 401, 406, 420, 424, 426, 442, 478, 482,
		484, 491, 494, 500, 508, 512, 514, 523, 526, 529, 539, 543,
	}
	deserializer := antlr.NewATNDeserializer(nil)
	staticData.atn = deserializer.Deserialize(staticData.serializedATN)
	atn := staticData.atn
	staticData.decisionToDFA = make([]*antlr.DFA, len(atn.DecisionToState))
	decisionToDFA := staticData.decisionToDFA
	for index, state := range atn.DecisionToState {
		decisionToDFA[index] = antlr.NewDFA(state, index)
	}
}

// YammmGrammarParserInit initializes any static state used to implement YammmGrammarParser. By default the
// static state used to implement the parser is lazily initialized during the first call to
// NewYammmGrammarParser(). You can call this function if you wish to initialize the static state ahead
// of time.
func YammmGrammarParserInit() {
	staticData := &YammmGrammarParserStaticData
	staticData.once.Do(yammmgrammarParserInit)
}

// NewYammmGrammarParser produces a new parser instance for the optional input antlr.TokenStream.
func NewYammmGrammarParser(input antlr.TokenStream) *YammmGrammarParser {
	YammmGrammarParserInit()
	this := new(YammmGrammarParser)
	this.BaseParser = antlr.NewBaseParser(input)
	staticData := &YammmGrammarParserStaticData
	this.Interpreter = antlr.NewParserATNSimulator(this, staticData.atn, staticData.decisionToDFA, staticData.PredictionContextCache)
	this.RuleNames = staticData.RuleNames
	this.LiteralNames = staticData.LiteralNames
	this.SymbolicNames = staticData.SymbolicNames
	this.GrammarFileName = "YammmGrammar.g4"

	return this
}

// YammmGrammarParser tokens.
const (
	YammmGrammarParserEOF            = antlr.TokenEOF
	YammmGrammarParserT__0           = 1
	YammmGrammarParserT__1           = 2
	YammmGrammarParserT__2           = 3
	YammmGrammarParserT__3           = 4
	YammmGrammarParserT__4           = 5
	YammmGrammarParserT__5           = 6
	YammmGrammarParserT__6           = 7
	YammmGrammarParserT__7           = 8
	YammmGrammarParserT__8           = 9
	YammmGrammarParserT__9           = 10
	YammmGrammarParserT__10          = 11
	YammmGrammarParserT__11          = 12
	YammmGrammarParserT__12          = 13
	YammmGrammarParserT__13          = 14
	YammmGrammarParserT__14          = 15
	YammmGrammarParserT__15          = 16
	YammmGrammarParserT__16          = 17
	YammmGrammarParserT__17          = 18
	YammmGrammarParserT__18          = 19
	YammmGrammarParserT__19          = 20
	YammmGrammarParserT__20          = 21
	YammmGrammarParserT__21          = 22
	YammmGrammarParserT__22          = 23
	YammmGrammarParserT__23          = 24
	YammmGrammarParserT__24          = 25
	YammmGrammarParserT__25          = 26
	YammmGrammarParserLBRACE         = 27
	YammmGrammarParserRBRACE         = 28
	YammmGrammarParserLBRACK         = 29
	YammmGrammarParserRBRACK         = 30
	YammmGrammarParserLPAR           = 31
	YammmGrammarParserRPAR           = 32
	YammmGrammarParserCOLON          = 33
	YammmGrammarParserCOMMA          = 34
	YammmGrammarParserEQUALS         = 35
	YammmGrammarParserASSOC          = 36
	YammmGrammarParserCOMP           = 37
	YammmGrammarParserARROW          = 38
	YammmGrammarParserSLASH          = 39
	YammmGrammarParserUSCORE         = 40
	YammmGrammarParserSTAR           = 41
	YammmGrammarParserATAT           = 42
	YammmGrammarParserAT             = 43
	YammmGrammarParserEXCLAMATION    = 44
	YammmGrammarParserPLUS           = 45
	YammmGrammarParserMINUS          = 46
	YammmGrammarParserOR             = 47
	YammmGrammarParserAND            = 48
	YammmGrammarParserEQUAL          = 49
	YammmGrammarParserNOTEQUAL       = 50
	YammmGrammarParserMATCH          = 51
	YammmGrammarParserNOTMATCH       = 52
	YammmGrammarParserQMARK          = 53
	YammmGrammarParserGT             = 54
	YammmGrammarParserGTE            = 55
	YammmGrammarParserLT             = 56
	YammmGrammarParserLTE            = 57
	YammmGrammarParserDOLLAR         = 58
	YammmGrammarParserPIPE           = 59
	YammmGrammarParserPERIOD         = 60
	YammmGrammarParserPERCENT        = 61
	YammmGrammarParserHAT            = 62
	YammmGrammarParserSTRING         = 63
	YammmGrammarParserDOC_COMMENT    = 64
	YammmGrammarParserSL_COMMENT     = 65
	YammmGrammarParserREGEXP         = 66
	YammmGrammarParserWS             = 67
	YammmGrammarParserVARIABLE       = 68
	YammmGrammarParserINTEGER        = 69
	YammmGrammarParserFLOAT          = 70
	YammmGrammarParserINVALID_NUMBER = 71
	YammmGrammarParserBOOLEAN        = 72
	YammmGrammarParserUC_WORD        = 73
	YammmGrammarParserLC_WORD        = 74
	YammmGrammarParserANY_OTHER      = 75
)

// YammmGrammarParser rules.
const (
	YammmGrammarParserRULE_schema          = 0
	YammmGrammarParserRULE_schema_name     = 1
	YammmGrammarParserRULE_import_decl     = 2
	YammmGrammarParserRULE_type            = 3
	YammmGrammarParserRULE_datatype        = 4
	YammmGrammarParserRULE_type_name       = 5
	YammmGrammarParserRULE_alias_name      = 6
	YammmGrammarParserRULE_type_ref        = 7
	YammmGrammarParserRULE_extends_types   = 8
	YammmGrammarParserRULE_type_body       = 9
	YammmGrammarParserRULE_property        = 10
	YammmGrammarParserRULE_rel_property    = 11
	YammmGrammarParserRULE_property_name   = 12
	YammmGrammarParserRULE_annotation      = 13
	YammmGrammarParserRULE_type_annotation = 14
	YammmGrammarParserRULE_annotation_args = 15
	YammmGrammarParserRULE_annotation_arg  = 16
	YammmGrammarParserRULE_data_type_ref   = 17
	YammmGrammarParserRULE_qualified_alias = 18
	YammmGrammarParserRULE_association     = 19
	YammmGrammarParserRULE_composition     = 20
	YammmGrammarParserRULE_any_name        = 21
	YammmGrammarParserRULE_multiplicity    = 22
	YammmGrammarParserRULE_relation_body   = 23
	YammmGrammarParserRULE_built_in        = 24
	YammmGrammarParserRULE_integerT        = 25
	YammmGrammarParserRULE_floatT          = 26
	YammmGrammarParserRULE_boolT           = 27
	YammmGrammarParserRULE_stringT         = 28
	YammmGrammarParserRULE_enumT           = 29
	YammmGrammarParserRULE_patternT        = 30
	YammmGrammarParserRULE_timestampT      = 31
	YammmGrammarParserRULE_vectorT         = 32
	YammmGrammarParserRULE_dateT           = 33
	YammmGrammarParserRULE_uuidT           = 34
	YammmGrammarParserRULE_listT           = 35
	YammmGrammarParserRULE_datatypeKeyword = 36
	YammmGrammarParserRULE_invariant       = 37
	YammmGrammarParserRULE_expr            = 38
	YammmGrammarParserRULE_arguments       = 39
	YammmGrammarParserRULE_parameters      = 40
	YammmGrammarParserRULE_literal         = 41
	YammmGrammarParserRULE_lc_keyword      = 42
)

// ISchemaContext is an interface to support dynamic dispatch.
type ISchemaContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Schema_name() ISchema_nameContext
	EOF() antlr.TerminalNode
	AllImport_decl() []IImport_declContext
	Import_decl(i int) IImport_declContext
	AllType_() []ITypeContext
	Type_(i int) ITypeContext
	AllDatatype() []IDatatypeContext
	Datatype(i int) IDatatypeContext

	// IsSchemaContext differentiates from other interfaces.
	IsSchemaContext()
}

type SchemaContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptySchemaContext() *SchemaContext {
	p := new(SchemaContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_schema
	return p
}

func InitEmptySchemaContext(p *SchemaContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_schema
}

func (*SchemaContext) IsSchemaContext() {}

func NewSchemaContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SchemaContext {
	p := new(SchemaContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_schema

	return p
}

func (s *SchemaContext) GetParser() antlr.Parser { return s.parser }

func (s *SchemaContext) Schema_name() ISchema_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ISchema_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ISchema_nameContext)
}

func (s *SchemaContext) EOF() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserEOF, 0)
}

func (s *SchemaContext) AllImport_decl() []IImport_declContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IImport_declContext); ok {
			len++
		}
	}

	tst := make([]IImport_declContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IImport_declContext); ok {
			tst[i] = t.(IImport_declContext)
			i++
		}
	}

	return tst
}

func (s *SchemaContext) Import_decl(i int) IImport_declContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IImport_declContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IImport_declContext)
}

func (s *SchemaContext) AllType_() []ITypeContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ITypeContext); ok {
			len++
		}
	}

	tst := make([]ITypeContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ITypeContext); ok {
			tst[i] = t.(ITypeContext)
			i++
		}
	}

	return tst
}

func (s *SchemaContext) Type_(i int) ITypeContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ITypeContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ITypeContext)
}

func (s *SchemaContext) AllDatatype() []IDatatypeContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IDatatypeContext); ok {
			len++
		}
	}

	tst := make([]IDatatypeContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IDatatypeContext); ok {
			tst[i] = t.(IDatatypeContext)
			i++
		}
	}

	return tst
}

func (s *SchemaContext) Datatype(i int) IDatatypeContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IDatatypeContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IDatatypeContext)
}

func (s *SchemaContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *SchemaContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *SchemaContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterSchema(s)
	}
}

func (s *SchemaContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitSchema(s)
	}
}

func (s *SchemaContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitSchema(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Schema() (localctx ISchemaContext) {
	localctx = NewSchemaContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 0, YammmGrammarParserRULE_schema)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(86)
		p.Schema_name()
	}
	p.SetState(90)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == YammmGrammarParserT__1 {
		{
			p.SetState(87)
			p.Import_decl()
		}

		p.SetState(92)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	p.SetState(97)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for (int64((_la-4)) & ^0x3f) == 0 && ((int64(1)<<(_la-4))&1152921504606846983) != 0 {
		p.SetState(95)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}

		switch p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 1, p.GetParserRuleContext()) {
		case 1:
			{
				p.SetState(93)
				p.Type_()
			}

		case 2:
			{
				p.SetState(94)
				p.Datatype()
			}

		case antlr.ATNInvalidAltNumber:
			goto errorExit
		}

		p.SetState(99)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(100)
		p.Match(YammmGrammarParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ISchema_nameContext is an interface to support dynamic dispatch.
type ISchema_nameContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	STRING() antlr.TerminalNode
	DOC_COMMENT() antlr.TerminalNode

	// IsSchema_nameContext differentiates from other interfaces.
	IsSchema_nameContext()
}

type Schema_nameContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptySchema_nameContext() *Schema_nameContext {
	p := new(Schema_nameContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_schema_name
	return p
}

func InitEmptySchema_nameContext(p *Schema_nameContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_schema_name
}

func (*Schema_nameContext) IsSchema_nameContext() {}

func NewSchema_nameContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Schema_nameContext {
	p := new(Schema_nameContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_schema_name

	return p
}

func (s *Schema_nameContext) GetParser() antlr.Parser { return s.parser }

func (s *Schema_nameContext) STRING() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, 0)
}

func (s *Schema_nameContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *Schema_nameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Schema_nameContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Schema_nameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterSchema_name(s)
	}
}

func (s *Schema_nameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitSchema_name(s)
	}
}

func (s *Schema_nameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitSchema_name(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Schema_name() (localctx ISchema_nameContext) {
	localctx = NewSchema_nameContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 2, YammmGrammarParserRULE_schema_name)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(103)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(102)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(105)
		p.Match(YammmGrammarParserT__0)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(106)
		p.Match(YammmGrammarParserSTRING)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IImport_declContext is an interface to support dynamic dispatch.
type IImport_declContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetPath returns the path token.
	GetPath() antlr.Token

	// SetPath sets the path token.
	SetPath(antlr.Token)

	// GetAlias returns the alias rule contexts.
	GetAlias() IAlias_nameContext

	// SetAlias sets the alias rule contexts.
	SetAlias(IAlias_nameContext)

	// Getter signatures
	STRING() antlr.TerminalNode
	Alias_name() IAlias_nameContext

	// IsImport_declContext differentiates from other interfaces.
	IsImport_declContext()
}

type Import_declContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	path   antlr.Token
	alias  IAlias_nameContext
}

func NewEmptyImport_declContext() *Import_declContext {
	p := new(Import_declContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_import_decl
	return p
}

func InitEmptyImport_declContext(p *Import_declContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_import_decl
}

func (*Import_declContext) IsImport_declContext() {}

func NewImport_declContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Import_declContext {
	p := new(Import_declContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_import_decl

	return p
}

func (s *Import_declContext) GetParser() antlr.Parser { return s.parser }

func (s *Import_declContext) GetPath() antlr.Token { return s.path }

func (s *Import_declContext) SetPath(v antlr.Token) { s.path = v }

func (s *Import_declContext) GetAlias() IAlias_nameContext { return s.alias }

func (s *Import_declContext) SetAlias(v IAlias_nameContext) { s.alias = v }

func (s *Import_declContext) STRING() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, 0)
}

func (s *Import_declContext) Alias_name() IAlias_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAlias_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAlias_nameContext)
}

func (s *Import_declContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Import_declContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Import_declContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterImport_decl(s)
	}
}

func (s *Import_declContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitImport_decl(s)
	}
}

func (s *Import_declContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitImport_decl(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Import_decl() (localctx IImport_declContext) {
	localctx = NewImport_declContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 4, YammmGrammarParserRULE_import_decl)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(108)
		p.Match(YammmGrammarParserT__1)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(109)

		_m := p.Match(YammmGrammarParserSTRING)

		localctx.(*Import_declContext).path = _m
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(112)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserT__2 {
		{
			p.SetState(110)
			p.Match(YammmGrammarParserT__2)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(111)

			_x := p.Alias_name()

			localctx.(*Import_declContext).alias = _x
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ITypeContext is an interface to support dynamic dispatch.
type ITypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetIs_abstract returns the is_abstract token.
	GetIs_abstract() antlr.Token

	// GetIs_part returns the is_part token.
	GetIs_part() antlr.Token

	// SetIs_abstract sets the is_abstract token.
	SetIs_abstract(antlr.Token)

	// SetIs_part sets the is_part token.
	SetIs_part(antlr.Token)

	// Getter signatures
	Type_name() IType_nameContext
	LBRACE() antlr.TerminalNode
	Type_body() IType_bodyContext
	RBRACE() antlr.TerminalNode
	DOC_COMMENT() antlr.TerminalNode
	Extends_types() IExtends_typesContext

	// IsTypeContext differentiates from other interfaces.
	IsTypeContext()
}

type TypeContext struct {
	antlr.BaseParserRuleContext
	parser      antlr.Parser
	is_abstract antlr.Token
	is_part     antlr.Token
}

func NewEmptyTypeContext() *TypeContext {
	p := new(TypeContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type
	return p
}

func InitEmptyTypeContext(p *TypeContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type
}

func (*TypeContext) IsTypeContext() {}

func NewTypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *TypeContext {
	p := new(TypeContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_type

	return p
}

func (s *TypeContext) GetParser() antlr.Parser { return s.parser }

func (s *TypeContext) GetIs_abstract() antlr.Token { return s.is_abstract }

func (s *TypeContext) GetIs_part() antlr.Token { return s.is_part }

func (s *TypeContext) SetIs_abstract(v antlr.Token) { s.is_abstract = v }

func (s *TypeContext) SetIs_part(v antlr.Token) { s.is_part = v }

func (s *TypeContext) Type_name() IType_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_nameContext)
}

func (s *TypeContext) LBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACE, 0)
}

func (s *TypeContext) Type_body() IType_bodyContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_bodyContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_bodyContext)
}

func (s *TypeContext) RBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACE, 0)
}

func (s *TypeContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *TypeContext) Extends_types() IExtends_typesContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExtends_typesContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExtends_typesContext)
}

func (s *TypeContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *TypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *TypeContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterType(s)
	}
}

func (s *TypeContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitType(s)
	}
}

func (s *TypeContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitType(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Type_() (localctx ITypeContext) {
	localctx = NewTypeContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 6, YammmGrammarParserRULE_type)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(115)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(114)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	p.SetState(119)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserT__3:
		{
			p.SetState(117)

			_m := p.Match(YammmGrammarParserT__3)

			localctx.(*TypeContext).is_abstract = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserT__4:
		{
			p.SetState(118)

			_m := p.Match(YammmGrammarParserT__4)

			localctx.(*TypeContext).is_part = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserT__5:

	default:
	}
	{
		p.SetState(121)
		p.Match(YammmGrammarParserT__5)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(122)
		p.Type_name()
	}
	p.SetState(124)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserT__6 {
		{
			p.SetState(123)
			p.Extends_types()
		}
	}
	{
		p.SetState(126)
		p.Match(YammmGrammarParserLBRACE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(127)
		p.Type_body()
	}
	{
		p.SetState(128)
		p.Match(YammmGrammarParserRBRACE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IDatatypeContext is an interface to support dynamic dispatch.
type IDatatypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Type_name() IType_nameContext
	EQUALS() antlr.TerminalNode
	Built_in() IBuilt_inContext
	DOC_COMMENT() antlr.TerminalNode

	// IsDatatypeContext differentiates from other interfaces.
	IsDatatypeContext()
}

type DatatypeContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyDatatypeContext() *DatatypeContext {
	p := new(DatatypeContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_datatype
	return p
}

func InitEmptyDatatypeContext(p *DatatypeContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_datatype
}

func (*DatatypeContext) IsDatatypeContext() {}

func NewDatatypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *DatatypeContext {
	p := new(DatatypeContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_datatype

	return p
}

func (s *DatatypeContext) GetParser() antlr.Parser { return s.parser }

func (s *DatatypeContext) Type_name() IType_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_nameContext)
}

func (s *DatatypeContext) EQUALS() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserEQUALS, 0)
}

func (s *DatatypeContext) Built_in() IBuilt_inContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBuilt_inContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBuilt_inContext)
}

func (s *DatatypeContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *DatatypeContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *DatatypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *DatatypeContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterDatatype(s)
	}
}

func (s *DatatypeContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitDatatype(s)
	}
}

func (s *DatatypeContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitDatatype(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Datatype() (localctx IDatatypeContext) {
	localctx = NewDatatypeContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 8, YammmGrammarParserRULE_datatype)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(131)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(130)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(133)
		p.Match(YammmGrammarParserT__5)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(134)
		p.Type_name()
	}
	{
		p.SetState(135)
		p.Match(YammmGrammarParserEQUALS)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(136)
		p.Built_in()
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IType_nameContext is an interface to support dynamic dispatch.
type IType_nameContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UC_WORD() antlr.TerminalNode

	// IsType_nameContext differentiates from other interfaces.
	IsType_nameContext()
}

type Type_nameContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyType_nameContext() *Type_nameContext {
	p := new(Type_nameContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_name
	return p
}

func InitEmptyType_nameContext(p *Type_nameContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_name
}

func (*Type_nameContext) IsType_nameContext() {}

func NewType_nameContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Type_nameContext {
	p := new(Type_nameContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_type_name

	return p
}

func (s *Type_nameContext) GetParser() antlr.Parser { return s.parser }

func (s *Type_nameContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *Type_nameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Type_nameContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Type_nameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterType_name(s)
	}
}

func (s *Type_nameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitType_name(s)
	}
}

func (s *Type_nameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitType_name(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Type_name() (localctx IType_nameContext) {
	localctx = NewType_nameContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 10, YammmGrammarParserRULE_type_name)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(138)
		p.Match(YammmGrammarParserUC_WORD)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAlias_nameContext is an interface to support dynamic dispatch.
type IAlias_nameContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UC_WORD() antlr.TerminalNode
	LC_WORD() antlr.TerminalNode

	// IsAlias_nameContext differentiates from other interfaces.
	IsAlias_nameContext()
}

type Alias_nameContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyAlias_nameContext() *Alias_nameContext {
	p := new(Alias_nameContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_alias_name
	return p
}

func InitEmptyAlias_nameContext(p *Alias_nameContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_alias_name
}

func (*Alias_nameContext) IsAlias_nameContext() {}

func NewAlias_nameContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Alias_nameContext {
	p := new(Alias_nameContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_alias_name

	return p
}

func (s *Alias_nameContext) GetParser() antlr.Parser { return s.parser }

func (s *Alias_nameContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *Alias_nameContext) LC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLC_WORD, 0)
}

func (s *Alias_nameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Alias_nameContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Alias_nameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAlias_name(s)
	}
}

func (s *Alias_nameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAlias_name(s)
	}
}

func (s *Alias_nameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAlias_name(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Alias_name() (localctx IAlias_nameContext) {
	localctx = NewAlias_nameContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 12, YammmGrammarParserRULE_alias_name)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(140)
		_la = p.GetTokenStream().LA(1)

		if !(_la == YammmGrammarParserUC_WORD || _la == YammmGrammarParserLC_WORD) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IType_refContext is an interface to support dynamic dispatch.
type IType_refContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetQualifier returns the qualifier rule contexts.
	GetQualifier() IAlias_nameContext

	// GetName returns the name rule contexts.
	GetName() IType_nameContext

	// SetQualifier sets the qualifier rule contexts.
	SetQualifier(IAlias_nameContext)

	// SetName sets the name rule contexts.
	SetName(IType_nameContext)

	// Getter signatures
	Type_name() IType_nameContext
	PERIOD() antlr.TerminalNode
	Alias_name() IAlias_nameContext

	// IsType_refContext differentiates from other interfaces.
	IsType_refContext()
}

type Type_refContext struct {
	antlr.BaseParserRuleContext
	parser    antlr.Parser
	qualifier IAlias_nameContext
	name      IType_nameContext
}

func NewEmptyType_refContext() *Type_refContext {
	p := new(Type_refContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_ref
	return p
}

func InitEmptyType_refContext(p *Type_refContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_ref
}

func (*Type_refContext) IsType_refContext() {}

func NewType_refContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Type_refContext {
	p := new(Type_refContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_type_ref

	return p
}

func (s *Type_refContext) GetParser() antlr.Parser { return s.parser }

func (s *Type_refContext) GetQualifier() IAlias_nameContext { return s.qualifier }

func (s *Type_refContext) GetName() IType_nameContext { return s.name }

func (s *Type_refContext) SetQualifier(v IAlias_nameContext) { s.qualifier = v }

func (s *Type_refContext) SetName(v IType_nameContext) { s.name = v }

func (s *Type_refContext) Type_name() IType_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_nameContext)
}

func (s *Type_refContext) PERIOD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserPERIOD, 0)
}

func (s *Type_refContext) Alias_name() IAlias_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAlias_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAlias_nameContext)
}

func (s *Type_refContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Type_refContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Type_refContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterType_ref(s)
	}
}

func (s *Type_refContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitType_ref(s)
	}
}

func (s *Type_refContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitType_ref(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Type_ref() (localctx IType_refContext) {
	localctx = NewType_refContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 14, YammmGrammarParserRULE_type_ref)
	p.EnterOuterAlt(localctx, 1)
	p.SetState(145)
	p.GetErrorHandler().Sync(p)

	if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 9, p.GetParserRuleContext()) == 1 {
		{
			p.SetState(142)

			_x := p.Alias_name()

			localctx.(*Type_refContext).qualifier = _x
		}
		{
			p.SetState(143)
			p.Match(YammmGrammarParserPERIOD)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	} else if p.HasError() { // JIM
		goto errorExit
	}
	{
		p.SetState(147)

		_x := p.Type_name()

		localctx.(*Type_refContext).name = _x
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IExtends_typesContext is an interface to support dynamic dispatch.
type IExtends_typesContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllType_ref() []IType_refContext
	Type_ref(i int) IType_refContext
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode

	// IsExtends_typesContext differentiates from other interfaces.
	IsExtends_typesContext()
}

type Extends_typesContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyExtends_typesContext() *Extends_typesContext {
	p := new(Extends_typesContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_extends_types
	return p
}

func InitEmptyExtends_typesContext(p *Extends_typesContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_extends_types
}

func (*Extends_typesContext) IsExtends_typesContext() {}

func NewExtends_typesContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Extends_typesContext {
	p := new(Extends_typesContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_extends_types

	return p
}

func (s *Extends_typesContext) GetParser() antlr.Parser { return s.parser }

func (s *Extends_typesContext) AllType_ref() []IType_refContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IType_refContext); ok {
			len++
		}
	}

	tst := make([]IType_refContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IType_refContext); ok {
			tst[i] = t.(IType_refContext)
			i++
		}
	}

	return tst
}

func (s *Extends_typesContext) Type_ref(i int) IType_refContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_refContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_refContext)
}

func (s *Extends_typesContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *Extends_typesContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *Extends_typesContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Extends_typesContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Extends_typesContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterExtends_types(s)
	}
}

func (s *Extends_typesContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitExtends_types(s)
	}
}

func (s *Extends_typesContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitExtends_types(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Extends_types() (localctx IExtends_typesContext) {
	localctx = NewExtends_typesContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 16, YammmGrammarParserRULE_extends_types)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(149)
		p.Match(YammmGrammarParserT__6)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(150)
		p.Type_ref()
	}
	p.SetState(155)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 10, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			{
				p.SetState(151)
				p.Match(YammmGrammarParserCOMMA)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(152)
				p.Type_ref()
			}

		}
		p.SetState(157)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 10, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}
	p.SetState(159)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserCOMMA {
		{
			p.SetState(158)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IType_bodyContext is an interface to support dynamic dispatch.
type IType_bodyContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllProperty() []IPropertyContext
	Property(i int) IPropertyContext
	AllAssociation() []IAssociationContext
	Association(i int) IAssociationContext
	AllComposition() []ICompositionContext
	Composition(i int) ICompositionContext
	AllInvariant() []IInvariantContext
	Invariant(i int) IInvariantContext
	AllType_annotation() []IType_annotationContext
	Type_annotation(i int) IType_annotationContext

	// IsType_bodyContext differentiates from other interfaces.
	IsType_bodyContext()
}

type Type_bodyContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyType_bodyContext() *Type_bodyContext {
	p := new(Type_bodyContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_body
	return p
}

func InitEmptyType_bodyContext(p *Type_bodyContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_body
}

func (*Type_bodyContext) IsType_bodyContext() {}

func NewType_bodyContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Type_bodyContext {
	p := new(Type_bodyContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_type_body

	return p
}

func (s *Type_bodyContext) GetParser() antlr.Parser { return s.parser }

func (s *Type_bodyContext) AllProperty() []IPropertyContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IPropertyContext); ok {
			len++
		}
	}

	tst := make([]IPropertyContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IPropertyContext); ok {
			tst[i] = t.(IPropertyContext)
			i++
		}
	}

	return tst
}

func (s *Type_bodyContext) Property(i int) IPropertyContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IPropertyContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IPropertyContext)
}

func (s *Type_bodyContext) AllAssociation() []IAssociationContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IAssociationContext); ok {
			len++
		}
	}

	tst := make([]IAssociationContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IAssociationContext); ok {
			tst[i] = t.(IAssociationContext)
			i++
		}
	}

	return tst
}

func (s *Type_bodyContext) Association(i int) IAssociationContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAssociationContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAssociationContext)
}

func (s *Type_bodyContext) AllComposition() []ICompositionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ICompositionContext); ok {
			len++
		}
	}

	tst := make([]ICompositionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ICompositionContext); ok {
			tst[i] = t.(ICompositionContext)
			i++
		}
	}

	return tst
}

func (s *Type_bodyContext) Composition(i int) ICompositionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICompositionContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICompositionContext)
}

func (s *Type_bodyContext) AllInvariant() []IInvariantContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IInvariantContext); ok {
			len++
		}
	}

	tst := make([]IInvariantContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IInvariantContext); ok {
			tst[i] = t.(IInvariantContext)
			i++
		}
	}

	return tst
}

func (s *Type_bodyContext) Invariant(i int) IInvariantContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IInvariantContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IInvariantContext)
}

func (s *Type_bodyContext) AllType_annotation() []IType_annotationContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IType_annotationContext); ok {
			len++
		}
	}

	tst := make([]IType_annotationContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IType_annotationContext); ok {
			tst[i] = t.(IType_annotationContext)
			i++
		}
	}

	return tst
}

func (s *Type_bodyContext) Type_annotation(i int) IType_annotationContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_annotationContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_annotationContext)
}

func (s *Type_bodyContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Type_bodyContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Type_bodyContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterType_body(s)
	}
}

func (s *Type_bodyContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitType_body(s)
	}
}

func (s *Type_bodyContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitType_body(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Type_body() (localctx IType_bodyContext) {
	localctx = NewType_bodyContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 18, YammmGrammarParserRULE_type_body)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(168)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&22196491653078) != 0) || _la == YammmGrammarParserDOC_COMMENT || _la == YammmGrammarParserLC_WORD {
		p.SetState(166)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}

		switch p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 12, p.GetParserRuleContext()) {
		case 1:
			{
				p.SetState(161)
				p.Property()
			}

		case 2:
			{
				p.SetState(162)
				p.Association()
			}

		case 3:
			{
				p.SetState(163)
				p.Composition()
			}

		case 4:
			{
				p.SetState(164)
				p.Invariant()
			}

		case 5:
			{
				p.SetState(165)
				p.Type_annotation()
			}

		case antlr.ATNInvalidAltNumber:
			goto errorExit
		}

		p.SetState(170)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IPropertyContext is an interface to support dynamic dispatch.
type IPropertyContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetIs_primary returns the is_primary token.
	GetIs_primary() antlr.Token

	// GetIs_required returns the is_required token.
	GetIs_required() antlr.Token

	// SetIs_primary sets the is_primary token.
	SetIs_primary(antlr.Token)

	// SetIs_required sets the is_required token.
	SetIs_required(antlr.Token)

	// Getter signatures
	Property_name() IProperty_nameContext
	Data_type_ref() IData_type_refContext
	DOC_COMMENT() antlr.TerminalNode
	AllAnnotation() []IAnnotationContext
	Annotation(i int) IAnnotationContext

	// IsPropertyContext differentiates from other interfaces.
	IsPropertyContext()
}

type PropertyContext struct {
	antlr.BaseParserRuleContext
	parser      antlr.Parser
	is_primary  antlr.Token
	is_required antlr.Token
}

func NewEmptyPropertyContext() *PropertyContext {
	p := new(PropertyContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_property
	return p
}

func InitEmptyPropertyContext(p *PropertyContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_property
}

func (*PropertyContext) IsPropertyContext() {}

func NewPropertyContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *PropertyContext {
	p := new(PropertyContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_property

	return p
}

func (s *PropertyContext) GetParser() antlr.Parser { return s.parser }

func (s *PropertyContext) GetIs_primary() antlr.Token { return s.is_primary }

func (s *PropertyContext) GetIs_required() antlr.Token { return s.is_required }

func (s *PropertyContext) SetIs_primary(v antlr.Token) { s.is_primary = v }

func (s *PropertyContext) SetIs_required(v antlr.Token) { s.is_required = v }

func (s *PropertyContext) Property_name() IProperty_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IProperty_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IProperty_nameContext)
}

func (s *PropertyContext) Data_type_ref() IData_type_refContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IData_type_refContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IData_type_refContext)
}

func (s *PropertyContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *PropertyContext) AllAnnotation() []IAnnotationContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IAnnotationContext); ok {
			len++
		}
	}

	tst := make([]IAnnotationContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IAnnotationContext); ok {
			tst[i] = t.(IAnnotationContext)
			i++
		}
	}

	return tst
}

func (s *PropertyContext) Annotation(i int) IAnnotationContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAnnotationContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAnnotationContext)
}

func (s *PropertyContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *PropertyContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *PropertyContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterProperty(s)
	}
}

func (s *PropertyContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitProperty(s)
	}
}

func (s *PropertyContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitProperty(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Property() (localctx IPropertyContext) {
	localctx = NewPropertyContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 20, YammmGrammarParserRULE_property)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(172)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(171)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(174)
		p.Property_name()
	}
	{
		p.SetState(175)
		p.Data_type_ref()
	}
	p.SetState(178)
	p.GetErrorHandler().Sync(p)

	if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 15, p.GetParserRuleContext()) == 1 {
		{
			p.SetState(176)

			_m := p.Match(YammmGrammarParserT__7)

			localctx.(*PropertyContext).is_primary = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	} else if p.HasError() { // JIM
		goto errorExit
	} else if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 15, p.GetParserRuleContext()) == 2 {
		{
			p.SetState(177)

			_m := p.Match(YammmGrammarParserT__8)

			localctx.(*PropertyContext).is_required = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	} else if p.HasError() { // JIM
		goto errorExit
	}
	p.SetState(183)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == YammmGrammarParserAT {
		{
			p.SetState(180)
			p.Annotation()
		}

		p.SetState(185)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IRel_propertyContext is an interface to support dynamic dispatch.
type IRel_propertyContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetIs_required returns the is_required token.
	GetIs_required() antlr.Token

	// SetIs_required sets the is_required token.
	SetIs_required(antlr.Token)

	// Getter signatures
	Property_name() IProperty_nameContext
	Data_type_ref() IData_type_refContext
	DOC_COMMENT() antlr.TerminalNode

	// IsRel_propertyContext differentiates from other interfaces.
	IsRel_propertyContext()
}

type Rel_propertyContext struct {
	antlr.BaseParserRuleContext
	parser      antlr.Parser
	is_required antlr.Token
}

func NewEmptyRel_propertyContext() *Rel_propertyContext {
	p := new(Rel_propertyContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_rel_property
	return p
}

func InitEmptyRel_propertyContext(p *Rel_propertyContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_rel_property
}

func (*Rel_propertyContext) IsRel_propertyContext() {}

func NewRel_propertyContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Rel_propertyContext {
	p := new(Rel_propertyContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_rel_property

	return p
}

func (s *Rel_propertyContext) GetParser() antlr.Parser { return s.parser }

func (s *Rel_propertyContext) GetIs_required() antlr.Token { return s.is_required }

func (s *Rel_propertyContext) SetIs_required(v antlr.Token) { s.is_required = v }

func (s *Rel_propertyContext) Property_name() IProperty_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IProperty_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IProperty_nameContext)
}

func (s *Rel_propertyContext) Data_type_ref() IData_type_refContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IData_type_refContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IData_type_refContext)
}

func (s *Rel_propertyContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *Rel_propertyContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Rel_propertyContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Rel_propertyContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterRel_property(s)
	}
}

func (s *Rel_propertyContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitRel_property(s)
	}
}

func (s *Rel_propertyContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitRel_property(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Rel_property() (localctx IRel_propertyContext) {
	localctx = NewRel_propertyContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 22, YammmGrammarParserRULE_rel_property)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(187)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(186)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(189)
		p.Property_name()
	}
	{
		p.SetState(190)
		p.Data_type_ref()
	}
	p.SetState(192)
	p.GetErrorHandler().Sync(p)

	if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 18, p.GetParserRuleContext()) == 1 {
		{
			p.SetState(191)

			_m := p.Match(YammmGrammarParserT__8)

			localctx.(*Rel_propertyContext).is_required = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	} else if p.HasError() { // JIM
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IProperty_nameContext is an interface to support dynamic dispatch.
type IProperty_nameContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LC_WORD() antlr.TerminalNode
	Lc_keyword() ILc_keywordContext

	// IsProperty_nameContext differentiates from other interfaces.
	IsProperty_nameContext()
}

type Property_nameContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyProperty_nameContext() *Property_nameContext {
	p := new(Property_nameContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_property_name
	return p
}

func InitEmptyProperty_nameContext(p *Property_nameContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_property_name
}

func (*Property_nameContext) IsProperty_nameContext() {}

func NewProperty_nameContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Property_nameContext {
	p := new(Property_nameContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_property_name

	return p
}

func (s *Property_nameContext) GetParser() antlr.Parser { return s.parser }

func (s *Property_nameContext) LC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLC_WORD, 0)
}

func (s *Property_nameContext) Lc_keyword() ILc_keywordContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ILc_keywordContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ILc_keywordContext)
}

func (s *Property_nameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Property_nameContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Property_nameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterProperty_name(s)
	}
}

func (s *Property_nameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitProperty_name(s)
	}
}

func (s *Property_nameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitProperty_name(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Property_name() (localctx IProperty_nameContext) {
	localctx = NewProperty_nameContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 24, YammmGrammarParserRULE_property_name)
	p.SetState(196)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserLC_WORD:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(194)
			p.Match(YammmGrammarParserLC_WORD)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserT__0, YammmGrammarParserT__1, YammmGrammarParserT__3, YammmGrammarParserT__5, YammmGrammarParserT__6, YammmGrammarParserT__7, YammmGrammarParserT__8, YammmGrammarParserT__9, YammmGrammarParserT__10, YammmGrammarParserT__24, YammmGrammarParserT__25:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(195)
			p.Lc_keyword()
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAnnotationContext is an interface to support dynamic dispatch.
type IAnnotationContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetName returns the name rule contexts.
	GetName() IProperty_nameContext

	// SetName sets the name rule contexts.
	SetName(IProperty_nameContext)

	// Getter signatures
	AT() antlr.TerminalNode
	Property_name() IProperty_nameContext
	Annotation_args() IAnnotation_argsContext

	// IsAnnotationContext differentiates from other interfaces.
	IsAnnotationContext()
}

type AnnotationContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	name   IProperty_nameContext
}

func NewEmptyAnnotationContext() *AnnotationContext {
	p := new(AnnotationContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_annotation
	return p
}

func InitEmptyAnnotationContext(p *AnnotationContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_annotation
}

func (*AnnotationContext) IsAnnotationContext() {}

func NewAnnotationContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *AnnotationContext {
	p := new(AnnotationContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_annotation

	return p
}

func (s *AnnotationContext) GetParser() antlr.Parser { return s.parser }

func (s *AnnotationContext) GetName() IProperty_nameContext { return s.name }

func (s *AnnotationContext) SetName(v IProperty_nameContext) { s.name = v }

func (s *AnnotationContext) AT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserAT, 0)
}

func (s *AnnotationContext) Property_name() IProperty_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IProperty_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IProperty_nameContext)
}

func (s *AnnotationContext) Annotation_args() IAnnotation_argsContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAnnotation_argsContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAnnotation_argsContext)
}

func (s *AnnotationContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *AnnotationContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *AnnotationContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAnnotation(s)
	}
}

func (s *AnnotationContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAnnotation(s)
	}
}

func (s *AnnotationContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAnnotation(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Annotation() (localctx IAnnotationContext) {
	localctx = NewAnnotationContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 26, YammmGrammarParserRULE_annotation)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(198)
		p.Match(YammmGrammarParserAT)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(199)

		_x := p.Property_name()

		localctx.(*AnnotationContext).name = _x
	}
	p.SetState(201)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLPAR {
		{
			p.SetState(200)
			p.Annotation_args()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IType_annotationContext is an interface to support dynamic dispatch.
type IType_annotationContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetName returns the name rule contexts.
	GetName() IProperty_nameContext

	// SetName sets the name rule contexts.
	SetName(IProperty_nameContext)

	// Getter signatures
	ATAT() antlr.TerminalNode
	Property_name() IProperty_nameContext
	DOC_COMMENT() antlr.TerminalNode
	Annotation_args() IAnnotation_argsContext

	// IsType_annotationContext differentiates from other interfaces.
	IsType_annotationContext()
}

type Type_annotationContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	name   IProperty_nameContext
}

func NewEmptyType_annotationContext() *Type_annotationContext {
	p := new(Type_annotationContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_annotation
	return p
}

func InitEmptyType_annotationContext(p *Type_annotationContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_type_annotation
}

func (*Type_annotationContext) IsType_annotationContext() {}

func NewType_annotationContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Type_annotationContext {
	p := new(Type_annotationContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_type_annotation

	return p
}

func (s *Type_annotationContext) GetParser() antlr.Parser { return s.parser }

func (s *Type_annotationContext) GetName() IProperty_nameContext { return s.name }

func (s *Type_annotationContext) SetName(v IProperty_nameContext) { s.name = v }

func (s *Type_annotationContext) ATAT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserATAT, 0)
}

func (s *Type_annotationContext) Property_name() IProperty_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IProperty_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IProperty_nameContext)
}

func (s *Type_annotationContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *Type_annotationContext) Annotation_args() IAnnotation_argsContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAnnotation_argsContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAnnotation_argsContext)
}

func (s *Type_annotationContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Type_annotationContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Type_annotationContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterType_annotation(s)
	}
}

func (s *Type_annotationContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitType_annotation(s)
	}
}

func (s *Type_annotationContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitType_annotation(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Type_annotation() (localctx IType_annotationContext) {
	localctx = NewType_annotationContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 28, YammmGrammarParserRULE_type_annotation)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(204)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(203)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(206)
		p.Match(YammmGrammarParserATAT)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(207)

		_x := p.Property_name()

		localctx.(*Type_annotationContext).name = _x
	}
	p.SetState(209)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLPAR {
		{
			p.SetState(208)
			p.Annotation_args()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAnnotation_argsContext is an interface to support dynamic dispatch.
type IAnnotation_argsContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Get_annotation_arg returns the _annotation_arg rule contexts.
	Get_annotation_arg() IAnnotation_argContext

	// Set_annotation_arg sets the _annotation_arg rule contexts.
	Set_annotation_arg(IAnnotation_argContext)

	// GetArgs returns the args rule context list.
	GetArgs() []IAnnotation_argContext

	// SetArgs sets the args rule context list.
	SetArgs([]IAnnotation_argContext)

	// Getter signatures
	LPAR() antlr.TerminalNode
	RPAR() antlr.TerminalNode
	AllAnnotation_arg() []IAnnotation_argContext
	Annotation_arg(i int) IAnnotation_argContext
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode

	// IsAnnotation_argsContext differentiates from other interfaces.
	IsAnnotation_argsContext()
}

type Annotation_argsContext struct {
	antlr.BaseParserRuleContext
	parser          antlr.Parser
	_annotation_arg IAnnotation_argContext
	args            []IAnnotation_argContext
}

func NewEmptyAnnotation_argsContext() *Annotation_argsContext {
	p := new(Annotation_argsContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_annotation_args
	return p
}

func InitEmptyAnnotation_argsContext(p *Annotation_argsContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_annotation_args
}

func (*Annotation_argsContext) IsAnnotation_argsContext() {}

func NewAnnotation_argsContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Annotation_argsContext {
	p := new(Annotation_argsContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_annotation_args

	return p
}

func (s *Annotation_argsContext) GetParser() antlr.Parser { return s.parser }

func (s *Annotation_argsContext) Get_annotation_arg() IAnnotation_argContext {
	return s._annotation_arg
}

func (s *Annotation_argsContext) Set_annotation_arg(v IAnnotation_argContext) { s._annotation_arg = v }

func (s *Annotation_argsContext) GetArgs() []IAnnotation_argContext { return s.args }

func (s *Annotation_argsContext) SetArgs(v []IAnnotation_argContext) { s.args = v }

func (s *Annotation_argsContext) LPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLPAR, 0)
}

func (s *Annotation_argsContext) RPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRPAR, 0)
}

func (s *Annotation_argsContext) AllAnnotation_arg() []IAnnotation_argContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IAnnotation_argContext); ok {
			len++
		}
	}

	tst := make([]IAnnotation_argContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IAnnotation_argContext); ok {
			tst[i] = t.(IAnnotation_argContext)
			i++
		}
	}

	return tst
}

func (s *Annotation_argsContext) Annotation_arg(i int) IAnnotation_argContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAnnotation_argContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAnnotation_argContext)
}

func (s *Annotation_argsContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *Annotation_argsContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *Annotation_argsContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Annotation_argsContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Annotation_argsContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAnnotation_args(s)
	}
}

func (s *Annotation_argsContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAnnotation_args(s)
	}
}

func (s *Annotation_argsContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAnnotation_args(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Annotation_args() (localctx IAnnotation_argsContext) {
	localctx = NewAnnotation_argsContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 30, YammmGrammarParserRULE_annotation_args)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(211)
		p.Match(YammmGrammarParserLPAR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(212)

		_x := p.Annotation_arg()

		localctx.(*Annotation_argsContext)._annotation_arg = _x
	}
	localctx.(*Annotation_argsContext).args = append(localctx.(*Annotation_argsContext).args, localctx.(*Annotation_argsContext)._annotation_arg)
	p.SetState(217)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 23, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			{
				p.SetState(213)
				p.Match(YammmGrammarParserCOMMA)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(214)

				_x := p.Annotation_arg()

				localctx.(*Annotation_argsContext)._annotation_arg = _x
			}
			localctx.(*Annotation_argsContext).args = append(localctx.(*Annotation_argsContext).args, localctx.(*Annotation_argsContext)._annotation_arg)

		}
		p.SetState(219)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 23, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}
	p.SetState(221)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserCOMMA {
		{
			p.SetState(220)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(223)
		p.Match(YammmGrammarParserRPAR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAnnotation_argContext is an interface to support dynamic dispatch.
type IAnnotation_argContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Property_name() IProperty_nameContext
	UC_WORD() antlr.TerminalNode
	Literal() ILiteralContext

	// IsAnnotation_argContext differentiates from other interfaces.
	IsAnnotation_argContext()
}

type Annotation_argContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyAnnotation_argContext() *Annotation_argContext {
	p := new(Annotation_argContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_annotation_arg
	return p
}

func InitEmptyAnnotation_argContext(p *Annotation_argContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_annotation_arg
}

func (*Annotation_argContext) IsAnnotation_argContext() {}

func NewAnnotation_argContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Annotation_argContext {
	p := new(Annotation_argContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_annotation_arg

	return p
}

func (s *Annotation_argContext) GetParser() antlr.Parser { return s.parser }

func (s *Annotation_argContext) Property_name() IProperty_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IProperty_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IProperty_nameContext)
}

func (s *Annotation_argContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *Annotation_argContext) Literal() ILiteralContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ILiteralContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ILiteralContext)
}

func (s *Annotation_argContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Annotation_argContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Annotation_argContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAnnotation_arg(s)
	}
}

func (s *Annotation_argContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAnnotation_arg(s)
	}
}

func (s *Annotation_argContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAnnotation_arg(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Annotation_arg() (localctx IAnnotation_argContext) {
	localctx = NewAnnotation_argContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 32, YammmGrammarParserRULE_annotation_arg)
	p.SetState(228)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserT__0, YammmGrammarParserT__1, YammmGrammarParserT__3, YammmGrammarParserT__5, YammmGrammarParserT__6, YammmGrammarParserT__7, YammmGrammarParserT__8, YammmGrammarParserT__9, YammmGrammarParserT__10, YammmGrammarParserT__24, YammmGrammarParserT__25, YammmGrammarParserLC_WORD:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(225)
			p.Property_name()
		}

	case YammmGrammarParserUC_WORD:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(226)
			p.Match(YammmGrammarParserUC_WORD)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserSTRING, YammmGrammarParserREGEXP, YammmGrammarParserINTEGER, YammmGrammarParserFLOAT, YammmGrammarParserBOOLEAN:
		p.EnterOuterAlt(localctx, 3)
		{
			p.SetState(227)
			p.Literal()
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IData_type_refContext is an interface to support dynamic dispatch.
type IData_type_refContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Built_in() IBuilt_inContext
	Qualified_alias() IQualified_aliasContext

	// IsData_type_refContext differentiates from other interfaces.
	IsData_type_refContext()
}

type Data_type_refContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyData_type_refContext() *Data_type_refContext {
	p := new(Data_type_refContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_data_type_ref
	return p
}

func InitEmptyData_type_refContext(p *Data_type_refContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_data_type_ref
}

func (*Data_type_refContext) IsData_type_refContext() {}

func NewData_type_refContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Data_type_refContext {
	p := new(Data_type_refContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_data_type_ref

	return p
}

func (s *Data_type_refContext) GetParser() antlr.Parser { return s.parser }

func (s *Data_type_refContext) Built_in() IBuilt_inContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBuilt_inContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBuilt_inContext)
}

func (s *Data_type_refContext) Qualified_alias() IQualified_aliasContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IQualified_aliasContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IQualified_aliasContext)
}

func (s *Data_type_refContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Data_type_refContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Data_type_refContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterData_type_ref(s)
	}
}

func (s *Data_type_refContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitData_type_ref(s)
	}
}

func (s *Data_type_refContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitData_type_ref(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Data_type_ref() (localctx IData_type_refContext) {
	localctx = NewData_type_refContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 34, YammmGrammarParserRULE_data_type_ref)
	p.SetState(232)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserT__11, YammmGrammarParserT__12, YammmGrammarParserT__13, YammmGrammarParserT__14, YammmGrammarParserT__15, YammmGrammarParserT__16, YammmGrammarParserT__17, YammmGrammarParserT__18, YammmGrammarParserT__19, YammmGrammarParserT__20, YammmGrammarParserT__21:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(230)
			p.Built_in()
		}

	case YammmGrammarParserUC_WORD, YammmGrammarParserLC_WORD:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(231)
			p.Qualified_alias()
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IQualified_aliasContext is an interface to support dynamic dispatch.
type IQualified_aliasContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetName returns the name token.
	GetName() antlr.Token

	// SetName sets the name token.
	SetName(antlr.Token)

	// GetQualifier returns the qualifier rule contexts.
	GetQualifier() IAlias_nameContext

	// SetQualifier sets the qualifier rule contexts.
	SetQualifier(IAlias_nameContext)

	// Getter signatures
	UC_WORD() antlr.TerminalNode
	PERIOD() antlr.TerminalNode
	Alias_name() IAlias_nameContext

	// IsQualified_aliasContext differentiates from other interfaces.
	IsQualified_aliasContext()
}

type Qualified_aliasContext struct {
	antlr.BaseParserRuleContext
	parser    antlr.Parser
	qualifier IAlias_nameContext
	name      antlr.Token
}

func NewEmptyQualified_aliasContext() *Qualified_aliasContext {
	p := new(Qualified_aliasContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_qualified_alias
	return p
}

func InitEmptyQualified_aliasContext(p *Qualified_aliasContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_qualified_alias
}

func (*Qualified_aliasContext) IsQualified_aliasContext() {}

func NewQualified_aliasContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Qualified_aliasContext {
	p := new(Qualified_aliasContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_qualified_alias

	return p
}

func (s *Qualified_aliasContext) GetParser() antlr.Parser { return s.parser }

func (s *Qualified_aliasContext) GetName() antlr.Token { return s.name }

func (s *Qualified_aliasContext) SetName(v antlr.Token) { s.name = v }

func (s *Qualified_aliasContext) GetQualifier() IAlias_nameContext { return s.qualifier }

func (s *Qualified_aliasContext) SetQualifier(v IAlias_nameContext) { s.qualifier = v }

func (s *Qualified_aliasContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *Qualified_aliasContext) PERIOD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserPERIOD, 0)
}

func (s *Qualified_aliasContext) Alias_name() IAlias_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAlias_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAlias_nameContext)
}

func (s *Qualified_aliasContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Qualified_aliasContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Qualified_aliasContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterQualified_alias(s)
	}
}

func (s *Qualified_aliasContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitQualified_alias(s)
	}
}

func (s *Qualified_aliasContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitQualified_alias(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Qualified_alias() (localctx IQualified_aliasContext) {
	localctx = NewQualified_aliasContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 36, YammmGrammarParserRULE_qualified_alias)
	p.EnterOuterAlt(localctx, 1)
	p.SetState(237)
	p.GetErrorHandler().Sync(p)

	if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 27, p.GetParserRuleContext()) == 1 {
		{
			p.SetState(234)

			_x := p.Alias_name()

			localctx.(*Qualified_aliasContext).qualifier = _x
		}
		{
			p.SetState(235)
			p.Match(YammmGrammarParserPERIOD)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	} else if p.HasError() { // JIM
		goto errorExit
	}
	{
		p.SetState(239)

		_m := p.Match(YammmGrammarParserUC_WORD)

		localctx.(*Qualified_aliasContext).name = _m
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAssociationContext is an interface to support dynamic dispatch.
type IAssociationContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetThisName returns the thisName rule contexts.
	GetThisName() IAny_nameContext

	// GetThisMp returns the thisMp rule contexts.
	GetThisMp() IMultiplicityContext

	// GetToType returns the toType rule contexts.
	GetToType() IType_refContext

	// GetReverse_name returns the reverse_name rule contexts.
	GetReverse_name() IAny_nameContext

	// GetReverseMp returns the reverseMp rule contexts.
	GetReverseMp() IMultiplicityContext

	// SetThisName sets the thisName rule contexts.
	SetThisName(IAny_nameContext)

	// SetThisMp sets the thisMp rule contexts.
	SetThisMp(IMultiplicityContext)

	// SetToType sets the toType rule contexts.
	SetToType(IType_refContext)

	// SetReverse_name sets the reverse_name rule contexts.
	SetReverse_name(IAny_nameContext)

	// SetReverseMp sets the reverseMp rule contexts.
	SetReverseMp(IMultiplicityContext)

	// Getter signatures
	ASSOC() antlr.TerminalNode
	AllAny_name() []IAny_nameContext
	Any_name(i int) IAny_nameContext
	Type_ref() IType_refContext
	DOC_COMMENT() antlr.TerminalNode
	SLASH() antlr.TerminalNode
	LBRACE() antlr.TerminalNode
	RBRACE() antlr.TerminalNode
	AllMultiplicity() []IMultiplicityContext
	Multiplicity(i int) IMultiplicityContext
	Relation_body() IRelation_bodyContext

	// IsAssociationContext differentiates from other interfaces.
	IsAssociationContext()
}

type AssociationContext struct {
	antlr.BaseParserRuleContext
	parser       antlr.Parser
	thisName     IAny_nameContext
	thisMp       IMultiplicityContext
	toType       IType_refContext
	reverse_name IAny_nameContext
	reverseMp    IMultiplicityContext
}

func NewEmptyAssociationContext() *AssociationContext {
	p := new(AssociationContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_association
	return p
}

func InitEmptyAssociationContext(p *AssociationContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_association
}

func (*AssociationContext) IsAssociationContext() {}

func NewAssociationContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *AssociationContext {
	p := new(AssociationContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_association

	return p
}

func (s *AssociationContext) GetParser() antlr.Parser { return s.parser }

func (s *AssociationContext) GetThisName() IAny_nameContext { return s.thisName }

func (s *AssociationContext) GetThisMp() IMultiplicityContext { return s.thisMp }

func (s *AssociationContext) GetToType() IType_refContext { return s.toType }

func (s *AssociationContext) GetReverse_name() IAny_nameContext { return s.reverse_name }

func (s *AssociationContext) GetReverseMp() IMultiplicityContext { return s.reverseMp }

func (s *AssociationContext) SetThisName(v IAny_nameContext) { s.thisName = v }

func (s *AssociationContext) SetThisMp(v IMultiplicityContext) { s.thisMp = v }

func (s *AssociationContext) SetToType(v IType_refContext) { s.toType = v }

func (s *AssociationContext) SetReverse_name(v IAny_nameContext) { s.reverse_name = v }

func (s *AssociationContext) SetReverseMp(v IMultiplicityContext) { s.reverseMp = v }

func (s *AssociationContext) ASSOC() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserASSOC, 0)
}

func (s *AssociationContext) AllAny_name() []IAny_nameContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IAny_nameContext); ok {
			len++
		}
	}

	tst := make([]IAny_nameContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IAny_nameContext); ok {
			tst[i] = t.(IAny_nameContext)
			i++
		}
	}

	return tst
}

func (s *AssociationContext) Any_name(i int) IAny_nameContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAny_nameContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAny_nameContext)
}

func (s *AssociationContext) Type_ref() IType_refContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_refContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_refContext)
}

func (s *AssociationContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *AssociationContext) SLASH() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSLASH, 0)
}

func (s *AssociationContext) LBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACE, 0)
}

func (s *AssociationContext) RBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACE, 0)
}

func (s *AssociationContext) AllMultiplicity() []IMultiplicityContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IMultiplicityContext); ok {
			len++
		}
	}

	tst := make([]IMultiplicityContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IMultiplicityContext); ok {
			tst[i] = t.(IMultiplicityContext)
			i++
		}
	}

	return tst
}

func (s *AssociationContext) Multiplicity(i int) IMultiplicityContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IMultiplicityContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IMultiplicityContext)
}

func (s *AssociationContext) Relation_body() IRelation_bodyContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IRelation_bodyContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IRelation_bodyContext)
}

func (s *AssociationContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *AssociationContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *AssociationContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAssociation(s)
	}
}

func (s *AssociationContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAssociation(s)
	}
}

func (s *AssociationContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAssociation(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Association() (localctx IAssociationContext) {
	localctx = NewAssociationContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 38, YammmGrammarParserRULE_association)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(242)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(241)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(244)
		p.Match(YammmGrammarParserASSOC)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(245)

		_x := p.Any_name()

		localctx.(*AssociationContext).thisName = _x
	}
	p.SetState(247)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLPAR {
		{
			p.SetState(246)

			_x := p.Multiplicity()

			localctx.(*AssociationContext).thisMp = _x
		}
	}
	{
		p.SetState(249)

		_x := p.Type_ref()

		localctx.(*AssociationContext).toType = _x
	}
	p.SetState(255)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserSLASH {
		{
			p.SetState(250)
			p.Match(YammmGrammarParserSLASH)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(251)

			_x := p.Any_name()

			localctx.(*AssociationContext).reverse_name = _x
		}
		p.SetState(253)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserLPAR {
			{
				p.SetState(252)

				_x := p.Multiplicity()

				localctx.(*AssociationContext).reverseMp = _x
			}
		}

	}
	p.SetState(262)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLBRACE {
		{
			p.SetState(257)
			p.Match(YammmGrammarParserLBRACE)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(259)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&100667350) != 0) || _la == YammmGrammarParserDOC_COMMENT || _la == YammmGrammarParserLC_WORD {
			{
				p.SetState(258)
				p.Relation_body()
			}
		}
		{
			p.SetState(261)
			p.Match(YammmGrammarParserRBRACE)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICompositionContext is an interface to support dynamic dispatch.
type ICompositionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetThisName returns the thisName rule contexts.
	GetThisName() IAny_nameContext

	// GetThisMp returns the thisMp rule contexts.
	GetThisMp() IMultiplicityContext

	// GetToType returns the toType rule contexts.
	GetToType() IType_refContext

	// GetReverse_name returns the reverse_name rule contexts.
	GetReverse_name() IAny_nameContext

	// GetReverseMp returns the reverseMp rule contexts.
	GetReverseMp() IMultiplicityContext

	// SetThisName sets the thisName rule contexts.
	SetThisName(IAny_nameContext)

	// SetThisMp sets the thisMp rule contexts.
	SetThisMp(IMultiplicityContext)

	// SetToType sets the toType rule contexts.
	SetToType(IType_refContext)

	// SetReverse_name sets the reverse_name rule contexts.
	SetReverse_name(IAny_nameContext)

	// SetReverseMp sets the reverseMp rule contexts.
	SetReverseMp(IMultiplicityContext)

	// Getter signatures
	COMP() antlr.TerminalNode
	AllAny_name() []IAny_nameContext
	Any_name(i int) IAny_nameContext
	Type_ref() IType_refContext
	DOC_COMMENT() antlr.TerminalNode
	SLASH() antlr.TerminalNode
	AllMultiplicity() []IMultiplicityContext
	Multiplicity(i int) IMultiplicityContext

	// IsCompositionContext differentiates from other interfaces.
	IsCompositionContext()
}

type CompositionContext struct {
	antlr.BaseParserRuleContext
	parser       antlr.Parser
	thisName     IAny_nameContext
	thisMp       IMultiplicityContext
	toType       IType_refContext
	reverse_name IAny_nameContext
	reverseMp    IMultiplicityContext
}

func NewEmptyCompositionContext() *CompositionContext {
	p := new(CompositionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_composition
	return p
}

func InitEmptyCompositionContext(p *CompositionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_composition
}

func (*CompositionContext) IsCompositionContext() {}

func NewCompositionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CompositionContext {
	p := new(CompositionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_composition

	return p
}

func (s *CompositionContext) GetParser() antlr.Parser { return s.parser }

func (s *CompositionContext) GetThisName() IAny_nameContext { return s.thisName }

func (s *CompositionContext) GetThisMp() IMultiplicityContext { return s.thisMp }

func (s *CompositionContext) GetToType() IType_refContext { return s.toType }

func (s *CompositionContext) GetReverse_name() IAny_nameContext { return s.reverse_name }

func (s *CompositionContext) GetReverseMp() IMultiplicityContext { return s.reverseMp }

func (s *CompositionContext) SetThisName(v IAny_nameContext) { s.thisName = v }

func (s *CompositionContext) SetThisMp(v IMultiplicityContext) { s.thisMp = v }

func (s *CompositionContext) SetToType(v IType_refContext) { s.toType = v }

func (s *CompositionContext) SetReverse_name(v IAny_nameContext) { s.reverse_name = v }

func (s *CompositionContext) SetReverseMp(v IMultiplicityContext) { s.reverseMp = v }

func (s *CompositionContext) COMP() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMP, 0)
}

func (s *CompositionContext) AllAny_name() []IAny_nameContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IAny_nameContext); ok {
			len++
		}
	}

	tst := make([]IAny_nameContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IAny_nameContext); ok {
			tst[i] = t.(IAny_nameContext)
			i++
		}
	}

	return tst
}

func (s *CompositionContext) Any_name(i int) IAny_nameContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAny_nameContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAny_nameContext)
}

func (s *CompositionContext) Type_ref() IType_refContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IType_refContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IType_refContext)
}

func (s *CompositionContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *CompositionContext) SLASH() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSLASH, 0)
}

func (s *CompositionContext) AllMultiplicity() []IMultiplicityContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IMultiplicityContext); ok {
			len++
		}
	}

	tst := make([]IMultiplicityContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IMultiplicityContext); ok {
			tst[i] = t.(IMultiplicityContext)
			i++
		}
	}

	return tst
}

func (s *CompositionContext) Multiplicity(i int) IMultiplicityContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IMultiplicityContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IMultiplicityContext)
}

func (s *CompositionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CompositionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *CompositionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterComposition(s)
	}
}

func (s *CompositionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitComposition(s)
	}
}

func (s *CompositionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitComposition(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Composition() (localctx ICompositionContext) {
	localctx = NewCompositionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 40, YammmGrammarParserRULE_composition)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(265)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(264)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(267)
		p.Match(YammmGrammarParserCOMP)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(268)

		_x := p.Any_name()

		localctx.(*CompositionContext).thisName = _x
	}
	p.SetState(270)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLPAR {
		{
			p.SetState(269)

			_x := p.Multiplicity()

			localctx.(*CompositionContext).thisMp = _x
		}
	}
	{
		p.SetState(272)

		_x := p.Type_ref()

		localctx.(*CompositionContext).toType = _x
	}
	p.SetState(278)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserSLASH {
		{
			p.SetState(273)
			p.Match(YammmGrammarParserSLASH)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(274)

			_x := p.Any_name()

			localctx.(*CompositionContext).reverse_name = _x
		}
		p.SetState(276)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserLPAR {
			{
				p.SetState(275)

				_x := p.Multiplicity()

				localctx.(*CompositionContext).reverseMp = _x
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAny_nameContext is an interface to support dynamic dispatch.
type IAny_nameContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UC_WORD() antlr.TerminalNode
	LC_WORD() antlr.TerminalNode

	// IsAny_nameContext differentiates from other interfaces.
	IsAny_nameContext()
}

type Any_nameContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyAny_nameContext() *Any_nameContext {
	p := new(Any_nameContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_any_name
	return p
}

func InitEmptyAny_nameContext(p *Any_nameContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_any_name
}

func (*Any_nameContext) IsAny_nameContext() {}

func NewAny_nameContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Any_nameContext {
	p := new(Any_nameContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_any_name

	return p
}

func (s *Any_nameContext) GetParser() antlr.Parser { return s.parser }

func (s *Any_nameContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *Any_nameContext) LC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLC_WORD, 0)
}

func (s *Any_nameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Any_nameContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Any_nameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAny_name(s)
	}
}

func (s *Any_nameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAny_name(s)
	}
}

func (s *Any_nameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAny_name(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Any_name() (localctx IAny_nameContext) {
	localctx = NewAny_nameContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 42, YammmGrammarParserRULE_any_name)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(280)
		_la = p.GetTokenStream().LA(1)

		if !(_la == YammmGrammarParserUC_WORD || _la == YammmGrammarParserLC_WORD) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IMultiplicityContext is an interface to support dynamic dispatch.
type IMultiplicityContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LPAR() antlr.TerminalNode
	RPAR() antlr.TerminalNode
	USCORE() antlr.TerminalNode
	COLON() antlr.TerminalNode

	// IsMultiplicityContext differentiates from other interfaces.
	IsMultiplicityContext()
}

type MultiplicityContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyMultiplicityContext() *MultiplicityContext {
	p := new(MultiplicityContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_multiplicity
	return p
}

func InitEmptyMultiplicityContext(p *MultiplicityContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_multiplicity
}

func (*MultiplicityContext) IsMultiplicityContext() {}

func NewMultiplicityContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *MultiplicityContext {
	p := new(MultiplicityContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_multiplicity

	return p
}

func (s *MultiplicityContext) GetParser() antlr.Parser { return s.parser }

func (s *MultiplicityContext) LPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLPAR, 0)
}

func (s *MultiplicityContext) RPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRPAR, 0)
}

func (s *MultiplicityContext) USCORE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUSCORE, 0)
}

func (s *MultiplicityContext) COLON() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOLON, 0)
}

func (s *MultiplicityContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *MultiplicityContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *MultiplicityContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterMultiplicity(s)
	}
}

func (s *MultiplicityContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitMultiplicity(s)
	}
}

func (s *MultiplicityContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitMultiplicity(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Multiplicity() (localctx IMultiplicityContext) {
	localctx = NewMultiplicityContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 44, YammmGrammarParserRULE_multiplicity)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(282)
		p.Match(YammmGrammarParserLPAR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(294)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserUSCORE:
		{
			p.SetState(283)
			p.Match(YammmGrammarParserUSCORE)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(286)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserCOLON {
			{
				p.SetState(284)
				p.Match(YammmGrammarParserCOLON)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(285)
				_la = p.GetTokenStream().LA(1)

				if !(_la == YammmGrammarParserT__9 || _la == YammmGrammarParserT__10) {
					p.GetErrorHandler().RecoverInline(p)
				} else {
					p.GetErrorHandler().ReportMatch(p)
					p.Consume()
				}
			}

		}

	case YammmGrammarParserT__9:
		{
			p.SetState(288)
			p.Match(YammmGrammarParserT__9)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(291)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserCOLON {
			{
				p.SetState(289)
				p.Match(YammmGrammarParserCOLON)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(290)
				_la = p.GetTokenStream().LA(1)

				if !(_la == YammmGrammarParserT__9 || _la == YammmGrammarParserT__10) {
					p.GetErrorHandler().RecoverInline(p)
				} else {
					p.GetErrorHandler().ReportMatch(p)
					p.Consume()
				}
			}

		}

	case YammmGrammarParserT__10:
		{
			p.SetState(293)
			p.Match(YammmGrammarParserT__10)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}
	{
		p.SetState(296)
		p.Match(YammmGrammarParserRPAR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IRelation_bodyContext is an interface to support dynamic dispatch.
type IRelation_bodyContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllRel_property() []IRel_propertyContext
	Rel_property(i int) IRel_propertyContext

	// IsRelation_bodyContext differentiates from other interfaces.
	IsRelation_bodyContext()
}

type Relation_bodyContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyRelation_bodyContext() *Relation_bodyContext {
	p := new(Relation_bodyContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_relation_body
	return p
}

func InitEmptyRelation_bodyContext(p *Relation_bodyContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_relation_body
}

func (*Relation_bodyContext) IsRelation_bodyContext() {}

func NewRelation_bodyContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Relation_bodyContext {
	p := new(Relation_bodyContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_relation_body

	return p
}

func (s *Relation_bodyContext) GetParser() antlr.Parser { return s.parser }

func (s *Relation_bodyContext) AllRel_property() []IRel_propertyContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IRel_propertyContext); ok {
			len++
		}
	}

	tst := make([]IRel_propertyContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IRel_propertyContext); ok {
			tst[i] = t.(IRel_propertyContext)
			i++
		}
	}

	return tst
}

func (s *Relation_bodyContext) Rel_property(i int) IRel_propertyContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IRel_propertyContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IRel_propertyContext)
}

func (s *Relation_bodyContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Relation_bodyContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Relation_bodyContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterRelation_body(s)
	}
}

func (s *Relation_bodyContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitRelation_body(s)
	}
}

func (s *Relation_bodyContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitRelation_body(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Relation_body() (localctx IRelation_bodyContext) {
	localctx = NewRelation_bodyContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 46, YammmGrammarParserRULE_relation_body)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(299)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for ok := true; ok; ok = ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&100667350) != 0) || _la == YammmGrammarParserDOC_COMMENT || _la == YammmGrammarParserLC_WORD {
		{
			p.SetState(298)
			p.Rel_property()
		}

		p.SetState(301)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IBuilt_inContext is an interface to support dynamic dispatch.
type IBuilt_inContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	IntegerT() IIntegerTContext
	FloatT() IFloatTContext
	BoolT() IBoolTContext
	StringT() IStringTContext
	EnumT() IEnumTContext
	PatternT() IPatternTContext
	TimestampT() ITimestampTContext
	DateT() IDateTContext
	UuidT() IUuidTContext
	VectorT() IVectorTContext
	ListT() IListTContext

	// IsBuilt_inContext differentiates from other interfaces.
	IsBuilt_inContext()
}

type Built_inContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyBuilt_inContext() *Built_inContext {
	p := new(Built_inContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_built_in
	return p
}

func InitEmptyBuilt_inContext(p *Built_inContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_built_in
}

func (*Built_inContext) IsBuilt_inContext() {}

func NewBuilt_inContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Built_inContext {
	p := new(Built_inContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_built_in

	return p
}

func (s *Built_inContext) GetParser() antlr.Parser { return s.parser }

func (s *Built_inContext) IntegerT() IIntegerTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IIntegerTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IIntegerTContext)
}

func (s *Built_inContext) FloatT() IFloatTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IFloatTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IFloatTContext)
}

func (s *Built_inContext) BoolT() IBoolTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBoolTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBoolTContext)
}

func (s *Built_inContext) StringT() IStringTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IStringTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IStringTContext)
}

func (s *Built_inContext) EnumT() IEnumTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IEnumTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IEnumTContext)
}

func (s *Built_inContext) PatternT() IPatternTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IPatternTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IPatternTContext)
}

func (s *Built_inContext) TimestampT() ITimestampTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ITimestampTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ITimestampTContext)
}

func (s *Built_inContext) DateT() IDateTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IDateTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IDateTContext)
}

func (s *Built_inContext) UuidT() IUuidTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IUuidTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IUuidTContext)
}

func (s *Built_inContext) VectorT() IVectorTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IVectorTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IVectorTContext)
}

func (s *Built_inContext) ListT() IListTContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IListTContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IListTContext)
}

func (s *Built_inContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Built_inContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Built_inContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterBuilt_in(s)
	}
}

func (s *Built_inContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitBuilt_in(s)
	}
}

func (s *Built_inContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitBuilt_in(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Built_in() (localctx IBuilt_inContext) {
	localctx = NewBuilt_inContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 48, YammmGrammarParserRULE_built_in)
	p.SetState(314)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserT__11:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(303)
			p.IntegerT()
		}

	case YammmGrammarParserT__12:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(304)
			p.FloatT()
		}

	case YammmGrammarParserT__13:
		p.EnterOuterAlt(localctx, 3)
		{
			p.SetState(305)
			p.BoolT()
		}

	case YammmGrammarParserT__14:
		p.EnterOuterAlt(localctx, 4)
		{
			p.SetState(306)
			p.StringT()
		}

	case YammmGrammarParserT__15:
		p.EnterOuterAlt(localctx, 5)
		{
			p.SetState(307)
			p.EnumT()
		}

	case YammmGrammarParserT__16:
		p.EnterOuterAlt(localctx, 6)
		{
			p.SetState(308)
			p.PatternT()
		}

	case YammmGrammarParserT__17:
		p.EnterOuterAlt(localctx, 7)
		{
			p.SetState(309)
			p.TimestampT()
		}

	case YammmGrammarParserT__19:
		p.EnterOuterAlt(localctx, 8)
		{
			p.SetState(310)
			p.DateT()
		}

	case YammmGrammarParserT__20:
		p.EnterOuterAlt(localctx, 9)
		{
			p.SetState(311)
			p.UuidT()
		}

	case YammmGrammarParserT__18:
		p.EnterOuterAlt(localctx, 10)
		{
			p.SetState(312)
			p.VectorT()
		}

	case YammmGrammarParserT__21:
		p.EnterOuterAlt(localctx, 11)
		{
			p.SetState(313)
			p.ListT()
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IIntegerTContext is an interface to support dynamic dispatch.
type IIntegerTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetNegMin returns the negMin token.
	GetNegMin() antlr.Token

	// GetMin returns the min token.
	GetMin() antlr.Token

	// GetNegMax returns the negMax token.
	GetNegMax() antlr.Token

	// GetMax returns the max token.
	GetMax() antlr.Token

	// SetNegMin sets the negMin token.
	SetNegMin(antlr.Token)

	// SetMin sets the min token.
	SetMin(antlr.Token)

	// SetNegMax sets the negMax token.
	SetNegMax(antlr.Token)

	// SetMax sets the max token.
	SetMax(antlr.Token)

	// Getter signatures
	LBRACK() antlr.TerminalNode
	COMMA() antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	AllUSCORE() []antlr.TerminalNode
	USCORE(i int) antlr.TerminalNode
	AllINTEGER() []antlr.TerminalNode
	INTEGER(i int) antlr.TerminalNode
	AllMINUS() []antlr.TerminalNode
	MINUS(i int) antlr.TerminalNode

	// IsIntegerTContext differentiates from other interfaces.
	IsIntegerTContext()
}

type IntegerTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	negMin antlr.Token
	min    antlr.Token
	negMax antlr.Token
	max    antlr.Token
}

func NewEmptyIntegerTContext() *IntegerTContext {
	p := new(IntegerTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_integerT
	return p
}

func InitEmptyIntegerTContext(p *IntegerTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_integerT
}

func (*IntegerTContext) IsIntegerTContext() {}

func NewIntegerTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *IntegerTContext {
	p := new(IntegerTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_integerT

	return p
}

func (s *IntegerTContext) GetParser() antlr.Parser { return s.parser }

func (s *IntegerTContext) GetNegMin() antlr.Token { return s.negMin }

func (s *IntegerTContext) GetMin() antlr.Token { return s.min }

func (s *IntegerTContext) GetNegMax() antlr.Token { return s.negMax }

func (s *IntegerTContext) GetMax() antlr.Token { return s.max }

func (s *IntegerTContext) SetNegMin(v antlr.Token) { s.negMin = v }

func (s *IntegerTContext) SetMin(v antlr.Token) { s.min = v }

func (s *IntegerTContext) SetNegMax(v antlr.Token) { s.negMax = v }

func (s *IntegerTContext) SetMax(v antlr.Token) { s.max = v }

func (s *IntegerTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *IntegerTContext) COMMA() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, 0)
}

func (s *IntegerTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *IntegerTContext) AllUSCORE() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserUSCORE)
}

func (s *IntegerTContext) USCORE(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUSCORE, i)
}

func (s *IntegerTContext) AllINTEGER() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserINTEGER)
}

func (s *IntegerTContext) INTEGER(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserINTEGER, i)
}

func (s *IntegerTContext) AllMINUS() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserMINUS)
}

func (s *IntegerTContext) MINUS(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserMINUS, i)
}

func (s *IntegerTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *IntegerTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *IntegerTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterIntegerT(s)
	}
}

func (s *IntegerTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitIntegerT(s)
	}
}

func (s *IntegerTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitIntegerT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) IntegerT() (localctx IIntegerTContext) {
	localctx = NewIntegerTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 50, YammmGrammarParserRULE_integerT)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(316)
		p.Match(YammmGrammarParserT__11)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(328)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLBRACK {
		{
			p.SetState(317)
			p.Match(YammmGrammarParserLBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(319)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserMINUS {
			{
				p.SetState(318)

				_m := p.Match(YammmGrammarParserMINUS)

				localctx.(*IntegerTContext).negMin = _m
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
		}
		{
			p.SetState(321)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*IntegerTContext).min = _lt

			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserUSCORE || _la == YammmGrammarParserINTEGER) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*IntegerTContext).min = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(322)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(324)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserMINUS {
			{
				p.SetState(323)

				_m := p.Match(YammmGrammarParserMINUS)

				localctx.(*IntegerTContext).negMax = _m
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
		}
		{
			p.SetState(326)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*IntegerTContext).max = _lt

			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserUSCORE || _la == YammmGrammarParserINTEGER) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*IntegerTContext).max = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(327)
			p.Match(YammmGrammarParserRBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IFloatTContext is an interface to support dynamic dispatch.
type IFloatTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetNegMin returns the negMin token.
	GetNegMin() antlr.Token

	// GetMin returns the min token.
	GetMin() antlr.Token

	// GetNegMax returns the negMax token.
	GetNegMax() antlr.Token

	// GetMax returns the max token.
	GetMax() antlr.Token

	// SetNegMin sets the negMin token.
	SetNegMin(antlr.Token)

	// SetMin sets the min token.
	SetMin(antlr.Token)

	// SetNegMax sets the negMax token.
	SetNegMax(antlr.Token)

	// SetMax sets the max token.
	SetMax(antlr.Token)

	// Getter signatures
	LBRACK() antlr.TerminalNode
	COMMA() antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	AllUSCORE() []antlr.TerminalNode
	USCORE(i int) antlr.TerminalNode
	AllINTEGER() []antlr.TerminalNode
	INTEGER(i int) antlr.TerminalNode
	AllFLOAT() []antlr.TerminalNode
	FLOAT(i int) antlr.TerminalNode
	AllMINUS() []antlr.TerminalNode
	MINUS(i int) antlr.TerminalNode

	// IsFloatTContext differentiates from other interfaces.
	IsFloatTContext()
}

type FloatTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	negMin antlr.Token
	min    antlr.Token
	negMax antlr.Token
	max    antlr.Token
}

func NewEmptyFloatTContext() *FloatTContext {
	p := new(FloatTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_floatT
	return p
}

func InitEmptyFloatTContext(p *FloatTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_floatT
}

func (*FloatTContext) IsFloatTContext() {}

func NewFloatTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *FloatTContext {
	p := new(FloatTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_floatT

	return p
}

func (s *FloatTContext) GetParser() antlr.Parser { return s.parser }

func (s *FloatTContext) GetNegMin() antlr.Token { return s.negMin }

func (s *FloatTContext) GetMin() antlr.Token { return s.min }

func (s *FloatTContext) GetNegMax() antlr.Token { return s.negMax }

func (s *FloatTContext) GetMax() antlr.Token { return s.max }

func (s *FloatTContext) SetNegMin(v antlr.Token) { s.negMin = v }

func (s *FloatTContext) SetMin(v antlr.Token) { s.min = v }

func (s *FloatTContext) SetNegMax(v antlr.Token) { s.negMax = v }

func (s *FloatTContext) SetMax(v antlr.Token) { s.max = v }

func (s *FloatTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *FloatTContext) COMMA() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, 0)
}

func (s *FloatTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *FloatTContext) AllUSCORE() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserUSCORE)
}

func (s *FloatTContext) USCORE(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUSCORE, i)
}

func (s *FloatTContext) AllINTEGER() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserINTEGER)
}

func (s *FloatTContext) INTEGER(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserINTEGER, i)
}

func (s *FloatTContext) AllFLOAT() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserFLOAT)
}

func (s *FloatTContext) FLOAT(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserFLOAT, i)
}

func (s *FloatTContext) AllMINUS() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserMINUS)
}

func (s *FloatTContext) MINUS(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserMINUS, i)
}

func (s *FloatTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *FloatTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *FloatTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterFloatT(s)
	}
}

func (s *FloatTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitFloatT(s)
	}
}

func (s *FloatTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitFloatT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) FloatT() (localctx IFloatTContext) {
	localctx = NewFloatTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 52, YammmGrammarParserRULE_floatT)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(330)
		p.Match(YammmGrammarParserT__12)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(342)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLBRACK {
		{
			p.SetState(331)
			p.Match(YammmGrammarParserLBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(333)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserMINUS {
			{
				p.SetState(332)

				_m := p.Match(YammmGrammarParserMINUS)

				localctx.(*FloatTContext).negMin = _m
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
		}
		{
			p.SetState(335)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*FloatTContext).min = _lt

			_la = p.GetTokenStream().LA(1)

			if !((int64((_la-40)) & ^0x3f) == 0 && ((int64(1)<<(_la-40))&1610612737) != 0) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*FloatTContext).min = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(336)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(338)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == YammmGrammarParserMINUS {
			{
				p.SetState(337)

				_m := p.Match(YammmGrammarParserMINUS)

				localctx.(*FloatTContext).negMax = _m
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
		}
		{
			p.SetState(340)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*FloatTContext).max = _lt

			_la = p.GetTokenStream().LA(1)

			if !((int64((_la-40)) & ^0x3f) == 0 && ((int64(1)<<(_la-40))&1610612737) != 0) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*FloatTContext).max = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(341)
			p.Match(YammmGrammarParserRBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IBoolTContext is an interface to support dynamic dispatch.
type IBoolTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsBoolTContext differentiates from other interfaces.
	IsBoolTContext()
}

type BoolTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyBoolTContext() *BoolTContext {
	p := new(BoolTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_boolT
	return p
}

func InitEmptyBoolTContext(p *BoolTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_boolT
}

func (*BoolTContext) IsBoolTContext() {}

func NewBoolTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BoolTContext {
	p := new(BoolTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_boolT

	return p
}

func (s *BoolTContext) GetParser() antlr.Parser { return s.parser }
func (s *BoolTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *BoolTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *BoolTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterBoolT(s)
	}
}

func (s *BoolTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitBoolT(s)
	}
}

func (s *BoolTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitBoolT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) BoolT() (localctx IBoolTContext) {
	localctx = NewBoolTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 54, YammmGrammarParserRULE_boolT)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(344)
		p.Match(YammmGrammarParserT__13)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IStringTContext is an interface to support dynamic dispatch.
type IStringTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetMin returns the min token.
	GetMin() antlr.Token

	// GetMax returns the max token.
	GetMax() antlr.Token

	// SetMin sets the min token.
	SetMin(antlr.Token)

	// SetMax sets the max token.
	SetMax(antlr.Token)

	// Getter signatures
	LBRACK() antlr.TerminalNode
	COMMA() antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	AllUSCORE() []antlr.TerminalNode
	USCORE(i int) antlr.TerminalNode
	AllINTEGER() []antlr.TerminalNode
	INTEGER(i int) antlr.TerminalNode

	// IsStringTContext differentiates from other interfaces.
	IsStringTContext()
}

type StringTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	min    antlr.Token
	max    antlr.Token
}

func NewEmptyStringTContext() *StringTContext {
	p := new(StringTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_stringT
	return p
}

func InitEmptyStringTContext(p *StringTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_stringT
}

func (*StringTContext) IsStringTContext() {}

func NewStringTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *StringTContext {
	p := new(StringTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_stringT

	return p
}

func (s *StringTContext) GetParser() antlr.Parser { return s.parser }

func (s *StringTContext) GetMin() antlr.Token { return s.min }

func (s *StringTContext) GetMax() antlr.Token { return s.max }

func (s *StringTContext) SetMin(v antlr.Token) { s.min = v }

func (s *StringTContext) SetMax(v antlr.Token) { s.max = v }

func (s *StringTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *StringTContext) COMMA() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, 0)
}

func (s *StringTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *StringTContext) AllUSCORE() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserUSCORE)
}

func (s *StringTContext) USCORE(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUSCORE, i)
}

func (s *StringTContext) AllINTEGER() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserINTEGER)
}

func (s *StringTContext) INTEGER(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserINTEGER, i)
}

func (s *StringTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *StringTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *StringTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterStringT(s)
	}
}

func (s *StringTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitStringT(s)
	}
}

func (s *StringTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitStringT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) StringT() (localctx IStringTContext) {
	localctx = NewStringTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 56, YammmGrammarParserRULE_stringT)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(346)
		p.Match(YammmGrammarParserT__14)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(352)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLBRACK {
		{
			p.SetState(347)
			p.Match(YammmGrammarParserLBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(348)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*StringTContext).min = _lt

			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserUSCORE || _la == YammmGrammarParserINTEGER) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*StringTContext).min = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(349)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(350)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*StringTContext).max = _lt

			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserUSCORE || _la == YammmGrammarParserINTEGER) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*StringTContext).max = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(351)
			p.Match(YammmGrammarParserRBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IEnumTContext is an interface to support dynamic dispatch.
type IEnumTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LBRACK() antlr.TerminalNode
	AllSTRING() []antlr.TerminalNode
	STRING(i int) antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode

	// IsEnumTContext differentiates from other interfaces.
	IsEnumTContext()
}

type EnumTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyEnumTContext() *EnumTContext {
	p := new(EnumTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_enumT
	return p
}

func InitEmptyEnumTContext(p *EnumTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_enumT
}

func (*EnumTContext) IsEnumTContext() {}

func NewEnumTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *EnumTContext {
	p := new(EnumTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_enumT

	return p
}

func (s *EnumTContext) GetParser() antlr.Parser { return s.parser }

func (s *EnumTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *EnumTContext) AllSTRING() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserSTRING)
}

func (s *EnumTContext) STRING(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, i)
}

func (s *EnumTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *EnumTContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *EnumTContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *EnumTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *EnumTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *EnumTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterEnumT(s)
	}
}

func (s *EnumTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitEnumT(s)
	}
}

func (s *EnumTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitEnumT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) EnumT() (localctx IEnumTContext) {
	localctx = NewEnumTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 58, YammmGrammarParserRULE_enumT)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(354)
		p.Match(YammmGrammarParserT__15)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(355)
		p.Match(YammmGrammarParserLBRACK)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(356)
		p.Match(YammmGrammarParserSTRING)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(359)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = 1
	for ok := true; ok; ok = _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		switch _alt {
		case 1:
			{
				p.SetState(357)
				p.Match(YammmGrammarParserCOMMA)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(358)
				p.Match(YammmGrammarParserSTRING)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}

		default:
			p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
			goto errorExit
		}

		p.SetState(361)
		p.GetErrorHandler().Sync(p)
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 50, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}
	p.SetState(364)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserCOMMA {
		{
			p.SetState(363)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(366)
		p.Match(YammmGrammarParserRBRACK)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IPatternTContext is an interface to support dynamic dispatch.
type IPatternTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LBRACK() antlr.TerminalNode
	AllSTRING() []antlr.TerminalNode
	STRING(i int) antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	COMMA() antlr.TerminalNode

	// IsPatternTContext differentiates from other interfaces.
	IsPatternTContext()
}

type PatternTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyPatternTContext() *PatternTContext {
	p := new(PatternTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_patternT
	return p
}

func InitEmptyPatternTContext(p *PatternTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_patternT
}

func (*PatternTContext) IsPatternTContext() {}

func NewPatternTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *PatternTContext {
	p := new(PatternTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_patternT

	return p
}

func (s *PatternTContext) GetParser() antlr.Parser { return s.parser }

func (s *PatternTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *PatternTContext) AllSTRING() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserSTRING)
}

func (s *PatternTContext) STRING(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, i)
}

func (s *PatternTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *PatternTContext) COMMA() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, 0)
}

func (s *PatternTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *PatternTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *PatternTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterPatternT(s)
	}
}

func (s *PatternTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitPatternT(s)
	}
}

func (s *PatternTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitPatternT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) PatternT() (localctx IPatternTContext) {
	localctx = NewPatternTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 60, YammmGrammarParserRULE_patternT)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(368)
		p.Match(YammmGrammarParserT__16)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(369)
		p.Match(YammmGrammarParserLBRACK)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(370)
		p.Match(YammmGrammarParserSTRING)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(373)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserCOMMA {
		{
			p.SetState(371)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(372)
			p.Match(YammmGrammarParserSTRING)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}
	{
		p.SetState(375)
		p.Match(YammmGrammarParserRBRACK)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ITimestampTContext is an interface to support dynamic dispatch.
type ITimestampTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetFormat returns the format token.
	GetFormat() antlr.Token

	// SetFormat sets the format token.
	SetFormat(antlr.Token)

	// Getter signatures
	LBRACK() antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	STRING() antlr.TerminalNode

	// IsTimestampTContext differentiates from other interfaces.
	IsTimestampTContext()
}

type TimestampTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	format antlr.Token
}

func NewEmptyTimestampTContext() *TimestampTContext {
	p := new(TimestampTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_timestampT
	return p
}

func InitEmptyTimestampTContext(p *TimestampTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_timestampT
}

func (*TimestampTContext) IsTimestampTContext() {}

func NewTimestampTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *TimestampTContext {
	p := new(TimestampTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_timestampT

	return p
}

func (s *TimestampTContext) GetParser() antlr.Parser { return s.parser }

func (s *TimestampTContext) GetFormat() antlr.Token { return s.format }

func (s *TimestampTContext) SetFormat(v antlr.Token) { s.format = v }

func (s *TimestampTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *TimestampTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *TimestampTContext) STRING() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, 0)
}

func (s *TimestampTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *TimestampTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *TimestampTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterTimestampT(s)
	}
}

func (s *TimestampTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitTimestampT(s)
	}
}

func (s *TimestampTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitTimestampT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) TimestampT() (localctx ITimestampTContext) {
	localctx = NewTimestampTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 62, YammmGrammarParserRULE_timestampT)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(377)
		p.Match(YammmGrammarParserT__17)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(381)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLBRACK {
		{
			p.SetState(378)
			p.Match(YammmGrammarParserLBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(379)

			_m := p.Match(YammmGrammarParserSTRING)

			localctx.(*TimestampTContext).format = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(380)
			p.Match(YammmGrammarParserRBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IVectorTContext is an interface to support dynamic dispatch.
type IVectorTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetDimensions returns the dimensions token.
	GetDimensions() antlr.Token

	// SetDimensions sets the dimensions token.
	SetDimensions(antlr.Token)

	// Getter signatures
	LBRACK() antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	INTEGER() antlr.TerminalNode

	// IsVectorTContext differentiates from other interfaces.
	IsVectorTContext()
}

type VectorTContext struct {
	antlr.BaseParserRuleContext
	parser     antlr.Parser
	dimensions antlr.Token
}

func NewEmptyVectorTContext() *VectorTContext {
	p := new(VectorTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_vectorT
	return p
}

func InitEmptyVectorTContext(p *VectorTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_vectorT
}

func (*VectorTContext) IsVectorTContext() {}

func NewVectorTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *VectorTContext {
	p := new(VectorTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_vectorT

	return p
}

func (s *VectorTContext) GetParser() antlr.Parser { return s.parser }

func (s *VectorTContext) GetDimensions() antlr.Token { return s.dimensions }

func (s *VectorTContext) SetDimensions(v antlr.Token) { s.dimensions = v }

func (s *VectorTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *VectorTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *VectorTContext) INTEGER() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserINTEGER, 0)
}

func (s *VectorTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *VectorTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *VectorTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterVectorT(s)
	}
}

func (s *VectorTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitVectorT(s)
	}
}

func (s *VectorTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitVectorT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) VectorT() (localctx IVectorTContext) {
	localctx = NewVectorTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 64, YammmGrammarParserRULE_vectorT)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(383)
		p.Match(YammmGrammarParserT__18)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(384)
		p.Match(YammmGrammarParserLBRACK)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(385)

		_m := p.Match(YammmGrammarParserINTEGER)

		localctx.(*VectorTContext).dimensions = _m
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(386)
		p.Match(YammmGrammarParserRBRACK)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IDateTContext is an interface to support dynamic dispatch.
type IDateTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsDateTContext differentiates from other interfaces.
	IsDateTContext()
}

type DateTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyDateTContext() *DateTContext {
	p := new(DateTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_dateT
	return p
}

func InitEmptyDateTContext(p *DateTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_dateT
}

func (*DateTContext) IsDateTContext() {}

func NewDateTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *DateTContext {
	p := new(DateTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_dateT

	return p
}

func (s *DateTContext) GetParser() antlr.Parser { return s.parser }
func (s *DateTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *DateTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *DateTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterDateT(s)
	}
}

func (s *DateTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitDateT(s)
	}
}

func (s *DateTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitDateT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) DateT() (localctx IDateTContext) {
	localctx = NewDateTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 66, YammmGrammarParserRULE_dateT)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(388)
		p.Match(YammmGrammarParserT__19)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IUuidTContext is an interface to support dynamic dispatch.
type IUuidTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsUuidTContext differentiates from other interfaces.
	IsUuidTContext()
}

type UuidTContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyUuidTContext() *UuidTContext {
	p := new(UuidTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_uuidT
	return p
}

func InitEmptyUuidTContext(p *UuidTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_uuidT
}

func (*UuidTContext) IsUuidTContext() {}

func NewUuidTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *UuidTContext {
	p := new(UuidTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_uuidT

	return p
}

func (s *UuidTContext) GetParser() antlr.Parser { return s.parser }
func (s *UuidTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *UuidTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *UuidTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterUuidT(s)
	}
}

func (s *UuidTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitUuidT(s)
	}
}

func (s *UuidTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitUuidT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) UuidT() (localctx IUuidTContext) {
	localctx = NewUuidTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 68, YammmGrammarParserRULE_uuidT)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(390)
		p.Match(YammmGrammarParserT__20)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IListTContext is an interface to support dynamic dispatch.
type IListTContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetMin returns the min token.
	GetMin() antlr.Token

	// GetMax returns the max token.
	GetMax() antlr.Token

	// SetMin sets the min token.
	SetMin(antlr.Token)

	// SetMax sets the max token.
	SetMax(antlr.Token)

	// GetElementType returns the elementType rule contexts.
	GetElementType() IData_type_refContext

	// SetElementType sets the elementType rule contexts.
	SetElementType(IData_type_refContext)

	// Getter signatures
	LT() antlr.TerminalNode
	GT() antlr.TerminalNode
	Data_type_ref() IData_type_refContext
	LBRACK() antlr.TerminalNode
	COMMA() antlr.TerminalNode
	RBRACK() antlr.TerminalNode
	AllUSCORE() []antlr.TerminalNode
	USCORE(i int) antlr.TerminalNode
	AllINTEGER() []antlr.TerminalNode
	INTEGER(i int) antlr.TerminalNode

	// IsListTContext differentiates from other interfaces.
	IsListTContext()
}

type ListTContext struct {
	antlr.BaseParserRuleContext
	parser      antlr.Parser
	elementType IData_type_refContext
	min         antlr.Token
	max         antlr.Token
}

func NewEmptyListTContext() *ListTContext {
	p := new(ListTContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_listT
	return p
}

func InitEmptyListTContext(p *ListTContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_listT
}

func (*ListTContext) IsListTContext() {}

func NewListTContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ListTContext {
	p := new(ListTContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_listT

	return p
}

func (s *ListTContext) GetParser() antlr.Parser { return s.parser }

func (s *ListTContext) GetMin() antlr.Token { return s.min }

func (s *ListTContext) GetMax() antlr.Token { return s.max }

func (s *ListTContext) SetMin(v antlr.Token) { s.min = v }

func (s *ListTContext) SetMax(v antlr.Token) { s.max = v }

func (s *ListTContext) GetElementType() IData_type_refContext { return s.elementType }

func (s *ListTContext) SetElementType(v IData_type_refContext) { s.elementType = v }

func (s *ListTContext) LT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLT, 0)
}

func (s *ListTContext) GT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserGT, 0)
}

func (s *ListTContext) Data_type_ref() IData_type_refContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IData_type_refContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IData_type_refContext)
}

func (s *ListTContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *ListTContext) COMMA() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, 0)
}

func (s *ListTContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *ListTContext) AllUSCORE() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserUSCORE)
}

func (s *ListTContext) USCORE(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUSCORE, i)
}

func (s *ListTContext) AllINTEGER() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserINTEGER)
}

func (s *ListTContext) INTEGER(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserINTEGER, i)
}

func (s *ListTContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ListTContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ListTContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterListT(s)
	}
}

func (s *ListTContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitListT(s)
	}
}

func (s *ListTContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitListT(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) ListT() (localctx IListTContext) {
	localctx = NewListTContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 70, YammmGrammarParserRULE_listT)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(392)
		p.Match(YammmGrammarParserT__21)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(393)
		p.Match(YammmGrammarParserLT)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(394)

		_x := p.Data_type_ref()

		localctx.(*ListTContext).elementType = _x
	}
	{
		p.SetState(395)
		p.Match(YammmGrammarParserGT)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(401)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserLBRACK {
		{
			p.SetState(396)
			p.Match(YammmGrammarParserLBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(397)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*ListTContext).min = _lt

			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserUSCORE || _la == YammmGrammarParserINTEGER) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*ListTContext).min = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(398)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(399)

			_lt := p.GetTokenStream().LT(1)

			localctx.(*ListTContext).max = _lt

			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserUSCORE || _la == YammmGrammarParserINTEGER) {
				_ri := p.GetErrorHandler().RecoverInline(p)

				localctx.(*ListTContext).max = _ri
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}
		{
			p.SetState(400)
			p.Match(YammmGrammarParserRBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IDatatypeKeywordContext is an interface to support dynamic dispatch.
type IDatatypeKeywordContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsDatatypeKeywordContext differentiates from other interfaces.
	IsDatatypeKeywordContext()
}

type DatatypeKeywordContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyDatatypeKeywordContext() *DatatypeKeywordContext {
	p := new(DatatypeKeywordContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_datatypeKeyword
	return p
}

func InitEmptyDatatypeKeywordContext(p *DatatypeKeywordContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_datatypeKeyword
}

func (*DatatypeKeywordContext) IsDatatypeKeywordContext() {}

func NewDatatypeKeywordContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *DatatypeKeywordContext {
	p := new(DatatypeKeywordContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_datatypeKeyword

	return p
}

func (s *DatatypeKeywordContext) GetParser() antlr.Parser { return s.parser }
func (s *DatatypeKeywordContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *DatatypeKeywordContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *DatatypeKeywordContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterDatatypeKeyword(s)
	}
}

func (s *DatatypeKeywordContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitDatatypeKeyword(s)
	}
}

func (s *DatatypeKeywordContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitDatatypeKeyword(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) DatatypeKeyword() (localctx IDatatypeKeywordContext) {
	localctx = NewDatatypeKeywordContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 72, YammmGrammarParserRULE_datatypeKeyword)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(403)
		_la = p.GetTokenStream().LA(1)

		if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&8384512) != 0) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IInvariantContext is an interface to support dynamic dispatch.
type IInvariantContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetMessage returns the message token.
	GetMessage() antlr.Token

	// SetMessage sets the message token.
	SetMessage(antlr.Token)

	// GetConstraint returns the constraint rule contexts.
	GetConstraint() IExprContext

	// SetConstraint sets the constraint rule contexts.
	SetConstraint(IExprContext)

	// Getter signatures
	EXCLAMATION() antlr.TerminalNode
	STRING() antlr.TerminalNode
	Expr() IExprContext
	DOC_COMMENT() antlr.TerminalNode

	// IsInvariantContext differentiates from other interfaces.
	IsInvariantContext()
}

type InvariantContext struct {
	antlr.BaseParserRuleContext
	parser     antlr.Parser
	message    antlr.Token
	constraint IExprContext
}

func NewEmptyInvariantContext() *InvariantContext {
	p := new(InvariantContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_invariant
	return p
}

func InitEmptyInvariantContext(p *InvariantContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_invariant
}

func (*InvariantContext) IsInvariantContext() {}

func NewInvariantContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *InvariantContext {
	p := new(InvariantContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_invariant

	return p
}

func (s *InvariantContext) GetParser() antlr.Parser { return s.parser }

func (s *InvariantContext) GetMessage() antlr.Token { return s.message }

func (s *InvariantContext) SetMessage(v antlr.Token) { s.message = v }

func (s *InvariantContext) GetConstraint() IExprContext { return s.constraint }

func (s *InvariantContext) SetConstraint(v IExprContext) { s.constraint = v }

func (s *InvariantContext) EXCLAMATION() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserEXCLAMATION, 0)
}

func (s *InvariantContext) STRING() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, 0)
}

func (s *InvariantContext) Expr() IExprContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *InvariantContext) DOC_COMMENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserDOC_COMMENT, 0)
}

func (s *InvariantContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *InvariantContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *InvariantContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterInvariant(s)
	}
}

func (s *InvariantContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitInvariant(s)
	}
}

func (s *InvariantContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitInvariant(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Invariant() (localctx IInvariantContext) {
	localctx = NewInvariantContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 74, YammmGrammarParserRULE_invariant)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(406)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserDOC_COMMENT {
		{
			p.SetState(405)
			p.Match(YammmGrammarParserDOC_COMMENT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(408)
		p.Match(YammmGrammarParserEXCLAMATION)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(409)

		_m := p.Match(YammmGrammarParserSTRING)

		localctx.(*InvariantContext).message = _m
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(410)

		_x := p.expr(0)

		localctx.(*InvariantContext).constraint = _x
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IExprContext is an interface to support dynamic dispatch.
type IExprContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsExprContext differentiates from other interfaces.
	IsExprContext()
}

type ExprContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyExprContext() *ExprContext {
	p := new(ExprContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_expr
	return p
}

func InitEmptyExprContext(p *ExprContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_expr
}

func (*ExprContext) IsExprContext() {}

func NewExprContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ExprContext {
	p := new(ExprContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_expr

	return p
}

func (s *ExprContext) GetParser() antlr.Parser { return s.parser }

func (s *ExprContext) CopyAll(ctx *ExprContext) {
	s.CopyFrom(&ctx.BaseParserRuleContext)
}

func (s *ExprContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ExprContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

type DatatypeNameContext struct {
	ExprContext
	left IDatatypeKeywordContext
}

func NewDatatypeNameContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *DatatypeNameContext {
	p := new(DatatypeNameContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *DatatypeNameContext) GetLeft() IDatatypeKeywordContext { return s.left }

func (s *DatatypeNameContext) SetLeft(v IDatatypeKeywordContext) { s.left = v }

func (s *DatatypeNameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *DatatypeNameContext) DatatypeKeyword() IDatatypeKeywordContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IDatatypeKeywordContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IDatatypeKeywordContext)
}

func (s *DatatypeNameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterDatatypeName(s)
	}
}

func (s *DatatypeNameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitDatatypeName(s)
	}
}

func (s *DatatypeNameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitDatatypeName(s)

	default:
		return t.VisitChildren(s)
	}
}

type PlusminusContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewPlusminusContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *PlusminusContext {
	p := new(PlusminusContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *PlusminusContext) GetOp() antlr.Token { return s.op }

func (s *PlusminusContext) SetOp(v antlr.Token) { s.op = v }

func (s *PlusminusContext) GetLeft() IExprContext { return s.left }

func (s *PlusminusContext) GetRight() IExprContext { return s.right }

func (s *PlusminusContext) SetLeft(v IExprContext) { s.left = v }

func (s *PlusminusContext) SetRight(v IExprContext) { s.right = v }

func (s *PlusminusContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *PlusminusContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *PlusminusContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *PlusminusContext) PLUS() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserPLUS, 0)
}

func (s *PlusminusContext) MINUS() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserMINUS, 0)
}

func (s *PlusminusContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterPlusminus(s)
	}
}

func (s *PlusminusContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitPlusminus(s)
	}
}

func (s *PlusminusContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitPlusminus(s)

	default:
		return t.VisitChildren(s)
	}
}

type PeriodContext struct {
	ExprContext
	left IExprContext
	name IExprContext
}

func NewPeriodContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *PeriodContext {
	p := new(PeriodContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *PeriodContext) GetLeft() IExprContext { return s.left }

func (s *PeriodContext) GetName() IExprContext { return s.name }

func (s *PeriodContext) SetLeft(v IExprContext) { s.left = v }

func (s *PeriodContext) SetName(v IExprContext) { s.name = v }

func (s *PeriodContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *PeriodContext) PERIOD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserPERIOD, 0)
}

func (s *PeriodContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *PeriodContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *PeriodContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterPeriod(s)
	}
}

func (s *PeriodContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitPeriod(s)
	}
}

func (s *PeriodContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitPeriod(s)

	default:
		return t.VisitChildren(s)
	}
}

type CompareContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewCompareContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CompareContext {
	p := new(CompareContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *CompareContext) GetOp() antlr.Token { return s.op }

func (s *CompareContext) SetOp(v antlr.Token) { s.op = v }

func (s *CompareContext) GetLeft() IExprContext { return s.left }

func (s *CompareContext) GetRight() IExprContext { return s.right }

func (s *CompareContext) SetLeft(v IExprContext) { s.left = v }

func (s *CompareContext) SetRight(v IExprContext) { s.right = v }

func (s *CompareContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CompareContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *CompareContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *CompareContext) GT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserGT, 0)
}

func (s *CompareContext) GTE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserGTE, 0)
}

func (s *CompareContext) LT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLT, 0)
}

func (s *CompareContext) LTE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLTE, 0)
}

func (s *CompareContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterCompare(s)
	}
}

func (s *CompareContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitCompare(s)
	}
}

func (s *CompareContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitCompare(s)

	default:
		return t.VisitChildren(s)
	}
}

type UminusContext struct {
	ExprContext
	op    antlr.Token
	right IExprContext
}

func NewUminusContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *UminusContext {
	p := new(UminusContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *UminusContext) GetOp() antlr.Token { return s.op }

func (s *UminusContext) SetOp(v antlr.Token) { s.op = v }

func (s *UminusContext) GetRight() IExprContext { return s.right }

func (s *UminusContext) SetRight(v IExprContext) { s.right = v }

func (s *UminusContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *UminusContext) MINUS() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserMINUS, 0)
}

func (s *UminusContext) Expr() IExprContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *UminusContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterUminus(s)
	}
}

func (s *UminusContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitUminus(s)
	}
}

func (s *UminusContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitUminus(s)

	default:
		return t.VisitChildren(s)
	}
}

type OrContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewOrContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *OrContext {
	p := new(OrContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *OrContext) GetOp() antlr.Token { return s.op }

func (s *OrContext) SetOp(v antlr.Token) { s.op = v }

func (s *OrContext) GetLeft() IExprContext { return s.left }

func (s *OrContext) GetRight() IExprContext { return s.right }

func (s *OrContext) SetLeft(v IExprContext) { s.left = v }

func (s *OrContext) SetRight(v IExprContext) { s.right = v }

func (s *OrContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *OrContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *OrContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *OrContext) OR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserOR, 0)
}

func (s *OrContext) HAT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserHAT, 0)
}

func (s *OrContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterOr(s)
	}
}

func (s *OrContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitOr(s)
	}
}

func (s *OrContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitOr(s)

	default:
		return t.VisitChildren(s)
	}
}

type InContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewInContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *InContext {
	p := new(InContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *InContext) GetOp() antlr.Token { return s.op }

func (s *InContext) SetOp(v antlr.Token) { s.op = v }

func (s *InContext) GetLeft() IExprContext { return s.left }

func (s *InContext) GetRight() IExprContext { return s.right }

func (s *InContext) SetLeft(v IExprContext) { s.left = v }

func (s *InContext) SetRight(v IExprContext) { s.right = v }

func (s *InContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *InContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *InContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *InContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterIn(s)
	}
}

func (s *InContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitIn(s)
	}
}

func (s *InContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitIn(s)

	default:
		return t.VisitChildren(s)
	}
}

type MatchContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewMatchContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *MatchContext {
	p := new(MatchContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *MatchContext) GetOp() antlr.Token { return s.op }

func (s *MatchContext) SetOp(v antlr.Token) { s.op = v }

func (s *MatchContext) GetLeft() IExprContext { return s.left }

func (s *MatchContext) GetRight() IExprContext { return s.right }

func (s *MatchContext) SetLeft(v IExprContext) { s.left = v }

func (s *MatchContext) SetRight(v IExprContext) { s.right = v }

func (s *MatchContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *MatchContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *MatchContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *MatchContext) MATCH() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserMATCH, 0)
}

func (s *MatchContext) NOTMATCH() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserNOTMATCH, 0)
}

func (s *MatchContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterMatch(s)
	}
}

func (s *MatchContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitMatch(s)
	}
}

func (s *MatchContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitMatch(s)

	default:
		return t.VisitChildren(s)
	}
}

type ListContext struct {
	ExprContext
	_expr  IExprContext
	values []IExprContext
}

func NewListContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *ListContext {
	p := new(ListContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *ListContext) Get_expr() IExprContext { return s._expr }

func (s *ListContext) Set_expr(v IExprContext) { s._expr = v }

func (s *ListContext) GetValues() []IExprContext { return s.values }

func (s *ListContext) SetValues(v []IExprContext) { s.values = v }

func (s *ListContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ListContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *ListContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *ListContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *ListContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *ListContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *ListContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *ListContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterList(s)
	}
}

func (s *ListContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitList(s)
	}
}

func (s *ListContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitList(s)

	default:
		return t.VisitChildren(s)
	}
}

type MuldivContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewMuldivContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *MuldivContext {
	p := new(MuldivContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *MuldivContext) GetOp() antlr.Token { return s.op }

func (s *MuldivContext) SetOp(v antlr.Token) { s.op = v }

func (s *MuldivContext) GetLeft() IExprContext { return s.left }

func (s *MuldivContext) GetRight() IExprContext { return s.right }

func (s *MuldivContext) SetLeft(v IExprContext) { s.left = v }

func (s *MuldivContext) SetRight(v IExprContext) { s.right = v }

func (s *MuldivContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *MuldivContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *MuldivContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *MuldivContext) STAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTAR, 0)
}

func (s *MuldivContext) SLASH() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSLASH, 0)
}

func (s *MuldivContext) PERCENT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserPERCENT, 0)
}

func (s *MuldivContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterMuldiv(s)
	}
}

func (s *MuldivContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitMuldiv(s)
	}
}

func (s *MuldivContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitMuldiv(s)

	default:
		return t.VisitChildren(s)
	}
}

type FcallContext struct {
	ExprContext
	left   IExprContext
	op     antlr.Token
	name   antlr.Token
	args   IArgumentsContext
	params IParametersContext
	body   IExprContext
}

func NewFcallContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *FcallContext {
	p := new(FcallContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *FcallContext) GetOp() antlr.Token { return s.op }

func (s *FcallContext) GetName() antlr.Token { return s.name }

func (s *FcallContext) SetOp(v antlr.Token) { s.op = v }

func (s *FcallContext) SetName(v antlr.Token) { s.name = v }

func (s *FcallContext) GetLeft() IExprContext { return s.left }

func (s *FcallContext) GetArgs() IArgumentsContext { return s.args }

func (s *FcallContext) GetParams() IParametersContext { return s.params }

func (s *FcallContext) GetBody() IExprContext { return s.body }

func (s *FcallContext) SetLeft(v IExprContext) { s.left = v }

func (s *FcallContext) SetArgs(v IArgumentsContext) { s.args = v }

func (s *FcallContext) SetParams(v IParametersContext) { s.params = v }

func (s *FcallContext) SetBody(v IExprContext) { s.body = v }

func (s *FcallContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *FcallContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *FcallContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *FcallContext) ARROW() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserARROW, 0)
}

func (s *FcallContext) LC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLC_WORD, 0)
}

func (s *FcallContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *FcallContext) LBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACE, 0)
}

func (s *FcallContext) RBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACE, 0)
}

func (s *FcallContext) Arguments() IArgumentsContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IArgumentsContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IArgumentsContext)
}

func (s *FcallContext) Parameters() IParametersContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IParametersContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IParametersContext)
}

func (s *FcallContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterFcall(s)
	}
}

func (s *FcallContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitFcall(s)
	}
}

func (s *FcallContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitFcall(s)

	default:
		return t.VisitChildren(s)
	}
}

type NotContext struct {
	ExprContext
	op    antlr.Token
	right IExprContext
}

func NewNotContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *NotContext {
	p := new(NotContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *NotContext) GetOp() antlr.Token { return s.op }

func (s *NotContext) SetOp(v antlr.Token) { s.op = v }

func (s *NotContext) GetRight() IExprContext { return s.right }

func (s *NotContext) SetRight(v IExprContext) { s.right = v }

func (s *NotContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *NotContext) EXCLAMATION() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserEXCLAMATION, 0)
}

func (s *NotContext) Expr() IExprContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *NotContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterNot(s)
	}
}

func (s *NotContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitNot(s)
	}
}

func (s *NotContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitNot(s)

	default:
		return t.VisitChildren(s)
	}
}

type AtContext struct {
	ExprContext
	left  IExprContext
	_expr IExprContext
	right []IExprContext
}

func NewAtContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *AtContext {
	p := new(AtContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *AtContext) GetLeft() IExprContext { return s.left }

func (s *AtContext) Get_expr() IExprContext { return s._expr }

func (s *AtContext) SetLeft(v IExprContext) { s.left = v }

func (s *AtContext) Set_expr(v IExprContext) { s._expr = v }

func (s *AtContext) GetRight() []IExprContext { return s.right }

func (s *AtContext) SetRight(v []IExprContext) { s.right = v }

func (s *AtContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *AtContext) LBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACK, 0)
}

func (s *AtContext) RBRACK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACK, 0)
}

func (s *AtContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *AtContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *AtContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *AtContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *AtContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAt(s)
	}
}

func (s *AtContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAt(s)
	}
}

func (s *AtContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAt(s)

	default:
		return t.VisitChildren(s)
	}
}

type RelationNameContext struct {
	ExprContext
	left antlr.Token
}

func NewRelationNameContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *RelationNameContext {
	p := new(RelationNameContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *RelationNameContext) GetLeft() antlr.Token { return s.left }

func (s *RelationNameContext) SetLeft(v antlr.Token) { s.left = v }

func (s *RelationNameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *RelationNameContext) UC_WORD() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUC_WORD, 0)
}

func (s *RelationNameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterRelationName(s)
	}
}

func (s *RelationNameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitRelationName(s)
	}
}

func (s *RelationNameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitRelationName(s)

	default:
		return t.VisitChildren(s)
	}
}

type AndContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewAndContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *AndContext {
	p := new(AndContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *AndContext) GetOp() antlr.Token { return s.op }

func (s *AndContext) SetOp(v antlr.Token) { s.op = v }

func (s *AndContext) GetLeft() IExprContext { return s.left }

func (s *AndContext) GetRight() IExprContext { return s.right }

func (s *AndContext) SetLeft(v IExprContext) { s.left = v }

func (s *AndContext) SetRight(v IExprContext) { s.right = v }

func (s *AndContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *AndContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *AndContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *AndContext) AND() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserAND, 0)
}

func (s *AndContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterAnd(s)
	}
}

func (s *AndContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitAnd(s)
	}
}

func (s *AndContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitAnd(s)

	default:
		return t.VisitChildren(s)
	}
}

type VariableContext struct {
	ExprContext
	left antlr.Token
}

func NewVariableContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *VariableContext {
	p := new(VariableContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *VariableContext) GetLeft() antlr.Token { return s.left }

func (s *VariableContext) SetLeft(v antlr.Token) { s.left = v }

func (s *VariableContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *VariableContext) VARIABLE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserVARIABLE, 0)
}

func (s *VariableContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterVariable(s)
	}
}

func (s *VariableContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitVariable(s)
	}
}

func (s *VariableContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitVariable(s)

	default:
		return t.VisitChildren(s)
	}
}

type NameContext struct {
	ExprContext
	left IProperty_nameContext
}

func NewNameContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *NameContext {
	p := new(NameContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *NameContext) GetLeft() IProperty_nameContext { return s.left }

func (s *NameContext) SetLeft(v IProperty_nameContext) { s.left = v }

func (s *NameContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *NameContext) Property_name() IProperty_nameContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IProperty_nameContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IProperty_nameContext)
}

func (s *NameContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterName(s)
	}
}

func (s *NameContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitName(s)
	}
}

func (s *NameContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitName(s)

	default:
		return t.VisitChildren(s)
	}
}

type ValueContext struct {
	ExprContext
	left ILiteralContext
}

func NewValueContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *ValueContext {
	p := new(ValueContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *ValueContext) GetLeft() ILiteralContext { return s.left }

func (s *ValueContext) SetLeft(v ILiteralContext) { s.left = v }

func (s *ValueContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ValueContext) Literal() ILiteralContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ILiteralContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ILiteralContext)
}

func (s *ValueContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterValue(s)
	}
}

func (s *ValueContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitValue(s)
	}
}

func (s *ValueContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitValue(s)

	default:
		return t.VisitChildren(s)
	}
}

type EqualityContext struct {
	ExprContext
	left  IExprContext
	op    antlr.Token
	right IExprContext
}

func NewEqualityContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *EqualityContext {
	p := new(EqualityContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *EqualityContext) GetOp() antlr.Token { return s.op }

func (s *EqualityContext) SetOp(v antlr.Token) { s.op = v }

func (s *EqualityContext) GetLeft() IExprContext { return s.left }

func (s *EqualityContext) GetRight() IExprContext { return s.right }

func (s *EqualityContext) SetLeft(v IExprContext) { s.left = v }

func (s *EqualityContext) SetRight(v IExprContext) { s.right = v }

func (s *EqualityContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *EqualityContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *EqualityContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *EqualityContext) EQUAL() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserEQUAL, 0)
}

func (s *EqualityContext) NOTEQUAL() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserNOTEQUAL, 0)
}

func (s *EqualityContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterEquality(s)
	}
}

func (s *EqualityContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitEquality(s)
	}
}

func (s *EqualityContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitEquality(s)

	default:
		return t.VisitChildren(s)
	}
}

type IfContext struct {
	ExprContext
	left        IExprContext
	op          antlr.Token
	trueBranch  IExprContext
	falseBranch IExprContext
}

func NewIfContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *IfContext {
	p := new(IfContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *IfContext) GetOp() antlr.Token { return s.op }

func (s *IfContext) SetOp(v antlr.Token) { s.op = v }

func (s *IfContext) GetLeft() IExprContext { return s.left }

func (s *IfContext) GetTrueBranch() IExprContext { return s.trueBranch }

func (s *IfContext) GetFalseBranch() IExprContext { return s.falseBranch }

func (s *IfContext) SetLeft(v IExprContext) { s.left = v }

func (s *IfContext) SetTrueBranch(v IExprContext) { s.trueBranch = v }

func (s *IfContext) SetFalseBranch(v IExprContext) { s.falseBranch = v }

func (s *IfContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *IfContext) LBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLBRACE, 0)
}

func (s *IfContext) RBRACE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRBRACE, 0)
}

func (s *IfContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *IfContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *IfContext) QMARK() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserQMARK, 0)
}

func (s *IfContext) COLON() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOLON, 0)
}

func (s *IfContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterIf(s)
	}
}

func (s *IfContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitIf(s)
	}
}

func (s *IfContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitIf(s)

	default:
		return t.VisitChildren(s)
	}
}

type LiteralNilContext struct {
	ExprContext
}

func NewLiteralNilContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *LiteralNilContext {
	p := new(LiteralNilContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *LiteralNilContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *LiteralNilContext) USCORE() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserUSCORE, 0)
}

func (s *LiteralNilContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterLiteralNil(s)
	}
}

func (s *LiteralNilContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitLiteralNil(s)
	}
}

func (s *LiteralNilContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitLiteralNil(s)

	default:
		return t.VisitChildren(s)
	}
}

type GroupContext struct {
	ExprContext
	left IExprContext
}

func NewGroupContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *GroupContext {
	p := new(GroupContext)

	InitEmptyExprContext(&p.ExprContext)
	p.parser = parser
	p.CopyAll(ctx.(*ExprContext))

	return p
}

func (s *GroupContext) GetLeft() IExprContext { return s.left }

func (s *GroupContext) SetLeft(v IExprContext) { s.left = v }

func (s *GroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *GroupContext) LPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLPAR, 0)
}

func (s *GroupContext) RPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRPAR, 0)
}

func (s *GroupContext) Expr() IExprContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *GroupContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterGroup(s)
	}
}

func (s *GroupContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitGroup(s)
	}
}

func (s *GroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Expr() (localctx IExprContext) {
	return p.expr(0)
}

func (p *YammmGrammarParser) expr(_p int) (localctx IExprContext) {
	var _parentctx antlr.ParserRuleContext = p.GetParserRuleContext()

	_parentState := p.GetState()
	localctx = NewExprContext(p, p.GetParserRuleContext(), _parentState)
	var _prevctx IExprContext = localctx
	var _ antlr.ParserRuleContext = _prevctx // TODO: To prevent unused variable warning.
	_startState := 76
	p.EnterRecursionRule(localctx, 76, YammmGrammarParserRULE_expr, _p)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(442)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case YammmGrammarParserSTRING, YammmGrammarParserREGEXP, YammmGrammarParserINTEGER, YammmGrammarParserFLOAT, YammmGrammarParserBOOLEAN:
		localctx = NewValueContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx

		{
			p.SetState(413)

			_x := p.Literal()

			localctx.(*ValueContext).left = _x
		}

	case YammmGrammarParserLBRACK:
		localctx = NewListContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(414)
			p.Match(YammmGrammarParserLBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(426)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&-9223282973602742314) != 0) || ((int64((_la-66)) & ^0x3f) == 0 && ((int64(1)<<(_la-66))&477) != 0) {
			{
				p.SetState(415)

				_x := p.expr(0)

				localctx.(*ListContext)._expr = _x
			}
			localctx.(*ListContext).values = append(localctx.(*ListContext).values, localctx.(*ListContext)._expr)
			p.SetState(420)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 56, p.GetParserRuleContext())
			if p.HasError() {
				goto errorExit
			}
			for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
				if _alt == 1 {
					{
						p.SetState(416)
						p.Match(YammmGrammarParserCOMMA)
						if p.HasError() {
							// Recognition error - abort rule
							goto errorExit
						}
					}
					{
						p.SetState(417)

						_x := p.expr(0)

						localctx.(*ListContext)._expr = _x
					}
					localctx.(*ListContext).values = append(localctx.(*ListContext).values, localctx.(*ListContext)._expr)

				}
				p.SetState(422)
				p.GetErrorHandler().Sync(p)
				if p.HasError() {
					goto errorExit
				}
				_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 56, p.GetParserRuleContext())
				if p.HasError() {
					goto errorExit
				}
			}
			p.SetState(424)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_la = p.GetTokenStream().LA(1)

			if _la == YammmGrammarParserCOMMA {
				{
					p.SetState(423)
					p.Match(YammmGrammarParserCOMMA)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
			}

		}
		{
			p.SetState(428)
			p.Match(YammmGrammarParserRBRACK)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserMINUS:
		localctx = NewUminusContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(429)

			_m := p.Match(YammmGrammarParserMINUS)

			localctx.(*UminusContext).op = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(430)

			_x := p.expr(20)

			localctx.(*UminusContext).right = _x
		}

	case YammmGrammarParserEXCLAMATION:
		localctx = NewNotContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(431)

			_m := p.Match(YammmGrammarParserEXCLAMATION)

			localctx.(*NotContext).op = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(432)

			_x := p.expr(16)

			localctx.(*NotContext).right = _x
		}

	case YammmGrammarParserLPAR:
		localctx = NewGroupContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(433)
			p.Match(YammmGrammarParserLPAR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(434)

			_x := p.expr(0)

			localctx.(*GroupContext).left = _x
		}
		{
			p.SetState(435)
			p.Match(YammmGrammarParserRPAR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserVARIABLE:
		localctx = NewVariableContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(437)

			_m := p.Match(YammmGrammarParserVARIABLE)

			localctx.(*VariableContext).left = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserT__0, YammmGrammarParserT__1, YammmGrammarParserT__3, YammmGrammarParserT__5, YammmGrammarParserT__6, YammmGrammarParserT__7, YammmGrammarParserT__8, YammmGrammarParserT__9, YammmGrammarParserT__10, YammmGrammarParserT__24, YammmGrammarParserT__25, YammmGrammarParserLC_WORD:
		localctx = NewNameContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(438)

			_x := p.Property_name()

			localctx.(*NameContext).left = _x
		}

	case YammmGrammarParserT__11, YammmGrammarParserT__12, YammmGrammarParserT__13, YammmGrammarParserT__14, YammmGrammarParserT__15, YammmGrammarParserT__16, YammmGrammarParserT__17, YammmGrammarParserT__18, YammmGrammarParserT__19, YammmGrammarParserT__20, YammmGrammarParserT__21:
		localctx = NewDatatypeNameContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(439)

			_x := p.DatatypeKeyword()

			localctx.(*DatatypeNameContext).left = _x
		}

	case YammmGrammarParserUC_WORD:
		localctx = NewRelationNameContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(440)

			_m := p.Match(YammmGrammarParserUC_WORD)

			localctx.(*RelationNameContext).left = _m
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case YammmGrammarParserT__23, YammmGrammarParserUSCORE:
		localctx = NewLiteralNilContext(p, localctx)
		p.SetParserRuleContext(localctx)
		_prevctx = localctx
		{
			p.SetState(441)
			_la = p.GetTokenStream().LA(1)

			if !(_la == YammmGrammarParserT__23 || _la == YammmGrammarParserUSCORE) {
				p.GetErrorHandler().RecoverInline(p)
			} else {
				p.GetErrorHandler().ReportMatch(p)
				p.Consume()
			}
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}
	p.GetParserRuleContext().SetStop(p.GetTokenStream().LT(-1))
	p.SetState(514)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 68, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			if p.GetParseListeners() != nil {
				p.TriggerExitRuleEvent()
			}
			_prevctx = localctx
			p.SetState(512)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}

			switch p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 67, p.GetParserRuleContext()) {
			case 1:
				localctx = NewPeriodContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*PeriodContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(444)

				if !p.Precpred(p.GetParserRuleContext(), 17) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 17)", ""))
					goto errorExit
				}
				{
					p.SetState(445)
					p.Match(YammmGrammarParserPERIOD)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(446)

					_x := p.expr(18)

					localctx.(*PeriodContext).name = _x
				}

			case 2:
				localctx = NewMuldivContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*MuldivContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(447)

				if !p.Precpred(p.GetParserRuleContext(), 15) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 15)", ""))
					goto errorExit
				}
				{
					p.SetState(448)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*MuldivContext).op = _lt

					_la = p.GetTokenStream().LA(1)

					if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&2305845757992763392) != 0) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*MuldivContext).op = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				{
					p.SetState(449)

					_x := p.expr(16)

					localctx.(*MuldivContext).right = _x
				}

			case 3:
				localctx = NewPlusminusContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*PlusminusContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(450)

				if !p.Precpred(p.GetParserRuleContext(), 14) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 14)", ""))
					goto errorExit
				}
				{
					p.SetState(451)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*PlusminusContext).op = _lt

					_la = p.GetTokenStream().LA(1)

					if !(_la == YammmGrammarParserPLUS || _la == YammmGrammarParserMINUS) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*PlusminusContext).op = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				{
					p.SetState(452)

					_x := p.expr(15)

					localctx.(*PlusminusContext).right = _x
				}

			case 4:
				localctx = NewCompareContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*CompareContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(453)

				if !p.Precpred(p.GetParserRuleContext(), 13) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 13)", ""))
					goto errorExit
				}
				{
					p.SetState(454)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*CompareContext).op = _lt

					_la = p.GetTokenStream().LA(1)

					if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&270215977642229760) != 0) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*CompareContext).op = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				{
					p.SetState(455)

					_x := p.expr(14)

					localctx.(*CompareContext).right = _x
				}

			case 5:
				localctx = NewInContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*InContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(456)

				if !p.Precpred(p.GetParserRuleContext(), 12) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 12)", ""))
					goto errorExit
				}
				{
					p.SetState(457)

					_m := p.Match(YammmGrammarParserT__22)

					localctx.(*InContext).op = _m
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(458)

					_x := p.expr(13)

					localctx.(*InContext).right = _x
				}

			case 6:
				localctx = NewMatchContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*MatchContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(459)

				if !p.Precpred(p.GetParserRuleContext(), 11) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 11)", ""))
					goto errorExit
				}
				{
					p.SetState(460)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*MatchContext).op = _lt

					_la = p.GetTokenStream().LA(1)

					if !(_la == YammmGrammarParserMATCH || _la == YammmGrammarParserNOTMATCH) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*MatchContext).op = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				{
					p.SetState(461)

					_x := p.expr(12)

					localctx.(*MatchContext).right = _x
				}

			case 7:
				localctx = NewEqualityContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*EqualityContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(462)

				if !p.Precpred(p.GetParserRuleContext(), 10) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 10)", ""))
					goto errorExit
				}
				{
					p.SetState(463)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*EqualityContext).op = _lt

					_la = p.GetTokenStream().LA(1)

					if !(_la == YammmGrammarParserEQUAL || _la == YammmGrammarParserNOTEQUAL) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*EqualityContext).op = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				{
					p.SetState(464)

					_x := p.expr(11)

					localctx.(*EqualityContext).right = _x
				}

			case 8:
				localctx = NewAndContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*AndContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(465)

				if !p.Precpred(p.GetParserRuleContext(), 9) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 9)", ""))
					goto errorExit
				}
				{
					p.SetState(466)

					_m := p.Match(YammmGrammarParserAND)

					localctx.(*AndContext).op = _m
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(467)

					_x := p.expr(10)

					localctx.(*AndContext).right = _x
				}

			case 9:
				localctx = NewOrContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*OrContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(468)

				if !p.Precpred(p.GetParserRuleContext(), 8) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 8)", ""))
					goto errorExit
				}
				{
					p.SetState(469)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*OrContext).op = _lt

					_la = p.GetTokenStream().LA(1)

					if !(_la == YammmGrammarParserOR || _la == YammmGrammarParserHAT) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*OrContext).op = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				{
					p.SetState(470)

					_x := p.expr(9)

					localctx.(*OrContext).right = _x
				}

			case 10:
				localctx = NewAtContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*AtContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(471)

				if !p.Precpred(p.GetParserRuleContext(), 19) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 19)", ""))
					goto errorExit
				}
				{
					p.SetState(472)
					p.Match(YammmGrammarParserLBRACK)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				p.SetState(484)
				p.GetErrorHandler().Sync(p)
				if p.HasError() {
					goto errorExit
				}
				_la = p.GetTokenStream().LA(1)

				if ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&-9223282973602742314) != 0) || ((int64((_la-66)) & ^0x3f) == 0 && ((int64(1)<<(_la-66))&477) != 0) {
					{
						p.SetState(473)

						_x := p.expr(0)

						localctx.(*AtContext)._expr = _x
					}
					localctx.(*AtContext).right = append(localctx.(*AtContext).right, localctx.(*AtContext)._expr)
					p.SetState(478)
					p.GetErrorHandler().Sync(p)
					if p.HasError() {
						goto errorExit
					}
					_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 60, p.GetParserRuleContext())
					if p.HasError() {
						goto errorExit
					}
					for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
						if _alt == 1 {
							{
								p.SetState(474)
								p.Match(YammmGrammarParserCOMMA)
								if p.HasError() {
									// Recognition error - abort rule
									goto errorExit
								}
							}
							{
								p.SetState(475)

								_x := p.expr(0)

								localctx.(*AtContext)._expr = _x
							}
							localctx.(*AtContext).right = append(localctx.(*AtContext).right, localctx.(*AtContext)._expr)

						}
						p.SetState(480)
						p.GetErrorHandler().Sync(p)
						if p.HasError() {
							goto errorExit
						}
						_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 60, p.GetParserRuleContext())
						if p.HasError() {
							goto errorExit
						}
					}
					p.SetState(482)
					p.GetErrorHandler().Sync(p)
					if p.HasError() {
						goto errorExit
					}
					_la = p.GetTokenStream().LA(1)

					if _la == YammmGrammarParserCOMMA {
						{
							p.SetState(481)
							p.Match(YammmGrammarParserCOMMA)
							if p.HasError() {
								// Recognition error - abort rule
								goto errorExit
							}
						}
					}

				}
				{
					p.SetState(486)
					p.Match(YammmGrammarParserRBRACK)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}

			case 11:
				localctx = NewFcallContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*FcallContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(487)

				if !p.Precpred(p.GetParserRuleContext(), 18) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 18)", ""))
					goto errorExit
				}
				{
					p.SetState(488)

					_m := p.Match(YammmGrammarParserARROW)

					localctx.(*FcallContext).op = _m
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(489)

					_lt := p.GetTokenStream().LT(1)

					localctx.(*FcallContext).name = _lt

					_la = p.GetTokenStream().LA(1)

					if !(_la == YammmGrammarParserUC_WORD || _la == YammmGrammarParserLC_WORD) {
						_ri := p.GetErrorHandler().RecoverInline(p)

						localctx.(*FcallContext).name = _ri
					} else {
						p.GetErrorHandler().ReportMatch(p)
						p.Consume()
					}
				}
				p.SetState(491)
				p.GetErrorHandler().Sync(p)

				if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 63, p.GetParserRuleContext()) == 1 {
					{
						p.SetState(490)

						_x := p.Arguments()

						localctx.(*FcallContext).args = _x
					}
				} else if p.HasError() { // JIM
					goto errorExit
				}
				p.SetState(494)
				p.GetErrorHandler().Sync(p)

				if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 64, p.GetParserRuleContext()) == 1 {
					{
						p.SetState(493)

						_x := p.Parameters()

						localctx.(*FcallContext).params = _x
					}
				} else if p.HasError() { // JIM
					goto errorExit
				}
				p.SetState(500)
				p.GetErrorHandler().Sync(p)

				if p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 65, p.GetParserRuleContext()) == 1 {
					{
						p.SetState(496)
						p.Match(YammmGrammarParserLBRACE)
						if p.HasError() {
							// Recognition error - abort rule
							goto errorExit
						}
					}
					{
						p.SetState(497)

						_x := p.expr(0)

						localctx.(*FcallContext).body = _x
					}
					{
						p.SetState(498)
						p.Match(YammmGrammarParserRBRACE)
						if p.HasError() {
							// Recognition error - abort rule
							goto errorExit
						}
					}

				} else if p.HasError() { // JIM
					goto errorExit
				}

			case 12:
				localctx = NewIfContext(p, NewExprContext(p, _parentctx, _parentState))
				localctx.(*IfContext).left = _prevctx

				p.PushNewRecursionContext(localctx, _startState, YammmGrammarParserRULE_expr)
				p.SetState(502)

				if !p.Precpred(p.GetParserRuleContext(), 7) {
					p.SetError(antlr.NewFailedPredicateException(p, "p.Precpred(p.GetParserRuleContext(), 7)", ""))
					goto errorExit
				}
				{
					p.SetState(503)

					_m := p.Match(YammmGrammarParserQMARK)

					localctx.(*IfContext).op = _m
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(504)
					p.Match(YammmGrammarParserLBRACE)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(505)

					_x := p.expr(0)

					localctx.(*IfContext).trueBranch = _x
				}
				p.SetState(508)
				p.GetErrorHandler().Sync(p)
				if p.HasError() {
					goto errorExit
				}
				_la = p.GetTokenStream().LA(1)

				if _la == YammmGrammarParserCOLON {
					{
						p.SetState(506)
						p.Match(YammmGrammarParserCOLON)
						if p.HasError() {
							// Recognition error - abort rule
							goto errorExit
						}
					}
					{
						p.SetState(507)

						_x := p.expr(0)

						localctx.(*IfContext).falseBranch = _x
					}

				}
				{
					p.SetState(510)
					p.Match(YammmGrammarParserRBRACE)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}

			case antlr.ATNInvalidAltNumber:
				goto errorExit
			}

		}
		p.SetState(516)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 68, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.UnrollRecursionContexts(_parentctx)
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IArgumentsContext is an interface to support dynamic dispatch.
type IArgumentsContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Get_expr returns the _expr rule contexts.
	Get_expr() IExprContext

	// Set_expr sets the _expr rule contexts.
	Set_expr(IExprContext)

	// GetArgs returns the args rule context list.
	GetArgs() []IExprContext

	// SetArgs sets the args rule context list.
	SetArgs([]IExprContext)

	// Getter signatures
	LPAR() antlr.TerminalNode
	RPAR() antlr.TerminalNode
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode
	AllExpr() []IExprContext
	Expr(i int) IExprContext

	// IsArgumentsContext differentiates from other interfaces.
	IsArgumentsContext()
}

type ArgumentsContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	_expr  IExprContext
	args   []IExprContext
}

func NewEmptyArgumentsContext() *ArgumentsContext {
	p := new(ArgumentsContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_arguments
	return p
}

func InitEmptyArgumentsContext(p *ArgumentsContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_arguments
}

func (*ArgumentsContext) IsArgumentsContext() {}

func NewArgumentsContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ArgumentsContext {
	p := new(ArgumentsContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_arguments

	return p
}

func (s *ArgumentsContext) GetParser() antlr.Parser { return s.parser }

func (s *ArgumentsContext) Get_expr() IExprContext { return s._expr }

func (s *ArgumentsContext) Set_expr(v IExprContext) { s._expr = v }

func (s *ArgumentsContext) GetArgs() []IExprContext { return s.args }

func (s *ArgumentsContext) SetArgs(v []IExprContext) { s.args = v }

func (s *ArgumentsContext) LPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserLPAR, 0)
}

func (s *ArgumentsContext) RPAR() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserRPAR, 0)
}

func (s *ArgumentsContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *ArgumentsContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *ArgumentsContext) AllExpr() []IExprContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExprContext); ok {
			len++
		}
	}

	tst := make([]IExprContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExprContext); ok {
			tst[i] = t.(IExprContext)
			i++
		}
	}

	return tst
}

func (s *ArgumentsContext) Expr(i int) IExprContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExprContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExprContext)
}

func (s *ArgumentsContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ArgumentsContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ArgumentsContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterArguments(s)
	}
}

func (s *ArgumentsContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitArguments(s)
	}
}

func (s *ArgumentsContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitArguments(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Arguments() (localctx IArgumentsContext) {
	localctx = NewArgumentsContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 78, YammmGrammarParserRULE_arguments)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(517)
		p.Match(YammmGrammarParserLPAR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(526)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&-9223282973602742314) != 0) || ((int64((_la-66)) & ^0x3f) == 0 && ((int64(1)<<(_la-66))&477) != 0) {
		{
			p.SetState(518)

			_x := p.expr(0)

			localctx.(*ArgumentsContext)._expr = _x
		}
		localctx.(*ArgumentsContext).args = append(localctx.(*ArgumentsContext).args, localctx.(*ArgumentsContext)._expr)
		p.SetState(523)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 69, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
		for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
			if _alt == 1 {
				{
					p.SetState(519)
					p.Match(YammmGrammarParserCOMMA)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}
				{
					p.SetState(520)

					_x := p.expr(0)

					localctx.(*ArgumentsContext)._expr = _x
				}
				localctx.(*ArgumentsContext).args = append(localctx.(*ArgumentsContext).args, localctx.(*ArgumentsContext)._expr)

			}
			p.SetState(525)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 69, p.GetParserRuleContext())
			if p.HasError() {
				goto errorExit
			}
		}

	}
	p.SetState(529)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserCOMMA {
		{
			p.SetState(528)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(531)
		p.Match(YammmGrammarParserRPAR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IParametersContext is an interface to support dynamic dispatch.
type IParametersContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Get_VARIABLE returns the _VARIABLE token.
	Get_VARIABLE() antlr.Token

	// Set_VARIABLE sets the _VARIABLE token.
	Set_VARIABLE(antlr.Token)

	// GetParams returns the params token list.
	GetParams() []antlr.Token

	// SetParams sets the params token list.
	SetParams([]antlr.Token)

	// Getter signatures
	AllPIPE() []antlr.TerminalNode
	PIPE(i int) antlr.TerminalNode
	AllVARIABLE() []antlr.TerminalNode
	VARIABLE(i int) antlr.TerminalNode
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode

	// IsParametersContext differentiates from other interfaces.
	IsParametersContext()
}

type ParametersContext struct {
	antlr.BaseParserRuleContext
	parser    antlr.Parser
	_VARIABLE antlr.Token
	params    []antlr.Token
}

func NewEmptyParametersContext() *ParametersContext {
	p := new(ParametersContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_parameters
	return p
}

func InitEmptyParametersContext(p *ParametersContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_parameters
}

func (*ParametersContext) IsParametersContext() {}

func NewParametersContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ParametersContext {
	p := new(ParametersContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_parameters

	return p
}

func (s *ParametersContext) GetParser() antlr.Parser { return s.parser }

func (s *ParametersContext) Get_VARIABLE() antlr.Token { return s._VARIABLE }

func (s *ParametersContext) Set_VARIABLE(v antlr.Token) { s._VARIABLE = v }

func (s *ParametersContext) GetParams() []antlr.Token { return s.params }

func (s *ParametersContext) SetParams(v []antlr.Token) { s.params = v }

func (s *ParametersContext) AllPIPE() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserPIPE)
}

func (s *ParametersContext) PIPE(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserPIPE, i)
}

func (s *ParametersContext) AllVARIABLE() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserVARIABLE)
}

func (s *ParametersContext) VARIABLE(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserVARIABLE, i)
}

func (s *ParametersContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(YammmGrammarParserCOMMA)
}

func (s *ParametersContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserCOMMA, i)
}

func (s *ParametersContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ParametersContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ParametersContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterParameters(s)
	}
}

func (s *ParametersContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitParameters(s)
	}
}

func (s *ParametersContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitParameters(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Parameters() (localctx IParametersContext) {
	localctx = NewParametersContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 80, YammmGrammarParserRULE_parameters)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(533)
		p.Match(YammmGrammarParserPIPE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(534)

		_m := p.Match(YammmGrammarParserVARIABLE)

		localctx.(*ParametersContext)._VARIABLE = _m
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	localctx.(*ParametersContext).params = append(localctx.(*ParametersContext).params, localctx.(*ParametersContext)._VARIABLE)
	p.SetState(539)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 72, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			{
				p.SetState(535)
				p.Match(YammmGrammarParserCOMMA)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(536)

				_m := p.Match(YammmGrammarParserVARIABLE)

				localctx.(*ParametersContext)._VARIABLE = _m
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			localctx.(*ParametersContext).params = append(localctx.(*ParametersContext).params, localctx.(*ParametersContext)._VARIABLE)

		}
		p.SetState(541)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 72, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}
	p.SetState(543)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == YammmGrammarParserCOMMA {
		{
			p.SetState(542)
			p.Match(YammmGrammarParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}
	{
		p.SetState(545)
		p.Match(YammmGrammarParserPIPE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ILiteralContext is an interface to support dynamic dispatch.
type ILiteralContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// GetV returns the v token.
	GetV() antlr.Token

	// SetV sets the v token.
	SetV(antlr.Token)

	// Getter signatures
	STRING() antlr.TerminalNode
	BOOLEAN() antlr.TerminalNode
	FLOAT() antlr.TerminalNode
	INTEGER() antlr.TerminalNode
	REGEXP() antlr.TerminalNode

	// IsLiteralContext differentiates from other interfaces.
	IsLiteralContext()
}

type LiteralContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
	v      antlr.Token
}

func NewEmptyLiteralContext() *LiteralContext {
	p := new(LiteralContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_literal
	return p
}

func InitEmptyLiteralContext(p *LiteralContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_literal
}

func (*LiteralContext) IsLiteralContext() {}

func NewLiteralContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *LiteralContext {
	p := new(LiteralContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_literal

	return p
}

func (s *LiteralContext) GetParser() antlr.Parser { return s.parser }

func (s *LiteralContext) GetV() antlr.Token { return s.v }

func (s *LiteralContext) SetV(v antlr.Token) { s.v = v }

func (s *LiteralContext) STRING() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserSTRING, 0)
}

func (s *LiteralContext) BOOLEAN() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserBOOLEAN, 0)
}

func (s *LiteralContext) FLOAT() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserFLOAT, 0)
}

func (s *LiteralContext) INTEGER() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserINTEGER, 0)
}

func (s *LiteralContext) REGEXP() antlr.TerminalNode {
	return s.GetToken(YammmGrammarParserREGEXP, 0)
}

func (s *LiteralContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *LiteralContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *LiteralContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterLiteral(s)
	}
}

func (s *LiteralContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitLiteral(s)
	}
}

func (s *LiteralContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitLiteral(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Literal() (localctx ILiteralContext) {
	localctx = NewLiteralContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 82, YammmGrammarParserRULE_literal)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(547)

		_lt := p.GetTokenStream().LT(1)

		localctx.(*LiteralContext).v = _lt

		_la = p.GetTokenStream().LA(1)

		if !((int64((_la-63)) & ^0x3f) == 0 && ((int64(1)<<(_la-63))&713) != 0) {
			_ri := p.GetErrorHandler().RecoverInline(p)

			localctx.(*LiteralContext).v = _ri
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ILc_keywordContext is an interface to support dynamic dispatch.
type ILc_keywordContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsLc_keywordContext differentiates from other interfaces.
	IsLc_keywordContext()
}

type Lc_keywordContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyLc_keywordContext() *Lc_keywordContext {
	p := new(Lc_keywordContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_lc_keyword
	return p
}

func InitEmptyLc_keywordContext(p *Lc_keywordContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = YammmGrammarParserRULE_lc_keyword
}

func (*Lc_keywordContext) IsLc_keywordContext() {}

func NewLc_keywordContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *Lc_keywordContext {
	p := new(Lc_keywordContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = YammmGrammarParserRULE_lc_keyword

	return p
}

func (s *Lc_keywordContext) GetParser() antlr.Parser { return s.parser }
func (s *Lc_keywordContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *Lc_keywordContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *Lc_keywordContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.EnterLc_keyword(s)
	}
}

func (s *Lc_keywordContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(YammmGrammarListener); ok {
		listenerT.ExitLc_keyword(s)
	}
}

func (s *Lc_keywordContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case YammmGrammarVisitor:
		return t.VisitLc_keyword(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *YammmGrammarParser) Lc_keyword() (localctx ILc_keywordContext) {
	localctx = NewLc_keywordContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 84, YammmGrammarParserRULE_lc_keyword)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(549)
		_la = p.GetTokenStream().LA(1)

		if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&100667350) != 0) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

func (p *YammmGrammarParser) Sempred(localctx antlr.RuleContext, ruleIndex, predIndex int) bool {
	switch ruleIndex {
	case 38:
		var t *ExprContext = nil
		if localctx != nil {
			t = localctx.(*ExprContext)
		}
		return p.Expr_Sempred(t, predIndex)

	default:
		panic("No predicate with index: " + fmt.Sprint(ruleIndex))
	}
}

func (p *YammmGrammarParser) Expr_Sempred(localctx antlr.RuleContext, predIndex int) bool {
	switch predIndex {
	case 0:
		return p.Precpred(p.GetParserRuleContext(), 17)

	case 1:
		return p.Precpred(p.GetParserRuleContext(), 15)

	case 2:
		return p.Precpred(p.GetParserRuleContext(), 14)

	case 3:
		return p.Precpred(p.GetParserRuleContext(), 13)

	case 4:
		return p.Precpred(p.GetParserRuleContext(), 12)

	case 5:
		return p.Precpred(p.GetParserRuleContext(), 11)

	case 6:
		return p.Precpred(p.GetParserRuleContext(), 10)

	case 7:
		return p.Precpred(p.GetParserRuleContext(), 9)

	case 8:
		return p.Precpred(p.GetParserRuleContext(), 8)

	case 9:
		return p.Precpred(p.GetParserRuleContext(), 19)

	case 10:
		return p.Precpred(p.GetParserRuleContext(), 18)

	case 11:
		return p.Precpred(p.GetParserRuleContext(), 7)

	default:
		panic("No predicate with index: " + fmt.Sprint(predIndex))
	}
}

package diag

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestCode_String(t *testing.T) {
	tests := []struct {
		code Code
		want string
	}{
		{E_LIMIT_REACHED, "E_LIMIT_REACHED"},
		{E_INTERNAL, "E_INTERNAL"},
		{E_TYPE_COLLISION, "E_TYPE_COLLISION"},
		{E_SYNTAX, "E_SYNTAX"},
		{E_IMPORT_CYCLE, "E_IMPORT_CYCLE"},
		{E_TYPE_MISMATCH, "E_TYPE_MISMATCH"},
		{E_DUPLICATE_PK, "E_DUPLICATE_PK"},
		{E_ADAPTER_PARSE, "E_ADAPTER_PARSE"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Errorf("Code.String() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestCode_Category(t *testing.T) {
	tests := []struct {
		code Code
		want CodeCategory
	}{
		{E_LIMIT_REACHED, CategorySentinel},
		{E_INTERNAL, CategorySentinel},
		{E_TYPE_COLLISION, CategorySchema},
		{E_INHERIT_CYCLE, CategorySchema},
		{E_SYNTAX, CategorySyntax},
		{E_IMPORT_RESOLVE, CategoryImport},
		{E_IMPORT_CYCLE, CategoryImport},
		{E_INSTANCE_TYPE_NOT_FOUND, CategoryInstance},
		{E_TYPE_MISMATCH, CategoryInstance},
		{E_DUPLICATE_PK, CategoryGraph},
		{E_UNRESOLVED_REQUIRED, CategoryGraph},
		{E_ADAPTER_PARSE, CategoryAdapter},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			if got := tt.code.Category(); got != tt.want {
				t.Errorf("%s.Category() = %s; want %s", tt.code, got, tt.want)
			}
		})
	}
}

func TestCode_IsZero(t *testing.T) {
	tests := []struct {
		name string
		code Code
		want bool
	}{
		{"zero value", Code{}, true},
		{"empty string value", code("", CategorySentinel), true},
		{"valid code", E_TYPE_COLLISION, false},
		{"sentinel code", E_LIMIT_REACHED, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.IsZero(); got != tt.want {
				t.Errorf("Code.IsZero() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestCodeCategory_String(t *testing.T) {
	tests := []struct {
		cat  CodeCategory
		want string
	}{
		{CategorySentinel, "sentinel"},
		{CategorySchema, "schema"},
		{CategorySyntax, "syntax"},
		{CategoryImport, "import"},
		{CategoryInstance, "instance"},
		{CategoryGraph, "graph"},
		{CategoryAdapter, "adapter"},
		{CodeCategory(255), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.cat.String(); got != tt.want {
				t.Errorf("CodeCategory(%d).String() = %q; want %q", tt.cat, got, tt.want)
			}
		})
	}
}

func TestAllCodes(t *testing.T) {
	codes := AllCodes()

	// Verify we have a reasonable number of codes
	if len(codes) < 40 {
		t.Errorf("AllCodes() returned %d codes; expected at least 40", len(codes))
	}

	// Verify the slice is a copy (modifications don't affect internal state)
	original := AllCodes()
	codes[0] = Code{}
	afterMod := AllCodes()
	if afterMod[0].IsZero() {
		t.Error("AllCodes() should return a copy, not the internal slice")
	}
	if original[0].IsZero() {
		t.Error("original should not be affected by modifications to copy")
	}
}

func TestAllCodes_Uniqueness(t *testing.T) {
	// Critical test: verify all code strings are unique
	codes := AllCodes()
	seen := make(map[string]Code)

	for _, c := range codes {
		str := c.String()
		if str == "" {
			t.Error("found code with empty string")
			continue
		}
		if prev, ok := seen[str]; ok {
			t.Errorf("duplicate code string %q: categories %s and %s",
				str, prev.Category(), c.Category())
		}
		seen[str] = c
	}

	// Verify count matches
	if len(seen) != len(codes) {
		t.Errorf("unique codes: %d, total codes: %d", len(seen), len(codes))
	}
}

func TestAllCodes_NoZeroValues(t *testing.T) {
	for _, c := range AllCodes() {
		if c.IsZero() {
			t.Errorf("AllCodes() contains zero-value code")
		}
	}
}

func TestCodesByCategory(t *testing.T) {
	tests := []struct {
		cat         CodeCategory
		minExpected int
		mustContain []Code
	}{
		{
			cat:         CategorySentinel,
			minExpected: 2,
			mustContain: []Code{E_LIMIT_REACHED, E_INTERNAL},
		},
		{
			cat:         CategorySchema,
			minExpected: 15,
			mustContain: []Code{E_TYPE_COLLISION, E_INHERIT_CYCLE, E_INVALID_NAME},
		},
		{
			cat:         CategorySyntax,
			minExpected: 1,
			mustContain: []Code{E_SYNTAX},
		},
		{
			cat:         CategoryImport,
			minExpected: 4,
			mustContain: []Code{E_IMPORT_RESOLVE, E_IMPORT_CYCLE},
		},
		{
			cat:         CategoryInstance,
			minExpected: 15,
			mustContain: []Code{E_TYPE_MISMATCH, E_MISSING_REQUIRED, E_CONSTRAINT_FAIL},
		},
		{
			cat:         CategoryGraph,
			minExpected: 5,
			mustContain: []Code{E_DUPLICATE_PK, E_UNRESOLVED_REQUIRED},
		},
		{
			cat:         CategoryAdapter,
			minExpected: 1,
			mustContain: []Code{E_ADAPTER_PARSE},
		},
	}

	for _, tt := range tests {
		t.Run(tt.cat.String(), func(t *testing.T) {
			codes := CodesByCategory(tt.cat)

			if len(codes) < tt.minExpected {
				t.Errorf("CodesByCategory(%s) returned %d codes; expected at least %d",
					tt.cat, len(codes), tt.minExpected)
			}

			// Verify all returned codes have the correct category
			for _, c := range codes {
				if c.Category() != tt.cat {
					t.Errorf("code %s has category %s; expected %s",
						c, c.Category(), tt.cat)
				}
			}

			// Verify must-contain codes are present
			codeSet := make(map[string]bool)
			for _, c := range codes {
				codeSet[c.String()] = true
			}
			for _, required := range tt.mustContain {
				if !codeSet[required.String()] {
					t.Errorf("CodesByCategory(%s) missing required code %s",
						tt.cat, required)
				}
			}
		})
	}
}

func TestCodesByCategory_ReturnsNewSlice(t *testing.T) {
	// Verify modifications don't affect internal state
	codes1 := CodesByCategory(CategorySchema)
	if len(codes1) == 0 {
		t.Skip("no schema codes to test with")
	}

	codes1[0] = Code{}
	codes2 := CodesByCategory(CategorySchema)

	if codes2[0].IsZero() {
		t.Error("CodesByCategory should return a new slice each time")
	}
}

func TestCodesByCategory_AllCategoriesCovered(t *testing.T) {
	// Verify every code in AllCodes appears in exactly one category
	allByCategory := make(map[string]bool)
	categories := []CodeCategory{
		CategorySentinel,
		CategorySchema,
		CategorySyntax,
		CategoryImport,
		CategoryInstance,
		CategoryGraph,
		CategoryAdapter,
	}

	for _, cat := range categories {
		for _, c := range CodesByCategory(cat) {
			if allByCategory[c.String()] {
				t.Errorf("code %s appears in multiple categories", c)
			}
			allByCategory[c.String()] = true
		}
	}

	for _, c := range AllCodes() {
		if !allByCategory[c.String()] {
			t.Errorf("code %s not returned by any CodesByCategory call", c)
		}
	}
}

// TestContractValidationCodesExist verifies that all 19 contract validation
// codes mentioned in the architecture doc are defined.
func TestContractValidationCodesExist(t *testing.T) {
	// These are codes specifically mentioned in the contracts/architecture
	requiredCodes := []struct {
		code     Code
		category CodeCategory
	}{
		// Sentinel
		{E_LIMIT_REACHED, CategorySentinel},
		{E_INTERNAL, CategorySentinel},
		// Schema - core validation
		{E_TYPE_COLLISION, CategorySchema},
		{E_INHERIT_CYCLE, CategorySchema},
		{E_SCHEMA_TYPE_NOT_FOUND, CategorySchema},
		{E_DUPLICATE_PROPERTY, CategorySchema},
		{E_DUPLICATE_RELATION, CategorySchema},
		{E_INVALID_NAME, CategorySchema},
		// Syntax
		{E_SYNTAX, CategorySyntax},
		// Import
		{E_IMPORT_RESOLVE, CategoryImport},
		{E_IMPORT_CYCLE, CategoryImport},
		// Instance - core validation
		{E_INSTANCE_TYPE_NOT_FOUND, CategoryInstance},
		{E_TYPE_MISMATCH, CategoryInstance},
		{E_MISSING_REQUIRED, CategoryInstance},
		{E_CONSTRAINT_FAIL, CategoryInstance},
		// Graph
		{E_DUPLICATE_PK, CategoryGraph},
		{E_UNRESOLVED_REQUIRED, CategoryGraph},
		// Adapter
		{E_ADAPTER_PARSE, CategoryAdapter},
	}

	for _, tc := range requiredCodes {
		t.Run(tc.code.String(), func(t *testing.T) {
			if tc.code.IsZero() {
				t.Errorf("code %s is zero", tc.code)
			}
			if tc.code.Category() != tc.category {
				t.Errorf("code %s has category %s; want %s",
					tc.code, tc.code.Category(), tc.category)
			}
		})
	}
}

// TestAllCodes_MatchesDefinedCodes uses AST parsing to verify that every
// exported E_* variable in code.go appears in allCodes exactly once.
// This prevents drift between code definitions and the allCodes slice.
func TestAllCodes_MatchesDefinedCodes(t *testing.T) {
	// Parse code.go to find all exported E_* variable declarations
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "code.go", nil, 0)
	if err != nil {
		t.Fatalf("failed to parse code.go: %v", err)
	}

	// Collect all E_* variable names from AST
	definedCodes := make(map[string]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			return true
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range valueSpec.Names {
				if strings.HasPrefix(name.Name, "E_") && name.IsExported() {
					definedCodes[name.Name] = true
				}
			}
		}
		return true
	})

	if len(definedCodes) == 0 {
		t.Fatal("no E_* variables found in code.go")
	}

	// Build map from allCodes
	allCodesMap := make(map[string]bool)
	for _, c := range AllCodes() {
		str := c.String()
		if allCodesMap[str] {
			t.Errorf("allCodes contains duplicate: %s", str)
		}
		allCodesMap[str] = true
	}

	// Check for codes in definitions but not in allCodes
	for name := range definedCodes {
		if !allCodesMap[name] {
			t.Errorf("E_* variable %s defined in code.go but missing from allCodes", name)
		}
	}

	// Check for codes in allCodes but not in definitions
	for name := range allCodesMap {
		if !definedCodes[name] {
			t.Errorf("allCodes contains %s but no matching E_* variable in code.go", name)
		}
	}

	// Log counts for visibility
	t.Logf("found %d E_* definitions, %d entries in allCodes", len(definedCodes), len(allCodesMap))
}

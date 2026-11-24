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
		{E_INTERNAL, "E_INTERNAL"},
		{E_SYNTAX, "E_SYNTAX"},
		{E_IMPORT_CYCLE, "E_IMPORT_CYCLE"},
		{E_TYPE_MISMATCH, "E_TYPE_MISMATCH"},
		{E_DUPLICATE_PK, "E_DUPLICATE_PK"},
		{E_DATA_PARSE, "E_DATA_PARSE"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Errorf("Code.String() = %q; want %q", got, tt.want)
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
		{"valid code", E_DUPLICATE_TYPE, false},
		{"sentinel code", E_INTERNAL, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.IsZero(); got != tt.want {
				t.Errorf("Code.IsZero() = %v; want %v", got, tt.want)
			}
		})
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
			t.Errorf("duplicate code string %q (%v and %v)", str, prev, c)
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

// TestAllCodes_MatchesDefinedCodes uses AST parsing to verify that every
// exported E_* or W_* variable in code.go is auto-registered via NewCode()
// and appears in AllCodes() exactly once.
//
// The W_ prefix names a code whose severity is fixed at Warning, and the E_
// prefix every other code. Severity is carried on the Issue, not the Code, so
// W_SNAPSHOT_UNRESOLVED_REQUIRED takes snapshot.WithRevalidation's; the prefix
// is a naming convention rather than a type-enforced property — the
// registry itself does not distinguish between the two.
func TestAllCodes_MatchesDefinedCodes(t *testing.T) {
	// Parse code.go to find all exported E_* / W_* variable declarations
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "code.go", nil, 0)
	if err != nil {
		t.Fatalf("failed to parse code.go: %v", err)
	}

	// Collect all E_* / W_* variable names from AST
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
				if !name.IsExported() {
					continue
				}
				if strings.HasPrefix(name.Name, "E_") || strings.HasPrefix(name.Name, "W_") {
					definedCodes[name.Name] = true
				}
			}
		}
		return true
	})

	if len(definedCodes) == 0 {
		t.Fatal("no E_* or W_* variables found in code.go")
	}

	// Build map from registry (via AllCodes)
	registeredMap := make(map[string]bool)
	for _, c := range AllCodes() {
		str := c.String()
		if registeredMap[str] {
			t.Errorf("registry contains duplicate: %s", str)
		}
		registeredMap[str] = true
	}

	// Check for codes in definitions but not in registry
	for name := range definedCodes {
		if !registeredMap[name] {
			t.Errorf("diagnostic code variable %s defined in code.go but missing from AllCodes()", name)
		}
	}

	// Log counts for visibility
	t.Logf("found %d E_*/W_* definitions in code.go, %d total registered codes", len(definedCodes), len(registeredMap))
}

// TestNewCodeDuplicatePanics verifies that registering a code with a
// duplicate identifier panics at registration time.
func TestNewCodeDuplicatePanics(t *testing.T) {
	// NewCode with an already-registered identifier should panic.
	// E_INTERNAL is already registered by the package-level var.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from duplicate NewCode, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "duplicate code") {
			t.Errorf("panic message %q does not mention 'duplicate code'", msg)
		}
	}()
	NewCode("E_INTERNAL")
}

package format

import (
	"strings"
	"testing"
)

// TestSource_ReemitsAConstructOneItemALine pins that a multiline enum or extends
// clause too wide for one line comes out one value or parent a line, whatever
// its lines held, and that the output is a fixed point. A construct with a
// blank line or a comment on an inner line keeps its lines, re-indented.
func TestSource_ReemitsAConstructOneItemALine(t *testing.T) {
	t.Parallel()
	a, b, c, d := strings.Repeat("a", 30), strings.Repeat("b", 30), strings.Repeat("c", 30), strings.Repeat("d", 30)
	for _, tt := range []struct {
		name, in, want string
	}{
		{
			"an enum with two values a line and two before the bracket",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"" + a + "\", \"" + b + "\",\n\t\t\"" + c + "\", \"" + d + "\"]\n}\n",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"" + a + "\",\n\t\t\"" + b + "\",\n\t\t\"" + c + "\",\n\t\t\"" + d + "\",\n\t]\n}\n",
		},
		{
			"an enum value holding a comma",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"" + a + ",x\", \"" + b + "\",\n\t\t\"" + c + "\",\n\t]\n}\n",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"" + a + ",x\",\n\t\t\"" + b + "\",\n\t\t\"" + c + "\",\n\t]\n}\n",
		},
		{
			"a datatype-alias enum",
			"schema \"s\"\n\ntype X = Enum[\n\t\"" + a + "\", \"" + b + "\",\n\t\"" + c + "\",\n]\n",
			"schema \"s\"\n\ntype X = Enum[\n\t\"" + a + "\",\n\t\"" + b + "\",\n\t\"" + c + "\",\n]\n",
		},
		{
			"an extends clause with two parents a line",
			"schema \"s\"\n\ntype C extends\n\tP" + a + ", Q" + b + ",\n\tR" + c + ",\n{\n}\n",
			"schema \"s\"\n\ntype C extends\n\tP" + a + ",\n\tQ" + b + ",\n\tR" + c + ",\n{\n}\n",
		},
		{
			"an enum with a blank line, short enough to join",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"a\", \"b\",\n\n\t\t\"c\", \"d\",\n\t]\n}\n",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"a\", \"b\",\n\n\t\t\"c\", \"d\",\n\t]\n}\n",
		},
		{
			"an enum with a comment on an inner line",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"" + a + "\", \"" + b + "\", // c\n\t\t\"" + c + "\",\n\t]\n}\n",
			"schema \"s\"\n\ntype T {\n\tk Enum[\n\t\t\"" + a + "\", \"" + b + "\", // c\n\t\t\"" + c + "\",\n\t]\n}\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := formatText(tt.in)
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if got != tt.want {
				t.Errorf("Source =\n%s\nwant\n%s", got, tt.want)
			}
			again, err := formatText(got)
			if err != nil {
				t.Fatalf("Source on its own output: %v", err)
			}
			if again != got {
				t.Errorf("Source is not a fixed point: a second pass gives\n%s\nfrom\n%s", again, got)
			}
		})
	}
}

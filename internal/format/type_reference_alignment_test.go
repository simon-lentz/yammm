package format

import "testing"

// TestSource_AlignsAPropertyOfAnImportedType pins that a property whose type an
// import's alias qualifies joins its run of properties, whatever the alias's case,
// and that the head and parent lines of a multi-line extends clause join none.
func TestSource_AlignsAPropertyOfAnImportedType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, in, want string
	}{
		{
			"a lower-case alias between two properties",
			"schema \"s\"\n\nimport \"./o\" as o\n\ntype T {\n\ta String\n\tbb o.Money\n\tccc Integer\n}\n",
			"schema \"s\"\n\nimport \"./o\" as o\n\ntype T {\n\ta   String\n\tbb  o.Money\n\tccc Integer\n}\n",
		},
		{
			"a lower-case alias with a modifier and a comment",
			"schema \"s\"\n\nimport \"./common\" as common\n\ntype T {\n\tcolor common.Color required // c\n\tn Integer // n\n}\n",
			"schema \"s\"\n\nimport \"./common\" as common\n\ntype T {\n\tcolor common.Color required // c\n\tn     Integer               // n\n}\n",
		},
		{
			"an alias with a digit, an underscore and an upper-case letter",
			"schema \"s\"\n\nimport \"./o\" as geo_v2Lib\n\ntype T {\n\ta String\n\tbb geo_v2Lib.Money\n}\n",
			"schema \"s\"\n\nimport \"./o\" as geo_v2Lib\n\ntype T {\n\ta  String\n\tbb geo_v2Lib.Money\n}\n",
		},
		{
			"an upper-case alias",
			"schema \"s\"\n\nimport \"./o\" as O\n\ntype T {\n\ta String\n\tbb O.Money\n}\n",
			"schema \"s\"\n\nimport \"./o\" as O\n\ntype T {\n\ta  String\n\tbb O.Money\n}\n",
		},
		{
			"a multi-line extends clause with two qualified parents a line",
			"schema \"s\"\n\nabstract type P {\n}\ntype C extends // head\n\taa.P, b.Q,\n\tc.R, d.S,\n{\n\tx String\n}\n",
			"schema \"s\"\n\nabstract type P {\n}\ntype C extends // head\n\taa.P, b.Q,\n\tc.R, d.S,\n{\n\tx String\n}\n",
		},
		{
			"a multi-line extends clause after an alias",
			"schema \"s\"\n\nabstract type P {\n}\ntype X = String\ntype C extends // head\n\tP,\n{\n\tx String\n}\n",
			"schema \"s\"\n\nabstract type P {\n}\ntype X = String\ntype C extends // head\n\tP,\n{\n\tx String\n}\n",
		},
		{
			"a multi-line invariant whose continuation lines start with a word",
			"schema \"s\"\n\ntype T {\n\ta Integer\n\tbb Integer\n\t! \"m\" a > 0 &&\n\t\tbb > 1 &&\n\t\ta < 9\n}\n",
			"schema \"s\"\n\ntype T {\n\ta  Integer\n\tbb Integer\n\t! \"m\" a > 0 &&\n\t\tbb > 1 &&\n\t\ta < 9\n}\n",
		},
		{
			"a property named type whose literal holds an equals sign",
			"schema \"s\"\n\ntype T {\n\tid String primary\n\ttype Pattern[\" = \"]\n\tccc Integer\n}\n",
			"schema \"s\"\n\ntype T {\n\tid   String primary\n\ttype Pattern[\" = \"]\n\tccc  Integer\n}\n",
		},
		{
			"an edge property",
			"schema \"s\"\n\nimport \"./o\" as o\n\ntype T {\n\t--> R (one) T {\n\t\tw o.Weight\n\t\tscore Integer\n\t}\n}\n",
			"schema \"s\"\n\nimport \"./o\" as o\n\ntype T {\n\t--> R (one) T {\n\t\tw     o.Weight\n\t\tscore Integer\n\t}\n}\n",
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

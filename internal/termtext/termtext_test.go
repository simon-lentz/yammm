package termtext

import "testing"

func TestString(t *testing.T) {
	t.Parallel()

	rows := []struct{ name, in, want string }{
		{"plain text", "a b", "a b"},
		{"a tab", "a\tb", "a\tb"},
		{"a newline", "a\nb", "a\u240ab"},
		{"a carriage return", "a\rb", "a\u240db"},
		{"an escape", "a\x1b[31mb", "a\u241b[31mb"},
		{"NUL", "a\x00b", "a\u2400b"},
		{"the last C0 control", "a\x1fb", "a\u241fb"},
		{"DEL", "a\x7fb", "a\u2421b"},
		{"a C1 control", "a\u009bb", "a\ufffdb"},
		{"the first C1 control", "a\u0080b", "a\ufffdb"},
		{"the last C1 control", "a\u009fb", "a\ufffdb"},
		{"the right-to-left mark", "a\u200fb", "a\ufffdb"},
		{"the first embedding", "a\u202ab", "a\ufffdb"},
		{"a right-to-left override", "a\u202eb", "a\ufffdb"},
		{"the Arabic letter mark", "a\u061cb", "a\ufffdb"},
		{"the left-to-right mark", "a\u200eb", "a\ufffdb"},
		{"the first isolate", "a\u2066b", "a\ufffdb"},
		{"the last isolate", "a\u2069b", "a\ufffdb"},
		{"a byte that is not UTF-8", "a\xffb", "a\ufffdb"},
		{"a replacement character as written", "a\ufffdb", "a\ufffdb"},
		{"a wide rune", "a\u4e16b", "a\u4e16b"},
		{"the soft hyphen", "a\u00adb", "a\u00adb"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := String(row.in); got != row.want {
				t.Errorf("String(%q) = %q, want %q", row.in, got, row.want)
			}
		})
	}
}

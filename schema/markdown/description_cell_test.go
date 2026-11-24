package markdown

import (
	"strings"
	"testing"
)

// TestDescriptionCell pins how a doc comment is written as a table cell, which
// is inline Markdown on one line: a line break folds to <br>, with a space
// beside it where an emphasis delimiter or a www link needs one, or to a
// space inside a code span or a tag; a www link or URL is written as it is;
// an unescaped backslash that ends a line before
// a line of text is the hard break and gives way to the <br>, and one that
// ends a line before a blank line or inside a pre element's text is doubled
// before the <br>; a pipe after an even backslash run gains one backslash; the
// "(" after a link text whose tail does not form is escaped. No block of the
// comment is read as a block.
func TestDescriptionCell(t *testing.T) {
	t.Parallel()

	ticks80, ticks81 := strings.Repeat("`", 80), strings.Repeat("`", 81)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "no special characters", "no special characters"},
		{"empty", "", ""},
		{"pipe", "a|b", `a\|b`},
		{"newline", "line1\nline2", "line1<br>line2"},
		{"crlf", "line1\r\nline2", "line1<br>line2"},
		{"bare cr", "line1\rline2", "line1<br>line2"},
		{"a link whose destination starts on the next line", "see [spec](\nhttps://x.y) now", "see [spec]( https://x.y) now"},
		{"a link whose title starts on the next line", "see [spec](https://x.y\n\"Title\") now", "see [spec](https://x.y \"Title\") now"},
		{"a link over three lines", "[a](\n/u\n'T')", "[a]( /u 'T')"},
		{"a link whose parenthesis closes on the next line", "[a](/u 'T'\n  )", "[a](/u 'T' )"},
		{"an image whose destination starts on the next line", "![alt](\nhttps://x.y/i.png)", "![alt]( https://x.y/i.png)"},
		{"a pointy destination on the next line", "[a](\n<b c>)", "[a]( <b c>)"},
		{"a parenthesis with no bracket before it", "a](\nb", "a](<br>b"},
		{"a link closed before the line ends", "[a](b) c\nd", "[a](b) c<br>d"},
		{"a line break that ends a failed tail's destination", "[a](b\nc)", "[a]\\(b<br>c)"},
		{"a failed tail's destination in a pre element", "<pre>[a](b\nc)", "<pre>[a]\\(b<br>c)"},
		{"white space after a failed tail's destination", "[a](b c\nd)", "[a]\\(b c<br>d)"},
		{"a hard break that ends a failed tail's destination", "[a](b\\\nc)", "[a]\\(b<br>c)"},
		{"a blank line inside a link's parentheses", "[a](\n\nb)", "[a]\\(<br><br>b)"},
		{"an escaped bracket opens no link", "\\[a](\nb)", "\\[a](<br>b)"},
		{"a bracket in a code span opens no link", "`[`a](\nb)", "`[`a](<br>b)"},
		{"a title that ends on another line", "[a](b \"T\nU\")", "[a](b \"T&#10;U\")"},
		{"a backslash that ends a title's line", "[a](b \"T\\\nU\")", "[a](b \"T\\\\&#10;U\")"},
		{"a line break a backslash takes into a pointy destination", "[a](<b\\\nc>)", "[a](<b\\\\&#10;c>)"},
		{"a title's next line loses its leading white space", "[a](b \"T  \n   U\")", "[a](b \"T  &#10;U\")"},
		{"an unclosed parenthesis in a destination", "[a](b(c\n\"T\")", "[a](b(c \"T\")"},
		{"a destination 33 parentheses deep", "[a](" + strings.Repeat("(", 33) + "\n)", "[a]\\(" + strings.Repeat("(", 33) + "<br>)"},
		{"a destination 32 parentheses deep", "[a](" + strings.Repeat("(", 32) + "b" + strings.Repeat(")", 32) + "\n)", "[a](" + strings.Repeat("(", 32) + "b" + strings.Repeat(")", 32) + " )"},
		{"a link inside a link's text", "[x [a](\nb) [c](\nd)", "[x [a]( b) [c]( d)"},
		{"an opener after a link opens the earlier one again", "[x [a](b) [c] ](\nd)", "[x [a](b) [c] ]( d)"},
		{"no opener before a link opens a link", "[x [a](b) ](\nd)", "[x [a](b) ](<br>d)"},
		{"a footnote opener is no image", "![^a](\nb)", "![^a]( b)"},
		{"a title whose quote a backslash may escape", "[a](b \"x\\\" y\"\n)", "[a](b \"x\\\" y\" )"},
		{"a closing quote with a backslash before it", "[a](b 'x\\' y\n)", "[a]\\(b 'x\\' y<br>)"},
		{"a pointy destination a line break ends", "see [a](<b\nc>) end", "see [a]\\(<b c>) end"},
		{"a comment in a failed tail", "[a](<!--\n-->)", "[a]\\(<!-- -->)"},
		{"a processing instruction in a failed tail", "[a](<?x\ny?>)", "[a]\\(<?x y?>)"},
		{"a declaration in a failed tail", "[a](<!X\ny>)", "[a]\\(<!X y>)"},
		{"a CDATA section in a failed tail", "[a](<![CDATA[x\ny]]>)", "[a]\\(<![CDATA[x y]]>)"},
		{"a tag's attribute in a failed tail", "[a](<a\nhref=b>)", "[a]\\(<a href=b>)"},
		{"a code span in a failed tail", "[a](<`q\nr`>)", "[a]\\(<`q r`>)"},
		{"a tag after a failed tail's white space", "[a]( <b\nc>)", "[a]\\( <b c>)"},
		{"a title in a failed tail", "[a](<x\ny> \"t\nu\")", "[a]\\(<x y> \"t<br>u\")"},
		{"a blank line inside a title", "x [a](b \"t\n\nu\") y", "x [a]\\(b \"t<br><br>u\") y"},
		{"a pre element that starts in a failed tail", "x [a](b<pre>\nc) y", "x [a]\\(b<pre>c) y"},
		{"a pre element that starts a failed tail's destination", "[a](<pre>\nc)", "[a]\\(<pre>c)"},
		{"a listing element in a failed tail", "[a](b<listing>\nc)", "[a]\\(b<listing>c)"},
		{"a failed tail inside a pre element", "x <pre>[a](b\nc)</pre> y", "x <pre>[a]\\(b<br>c)</pre> y"},
		{"an image whose tail fails", "![i](x\n x)/p", "![i]\\(x<br>x)/p"},
		{"a link inside a failed tail", "[a](x[b](c)\nd)", "[a]\\(x[b](c)<br>d)"},
		{"a link and a hard break inside a failed pointy tail", "[a](<x[b](c)\\\nd>\ne)", "[a]\\(<x[b](c)<br>d><br>e)"},
		{"a www link holds the brackets after it", "see www.example.com/[a](b c\nnext", "see www.example.com/[a](b c<br>next"},
		{"a URL holds the brackets after it", "see http://x.co/[a](b c\nnext", "see http://x.co/[a](b c<br>next"},
		{"a URL holds an image's brackets", "see https://x.co/![a](b c\nnext", "see https://x.co/![a](b c<br>next"},
		{"a www link ends at a line break", "www.example.com/[a](b\nc d)", "www.example.com/[a](b<br>c d)"},
		{"a backslash that ends a www link is the link's", "www.x.co![[a](\\\n)", "www.x.co![[a](\\<br>)"},
		{"a backslash that ends a www link before a space", "www.x.co\\ \nb", "www.x.co\\<br>b"},
		{"a www link inside a link's text is text", "[x www.y.co/[a](b \"t\n\nu\")", "[x www.y.co/[a]\\(b \"t<br><br>u\")"},
		{"a www link after a line break", "a\nwww.x.co/[b](c d", "a<br> www.x.co/[b](c d"},
		{"a star before a line break opens nothing", "[a](*\n x*", "[a]\\(* <br>x*"},
		{"an underscore before a line break opens nothing", "[a](_\n x_", "[a]\\(_ <br>x_"},
		{"a tilde before a line break opens nothing", "[a](~\nb\n ~", "[a]\\(~ <br>b<br> ~"},
		{"emphasis does not span a star before a line break", "x *\ny*", "x * <br>y*"},
		{"a star after a line break closes nothing", "(*a\n*)", "(*a<br> *)"},
		{"underscores on both sides of a line break", "a _\n_b", "a _ <br> _b"},
		{"a star before a line break in a pre element", "<pre>a *\nb*</pre>", "<pre>a * <br>b*</pre>"},
		{"an escaped star before a line break takes no space", "a\\*\nb*", "a\\*<br>b*"},
		{"a backslash that ends a www link in a pre element", "x <pre> www.x.co\\\nb</pre>", "x <pre> www.x.co\\<br>b</pre>"},
		{"a vertical tab is part of a destination", "[a](b\v\"T\"\n)", "[a](b\v\"T\" )"},
		{"a pre end tag does not close a listing", "<listing>\nx\n</pre>\n  y\n</listing>", "<listing>x<br></pre><br>  y<br></listing>"},
		{"a listing end tag does not close a pre", "<pre>\nx\n</listing>\n  y\n</pre>", "<pre>x<br></listing><br>  y<br></pre>"},
		{"an end tag closes the elements inside its own", "<pre>\n<listing>\nx\n</pre>\n  y", "<pre><listing>x<br></pre><br>y"},
		{"a processing instruction's closer starts after its opener", "a <?>\nb ?> c", "a <?> b ?> c"},
		{"an empty comment ends at once", "a <!---->b\nc --> d", "a <!---->b<br>c --> d"},
		{"a declaration name that ends the text", "a\n<!AB", "a<br><!AB"},
		{"a quote that ends the text", "x >\n<a b=\"", "x ><br><a b=\""},
		{"a less-than sign that ends the text", "a\nb <", "a<br>b <"},
		{"a vertical tab is white space in a tag", "<a\vb='x\ny'>z", "<a\vb='x y'>z"},
		{"upper-case letters after a tag name start no declaration", "a <xAB y\nz", "a <xAB y<br>z"},
		{"pipe and newline", "a|b\nc", `a\|b<br>c`},
		{"indentation around fold dropped", "line1: \n\tline2 indented", "line1:<br>line2 indented"},
		{"white space at the cell's ends goes", " \ta\nb \n ", "a<br>b<br>"},
		{"a blank line is two breaks", "a\n\nb", "a<br><br>b"},
		{"an author escape stays", `a\*b\*`, `a\*b\*`},
		{"a lone backslash stays", `a\b`, `a\b`},
		{"an escaped pipe stays escaped", `a\|b`, `a\|b`},
		{"a pipe after an escaped backslash is escaped", `a\\|b`, `a\\\|b`},
		{"a pipe after three backslashes stays", `a\\\|b`, `a\\\|b`},
		{"leading pipe", "|x", `\|x`},
		{"a pipe after an escape the run restarts at", `\*x|y`, `\*x\|y`},
		{"a pipe in a code span is escaped", "`a|b`", "`a\\|b`"},
		{"a backslash-pipe in a code span stays", "`a\\|b`", "`a\\|b`"},

		{"a line-end backslash is the break", "C:\\\nnext", "C:<br>next"},
		{"an escaped backslash at a line end stays", "C:\\\\\nnext", "C:\\\\<br>next"},
		{"three backslashes at a line end are one and the break", "C:\\\\\\\nnext", "C:\\\\<br>next"},
		{"a backslash before two spaces is a backslash", "foo\\  \nbar", "foo\\\\<br>bar"},
		{"a backslash before one space is a backslash", "foo\\ \nbar", "foo\\\\<br>bar"},
		{"a backslash before a tab is a backslash", "foo\\\t\nbar", "foo\\\\<br>bar"},
		{"a backslash before a blank line is a backslash", "foo\\\n\nbar", "foo\\\\<br><br>bar"},
		{"a backslash before a line of spaces is a backslash", "foo\\\n  \nbar", "foo\\\\<br><br>bar"},
		{"an escaped backslash before spaces stays", "foo\\\\  \nbar", "foo\\\\<br>bar"},
		{"a backslash before the last line break is a backslash", "foo\\\n", "foo\\\\<br>"},
		{"a doc that ends in a backslash keeps it", "ends in a backslash\\", "ends in a backslash\\"},
		{"an escaped character before a break stays", "a\\*\nb", "a\\*<br>b"},
		{"spaces before a line-end backslash stay", "a \\\nb", "a <br>b"},

		{"a code span across a line folds to a space", "a `x\ny` b", "a `x y` b"},
		{"a code span's line-end backslash stays", "`x\\\ny`", "`x\\ y`"},
		{"a code span of two backticks", "``a ` \n b``\nc", "``a `  b``<br>c"},
		{"a run with no closer is text", "a ``x\ny` b", "a ``x<br>y` b"},
		{"a longer run closes no span", "a `x ``y\nz` b", "a `x ``y z` b"},
		{"a shorter run closes no span", "a ``x `y\nz`` b", "a ``x `y z`` b"},
		{"a run after a failed search still closes", "``x `a\nb` c", "``x `a b` c"},
		{"a run with no closer after a failed search", "``x ```a\nb` c", "``x ```a<br>b` c"},
		{"an escaped backtick opens no span", "a \\`x\ny` b", "a \\`x<br>y` b"},
		{"the run after an escaped backtick opens one", "\\``x\ny` b", "\\``x y` b"},
		{"a backslash in a code span escapes nothing", "`a\\`\nb`c`", "`a\\`<br>b`c`"},
		{"a fence reads as a code span", "fence:\n```\ncode\nmore\n```\nend", "fence:<br>``` code more ```<br>end"},
		{"a tilde fence is text", "~~~\ncode\n~~~", "~~~ <br>code<br> ~~~"},
		{"a code span across a blank line", "`a\n\nb`", "`a  b`"},
		{"a run of 81 backticks opens no span", ticks81 + "a\nb" + ticks81, ticks81 + "a<br>b" + ticks81},
		{"a run of 80 backticks opens a span", ticks80 + "a\nb" + ticks80, ticks80 + "a b" + ticks80},

		{"a tag across a line folds to a space", "<a\nhref=\"#x\">y</a>", "<a href=\"#x\">y</a>"},
		{"an attribute value across a line", "<a title='x\n  y'>z</a>", "<a title='x y'>z</a>"},
		{"a valueless attribute across a line", "<a b\n c = d\n e>z</a>", "<a b c = d e>z</a>"},
		{"a comment across a line", "a <!-- b\nc --> d", "a <!-- b c --> d"},
		{"an empty comment", "a <!-->b\nc --> d", "a <!-->b<br>c --> d"},
		{"an empty comment with a dash", "a <!--->b\nc --> d", "a <!--->b<br>c --> d"},
		{"a processing instruction across a line", "a <?b\nc?> d", "a <?b c?> d"},
		{"a declaration across a line", "a <!X b\nc> d", "a <!X b c> d"},
		{"a declaration with no end is escaped", "a <!DOCTYPE html\nb", "a \\<!DOCTYPE html<br>b"},
		{"a second declaration with no end is escaped too", "a <!X b\nc <!Y d\ne", "a \\<!X b<br>c \\<!Y d<br>e"},
		{"no declaration is read after a comment with no end", "a <!-- b\nc <!X d\ne> f", "a <!-- b<br>c \\<!X d<br>e> f"},
		{"no CDATA section is read after a comment with no end", "a <!-- b\nc <![CDATA[d\ne]]> f", "a <!-- b<br>c <![CDATA[d<br>e]]> f"},
		{"a processing instruction is read after a comment with no end", "a <!-- b\nc <?d\ne?> f", "a <!-- b<br>c <?d e?> f"},
		{"a declaration is read after a CDATA section with no end", "a <![CDATA[ b\nc <!X d\ne> f", "a <![CDATA[ b<br>c <!X d e> f"},
		{"a comment is read after a closed comment", "a <!-- b\nc --> <!-- d\ne --> f", "a <!-- b c --> <!-- d e --> f"},
		{"a declaration that ends its name at the line", "a <!XY\nb> d", "a <!XY b> d"},
		{"a lower-case declaration is text", "a <!x b\nc> d", "a <!x b<br>c> d"},
		{"a declaration with no white space is text", "a <!X>b\nc> d", "a <!X>b<br>c> d"},
		{"a declaration name with a digit is text", "a <!X1 b\nc> d", "a <!X1 b<br>c> d"},
		{"a comment holding two hyphens", "a <!-- b\nc -- d\ne --> f", "a <!-- b c -- d e --> f"},
		{"a comment ends at its first closer", "a <!-- b\nc --> d\n--> e", "a <!-- b c --> d<br>--> e"},
		{"a processing instruction ends at its first closer", "a <?x\ny?>z\n?> w", "a <?x y?>z<br>?> w"},
		{"a processing instruction holds an angle bracket", "a <?b> c\nd ?> e", "a <?b> c d ?> e"},
		{"an unquoted value holding a slash", "a <a b=\"c\nd\" e=f/> w", "a <a b=\"c d\" e=f/> w"},
		{"a tag closed on the next line", "a <a\n/> w", "a <a /> w"},
		{"two attributes with no space between are text", "a <a b=\"c\"d=\"e\"\nx> w", "a <a b=\"c\"d=\"e\"<br>x> w"},
		{"a CDATA section across a line", "a <![CDATA[b\nc]]> d", "a <![CDATA[b c]]> d"},
		{"an end tag across a line", "<b>x</b\n>y", "<b>x</b >y"},
		{"an end tag with no name is text", "a </\n> b", "a </<br>> b"},
		{"an end tag with an attribute is text", "a </b c\n> d", "a </b c<br>> d"},
		{"a tag name with a digit and a hyphen", "<h1-x\n>y", "<h1-x >y"},
		{"a tag name starts with a letter", "<1a\n>y", "<1a<br>>y"},
		{"an attribute name starts with a colon or an underscore", "<a :b _c\n>y", "<a :b _c >y"},
		{"an attribute name starts with no digit", "<a 1b\n>y", "<a 1b<br>>y"},
		{"an unquoted value holds no backtick", "<a b=c`d>\ne`", "<a b=c`d> e`"},
		{"an unquoted value is not empty", "<a b=\n>y", "<a b=<br>>y"},
		{"an attribute name holds a dot and a hyphen", "<a b.c-d\n>y", "<a b.c-d >y"},
		{"a single-quoted value across a line", "<a b='c\n\"d'>y", "<a b='c \"d'>y"},
		{"a value needs its closing quote", "<a b=\"c\nd>y", "<a b=\"c<br>d>y"},
		{"a slash before the end of a tag", "<a b /\n>y", "<a b /<br>>y"},
		{"an open comment is text", "<!-- b\nc", "<!-- b<br>c"},
		{"an open tag is text", "<a href=\"x\ny", "<a href=\"x<br>y"},
		{"an attribute with no value after its equals sign is text", "<a b=\nc", "<a b=<br>c"},
		{"a tag in a code span is code", "`<a`\n`href>`", "`<a`<br>`href>`"},
		{"a backtick in an autolink opens no span", "<http://a`b>\nc`d", "<http://a`b><br>c`d"},
		{"a backtick in an email autolink opens no span", "<a`b@c.de>\nc`d", "<a`b@c.de><br>c`d"},
		{"a scheme of one letter is no autolink", "<a:`b>\nc`d", "<a:`b> c`d"},
		{"a scheme of 32 characters is an autolink", "<" + strings.Repeat("s", 32) + ":`b>\nc`d", "<" + strings.Repeat("s", 32) + ":`b><br>c`d"},
		{"a scheme of 33 characters is no autolink", "<" + strings.Repeat("s", 33) + ":`b>\nc`d", "<" + strings.Repeat("s", 33) + ":`b> c`d"},
		{"an autolink holds no angle bracket", "<http://a<`b>\nc`d", "<http://a<`b> c`d"},
		{"an autolink holds no space", "<http://a `b>\nc`d", "<http://a `b> c`d"},
		{"an email label starts with no hyphen", "<a`b@-c.de>\nc`d", "<a`b@-c.de> c`d"},
		{"an email label ends with no hyphen", "<a`b@c-.de>\nc`d", "<a`b@c-.de> c`d"},
		{"an email address needs a host", "<a`b@>\nc`d", "<a`b@> c`d"},
		{"an email label of 64 characters is no autolink", "<a`b@" + strings.Repeat("c", 64) + ">\nc`d", "<a`b@" + strings.Repeat("c", 64) + "> c`d"},
		{"an email label of 63 characters is an autolink", "<a`b@" + strings.Repeat("c", 63) + ">\nc`d", "<a`b@" + strings.Repeat("c", 63) + "><br>c`d"},
		{"an email address holds no space", "<a`b @c.de>\nc`d", "<a`b @c.de> c`d"},
		{"an escaped angle bracket opens no tag", "\\<a\nhref=x>", "\\<a<br>href=x>"},
		{"the text between tags is the cell's text", "<div>\nx\n</div>", "<div><br>x<br></div>"},
		{"Markdown between tags stays Markdown", "<div>\n*a* \\* [x](y)\n</div>", "<div><br> *a* \\* [x](y)<br></div>"},

		{"a pre element keeps its breaks", "<pre>\na\n  b\n</pre>", "<pre>a<br>  b<br></pre>"},
		{"a listing element keeps its breaks", "<listing>\na\nb\n</listing>", "<listing>a<br>b<br></listing>"},
		{"a break after a tag inside pre is kept", "<pre>a<b>\nc</b>\nd</pre>", "<pre>a<b><br>c</b><br>d</pre>"},
		{"a break after a pre element is a break", "<div><pre>a</pre>\nb\n</div>", "<div><pre>a</pre><br>b<br></div>"},
		{"a pre element after text", "text <pre>\n  a\n  b\n</pre>", "text <pre>  a<br>  b<br></pre>"},
		{"a pre start tag written self-closing", "<pre/>\na\n  b\n</pre>", "<pre/>a<br>  b<br></pre>"},
		{"a pre element inside a filtered element", "<textarea>\n<pre>\na\n  b\n</pre>", "<textarea><br><pre>a<br>  b<br></pre>"},
		{"an upper-case pre element", "<PRE>\na\n b</PRE>\nc\n d", "<PRE>a<br> b</PRE><br>c<br>d"},
		{"a pre element with attributes", "<pre class=\"x\"\n id=y>\na\n b</pre>", "<pre class=\"x\" id=y>a<br> b</pre>"},
		{"a pre element inside a pre element", "<pre>\n<pre>\n a\n</pre>\n b\n</pre>\n c", "<pre><pre> a<br></pre><br> b<br></pre><br>c"},
		{"a line-end backslash inside pre stays a backslash", "<pre>\na\\\nb</pre>", "<pre>a\\\\<br>b</pre>"},
		{"an escaped backslash at a line end inside pre stays", "<pre>\na\\\\\nb</pre>", "<pre>a\\\\<br>b</pre>"},
		{"a pre tag in a code span opens nothing", "`<pre>`\n  a", "`<pre>`<br>a"},
		{"a pre end tag with no start tag", "</pre>\n  a\n<pre>\n  b", "</pre><br>a<br><pre>  b"},
		{"a prefix of pre is another element", "<prex>\n  a", "<prex><br>a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := descriptionCell(tt.in); got != tt.want {
				t.Errorf("descriptionCell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestExtendedAutolinkLength pins the www links and URLs the fold writes as
// they are, each length the one cmark-gfm 0.29.0.gfm's autolink extension
// reads at s[i]: the text before a www link, the domain's underscores and
// dots, the trailing characters it drops, and the URL's scheme and host.
func TestExtendedAutolinkLength(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		s    string
		i    int
		want int
	}{
		{"www.x.co", 0, 8},
		{"awww.x.co", 1, 0},
		{"(www.x.co", 1, 8},
		{"*www.x.co", 1, 8},
		{"_www.x.co", 1, 8},
		{"~www.x.co", 1, 8},
		{" www.x.co", 1, 8},
		{"a\twww.x.co", 2, 8},
		{"!www.x.co", 1, 0},
		{"www.", 0, 0},
		{"www.x", 0, 5},
		{"wwwx.co", 0, 0},
		{"www.x_y.co", 0, 0},
		{"www.a_b.x.co", 0, 12},
		{"www.x.c_o", 0, 0},
		{"www._x.y.co", 0, 11},
		{"www.a.b.c.d.e.f.g.h.i.j_k.l", 0, 27},
		{"www.a.b.c.d.e.f.g.h.i_j.k", 0, 0},
		{"www.x.co.", 0, 8},
		{"www.x.co?!,:*_~'\"", 0, 8},
		{"www.x.co)", 0, 8},
		{"www.x.co/(a)", 0, 12},
		{"www.x.co/a)", 0, 10},
		{"www.x.co/((a)", 0, 13},
		{"www.x.co/&amp;", 0, 9},
		{"www.x.co/a;", 0, 10},
		{"www.x.co/&;", 0, 10},
		{"www.x.co<b>", 0, 8},
		{"www.x.co/[a](b c", 0, 14},
		{"www.x.co/`a` b", 0, 12},
		{"www.x\\_y.co", 0, 0},
		{"www.x\\.co", 0, 9},
		{"www.é.co", 0, 9},
		{"www.x.co—a b", 0, 12},
		{"www.x-y.co", 0, 10},
		{"www.x.co\\", 0, 9},
		{"http://x.co", 4, 7},
		{"https://x.co", 5, 7},
		{"ftp://x.co", 3, 7},
		{"HTTP://x.co", 4, 7},
		{"hTtPs://x.co", 5, 7},
		{"xhttp://x.co", 5, 0},
		{"(http://x.co", 5, 7},
		{"mailto://x", 6, 0},
		{"https://_x", 5, 0},
		{"http:/x", 4, 0},
		{"http://", 4, 0},
		{"ftp://x", 3, 4},
		{"http://x_y.z_w", 4, 0},
		{"http://x.co/a.", 4, 9},
		{"http://x", 4, 4},
		{"http://é", 4, 5},
		{"http://.x", 4, 0},
		{"http://x.co<b>", 4, 7},
		{"http://x.co/(a))", 4, 11},
		{"https://x.co/a b", 5, 9},
		{"www.x-y_z.co", 0, 0},
		{"http://\uFFFD", 4, 6},
		{"http://\u00a0x", 4, 0},
		{"http://\u2003x", 4, 0},
		{"http://$x", 4, 0},
		{"http://©x", 4, 6},
	} {
		if got := extendedAutolinkLength(tt.s, tt.i); got != tt.want {
			t.Errorf("extendedAutolinkLength(%q, %d) = %d, want %d", tt.s, tt.i, got, tt.want)
		}
	}
}

// TestAutolinkLength pins the two autolink forms the fold reads past: a
// scheme of 2 to 32 characters, a colon and no white space or "<", and an
// email address with a name and a host of labels.
func TestAutolinkLength(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   string
		want int
	}{
		{"http://a.b>", 11},
		{"a:>", 0},
		{"ab:>", 4},
		{"a+b.c-d:x>", 10},
		{"1ab:x>", 0},
		{"ab:x y>", 0},
		{"ab:x<y>", 0},
		{"ab:x", 0},
		{"m@n.io>", 7},
		{"@n.io>", 0},
		{"m@>", 0},
		{"m@n>", 4},
		{"m@n.>", 0},
		{"m@n..io>", 0},
		{"m@-n.io>", 0},
		{"m@n-.io>", 0},
		{"m@n-o.io>", 9},
		{"m@n.io", 0},
		{"m n@o.io>", 0},
		{"", 0},
	} {
		if got := autolinkLength(tt.in); got != tt.want {
			t.Errorf("autolinkLength(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestMarshal_WritesALinkTailAcrossLinesInItsCell pins that a property's doc
// comment whose link tail crosses a line is written as one link in its cell,
// and that the self-check reads the cell.
func TestMarshal_WritesALinkTailAcrossLinesInItsCell(t *testing.T) {
	t.Parallel()

	doc := marshalString(t, "schema \"s\"\ntype A {\n  /* see [spec](\n  https://x.y \"T\n  U\") now */\n  id String primary\n}\n")
	if want := `see [spec]( https://x.y "T&#10;U") now`; !strings.Contains(doc, want) {
		t.Errorf("document does not hold %s:\n%s", want, doc)
	}
}

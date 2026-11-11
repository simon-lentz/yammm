# The YAMMM Language Specification

## Introduction

This is a reference manual for the YAMMM data schema language. YAMMM is a domain-specific language for expressing schemas that define types, their properties, relationships, and constraints. The Go library API for loading, validating, and traversing schemas is documented in [API.md](API.md).

YAMMM is designed for structured data modeling with a focus on:

- Type definitions with properties and inheritance
- Relationships between types (associations and compositions)
- Invariants expressed as constraint expressions
- Structured diagnostics with stable error codes

YAMMM's design draws inspiration from [CUE](https://cuelang.org/)'s constraint-based approach to data validation, adapted for a nominal type system with explicit relationships. Where CUE uses structural typing and lattice-based unification, YAMMM uses named types with inheritance and constraint narrowing.

The grammar is compact and regular, which keeps it easy to analyze with automatic tools. The [Grammar Summary](#grammar-summary) at the end of this document is the normative statement of the syntax. The reference implementation parses it in two parts: the declaration grammar comes from annotated Go types, and expressions are parsed by a hand-written Pratt parser.

## Notation

The syntax is specified using [Extended Backus-Naur Form (EBNF)](https://en.wikipedia.org/wiki/Extended_Backus%E2%80%93Naur_form):

```text
Production  = production_name "=" [ Expression ] "." .
Expression  = Alternative { "|" Alternative } .
Alternative = Term { Term } .
Term        = [ label "=" ] ( production_name | token [ "..." token ] | Group | Option | Repetition | Description ) .
Description = "/*" /* informal prose */ "*/" .
Group       = "(" Expression ")" .
Option      = "[" Expression "]" .
Repetition  = "{" Expression "}" .
```

Productions are expressions constructed from terms and the following operators, in increasing precedence:

```text
|   alternation
()  grouping
[]  option (0 or 1 times)
{}  repetition (0 to n times)
```

Three naming conventions are used. **UPPER_SNAKE** names a lexical token produced by the lexer (`UC_WORD`, `LC_WORD`, `STRING`, `INTEGER`, `FLOAT`, `REGEXP`, `VARIABLE`, `DOC_COMMENT`, `EOF`) — with one exception, `BOOLEAN`, which is a non-terminal over the two word spellings `true` and `false` and is spelled in upper snake because it stands in for a literal. **CamelCase** names a syntactic non-terminal. **lower case** names a helper non-terminal defined where it is first used — a character class (`upper_letter`, `lower_letter`, `decimal_digit`, `newline`, `line_char`, `escape_sequence`), a single token (`format`, `dimensions`, `minLen`, `maxLen`), or a phrase (`min`, `max`, `exponent`). Literal tokens are enclosed in double quotes `""` or back quotes `` ` ``.

Some productions carry `label=` prefixes, which name the field the reference implementation binds (`path=` and `alias=` on `ImportDecl`, `qualifier=` and `name=` on `TypeRef`, `message=` and `expr=` on `Invariant`); the Grammar Summary writes the same productions without them. A `/* … */` description is informal prose standing in for a rule not written out, and a `//` comment is commentary, not grammar.

Inside a production, `"a" ... "z"` is a character range — the closed set from `a` to `z`. Outside a production, the horizontal ellipsis `...` informally denotes an enumeration or code snippet that is not further specified.

## Source Code Representation

Source code is Unicode text encoded in UTF-8. A byte sequence that is not valid UTF-8 is a syntax error (`E_SYNTAX`), and so is the NUL character (U+0000), which a string literal writes with the `\0` escape instead. Inside a comment or a string or regex literal, each run of one kind draws one report. Anywhere else, the sequence is an unexpected token, and the parser recovers from it as from any other: the recovery can skip a later sequence, which then draws no report of its own. The text is not canonicalized, so a single accented code point is distinct from the same character constructed from combining an accent and a letter. For simplicity, this document will use the unqualified term _character_ to refer to a Unicode code point in the source text.

A schema file can start with one UTF-8 byte order mark (U+FEFF). The lexer skips it: it is no token, though line 1's columns count it. The language server is the exception: for a file it reads from disk, it counts line 1's characters from the first character after the mark. The formatter removes a leading one: `yammm fmt --check` reports a file that starts with one as unformatted, and `yammm fmt --write` removes it. A byte order mark anywhere else is a syntax error (`E_SYNTAX`), inside a comment or a literal too; a string literal writes U+FEFF with the `\uFEFF` escape. `yammm fmt` does not refuse a mark inside a comment or a string literal: it writes the mark back where it stands, and `yammm validate` refuses the file. The `yammm.mod` marker file is the exception, below.

Each code point is distinct; upper and lower case letters are different characters.

### Characters

Line comments, string literals and regex literals run to the end of a line, which is either code point; a block comment may span lines:

```text
newline    = /* the Unicode code point U+000A or U+000D */ .
line_char  = /* a Unicode code point except newline, U+0000 and U+FEFF */ .
```

### Letters and Digits

**Identifier characters are ASCII only.** An identifier starts with an ASCII letter and continues with ASCII letters, ASCII digits and underscores; the underscore is permitted only after the first character. A non-ASCII letter is not an identifier character at all — `Pérson` is not one identifier.

```text
upper_letter  = "A" ... "Z" .
lower_letter  = "a" ... "z" .
decimal_digit = "0" ... "9" .
```

## Lexical Elements

### Comments

Comments serve as program documentation. YAMMM supports two forms of comments:

**Line comments** start with the character sequence `//` and stop at the end of the line:

```yammm
// This is a line comment
type Person {
    name String required  // inline comment
}
```

**Block comments** start with `/*` and end with `*/`. Block comments can span multiple lines:

```yammm
/* This is a block comment
   that spans multiple lines */
type Person {
    name String required
}
```

Block comments immediately preceding a schema, type, property, association, composition, data type, invariant, or type-level annotation declaration become that element's documentation and are preserved in the parsed model. The documentation is the text between `/*` and `*/` with its trailing white space removed and its leading white space removed up to the first text. Every line that begins after a newline is a continuation line. When a newline comes before the first text, the white space is removed only up to the start of that text's line, which is then a continuation line too. Each continuation line loses the leading spaces and tabs that every continuation line holding text shares. The shared indentation is compared byte by byte: a line indented by a tab and one indented by spaces share none. A line ends at a newline, U+000A or U+000D, a CR LF pair counting as one. In the example above, the documentation's second line is `that spans multiple lines`.

A comment cannot start inside a string literal or inside another comment.

### Tokens

Tokens form the vocabulary of the YAMMM language. There are four classes: identifiers, keywords, operators and punctuation, and literals. White space, formed from spaces (U+0020), horizontal tabs (U+0009), carriage returns (U+000D), and newlines (U+000A), is ignored except as it separates tokens that would otherwise combine into a single token.

### Identifiers

Identifiers name entities such as types, properties, and relationships. There are two classes of identifiers:

**Upper-case identifiers** start with an ASCII upper-case letter (A-Z). Type names and data type aliases must take this form. An import alias may, reserved keywords excepted. A relationship name takes the narrower `RelationName` form under [Associations](#associations), which admits no lower-case letter:

```text
UC_WORD = upper_letter { upper_letter | lower_letter | decimal_digit | "_" } .
```

**Lower-case identifiers** start with an ASCII lower-case letter (a-z). Property names and annotation names must take this form. An import alias may, reserved keywords excepted. A relationship name never does:

```text
LC_WORD = lower_letter { upper_letter | lower_letter | decimal_digit | "_" } .
```

Note: Identifiers must start with ASCII letters (A-Z or a-z). Subsequent characters may include ASCII letters, digits, and underscores.

Examples:

```text
Person          // type name (UC_WORD)
Car             // type name (UC_WORD)
OWNER           // relationship name (UC_WORD)
name            // property name (LC_WORD)
firstName       // property name (LC_WORD)
regNbr          // property name (LC_WORD)
```

### Keywords

YAMMM has a limited set of keywords. These keywords are reserved and have special meaning in the grammar:

**Schema keywords:**

```text
schema    import    as
```

**Type keywords:**

```text
type      abstract    part    extends
```

**Property keywords:**

```text
required    primary
```

**Multiplicity keywords:**

```text
one    many
```

**Expression keywords:**

```text
in
```

**Data type keywords:**

```text
Integer    Float    Boolean    String    Enum    Pattern
Timestamp    Date    UUID    Vector    List
```

**Boolean literals:**

```text
true    false
```

**Nil literal:**

```text
nil
```

**Reserved identifiers** (`datatype`, `includes`) are not used as structural grammar tokens but are reserved for forward compatibility.

Every keyword and reserved identifier except six may be used as a property name — the property-name production admits any `LC_WORD`, and only the six spellings named below are refused. So properties named `schema`, `type`, etc. carry no ambiguity:

```text
schema    type    datatype    required    primary    extends
includes    abstract    one    many    import
```

Six spellings cannot be used as property names. The keywords `as`, `part`, and `in` would create parsing ambiguity in import declarations (`as`), type modifiers (`part`), and membership expressions (`in`) respectively. The literals `true`, `false`, and `nil` are also barred. The rule table is keywordless, so all six lex as ordinary words; what rejects them is a negative lookahead on the property-name production. All six are rejected where the property is declared, not where it is used.

A seventh spelling, `self`, is grammatical, and on a type's property or a relation's field name is refused at schema load rather than by the grammar (`E_INVALID_NAME`, at the declaration): `self` is bound to the instance in every invariant, at load and at evaluation alike, so the bare name and `$self` reach the instance first, and a property so named has no read under its own name. The rule is the schema layer's.

### Operators and Punctuation

The following character sequences represent operators and punctuation:

```text
+     -     *     /     %           // arithmetic
==    !=    <     <=    >     >=    // comparison
&&    ||    ^     !                 // logical
=~    !~                            // regex match
in                                  // membership
->                                  // pipeline/function call
-->                                 // association
*->                                 // composition
@     @@                            // annotation (property / type level)
.                                   // property access
?                                   // ternary conditional
{     }                             // braces
[     ]                             // brackets
(     )                             // parentheses
,     :     =     /     |     _     // punctuation
```

### Numeric Literals

There are two kinds of numeric literals:

```text
INTEGER = decimal_digit { decimal_digit } .
FLOAT   = decimal_digit { decimal_digit } "." decimal_digit { decimal_digit } [ exponent ] .
exponent = ( "e" | "E" ) [ "+" | "-" ] decimal_digit { decimal_digit } .
```

Examples:

```text
42
0
1000
3.14
2.5e10
1.0e-5
```

### String Literals

A string literal represents a string constant obtained from a sequence of characters. Strings may be enclosed in single or double quotes:

```text
STRING          = `"` { string_char | escape_sequence } `"` |
                  `'` { string_char | escape_sequence } `'` .
string_char     = /* line_char except the enclosing quote and `\` */ .
escape_sequence = `\` ( "b" | "t" | "n" | "f" | "r" | "u" | "x" | "0" | `"` | "'" | `\` ) .
```

Several escape sequences allow arbitrary values to be encoded as ASCII text:

```text
\b   U+0008 backspace
\t   U+0009 horizontal tab
\n   U+000A line feed or newline
\f   U+000C form feed
\r   U+000D carriage return
\\   U+005C backslash
\'   U+0027 single quote
\"   U+0022 double quote
\uXXXX       Unicode code point (4 hex digits)
\xXX         byte value (2 hex digits)
\0           U+0000 null character
```

This is the complete escape vocabulary — a backslash before any other character does not lex. **A string's value is valid UTF-8.** `\xXX` writes one byte, so the bytes a literal's characters and escapes write must together be UTF-8: `"\xc3\xa9"` is `é`, and `"\xff"` is refused, as is a `\uXXXX` escape naming a surrogate (U+D800 to U+DFFF). The refusal is `E_SYNTAX`, or `E_INVALID_INVARIANT` for a literal inside an invariant expression; an annotation argument that will not unquote keeps its written spelling as a bare literal instead, so it never holds a value the escapes wrote. `\0` always means U+0000 and is not an octal prefix: `'\012'` is the null character followed by the two characters `12`. The quote character not delimiting the literal may appear unescaped: `'say "hi"'` and `"don't"` are both valid, so single-quoted literals can carry double quotes directly and vice versa.

Examples:

```text
"hello"
'world'
'say "hi"'
"line1\nline2"
"path\\to\\file"
"unicode: \u00e9"
```

### Regular Expression Literals

Regular expression literals are enclosed in forward slashes:

```text
REGEXP      = "/" { regexp_char | `\` line_char } "/" .
regexp_char = /* line_char except "/" and `\` */ .
```

The content follows Go's `regexp` package syntax. Backslashes within the pattern escape the following character.

Examples:

```text
/.+@.+/              // simple email pattern
/^[A-Z][a-z]+$/      // capitalized word
/\d{3}-\d{4}/        // phone number pattern
```

> **A `/` that does not open a comment starts a regex literal when another `/` follows on the same line.** A `/` immediately followed by `/` opens a line comment and is never a regex, because the comment rules precede the regex rule; a `/` followed by `*` opens a block comment only when a closing `*/` exists, and an unterminated `/*` followed by another `/` on its line lexes as a regex literal instead. Any other `/` begins a pattern that runs to the next `/`, so `(0.0 / 0.0) == (0.0 / 0.0)` fails to parse. This is reachable from ordinary arithmetic. The workaround is one such `/` per source line: `(x / 2.0) > 0.0 // halve it` fails, because the division's `/` opens a regex that swallows the `//`. Put the comment on its own line, where it lexes as a comment.

### Boolean Literals

```text
BOOLEAN = "true" | "false" .
```

### Variables

Variables are used within expression contexts, particularly in lambda parameters:

```text
VARIABLE = "$" ( decimal_digit { decimal_digit } | LC_WORD ) .
```

Examples:

```text
$0          // positional variable
$1          // positional variable
$self       // self reference in invariants
$item       // named parameter in lambda
$acc        // accumulator in reduce
```

## Schemas

A YAMMM file defines a single schema. The schema is the top-level container for all type and data type definitions.

### Schema Declaration

Every YAMMM file must begin with a schema declaration:

```text
Schema     = SchemaName { ImportDecl } { TypeDecl | DataTypeDecl } EOF .
SchemaName = [ DOC_COMMENT ] "schema" STRING .
```

The schema name is a string literal that identifies the model:

```yammm
schema "Vehicles"
```

An optional documentation comment may precede the schema declaration:

```yammm
/* Vehicle management schema
   Defines cars, dealers, and their relationships */
schema "Vehicles"
```

### Imports

Imports allow types to be shared across schema files. Import declarations appear after the schema name and before any type or data type declarations:

```text
ImportDecl = "import" path=STRING [ "as" alias=AliasName ] .   // labels: see Notation
AliasName  = UC_WORD | LC_WORD .
```

Examples:

```yammm
schema "Vehicles"

import "./parts"                    // alias: parts (derived from path)
import "./common/types" as common   // explicit alias
```

**Path resolution:**

- Relative paths (`./parts`, `../common`) are resolved against the importing file's directory
- Module-style paths (`internal/common`) are resolved against the module root (see `WithModuleRoot` option)
- The `.yammm` extension is optional and will be appended if not present
- Imports must resolve to `.yammm` files, not directories: an empty path, a path of separators alone, and a path whose last segment is `.` or `..`, names no file and draws `E_IMPORT_RESOLVE`. A trailing `/` after a file name is read past
- The extension is part of the path that is resolved, so a relative path spelling the module root or an ancestor of it, such as `../z` from the root directory `z`, names a file above the root. A load looks among its in-memory sources first, so one that holds that file binds it. A load that then reads the disk draws `E_PATH_ESCAPE`, and a `WithSourcesOnly` load draws `E_IMPORT_RESOLVE`

**The module root:** A directory is a module root when it holds a `yammm.mod` marker file. The loader resolves a schema's root in this order: the root the caller supplied (`WithModuleRoot`, or the `--module-root` flag), then the directory of the nearest ancestor holding a `yammm.mod`, then the schema file's own directory. The editor inserts its workspace folder between the second and the third.

In this version the marker file must be empty or hold only comment lines, where a comment line's first non-space byte is `#`. Any other content — including a UTF-8 byte order mark — is an error (`E_LOAD_MODULE_ROOT_MALFORMED`), and the load fails rather than ignoring the marker. Refusing content now reserves the whole format space, so no file written today can be reinterpreted by a later release.

The nearest marker wins, so a nested marker deliberately narrows a sub-module's root. The walk runs on the canonical (symlink-resolved) ancestor chain: a marker reachable only through a symlinked spelling of the path is not consulted. Under an explicit root no marker is read at all.

**Size:** A schema source — the entry file or an import — is read up to 16 MiB; a larger file is refused before it is parsed.

**Security:** A load that reads imports from disk sandboxes their paths using `os.Root` to prevent path traversal attacks: `os.Root` resolves every component, a symlink's target included, against an open handle on the module root and rejects a path that would leave it. The module root is that sandbox's boundary, so committing a `yammm.mod` grants the loader read access to that directory's whole subtree for imports — which is what makes a repository-relative import resolve. A load under `schema.WithSyntheticRoot` reads no disk: every import is looked up among the sources the caller passed, with or without `WithSourcesOnly`, and a relative import resolves against the importing source's key as text, so one that climbs above the root keeps its `..` rather than being rejected (`schema.SyntheticImportKey` states the rule).

**Default alias derivation:** When no explicit `as` clause is provided, the alias is derived from the last path segment:

- Strip trailing slashes and `.yammm` extension
- Replace non-alphanumeric/underscore characters with underscore (e.g., `address-types` becomes `address_types`)
- If the first character is not a letter, `n` is prepended to produce a valid identifier (e.g., `3rdparty` becomes `n3rdparty`, `_internal` becomes `n_internal`)

**Alias identifier requirements:** Aliases must be valid identifiers per the grammar—they must start with a letter (A-Z or a-z) and contain only letters, digits, and underscores. When automatic derivation would produce an invalid identifier (starting with a digit or underscore), `n` is prepended automatically. An explicit `as` clause can always be used to override the derived alias:

```yammm
// "./3rdparty" derives alias "n3rdparty" (digit-first, so "n" is prepended)
import "./3rdparty"
```

```yammm
// An explicit alias overrides the derived one
import "./3rdparty" as thirdparty
```

The two forms are alternatives, not a pair: importing one file twice draws `E_DUPLICATE_IMPORT` however the aliases differ.

**Reserved keyword restriction:** Aliases cannot be reserved keywords. The rule table holds no keywords — every keyword lexes as an ordinary word — so this is a parser rule. A written `as` clause that holds one is `E_SYNTAX`, and a path that derives one draws `E_INVALID_ALIAS`. Reserved keywords include:

- DSL keywords: `schema`, `import`, `as`, `type`, `datatype`, `required`, `primary`, `extends`, `includes`, `abstract`, `part`, `one`, `many`, `in`
- Built-in type keywords: `Integer`, `Float`, `Boolean`, `String`, `Enum`, `Pattern`, `Timestamp`, `Date`, `UUID`, `Vector`, `List`
- Boolean literals: `true`, `false`
- Nil literal: `nil`

**Qualified type references:** Imported types must be referenced with their alias qualifier:

```yammm-snippet
type Car {
    color common.Color required      // qualified datatype reference
    --> WHEELS (one:many) parts.Wheel // qualified type reference
}
```

**Qualified datatype references:** Custom datatypes from imported schemas must also be qualified with their alias:

```yammm-snippet
// common.yammm
schema "Common"
type Money = Float[0, _]

// main.yammm
schema "Main"
import "./common" as common

type Money = Integer[0, _]  // local datatype (different constraint!)

type Product {
    count Money required            // uses local Money (Integer)
    price common.Money required     // uses imported Money (Float)
}
```

**Import cycles:** Circular imports are detected and reported as errors during loading.

## Types

Types are the fundamental building blocks of a YAMMM schema. A type defines a named structure with properties, relationships, and constraints.

### Type Declaration

```text
TypeDecl = [ DOC_COMMENT ] [ "abstract" | "part" ] "type" TypeName [ ExtendsClause ] "{" TypeBody "}" .
TypeName = UC_WORD .
TypeBody = { Property | Association | Composition | Invariant | TypeAnnotation } .
```

A basic type declaration:

```yammm
type Person {
    name String required
    age Integer[0, 150]
}
```

### Type Identity

A type's identity is the pair (schema source, type name). Types with the same name from different schemas are distinct — an imported `Person` and a local `Person` are not the same type. This enables:

- Cross-schema type comparison without name collisions
- Proper diamond inheritance deduplication (same ancestor via different paths is recognized as one type)
- Safe use as map keys in type-indexed data structures (graph, snapshot)

### Type Modifiers

**Abstract types** cannot be instantiated directly but can be extended by other types:

```yammm
abstract type Vehicle {
    vin String primary
}

type Car extends Vehicle {
    model String required
}
```

**Part types** are used as composition targets. They represent entities that are owned by and embedded within their parent:

```yammm
part type Wheel {
    position Enum["FL", "FR", "RL", "RR"] required
    size Integer[14, 22] required
}

type Car {
    *-> WHEELS (one:many) Wheel
}
```

### Immutability

After compilation, all schema objects — types, relations, data types, and the schema itself — are sealed and become immutable. An accessor that returns a slice returns a fresh copy. This guarantees thread-safe concurrent read access without synchronization.

### Inheritance

Types may extend one or more parent types using the `extends` clause:

```text
ExtendsClause = "extends" TypeRef { "," TypeRef } [ "," ] .
TypeRef       = [ qualifier=AliasName "." ] name=TypeName .
```

Multiple inheritance is supported:

```yammm
abstract type Named {
    name String required
}

abstract type Timestamped {
    createdAt Timestamp required
}

type Document extends Named, Timestamped {
    content String required
}
```

Inheritance rules:

- Properties, associations, and compositions are inherited from parent types
- Child types may override inherited properties with compatible narrower constraints
- A part type carries no association, declared or inherited (`E_INVALID_ASSOCIATION_TARGET`)
- Relationship definitions must be unique after inheritance. Two relations of one kind reaching a type under one field name are an error when their definitions **differ** — a different declared spelling, target, multiplicity, or edge-property set; structurally identical ones are deduplicated keep-first with no diagnostic. An association and a composition that reach one type under one name are caught separately, after both merges, as `E_RELATION_COLLISION` naming both declarations. `E_RELATION_COLLISION` is reported once per collision per affected type: ancestors that merely carry a relation forward do not repeat it, and two ancestors declaring structurally identical rivals are one collision, since a single edit to the surviving declaration resolves both. The diagnostic's related locations name the surviving declaration and every rival

**One edge per supertype:** An `extends` clause that names one supertype twice declares one edge.

**Linearization order:** Ancestors are flattened **ancestors-first**: each supertype's own ancestors are appended before the supertype itself, left to right across the `extends` clause, with keep-first deduplication. For `D extends C`, `C extends B`, `B extends A` the order is `[A, B, C]`; for `D extends B, C` with both extending `A` it is `[A, B, C]`.

1. Own declarations (from the type body) come first
2. Inherited **properties, relations and type-level annotations** follow in that ancestors-first order, so the most distant ancestor is read before the nearest. **Invariants and property-level annotations do not** — see Invariant merging and Annotation Inheritance below, where a subtype reads each direct supertype's already-merged set so a nearer declaration wins
3. When the same ancestor is reachable via multiple paths (diamond inheritance), the first occurrence is kept and duplicates are skipped

**Property conflict resolution:**

| Situation | Result |
| --------- | ------ |
| Child re-declares with narrower constraint | Child's version is used |
| Inherited property narrows an earlier ancestor's | Narrower version replaces wider |
| Child re-declares with a constraint that is not a valid narrowing | `E_PROPERTY_CONFLICT` is emitted |
| Two inherited properties are incompatible | `E_PROPERTY_CONFLICT` is emitted |

`E_PROPERTY_CONFLICT` is reported once per conflict per affected type, so ancestors that merely carry a conflicting property forward do not repeat the diagnostic. The message names the declaration that survived the merge and the first one that clashed with it; the diagnostic's related locations name **every** declaration involved — both the full set that merged into the surviving definition (including declarations absorbed as equal-or-wider, and any that later took over as a narrower survivor) and every declaration on the conflicting side. Resolving the conflict may require editing any of them, so none is omitted. Each subtype that inherits a conflicting combination re-detects and reports it independently.

**Invariant merging:** Own invariants come first, then those inherited from each direct supertype in declaration order, each supertype contributing the set it has already merged. An invariant's message is its identity, and must not be empty (`E_INVALID_INVARIANT`). A subtype's declaration overrides an inherited one of the same name at any depth — a grandparent's rule cannot reappear past a parent that replaced it. One type may not declare a message twice (`E_DUPLICATE_INVARIANT`), and two inherited definitions of one message must carry the same expression; a disagreement is `E_INVARIANT_CONFLICT`, reported once on the type that inherits them, naming every rival. Expressions are compared structurally, as the structural hash compares them: `n > 1` and `n > 1.0` are different expressions, because an integer and a float literal are different literals. A literal with no value, whose own diagnostic already refuses the source, matches any literal of the kind the source wrote there (a string, an integer, a float or a regular expression), so the definitions of one message conflict only when no value for each such literal makes them all one, whatever order the supertypes are named in. One definition reached through two ancestors merges silently.

### Constraint Narrowing

When a child type re-declares an inherited property, the child's constraint must be a valid _narrowing_ of the parent's constraint. The principle: every value accepted by the child constraint must also be accepted by the parent (the child's valid set is a subset of the parent's).

| Constraint | Narrowing Rule |
| ---------- | -------------- |
| `String` | Child min length >= parent min AND child max length <= parent max |
| `Integer` | Child min >= parent min AND child max <= parent max |
| `Float` | Child min >= parent min AND child max <= parent max |
| `Enum` | Child values must be a subset of parent values |
| `List` | Element constraint narrows AND length bounds narrow |
| `Boolean`, `Date`, `UUID` | Equal only (no parameterized narrowing) |
| `Timestamp` | Equal only (format must match exactly) |
| `Pattern` | Equal only (pattern strings must match) |
| `Vector` | Equal only (dimension must match) |

Data type aliases are resolved to their underlying constraint before narrowing is checked.

**Property narrowing rules:**

- **Optionality** can narrow (optional to required) but not widen (required to optional)
- **Primary key status** cannot change (structural identity)
- **Names** must match exactly (case-sensitive)

### Type References

Type references may be local or qualified with an import alias:

```yammm-snippet
type Car {
    --> OWNER Person                    // local type reference
    --> DEALER (one) dealers.Dealer     // qualified reference
}
```

## Properties

Properties define the data fields of a type.

### Property Declaration

```text
Property     = [ DOC_COMMENT ] PropertyName DataTypeRef [ "primary" | "required" ] { Annotation } .
PropertyName = LC_WORD .   // except as, part, in, nil, true, false
```

Property names must start with a lower-case letter. A property may not be named `self`; see Lexical Structure.

### Property Modifiers

**Primary properties** form part of the type's identity. They are implicitly required:

```yammm
type Car {
    vin String primary      // primary key
    regNbr String required
}
```

Every concrete (instantiable) type must declare or inherit at least one primary property. A type with no primary key has no identity — it cannot be added to a graph or referenced by an association — so it is rejected at load with `E_NO_PRIMARY_KEY`. Abstract types (never instantiated) and part types (identified through their parent composition) are exempt. A type may declare more than one primary property; together they form a composite key.

**Required properties** must be present in all instances:

```yammm
type Person {
    name String required    // must be present
    age Integer             // optional
}
```

Properties without modifiers are **optional** and may be omitted from instance data.

**An explicit `null` is the absent case.** For a property — on a type or on an
association's edge — instance data carrying `null` is treated exactly as if the
key were not there: a required property draws `E_MISSING_REQUIRED`, and an
optional one is dropped rather than stored. The other slots that accept a
`null` keep their own rules, because none is a property: an association's
value must be an object or an array of objects, so `{"employer": null}` reports
`E_EDGE_SHAPE_MISMATCH` rather than reading as absent (leave the key out to
omit the association); a foreign-key component is a structural key position and
reports `E_TYPE_MISMATCH`; a composition value must be an array and reports
`E_EDGE_SHAPE_MISMATCH`; and a `null` element of a `(many)` association array
or a `null` composition child reports `E_EDGE_SHAPE_MISMATCH` too.

**A `null` inside a collection is not the absent case.** No element position is
optional, so the element constraint applies to it and `["a", null]` is rejected
for a `List<String>`. This is what distinguishes a whole-value `null` — which
drops an optional property — from a `null` element inside one.

**The empty string is a value.** `""` satisfies `required` and is accepted as a
primary key, yielding the key `[""]`. A bare `String` declares no lower bound;
`String[1, _]` is what excludes the empty string.

#### Primary Key Types

Only the following types may be used as primary keys:

| Allowed | Why |
| ------- | --- |
| `String` | Natural identifiers (names, codes, external IDs) |
| `UUID` | Purpose-built for identity |
| `Date` | Natural key for temporal data (daily reports, events) |
| `Timestamp` | Natural key for time-series data (event logs) |

All other types are rejected:

| Banned | Why |
| ------ | --- |
| `Integer`, `Float` | Numeric values are typically mutable; no auto-increment |
| `Boolean` | Cardinality of 2, useless as identity |
| `Enum` | Small finite set, poor identity |
| `Pattern` | A validation constraint over a string, not an identity |
| `Vector` | Collection, not comparable for identity |
| `List` | Collection, not comparable for identity |

DataType aliases are resolved before checking: `type VIN = String[17, 17]` followed by `vin VIN primary` is valid because `VIN` resolves to `String`.

### Relationship Properties

Associations may have their own properties, declared within the relationship body:

```text
RelProperty = [ DOC_COMMENT ] PropertyName DataTypeRef [ "required" ] .
```

```yammm-snippet
type Person {
    --> WORKS_AT Company {
        startDate Date required
        title String
    }
}
```

Relationship properties are narrower than type properties: they cannot use `Vector` or `List` data types, cannot be `primary`, cannot carry annotations, and default to optional unless `required` is specified.

## Data Types

YAMMM provides a set of built-in data types and supports user-defined type aliases.

### Built-in Data Types

```text
DataTypeRef = BuiltIn | QualifiedAlias .
BuiltIn     = IntegerT | FloatT | BoolT | StringT | EnumT | PatternT |
              TimestampT | DateT | UUIDT | VectorT | ListT .
```

#### Integer

Represents signed integer values with optional bounds:

```text
IntegerT = "Integer" [ "[" min "," max "]" ] .
min      = [ "-" ] ( "_" | INTEGER ) .
max      = [ "-" ] ( "_" | INTEGER ) .
```

A minus sign before `_` is accepted and has no effect; the loader reports it at Warning severity under `E_INVALID_CONSTRAINT`, and the schema still loads.

The underscore `_` represents an unbounded limit. An optional leading `-` allows negative bounds.

Examples:

```yammm-snippet
age Integer                  // unbounded integer
age Integer[0, 150]          // 0 to 150 inclusive
count Integer[1, _]          // minimum 1, no maximum
index Integer[_, 99]         // no minimum, maximum 99
temperature Integer[-40, 50] // negative lower bound
```

Validation accepts signed and unsigned integers, a named type over one included, and refuses every float, whole or not. A pointer to an integer is no integer. In a data document an `Integer` value is an integer literal — no `.`, `e` or `E` — read exactly, so `5.0` and `1e2` are floats and are refused. Unsigned inputs larger than `int64` are rejected before bound checks, and so is an integer literal outside the `int64` range: validation never reads it as its nearest float, which for a literal just below the minimum is the minimum itself. A `.ys` snapshot stores such a literal as its text and writes it back unchanged, and revalidation refuses it.

#### Float

Represents floating-point values with optional bounds:

```text
FloatT = "Float" [ "[" min "," max "]" ] .
min    = [ "-" ] ( "_" | INTEGER | FLOAT ) .
max    = [ "-" ] ( "_" | INTEGER | FLOAT ) .
```

As for `Integer`, a minus sign before `_` is accepted with a Warning and no effect.

Validation accepts floats and integers; an integer is widened to `float64`. In a JSON data document a `Float` value is any number literal, read as its nearest `float64`, so `-0` keeps its sign; a literal no finite `float64` holds, such as `1e400`, is refused. `NaN` and the infinities are refused. A `.ys` snapshot reads a number literal under a `Float` as its nearest `float64`, with or without revalidation: `5` loads as `5.0`, `-0` keeps its sign, and `9007199254740993` loads as its nearest `float64`, `9007199254740992.0`, which the next write states. A literal no finite `float64` holds stays as written, and revalidation refuses it.

Examples:

```yammm-snippet
temperature Float            // unbounded float
percentage Float[0.0, 100.0] // 0 to 100 inclusive
ratio Float[0, 1.0]          // 0 to 1 inclusive
latitude Float[-90.0, 90.0]  // negative lower bound
```

#### Boolean

Represents true/false values:

```text
BoolT = "Boolean" .
```

Example:

```yammm-snippet
active Boolean
isPublished Boolean required
```

#### String

Represents UTF-8 string values with optional length bounds (counted in runes, not bytes):

```text
StringT = "String" [ "[" minLen "," maxLen "]" ] .
minLen  = "_" | INTEGER .
maxLen  = "_" | INTEGER .
```

Examples:

```yammm-snippet
name String                  // unbounded string
name String[1, 100]          // 1 to 100 runes
code String[3, 3]            // exactly 3 runes
notes String[_, 1000]        // maximum 1000 runes
```

#### Enum

Represents a value from a fixed set of string options:

```text
EnumT = "Enum" "[" STRING "," STRING { "," STRING } [ "," ] "]" .
```

At least two options must be provided, and they must be distinct and non-empty. The count is of the values written, and an empty or repeated value is refused at the value, so `Enum["a", "a"]` and `Enum["a", ""]` are each refused with one diagnostic, at the second value, and `Enum["a"]` is refused for its count.

Examples:

```yammm-snippet
status Enum["pending", "approved", "rejected"]
color Enum["red", "green", "blue"]
priority Enum["low", "medium", "high", "critical"]
```

#### Pattern

Represents a string that must match one or more regular expressions:

```text
PatternT = "Pattern" "[" STRING [ "," STRING ] "]" .
```

Examples:

```yammm-snippet
email Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
phone Pattern["^\\d{3}-\\d{3}-\\d{4}$"]
```

When two patterns are provided, the value must match both. A list of more than two is `E_INVALID_CONSTRAINT`, counting every pattern written, so a pattern refused for its own fault counts too and draws its own diagnostic beside the count's.

A pattern follows Go's `regexp` syntax, and one that does not compile is `E_INVALID_CONSTRAINT`. So is a pattern that writes a surrogate code point (U+D800 to U+DFFF) with a `\x{…}` escape, alone or in a class: Go compiles it, but no string holds a surrogate, so the surrogate matches nothing. A class that spans the surrogate block, such as `[\x{D000}-\x{E000}]`, writes none.

#### Timestamp

Represents a date-time value with optional format specification:

```text
TimestampT = "Timestamp" [ "[" format "]" ] .
format     = STRING .
```

The format string follows Go's time formatting conventions. When omitted, RFC 3339 with fractional seconds (`"2006-01-02T15:04:05.999999999Z07:00"`) is used.

**Accepted input and stored form.** A `Timestamp` property accepts a Go `time.Time` or a string, and **stores the string** rendered through the declared format when one is given and through RFC 3339 with nanoseconds otherwise. A string is parsed and re-rendered, so one instant at one offset reaches one spelling however it was written: `"…+00:00"` stores as `"…Z"`, and `".500Z"` stores as `".5Z"`. The UTC offset survives — an offset records where an observation happened and is data, not formatting. A `time.Time` is rendered through the layout, and that text is judged and stored as a string is, so a time and the text it renders as reach one spelling: RFC 3339 writes an offset under a minute as `+00:00`, which is stored as `Z`. A time whose text the rule refuses is refused, such as a time outside years 0 to 9999. Stored text is text the layout reads and stores as itself: a string that its layout reads and stores as text the layout does not read back as written is refused. Under `"2006-01-02 15:04:05 -07:00:00"` an offset two seconds west of UTC is one, since Go writes it as `+00:00:-02`, which it does not read. Under a layout that writes an offset's minutes, and writes the offset once, the offset `+24:60` is one, since Go reads it as 25 hours and writes `+25:00`, which it does not read. Go reads the offset `-00:00:01` as no offset, so text at it is stored at UTC.

**A declared format that cannot represent an instant loses what it omits.** A value is stored as the text its layout writes, so a value stored under a layout keeps none of the fields the layout drops, and a reader parsing the text back gets a different instant when the value held more than its text keeps. At schema load a declared layout that is not refused writes two instants and reads each text back: 13:34:56.789123456 at UTC+05:30 on 2026-08-19, and 00:07:08.1 at UTC-09:30 on 1950-11-23. It draws `W_TIMESTAMP_LOSSY_FORMAT` when a field of either instant reads back changed, and the message names each field lost: the year or its century, the month, the day, the hour or its half of the day, the minute, the second, the fractional second or its digits past a count, and the UTC offset or its minutes. So `"2006-01-02T15:04:05.000Z07:00"` loses the fractional second past 3 digits. The layout still loads, since a wall-clock layout is a legitimate domain choice. Go reads a fractional second after a seconds field whose layout declares none, so under such a layout a string that holds a fraction is accepted and stored without it. Neither instant's offset holds seconds, since RFC 3339 writes none, so a layout that drops an offset's seconds draws no warning for them. The two instants judge a layout and do not prove it: a layout whose number fields touch with no separator, such as `"2006-01-215:04:05"`, reads both back, and still refuses or misreads a value whose day has one digit.

**A format that no `Timestamp` can hold is refused.** Each of these is `E_INVALID_CONSTRAINT` on the format literal, and draws no warning:

- An empty format, `Timestamp[""]`. `Timestamp` with no format is RFC 3339.
- A format that holds a zone abbreviation, alone as in `"2006-01-02 15:04 MST"` or beside a numeric offset as in `"2006-01-02 15:04 -0700 MST"`. An abbreviation names no one offset, and Go reads the text `UTC` at an abbreviation as UTC whatever numeric offset stands beside it. A numeric offset, such as `Z07:00`, states the zone. Go's `time.RFC1123`, `time.RFC850`, `time.RFC822` and `time.UnixDate` each hold an abbreviation.
- A format that does not read back the text it writes for one of the two instants above. `"2006-12-02 15:04:05"` is one: Go reads its `12` as a month and a day, so it writes 19 August 2026 as `2026-819-19`, which it cannot read. The message shows the instant and the text.

Examples:

```yammm-snippet
createdAt Timestamp                                          // the default, RFC3339Nano
eventTime Timestamp["2006-01-02T15:04:05.000000000Z07:00"]   // explicit, fixed 9-digit fraction
logTime Timestamp["2006-01-02 15:04:05"]                     // custom, and lossy: no zone, no fraction
```

#### Date

Represents a date value (without time component):

```text
DateT = "Date" .
```

A `Date` property accepts a Go `time.Time` or a `"YYYY-MM-DD"` string, and **stores the string**. A `time.Time` is truncated to its calendar day **in its own location**: an instant at 00:30 `+02:00` is the 19th, and the same instant written 22:30 `Z` is the 18th. The location the caller built the value in decides. A `time.Time` whose text is no `"YYYY-MM-DD"` date, such as one outside years 0 to 9999, is refused.

Example:

```yammm-snippet
birthDate Date
expiryDate Date required
```

#### UUID

Represents a universally unique identifier:

```text
UUIDT = "UUID" .
```

A `UUID` property accepts a Go `uuid.UUID` or a string, and **stores the canonical lowercase hyphenated text**. Every spelling the parser accepts — uppercase, brace-wrapped, `urn:uuid:`-prefixed, or bare 32 hex digits — stores as the same text, so one identifier is one value and one primary key.

Example:

```yammm-snippet
externalId UUID
correlationId UUID required
```

#### Vector

Represents a fixed-dimension numeric vector (for embeddings, coordinates, etc.):

```text
VectorT = "Vector" "[" dimensions "]" .
dimensions = INTEGER . /* 1 to 65536 inclusive */
```

Examples:

```yammm-snippet
embedding Vector[768]       // 768-dimensional vector
coordinates Vector[3]       // 3D coordinates
```

Validation accepts any Go slice whose elements are numbers, floats or integers, such as `[]float64`, `[]float32`, `[]int`, `[]json.Number` or `[]any`, a named slice type and a slice of a named number type included, or an `immutable.Slice` of numbers. Each element is stored as a `float64`. A Go array and a pointer to a slice are refused; a nil slice holds no value. `NaN`, `Inf`, and non-numeric elements are rejected.

#### List

Represents an ordered collection of typed values:

```text
ListT       = "List" "<" ElementType ">" [ "[" minLen "," maxLen "]" ] .
ElementType = DataTypeRef .
minLen      = "_" | INTEGER .
maxLen      = "_" | INTEGER .
```

The element type can be any built-in type (including `List` for nesting), a `DataType` alias, or `Vector`. The underscore `_` represents an unbounded limit.

Examples:

```yammm-snippet
tags List<String>                          // unbounded list of strings
tags List<String[_, 6]>                    // each string max 6 runes
tags List<String>[1, 5]                    // 1 to 5 elements
tags List<String[_, 6]>[1, 5]             // element + length constrained
matrix List<List<Integer>>                 // nested list
embeddings List<Vector[768]>               // list of vectors
```

Validation accepts JSON arrays where each element passes the element type's constraint. Length bounds (when present) are checked against the array length.

**Restrictions:**

- List types cannot be used as primary keys (see [Primary Key Types](#primary-key-types)).
- List types cannot be used in relationship (edge) properties.

**Neo4j portability.** A list whose *element* is itself a collection — `List<List<T>>`, `List<Vector[N]>`, or a list of a list-typed alias — is valid yammm and validates normally, but has no equivalent in Neo4j, which has no nested collection property type. `yammm neo4j constraints` rejects such a property with `E_NEO4J_UNSUPPORTED_TYPE` under every flag, `--scalar-types=false` included, and because constraint generation is all-or-nothing it emits **nothing for the whole schema**, not just for that property. `yammm neo4j indexes` does not make the check, and prints the schema's index statements. The Neo4j adapter's write path refuses a value of such a property, an empty list included, and not an absent one. `yammm neo4j constraints` judges a property on each concrete type and part type with a valid label that declares or inherits it. Two exceptions follow from that: it does not report a property of a type whose label is no valid identifier, which it reports with `E_NEO4J_INVALID_IDENTIFIER` instead, or a property declared on an abstract type that no such type extends. Model the inner collection as a `part type` reached by a composition if the model must be written to Neo4j.

**Narrowing:**

When a child type re-declares a parent's List property, both the element constraint and the length bounds must narrow (element values form a subset, length range is a subrange):

```yammm-snippet
abstract type Base {
    tags List<String>                      // unbounded
}
type Child extends Base {
    tags List<String[1, 50]>[1, 10]       // element + bounds narrowed
}
```

**Data Type Aliases:**

List types can be used as DataType alias targets:

```yammm-snippet
type Tags = List<String[1, 50]>[1, 10]
type Article {
    tags Tags required
}
```

### Data Type Aliases

Custom data types are defined as aliases over built-in types:

```text
DataTypeDecl = [ DOC_COMMENT ] "type" TypeName "=" BuiltIn .
```

Examples:

```yammm
type Color = Enum["red", "green", "blue"]
type PositiveInt = Integer[1, _]
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
type Money = Float[0, _]
```

Aliases are:

- Declared with an upper-case identifier
- Case-preserved: the declared name is canonical (not lowercased internally)
- Referenced by name in property declarations
- Not chainable: an alias target must be a built-in, so `type Money = PositiveInt` is a syntax error
- Resolved by reference, not by declaration order: a `List` target may name another alias as its element, declared before or after it (`type Codes = List<Code>`). An alias that reaches itself through `List` elements — `type Tree = List<Tree>`, or two aliases that list each other — has no constraint to resolve to, and each alias on the cycle is refused with `E_INVALID_CONSTRAINT`

Using aliases:

```yammm
type Color = Enum["red", "green", "blue"]

type Car {
    paintColor Color required
    accentColor Color
}
```

## Relationships

YAMMM supports two types of relationships between types: associations and compositions.

### Associations

Associations represent references between independent entities:

```text
Association  = [ DOC_COMMENT ] "-->" RelationName [ Multiplicity ] TypeRef
               [ "{" { RelProperty } "}" ] .
RelationName = upper_letter { upper_letter | decimal_digit | "_" } .
```

A relation name is UPPER_SNAKE. Its **field name** — the key instance data
carries the relation under, and the entry an invariant expression reads — is
the name in lower case: `WORKS_AT` is read as `works_at`. An invariant reads
the relation by either spelling, as a bare name or as a member, and a read in
any other casing is rejected at load with `E_INVALID_NAME` wherever the checker
knows the value read from is an instance of a type holding the relation. A read
from a value whose type it cannot tell, such as an element of a list literal
that mixes kinds, reads nothing in another casing at evaluation, as a member the
instance lacks reads nothing. A variable names
it by its field name alone (`$works_at`), since variable names are matched
exactly. Instance data carries it under the field name, matched as a property
name is.

Examples:

```yammm-snippet
type Person {
    --> WORKS_AT Company              // optional, one
    --> MANAGES (many) Person         // optional, many
    --> REPORTS_TO (one) Person       // required, one
}

type Car {
    --> OWNER (one:many) Person       // required, many owners
}
```

The target must be a concrete type (not abstract or a `part` type), and a `part` type cannot declare or inherit an association at all — both draw `E_INVALID_ASSOCIATION_TARGET`, an inherited one once, on the first part type in the chain that inherits it. An association
edge is resolved by the target's identity, and neither an abstract type (never
instantiated) nor a part type (reachable only through composition) can be the
referenced node.

### Compositions

Compositions represent ownership where child entities are embedded within their parent:

```text
Composition = [ DOC_COMMENT ] "*->" RelationName [ Multiplicity ] TypeRef .
```

The target must be a concrete `part` type (not abstract).

Examples:

```yammm
part type Wheel {
    position String required
}

type Car {
    *-> WHEELS (one:many) Wheel       // embedded wheel instances
}
```

Composition data is embedded inline in instance documents rather than using reference objects, and is **always an array**, `(one)` included. Anything else draws `E_EDGE_SHAPE_MISMATCH`. This differs from associations, where `(one)` takes a single object.

A part instance is identified through its parent composition, not by a global key. The library mints no address for one. An exporter that must give each part node an identity of its own — the Neo4j adapter's `_composed_key` property is the only one that does — derives it from the **owning root**, not from the immediate parent: the root's identity in that store, the root's key, then one segment per composition hop — for a `(one)` composition the relation name alone, since the slot holds exactly one child; for a `many` composition the relation name and either the child's key or, for a **keyless** part, its array position. Rooting the address at the owning root is what keeps it correct below depth one, and carrying the root's identity is what stops two root types that share a key value and a relation name from naming one part node. A positional segment is **not stable across writes**; it is safe only under replace semantics, where a parent write deletes and recreates its composed subtree.

### Multiplicity

Multiplicity specifies the cardinality of a relationship:

```text
Multiplicity     = "(" MultiplicitySpec ")" .
MultiplicitySpec = "_" [ ":" ( "one" | "many" ) ]
                 | "one" [ ":" ( "one" | "many" ) ]
                 | "many" .
```

| Syntax | Required | Cardinality |
| ------ | -------- | ----------- |
| (omitted) | no | one |
| `(_)` | no | one |
| `(_:one)` | no | one |
| `(_:many)` | no | many |
| `(one)` | yes | one |
| `(one:one)` | yes | one |
| `(one:many)` | yes | many |
| `(many)` | no | many |

Examples:

```yammm-snippet
--> OWNER Person              // optional, single owner
--> OWNER (one) Person        // required, single owner
--> OWNERS (many) Person      // optional, multiple owners
--> OWNERS (one:many) Person  // required, at least one owner
```

### Association Data in Instances

Association edges in instance data are represented as objects containing:

- Target primary key(s) using reserved `_target_` prefixed fields
- Any association properties defined in the relationship

For **single primary key** targets, use `_target_<pk_name>` — the target's primary-key property name, not a literal `id`:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Alice",
  "works_at": {
    "_target_id": "660e8400-e29b-41d4-a716-446655440001",
    "startDate": "2020-01-15",
    "title": "Engineer"
  }
}
```

For **composite primary key** targets, use `_target_<fieldName>` for each key component:

```json
{
  "id": "order-001",
  "customer": {
    "_target_firstName": "John",
    "_target_lastName": "Doe"
  }
}
```

For **to-many associations**, use an array of edge objects:

```json
{
  "id": "alice",
  "knows": [
    { "_target_id": "bob", "weight": 0.9 },
    { "_target_id": "carol", "weight": 0.5 }
  ]
}
```

**Reserved prefix:** The `_target_` prefix is reserved for foreign key fields. An identifier cannot start with an underscore, so no user-defined relation name starts with it.

## Annotations

Annotations attach validated, store-agnostic metadata to properties and types. The core validates their structure and eligibility at load; downstream adapters interpret them (for example, turning them into index or write-shape DDL). Annotations do not change what instance data is valid, and they are excluded from the structural hash.

### Syntax

A **property-level** annotation trails a property, after its datatype and any `primary`/`required` modifier. A property may carry zero or more:

```yammm-schema
state        String      @index
title        String      @fulltext
first_seen   Timestamp   @writeOnce
embedding    Vector[768] @vector(cosine)
```

A **type-level** annotation is a type-body member written with the doubled sigil `@@`, optionally preceded by a doc comment. It may appear anywhere in the body:

```yammm-schema
type Document {
  content_hash String primary
  state        String
  title        String
  published_on Date

  @@index(state, published_on)
  @@fulltext(title, state)
}
```

The single/double sigil split is required, not stylistic. Because whitespace — including newlines — is not significant to the parser, a standalone type-level annotation written with a single `@` after a property would be parsed as another annotation on that property; the `@@` sigil disambiguates.

**Placement on the line.** A property-level annotation must be written on the same line as the property it annotates. Because whitespace is not significant, an annotation on a line of its own still binds to the property **above** it — never the one below — so writing it prefix-style, as decorators are written in Python, TypeScript, or Java, would silently mark the wrong property and emit the wrong store DDL. That shape is rejected with `E_INVALID_ANNOTATION` naming the property it would have attached to. A property may carry several annotations, all on its own line (`first_seen Timestamp @writeOnce @index`); a marker meant for the type as a whole is written `@@name` on its own line.

**Ordering.** A property-level annotation must follow the datatype and any modifier: `state String @index` is valid, but `state String @index primary` is a syntax error — the modifier must precede the annotation. Recovery does not target the misplaced modifier, because `primary` is also a legal property name and the parser cannot tell the two readings apart. The reported `E_SYNTAX` is anchored at the first token that cannot continue the type body — often its closing brace, a line below the mistake. The property itself is still recorded, so the rest of the type is checked normally.

**Arguments** are positional. When parentheses are present they must hold at least one argument — `@index()` is a syntax error; a no-argument annotation is written without parentheses (`@index`). A trailing comma is allowed.

### Blessed annotations (v1)

| Annotation | Placement | Arguments | Eligible target | Meaning |
|---|---|---|---|---|
| `@index` | property | none | a scalar property (String, UUID, Enum, Pattern, Integer, Float, Boolean, Date, Timestamp); rejected only when it is the sole primary key of every type that emits a constraint for it (see below) | single-property range index |
| `@@index(p1, p2, …)` | type | one or more property references, ordered, no duplicates | each reference a scalar property of the type (own or inherited); primary-key members allowed | composite range index (order significant) |
| `@vector(similarity)` | property | exactly one keyword: `cosine` or `euclidean` | a `Vector[N]` property | vector (approximate-nearest-neighbour) index |
| `@fulltext` | property | none | a text property (String, Pattern, Enum); primary keys allowed, sole or composite (see below) | single-property fulltext index |
| `@@fulltext(p1, p2, …)` | type | one or more property references, ordered, no duplicates | each reference a text property of the type (own or inherited); primary-key members allowed | fulltext index over the referenced properties (order significant) |
| `@writeOnce` | property | none | any non-primary-key property of a non-`part` type | marks the property immutable after node creation |

A member of a **composite** primary key may carry `@index`: the composite backing index does not serve single-property lookups on it. A **sole** primary key may not, because its uniqueness constraint already backs an index.

Which of the two applies is decided over the types that actually **emit** the constraint — the annotated type when it is concrete, plus its concrete descendants — not over the type carrying the annotation. An abstract type emits nothing, and its key is only settled once a concrete type assembles it, so a key split across two abstract mixins is composite where it lands even though each mixin declares one member.

`@index` is rejected only when **every** emitter keys on the property alone. When emitters disagree — one keys on it alone while another keys on it compositely and genuinely needs the index — no edit satisfies both, so the annotation is accepted and the redundant index ships on the sole-keyed emitter. No tool reports it: `DiffIndexes` counts a constraint-backed index as a match, deliberately, so that a fully-applied schema does not diff dirty forever. Descendants declared in **other schema files** are not visible to this check, so the same model can be accepted when split across files and rejected when written in one; that is a known limit, not a contract.

`@writeOnce` is rejected on every primary-key member, sole or composite.

`@fulltext` and `@@fulltext` are narrower than the index pair: fulltext applies to **text**, so the eligible kinds are `String`, `Pattern`, and `Enum`. `UUID` is deliberately excluded — it is stored as a string but is an opaque identifier, not tokenized text. **Primary keys are allowed for both fulltext placements, with no redundancy rule**: a uniqueness constraint's backing index is a range index and cannot serve fulltext queries, so a fulltext index over a sole primary key is a distinct, legitimate object — the `@index` sole-key rule deliberately does not transfer.

Annotation eligibility is **not** independent of type category. `@writeOnce` is rejected on every property of a `part` type, and the `@index` sole-primary-key rule skips abstract types, so an abstract mixin may carry `@index` on a sole declared primary key where a concrete type may not.

### Validation

Annotations are validated at load, after inheritance linearization. Violations produce these diagnostics:

- `E_UNKNOWN_ANNOTATION` — the name is not a blessed annotation at either placement. A name blessed at the *other* placement draws `E_INVALID_ANNOTATION` instead.
- `E_INVALID_ANNOTATION` — a structural violation: wrong placement (e.g. `@@writeOnce`), a property annotation written on a line other than its property's, wrong arity, a literal where a reference or keyword is required, an unknown similarity keyword, or a duplicate.
- `E_UNKNOWN_ANNOTATION_TARGET` — a `@@index` or `@@fulltext` reference names no property of the type (the same failure class as `E_UNKNOWN_PROPERTY`, on a different construct).
- `E_INVALID_ANNOTATION_TARGET` — the annotation is attached to an ineligible property (a non-scalar `@index`, a non-`Vector` `@vector`, a non-text `@fulltext`, `@writeOnce` on a primary key, and so on).
- `W_ANNOTATION_SHADOWED` — a warning (not an error): a subtype re-declaration drops an inherited property's annotations without re-stating them (see [Annotation Inheritance](#annotation-inheritance)). The load still succeeds.

### Annotation Inheritance

Property-level annotations travel with the property they decorate: a subtype that inherits a property unchanged inherits its annotations. Type-level annotations linearize like properties and relations, not like invariants — the subtype's own annotations first, then inherited ones in ancestors-first order, with exact duplicates (same name and argument list) deduplicated keep-first and no override by name. Two `@@index` members differing only in argument list are distinct indexes and both survive.

Annotations **do not survive a property re-declaration.** When a subtype re-declares an inherited property — identically or by narrowing — without re-stating the inherited annotations, those annotations drop from the subtype. The load warns of the drop with `W_ANNOTATION_SHADOWED`, which does not fail it, for an annotation no direct supertype dropped, unless that annotation drew its own diagnostic or the re-declaration drew `E_PROPERTY_CONFLICT`. Re-state the annotations on the re-declaration to keep them.

When a subtype does not re-declare an inherited property, its annotations are the **union across its direct supertypes' own resolved views**, independent of `extends` order. Because each supertype's view already reflects that supertype's decisions, this is transitive in the ordinary sense:

- **Mixins** both contribute, so a `@writeOnce` on either ancestor's copy survives.
- **A chain** contributes only its most-derived member. A type's decision binds its own subtypes: if `Mid extends Base` re-declares `Base`'s annotated property without the annotation, then `Leaf extends Mid` sees `Mid`'s version — the annotation does not reappear further down.

**Never declaring an annotation is not the same as dropping one.** An ancestor that simply never mentioned `@writeOnce` has no opinion about it, which is why the mixin union above is silent. An ancestor that *inherited* it and re-declared the property without it has made a decision, and that decision is carried forward to every subtype.

When those two meet — one direct supertype carries an annotation while another **dropped** it — the supertypes **disagree**, and there is no merge that honours both. This is reported as `E_INVALID_ANNOTATION`:

```yammm
abstract type Base    { first_seen Timestamp @writeOnce }
abstract type Mutable extends Base { first_seen Timestamp }   // drops it — a decision
abstract type Other   extends Base { tag String }             // carries it
type Doc extends Mutable, Other { id String primary }         // E_INVALID_ANNOTATION
```

Resolve it on the subtype, which is authoritative: re-declare the property **with** the annotation to keep it, or **without** it to drop it. Two arms that both drop it agree: the subtype draws neither `E_INVALID_ANNOTATION` nor a warning, and each arm draws its own `W_ANNOTATION_SHADOWED` at its re-declaration. `Leaf` draws no `W_ANNOTATION_SHADOWED` of its own if it re-declares the property, because by then there is nothing left to drop.

Two nearest ancestors annotating the same property with the same annotation name but conflicting arguments — in practice only differing `@vector` similarities, since a `Vector[N]` property is equal regardless of its similarity keyword — is an unsatisfiable conflict, reported as `E_INVALID_ANNOTATION`. One shadowed annotation, and one such conflict, draws exactly one diagnostic however many ancestors carry the property forward.

## Expressions and Invariants

Invariants are constraints attached to types that are evaluated during instance validation.

### Invariant Declaration

```text
Invariant = [ DOC_COMMENT ] "!" message=STRING expr=Expr .
```

The message is displayed when the invariant evaluates to false:

```yammm
type Person {
    name String required
    age Integer[0, 150]

    ! "name must not be empty" name -> Len > 0
    ! "age must be positive" age == nil || age >= 0
}
```

An invariant whose expression evaluates to `nil` **fails** the invariant (`E_INVARIANT_FAIL` — nil is falsey). An invariant whose expression evaluates to any non-boolean, non-nil value is an **evaluation error** (`E_EVAL_ERROR`).

### Expression Grammar

Expressions support a rich set of operators and built-in functions.

```text
Expr = Literal
     | "[" [ Expr { "," Expr } [ "," ] ] "]"           // list
     | "-" Expr                                         // unary minus
     | Expr "[" [ Expr { "," Expr } ] "]"              // indexing
     | Expr "->" Name [ Arguments ] [ Parameters ] [ "{" Expr "}" ]  // pipeline
     | Expr "." Name                                    // property access
     | "!" Expr                                         // logical not
     | Expr ( "*" | "/" | "%" ) Expr                   // multiplicative
     | Expr ( "+" | "-" ) Expr                         // additive
     | Expr ( "<" | "<=" | ">" | ">=" ) Expr           // comparison
     | Expr "in" Expr                                   // membership
     | Expr ( "=~" | "!~" ) Expr                       // regex match
     | Expr ( "==" | "!=" ) Expr                       // equality
     | Expr "&&" Expr                                   // logical and
     | Expr ( "||" | "^" ) Expr                        // logical or/xor
     | Expr "?" "{" Expr ":" Expr "}"                  // ternary
     | "(" Expr ")"                                     // grouping
     | VARIABLE                                         // variable reference
     | PropertyName                                     // property reference
     | DataTypeKeyword                                  // datatype literal
     | UC_WORD                                          // relation name
     | "_"                                              // nil literal
     | "nil"                                            // nil literal (alias)
     .

Arguments  = "(" [ Expr { "," Expr } ] [ "," ] ")" .
Parameters = "|" VARIABLE { "," VARIABLE } [ "," ] "|" .
Name       = LC_WORD | UC_WORD .
```

The `Name` after `->` is a built-in function's name. The `Name` after `.` is a
member — a property or a relation's field name — and admits any word, keywords
included: a property may be named `type` and a relation `IN`, so `$self.type`
and `$self.in` are member accesses. Whether the member exists is the type's
question, answered by the static check below, not the grammar's.

> **`_` and `nil` in expressions.** Within invariant expressions, `_` and `nil` are interchangeable — both produce a nil literal. Use whichever reads more naturally: `end_date == nil` for null-guard idioms, `end_date == _` for consistency with other DSL contexts. Note that `_` retains distinct, non-nil roles in constraint bounds (`Integer[0, _]`) and multiplicity (`(_:many)`), where `nil` cannot be used.

### Operator Precedence

Operators are listed from highest to lowest precedence:

| Precedence | Operators | Associativity |
| ---------- | --------- | ------------- |
| 1 | Literals, list literals `[...]` | - |
| 2 | Unary minus `-x` | Right |
| 3 | Postfix: indexing `expr[i]`, pipeline `lhs -> name(args)\|$params\|{body}`, property access `lhs.name` | Left |
| 4 | Logical not `!expr` | Right |
| 5 | Multiplicative `*`, `/`, `%` | Left |
| 6 | Additive `+`, `-` | Left |
| 7 | Comparisons `<`, `<=`, `>`, `>=` | Left |
| 8 | Membership `in` | Left |
| 9 | Regex match `=~`, `!~` | Left |
| 10 | Equality `==`, `!=` | Left |
| 11 | Logical and `&&` | Left |
| 12 | Logical or/xor `\|\|`, `^` | Left |
| 13 | Ternary `? { then : else }` | Left |

The three postfix operators share one level and apply left to right: `$self.tags[0]` indexes the property, `$i.name -> Len` pipes it, and `LINES -> First.qty` reads a member of the pipeline's result. The name after `.` is a property or relation name, never an expression.

Parentheses group as usual.

### Operators

#### Arithmetic Operators

```text
+    addition (numbers) or concatenation (strings and lists)
-    subtraction or unary negation
*    multiplication
/    division
%    modulo (integers only)
```

`+` is polymorphic three ways: numbers add (mixed integer/float promotes to float), strings concatenate, and lists concatenate (`[1] + [2]` is `[1, 2]`). The static check refuses any other pairing at load — a list beside a scalar, a string beside a number, a boolean on either side, the `nil` literal on either side, an instance on either side. An association to a String-keyed type is a string here, as it is everywhere: `PLACED_BY + "!"` concatenates the key.

Integer arithmetic is exact within `int64`. A result outside it — `+`, `-`, `*`, `/` (the minimum divided by `-1`), unary `-` of the minimum, `Sum`, `Abs` — is an evaluation error, as division and modulo by zero are; it never wraps and never promotes to a float. An arithmetic operator applied to `nil` is an evaluation error too, `+` included: guard an absent value with `IsNil`, `Then`, `Lest` or `Default` — an absent list under `+` is written `(xs -> Default([])) + [1]`.

#### Comparison Operators

```text
==   equal
!=   not equal
<    less than
<=   less than or equal
>    greater than
>=   greater than or equal
```

`==` and `!=` do not raise evaluation errors across mismatched operand types: `==` evaluates to not-equal (false) and `!=` to true, and `in` evaluates to false when element types do not match. Equality is **structural** and never errors: two instances are equal when they hold the same members and each member is equal by this rule, recursively; two lists when their elements are pairwise equal; `1 == 1.0` and `NaN == NaN` hold. `in`, `Contains` and `Unique` use the same equality, so `LINES -> Contains(LINES[0])` is true and two identical composed children are one under `Unique`. The ordered comparisons below still refuse an instance.

Ordered comparisons (`<`, `<=`, `>`, `>=`) follow a **total order across type strata** — `nil` < booleans < numbers < strings < lists — so a mixed-type ordered comparison is defined, not false: `1 < "a"` is true, `nil < 0` is true, and `false < true` is true. Within the numeric stratum, integers and floats compare exactly. Floats order `-Inf` < finite < `+Inf` < `NaN`, and `NaN` equals `NaN`, so `Sort`, `Unique`, and `Contains` treat non-finite values consistently. An ordered comparison on an unsupported shape (a map) is an evaluation error.

Timestamp comparison is string comparison: RFC 3339 values with mixed UTC offsets do not order by instant. `Date` values (`YYYY-MM-DD`) order correctly by construction. `UUID` values order over their canonical lowercase text.

#### Logical Operators

```text
!    logical not
&&   logical and (short-circuit)
||   logical or (short-circuit)
^    logical xor
```

#### Membership Operator

```text
in   membership test (value in collection)
```

Example:

```yammm-snippet
! "status must be valid" status in ["active", "inactive", "pending"]
```

#### Pattern and Type Match Operators

```text
=~   matches pattern or type
!~   does not match pattern or type
```

These operators support two modes:

**Regular expression matching:** When the right operand is a regex literal, performs pattern matching:

```yammm-snippet
! "email must be valid" email =~ /.+@.+\..+/
```

**Type checking:** When the right operand is one of the datatype keywords listed below, checks whether the value matches that type at runtime:

```yammm-snippet
! "value must be integer" value =~ Integer
! "price must be numeric" price =~ Float
! "must not be string" data !~ String
```

Supported datatype keywords for type checking:

- `String` - checks for string values
- `Integer` - checks for integer values
- `Float` - checks for floating-point values
- `Boolean` - checks for boolean values
- `UUID` - checks for valid UUID strings
- `Timestamp` - checks for valid timestamp values
- `Date` - checks for valid date values

Each check applies the rule a property of that kind applies when it declares no constraint: `=~ Float` is false for NaN and infinities, `=~ String` is false for a number, and `=~ Timestamp` accepts an RFC 3339 string or a `time.Time` whose RFC 3339 text reads back. A nil is the exception: a property accepts an absent value, and a nil is of no kind, so `x =~ String` is false and `x !~ String` is true while `x` is nil. `=~ Timestamp` reads RFC 3339 whatever layout a property declares: a value stored under `d Timestamp["2006-01-02"]`, such as `"2030-01-02"`, validates there and `d =~ Timestamp` is false. An invariant has no layout to apply, since `=~` judges a value at run time and sees no property's constraint. `Vector`, `List`, `Enum` and `Pattern` are datatype keywords but not type checks — they name a shape or a constraint, not a kind a value can have — and `=~` against one is refused at schema load with `E_INVALID_INVARIANT`.

Numeric type checks follow the property rules: `5 =~ Float` holds, because an integer widens to a float, and `5.0 =~ Integer` does not — a float is never an integer, whole or not.

#### Ternary Operator

```text
condition ? { trueExpr : falseExpr }
```

Example:

```yammm-snippet
! "adult status" age >= 18 ? { "adult" : "minor" } == category
```

The else branch is required. Omitting the `:` branch is a load-time syntax error:

```yammm-invalid
schema "bad"

type T {
    id String primary
    // E_SYNTAX: a conditional requires an else branch
    ! "no else" id != nil ? { true }
}
```

### Indexing

The bracket operator accepts exactly one index:

```text
expr[index]        // single element access
```

Works on:

- Strings (rune indexing)
- Lists

A non-integer index is an evaluation error. An out-of-range index evaluates to `nil`, not an error. Indexing `nil` evaluates to `nil`. There is no range slice: two or more bracket arguments draw an evaluation error.

The bracket grammar is more permissive than the language: it also accepts an empty index list (`expr[]`, an evaluation error) and a trailing comma (`expr[a,]`, evaluated as `expr[a]`). These parse without a diagnostic — do not write either.

### Property Access

Properties are accessed using dot notation:

```yammm-snippet
$self.name         // explicit self reference
name               // implicit property reference
$item.price        // lambda parameter property
```

Invariant expressions are checked **statically** at schema load. The checker types every sub-expression — an instance, an association key, a list, a scalar — and follows the type through member access, indexing and every pipeline stage whose builtin declares its result (`Reduce` declares none), so a lambda parameter is the element its collection holds and `ITEMS -> Map |$i| { $i.PART } -> All |$p| { $p.sku != "" }` binds `$p` to a `Part`. A builtin's result is typed by its subkind — `Len`, `Sum`, `Count`, `Compare` and the numeric functions yield a number; `Upper`, `Lower`, `Trim`, `TrimPrefix`, `TrimSuffix`, `Join`, `Replace`, `Substring` and `TypeOf` a string; `Split` and `Match` a list of strings; the predicates and `IsNil` a boolean; `Min` and `Max` with an argument the receiver or the argument, ranked by the total order — so the stage after it is judged: `name -> Len -> Upper` is refused, as `age -> Upper` is. Among what it rejects: a reference to a property not declared on the type (`E_UNKNOWN_PROPERTY`), however the instance was reached; a member read through an association key, a scalar or a list (`E_INVALID_INVARIANT`); an undefined named variable; a function name the language does not define; and a call shape its builtin refuses — a missing or unexpected lambda, too many parameters, too few or too many arguments; and a receiver or an argument of a kind its builtin refuses on every input (`E_INVALID_INVARIANT`) — `age -> Upper` on a number, `name -> Match("nor")` with a string where a pattern is required, `name -> Substring("a")` with a string where a number is — because an invariant that fails on every instance is a defect in the schema, not in the data.

**The nil guards are typed by one rule.** `Default`, `Coalesce` and `Lest` evaluate to one of their alternatives — the receiver, or the fallback that stands in for it when the receiver is nil — and `Then` to its body, or nil for an absent receiver. The checker types each as what every alternative agrees on, so the stage after the guard is typed by a value it predicted. Alternatives of different kinds — a string beside a number, a list beside a scalar, an instance beside either — are refused at load (`E_INVALID_INVARIANT`): `(name -> Coalesce(1)) -> Upper` and `(note -> Lest { 1 }) -> Upper` are refused as `(name -> Default(1)) -> Upper` is, and `note -> Lest { true }` is refused because with `note` present the invariant evaluates to a string. The nil literal stands in for any receiver and the empty list literal for any list. Two instance types agree on the members both declare, whatever their ancestry: after `(A_SLOT -> Default(B_SLOT))` a member declared on `A` and on `B` reads, and a member declared on one of them alone is `E_UNKNOWN_PROPERTY`, since the evaluator would read nil there on the input that selects the other. A conditional and a list literal join their branches the same way, and admit alternatives of different kinds as a value of unknown kind. At evaluation time, a declared-but-absent optional property evaluates to `nil` — this is what makes the `IsNil` / `Then` / `Lest` / `Default` guard idioms work. Member access on a non-map value is an evaluation error; member access on `nil` evaluates to `nil`.

**Relations are in scope**, under the relation's field name — the UPPER_SNAKE
name in lower case — so `WORKS_AT` and `works_at` read one entry as a bare name
or as a member, and any other casing there, such as `Works_At`, is rejected at
load (`E_INVALID_NAME`) wherever the checker knows the value read from, and
reads nothing at evaluation where it does not; a variable reads it as
`$works_at` alone. What a
relation evaluates to depends on where its data lives:

- A **composition**'s children are part of the instance, so the relation
  evaluates to those children — a list for a `many` composition, the single
  child otherwise — and each child's own properties are readable. `ITEMS -> Len`,
  `ITEMS -> All |$i| { $i.qty > 0 }` and `ITEMS[0].qty` all read real values.
- An **association**'s target is a reference. The instance holds the foreign
  key, not the target's row, so the relation evaluates to the target key — a
  list for a `many` association, the single key otherwise. A key reads as the
  target's primary-key property, which is a string for every key kind, and a
  composite key as a list of those strings: `PLACED_BY -> Upper`,
  `PLACED_BY + "!"` and `REGION -> Len == 2` are typed as the values they
  are. Presence, cardinality and key comparison are answerable; the target
  type's properties are not in this instance to answer with, and reaching for
  one is rejected at load (`E_INVALID_INVARIANT`).

A relation with no value evaluates to `nil`, so the null-guard idioms apply to a
relation exactly as they do to a property.

```yammm-schema
schema "orders"

type Customer { id String primary }

part type Line {
    sku String primary
    qty Integer required
}

type Order {
    id String primary
    --> PLACED_BY (one) Customer
    *-> LINES (one:many) Line

    ! "has_a_customer" PLACED_BY != nil
    ! "has_lines" LINES -> Len > 0
    ! "positive_quantities" LINES -> All |$l| { $l.qty > 0 }
}
```

### Variables and Scope

**Numeric variables** (`$0`, `$1`, ...) are evaluator-local and default to `nil` when unset.

**Named variables** resolve in the invariant's scope — lambda parameters first, then the instance's own members, so `$age` reads the property `age` when no parameter shadows it. A name bound by neither, save `$self` below, is rejected at schema load (`E_INVALID_INVARIANT`). Variable names are matched exactly: `$myVar` and `$myvar` are two names, where property names are matched case-insensitively. A lambda parameter may be named `$self`, and then shadows the instance for the body. A property may not be named `self`: the binding reaches the instance first, so it is refused at load (`E_INVALID_NAME`).

**`$self`** is bound when evaluating invariants against property maps. A lambda body's scope keeps the binding unless a parameter named `$self` overrides it.

Lambda parameters shadow outer variables.

### Built-in Functions

Built-in functions are invoked via the pipeline `->` syntax; the left-hand side is implicitly the first argument:

```text
expr -> function(args)|$params|{ body }
```

#### Collection Functions

| Function | Description |
| -------- | ----------- |
| `Map` | Transform each element: `items -> Map \|$x\| { $x.value * 2 }` |
| `Filter` | Keep matching elements: `items -> Filter \|$x\| { $x.active }` |
| `Count` | Count matching elements: `items -> Count \|$x\| { $x.valid }` |
| `All` | True if all match (true on empty): `items -> All \|$x\| { $x.valid }` |
| `Any` | True if any match (false on empty): `items -> Any \|$x\| { $x.enabled }` |
| `AllOrNone` | True if all match or none (true on empty) |
| `Reduce` | Aggregate with accumulator: `items -> Reduce(0) \|$acc, $item\| { $acc + $item }` |
| `Compact` | Remove nil entries from slices |
| `Unique` | Deduplicate slice/array inputs |
| `Sum` | Sum numeric elements: `items -> Sum` |
| `First` | First element (nil if empty): `items -> First` |
| `Last` | Last element (nil if empty): `items -> Last` |
| `Sort` | Sort elements: `items -> Sort` |
| `Reverse` | Reverse element order: `items -> Reverse` |
| `Flatten` | Flatten one level of nesting: `items -> Flatten` |
| `Contains` | Check if element exists: `items -> Contains(value)` |

Notes:

- `nil` inputs are treated as empty collections
- A scalar receiver is never a collection: piping a number or string into a collection function is an evaluation error

Results on an empty collection are a mixed family:

| Functions | Result on empty |
| --------- | --------------- |
| `All`, `AllOrNone` | `true` (vacuous truth) |
| `Any`, `Contains` | `false` |
| `First`, `Last` | `nil` |
| `Sum`, `Count` | `0` |
| `Sort`, `Reverse`, `Flatten`, `Unique`, `Compact`, `Map`, `Filter` | empty list |
| `Min`, `Max` | evaluation error |
| `Reduce` without an initial value | evaluation error |

#### Numeric Functions

| Function | Description |
| -------- | ----------- |
| `Len` | Length of string (runes), list, or map (nil yields 0) |
| `Abs` | Absolute value |
| `Floor` | Floor of float |
| `Ceil` | Ceiling of float |
| `Round` | Round to the nearest whole number (banker's rounding); the result keeps the operand's kind, as `Floor` and `Ceil` do — a Float yields a Float |
| `Min` | Minimum value: `a -> Min(b)` or `items -> Min` |
| `Max` | Maximum value: `a -> Max(b)` or `items -> Max` |
| `Compare` | Three-way comparison: `a -> Compare(b)` returns -1, 0, or 1, for any two values the total order ranks; an instance is refused |

#### String Functions

| Function | Description |
| -------- | ----------- |
| `Upper` | Convert to uppercase: `s -> Upper` |
| `Lower` | Convert to lowercase: `s -> Lower` |
| `Trim` | Remove leading/trailing whitespace: `s -> Trim` |
| `TrimPrefix` | Remove prefix: `s -> TrimPrefix("pre")` |
| `TrimSuffix` | Remove suffix: `s -> TrimSuffix("suf")` |
| `Split` | Split by separator: `s -> Split(",")` |
| `Join` | Join elements: `items -> Join(",")` |
| `StartsWith` | Check prefix: `s -> StartsWith("pre")` |
| `EndsWith` | Check suffix: `s -> EndsWith("suf")` |
| `Replace` | Replace all occurrences: `s -> Replace("old", "new")` |
| `Substring` | Extract substring: `s -> Substring(start)` or `s -> Substring(start, end)`; negative indices count from the end; out-of-range indices clamp |

#### Control Flow Functions

| Function | Description |
| -------- | ----------- |
| `Then` | Execute body when non-nil: `value -> Then \|$v\| { $v.prop }`; typed as the body |
| `Lest` | Execute body when nil: `value -> Lest { default }`; typed as the join of receiver and body |
| `With` | Bind params and execute: `value -> With \|$v\| { $v.prop }` |

#### Pattern Matching

| Function | Description |
| -------- | ----------- |
| `Match` | Regex match with captures: `s -> Match(/pattern/)` |

#### Utility Functions

| Function | Description |
| -------- | ----------- |
| `TypeOf` | DSL type name as string: `value -> TypeOf` yields `"nil"`, `"boolean"`, `"integer"`, `"float"`, `"string"`, `"list"`, `"map"`, or `"pattern"`; any value outside that vocabulary yields `"unknown"`. The name comes from the value, not from the declared property type, so a `Timestamp`, `Date` or `UUID` property yields `"string"` — validation stores all three as text, whichever Go representation the caller submitted |
| `IsNil` | Check if nil: `value -> IsNil` |
| `Default` | Return default if nil: `value -> Default(fallback)`; typed as the join of receiver and fallback |
| `Coalesce` | Return first non-nil: `a -> Coalesce(b, c)`; typed as the join of receiver and every argument |

### Example Invariants

```yammm
part type Part {
    name String required
    price Float[0, _] required
}

type Person {
    name String[1, 100] required
    age Integer[0, 150]
    email String
    description String
    start_date Date required
    end_date Date
    hasA Boolean
    hasB Boolean

    // Composition to Part (many cardinality)
    *-> PARTS (many) Part

    // Required string length
    ! "name is required" name -> Len > 0 && name -> Len <= 100

    // Age range check — guarded, because age is optional
    ! "age must be valid" age == nil || (age >= 0 && age <= 130)


    // Email format validation — guarded, because email is optional
    ! "email must be valid" email == nil || email =~ /.+@.+/

    // Mutually exclusive fields — guarded, because both flags are optional and
    // && over an absent operand is an evaluation error, not false
    ! "cannot have both" hasA == nil || hasB == nil || !(hasA && hasB)

    // Nil-guard: skip validation when optional field is absent (using _)
    ! "desc not empty if present" description == _ || description != ""

    // Nil-guard: same check using nil (equivalent to _ in expressions)
    ! "email not empty if present" email == nil || email != ""

    // Cross-field date validation with nil guard
    ! "dates valid" end_date == nil || end_date > start_date
}
```

### Evaluation Notes

- The evaluator only works against the in-memory instance graph
- There is no implicit database lookup. An association reads as its target key, so no navigation passes through one; a composition reads as its children, whose members an expression reads in turn, at any depth
- Evaluation errors surface as error-severity diagnostics, among them a type error, an integer division or modulo by zero, an integer overflow, and a `$` read of a member the instance leaves unset; validation continues collecting further issues. A named variable that nothing binds is a load-time error, not an evaluation error, and so is a reference to an undeclared property wherever the checker knows the type it reads from. A declared member the instance leaves unset evaluates to `nil` read by its bare name; read as a `$` variable it binds nothing, which is the evaluation error above
- A genuine panic is recovered as a Fatal `E_INTERNAL` carrying a Go stack trace, which stops that instance's validation. Division by zero is not a panic — see Division semantics

### Evaluation Model

Invariant expressions are always evaluated against concrete instance data. There is no concept of deferred or incomplete evaluation — all property values must be resolved before invariant checking begins.

**Scope chain:** Invariant expressions are evaluated in a scope containing the instance's property values, overlaid with variables. When a variable and a property share a name, the variable takes precedence.

**`$self` binding:** `$self` is bound to the instance's property map during invariant evaluation. It is inherited by child scopes (e.g., inside lambda bodies) unless explicitly overridden by a lambda parameter named `$self`.

**Evaluation order:** Expressions are evaluated eagerly, left-to-right, with the following exceptions:

- `&&` and `||` short-circuit: the right operand is skipped when the left operand determines the result
- The ternary operator (`?`) evaluates only the selected branch
- Collection functions (`Map`, `Filter`, `All`, etc.) evaluate their body expression once per element

**Variable resolution:**

- Numeric variables (`$0`, `$1`, ...) evaluate to `nil` when not bound
- Named variables (`$item`, `$acc`, ...) are rejected at schema load if not bound by a lambda parameter or an instance member
- Lambda parameters shadow outer variables for the duration of the body

**Division semantics:** Integer division by zero produces an evaluation error. Float division by zero yields +/-Inf per IEEE 754. Integer modulo by zero produces an evaluation error.

**Panic recovery:** Panics during constraint checking or invariant evaluation are recovered at the validator boundary. Recovered panics become `E_INTERNAL` fatal diagnostics with a captured stack trace.

## Diagnostics

YAMMM uses structured diagnostics with stable error codes. The Go programmatic interface (`diag.Result` methods, `diag.Renderer`) is documented in [API.md](API.md#diagnostics).

### Severity Levels

| Severity | Description |
| -------- | ----------- |
| `Fatal` | Unrecoverable condition |
| `Error` | Validation failure but processing continues |
| `Warning` | Non-blocking advisory |
| `Info` | Informational message |
| `Hint` | Suggestion for improvement |

### Diagnostic Codes

Codes are stable identifiers for programmatic matching. `diag/code.go` holds the built-in codes, and `adapter/neo4j` registers its own on import. Categories and their codes:

**Sentinel** — internal conditions:

- `E_INTERNAL` — unexpected invariant failure (internal bug indicator)
- `E_COMMAND_FAILED` — a command-line failure that is not itself a diagnostic, carried into a `--format json` document with the process exit code as its `exit_code` detail
- `E_CONTEXT_CANCELLED` — operation cancelled via context

**Schema** — schema compilation errors:

- `E_DUPLICATE_TYPE` — type name conflicts
- `E_DUPLICATE_SCHEMA` — two schemas in one load declare one name. A schema whose load reported an error takes no name and draws no such report
- `E_INHERIT_CYCLE` — circular inheritance chain
- `E_UNKNOWN_TYPE` — unresolvable type reference
- `E_DUPLICATE_PROPERTY`, `E_UNKNOWN_PROPERTY` — a property declared twice on one type or an edge property twice on one association, or an invariant reading a member the type it reads neither declares nor inherits
- `E_DUPLICATE_RELATION`, `E_RELATION_COLLISION` — relation conflicts
- `E_DUPLICATE_INVARIANT`, `E_INVARIANT_CONFLICT` — an invariant message declared twice on one type, or inherited with two different expressions (a literal with no value matches any literal of its kind)
- `E_CASE_COLLISION` — two property names of a type, or two edge property names of an association, differ only by case
- `E_PROPERTY_RELATION_COLLISION` — a property's lower-cased name equals a relation's field name, the relation name in lower case
- `E_INVALID_ASSOCIATION_TARGET`, `E_INVALID_COMPOSITION_TARGET` — relation definition errors
- `E_INVALID_CONSTRAINT` — constraint definition error
- `E_INVALID_INVARIANT` — an invariant's expression or message is invalid
- `E_INVALID_NAME` — a name the schema refuses: a relation name that is not UPPER_SNAKE, an empty schema name, a property or relation field named `self`, or an invariant that reads a relation in a casing other than its name or its field name
- `E_INVALID_PRIMARY_KEY_TYPE` — disallowed type for primary key
- `E_NO_PRIMARY_KEY` — concrete type declares or inherits no primary key
- `E_LIST_ON_EDGE` — List type used in relationship property
- `E_PROPERTY_CONFLICT` — conflicting inherited properties
- `E_UPSTREAM_FAIL` — imported schema failed to compile
- `E_LOAD_IO_FAILURE` — I/O error during schema loading, or a load whose arguments or options the loader refuses
- `E_LOAD_MODULE_ROOT_MALFORMED` — a `yammm.mod` module-root marker holds content other than comment lines, is not a regular file, is larger than the size limit, or is a link that resolves to no file
- `E_UNKNOWN_ANNOTATION`, `E_INVALID_ANNOTATION` — annotation name, placement, arity, argument kind or value, duplicate, or inherited-disagreement errors
- `E_UNKNOWN_ANNOTATION_TARGET`, `E_INVALID_ANNOTATION_TARGET` — annotation target-property errors (unknown reference / ineligible property)
- `W_ANNOTATION_SHADOWED` — a re-declaration silently drops an inherited property's annotations (warning)
- `W_TIMESTAMP_LOSSY_FORMAT` (Warning) — a `Timestamp["layout"]` declares a layout that cannot reproduce an instant, and the message names each field that one of the two probe instants loses; one that holds a zone abbreviation, or that does not read back the text it writes, draws `E_INVALID_CONSTRAINT` in its place
- `E_REVERSE_CLAUSE_REMOVED` — the schema carries the reverse clause removed in v0.15.0

**Syntax** — parse errors:

- `E_SYNTAX` — syntax error in schema source

**Import** — import resolution errors:

- `E_IMPORT_RESOLVE` — import path could not be resolved
- `E_IMPORT_CYCLE` — circular import dependency
- `E_INVALID_ALIAS` — an import with no `as` clause derives a reserved word as its alias from its path, such as `import "./schema"`. A written `as` clause that holds a reserved word is `E_SYNTAX`
- `E_PATH_ESCAPE` — import path escapes allowed directory
- `E_IMPORT_NOT_ALLOWED` — imports refused via `WithImportsAllowed(false)`, and by `LoadString`, which takes none
- `E_DUPLICATE_IMPORT` — the same schema imported multiple times, or two different schemas sharing one alias
- `E_IMPORT_ALIAS_COLLISION` — import alias collides with local name

**Instance** — data parse and validation errors:

- `E_INSTANCE_TYPE_NOT_FOUND` — type not found in schema
- `E_ABSTRACT_TYPE` — attempt to instantiate abstract type, an empty batch under its name included
- `E_PART_TYPE_DIRECT` — attempt to directly instantiate part type, an empty batch under its name included
- `E_TYPE_MISMATCH` — value has wrong type
- `E_MISSING_REQUIRED` — required property missing
- `E_UNKNOWN_FIELD` — unexpected field in instance data
- `E_CONSTRAINT_FAIL` — constraint check failed
- `E_INVARIANT_FAIL` — invariant check failed
- `E_EVAL_ERROR` — expression evaluation error. A call to an unknown built-in function is refused at load with `E_INVALID_INVARIANT`, so no loaded schema reaches the evaluator's own refusal of one
- `E_MISSING_FK_TARGET` — an edge object holds none of its target's `_target_` foreign-key fields
- `E_PARTIAL_COMPOSITE_FK` — partial composite foreign key
- `E_UNKNOWN_EDGE_FIELD` — unknown field in edge data
- `E_EDGE_SHAPE_MISMATCH` — an edge or a composition value has a JSON shape its relation does not take, such as an object where an array belongs or the reverse, a null or a scalar, or a composed child or a `(many)` edge element that is not an object; and, at `graph.NewFromSnapshot` under another schema, an `"empty"` unresolved record under a `(one)` association, the record an empty list leaves
- `E_UNRESOLVED_REQUIRED_COMPOSITION` — required composition unresolved
- `E_COMPOSITION_DEPTH_EXCEEDED` — composed nesting exceeds the depth limit (32), the bound the `.ys` reader holds a document to with `E_SNAPSHOT_DEPTH_EXCEEDED`, so a snapshot can carry every nesting a validator accepts
- `E_DUPLICATE_COMPOSED_PK` — a composition slot cannot hold this child: two children of one `(many)` slot share a primary key, or a `(one)` slot is given several. Listed here as well as under Graph: the validator raises it over the children it validates together. The import raises it over the snapshot it is given, and `snapshot.Load` and `snapshot.Verify` raise it for two children of a keyed `(many)` slot at one canonical key and for a `(one)` slot a document holds several children in; `snapshot.Info` reads without a schema and makes no such claim
- `E_DUPLICATE_EDGE` — a `(many)` association names one target twice for one source: a graph holds one edge per source, association and target. The validator raises it at each listing of a target key that an earlier listing names, where no other check refused either listing, since two listings can carry different edge properties; a refused listing's own issue stands for the repeat. `graph.NewFromSnapshot` raises it over a snapshot built under another schema, for an edge and a `target_missing` record, or two such records, of one source that name one canonical target key, where no other check refused either listing, one sound root (of a type that can hold a root, its stored key admitted by the key rule) holds the source's address alone, and any root at the target's address is one sound root alone; elsewhere the issue at the listing, the source or the target stands for the repeat. Two edges there would name two roots at one address, which the import refuses at the address: `E_DUPLICATE_PK` where both roots are sound, the refused root's own issue otherwise. `snapshot.Load` and `snapshot.Verify` refuse such a document with `E_SNAPSHOT_MALFORMED` where no other check refused the repeating listing, one sound root holds the source's address alone, and an edge's target's, and no root stands at a record's target and no refused unit states it; elsewhere the issue at the listing, the source or the target stands for the repeat; `snapshot.Info` reads no schema, so it knows no multiplicity and does not judge it. A `(one)` association never draws this code: its count, `E_GRAPH_CARDINALITY`, judges a second record there
- `E_INVALID_TYPE_TAG` — a type tag that is not a type name by the grammar: a JSON document's top-level key
- `E_DATA_PARSE` — an instance document `yammm check` or `yammm snapshot save` reads is not well formed, is not one object, holds a type name whose value is not an array of objects, states one member name twice in one object, or nests deeper than the decoder's 10,000-level limit
- `E_CASE_FOLD_COLLISION` — multiple input fields map to the same schema property, relation field, edge property or `_target_` foreign-key field after case-folding. Property name matching is case-insensitive by default (see `WithStrictPropertyNames` in [API.md](API.md)). When two or more input fields fold to one schema property and none of them matches it exactly (schema `name`, input `Name` and `NAME`), the collision is reported and neither field is mapped. An input name that matches a schema property exactly is claimed first and never collides

**Graph** — graph construction errors:

- `E_DUPLICATE_PK` — duplicate primary key
- `E_DUPLICATE_COMPOSED_PK` — a composition slot cannot hold this child (also raised by the validator and the snapshot reader; see Instance)
- `E_DUPLICATE_EDGE` — a `(many)` association names one target twice for one source (also raised by the validator; see Instance)
- `E_UNRESOLVED_REQUIRED` — required association unresolved
- `E_GRAPH_TYPE_NOT_FOUND` — at a snapshot's import, a type identity the importing schema cannot resolve
- `E_GRAPH_INVALID_COMPOSITION` — invalid composition
- `E_GRAPH_MISSING_PK` — a root, or an imported snapshot's types entry, whose type declares no primary key
- `E_GRAPH_INVALID_PK` — a keyed type's primary key empty, of the wrong arity, with a key property absent or null, or disagreeing with the instance's own key properties; an association target key of the wrong arity; or a key stored on a composed child whose type declares no primary key
- `E_GRAPH_CARDINALITY` — a (one) association holds more than one record: a merge at `Graph.AddOrMerge` that would add a second target, several edges and unresolved records no check refused, in a snapshot or a `.ys` document
- `E_GRAPH_UNKNOWN_RELATION` — instance data or an association record under a relation name the type does not declare in that slot, or an association record naming a target type the schema resolves, other than the declared one
- `E_GRAPH_ABSTRACT_TYPE` — an imported snapshot's root or types entry names a type the importing schema makes abstract

**Snapshot** — persistence errors:

- `E_SNAPSHOT_MALFORMED` — invalid JSON, missing required fields, a wrong outer shape (the four top-level keys, in order, each once and none null, with nothing after them, and no object repeating a member name or spelling a declared one in another case), or content a structural rule refuses, such as an undeclared name, a stored key its key properties contradict, a key or target key component `graph.ParseKey` cannot read back, a target key of the wrong arity, an undocumented reason, a record contradicting itself, records `graph.Add` would not derive, a duplicate with no conflict or another one, a duplicate stating a parent or a relation (a `null` `parent_type`, and a `null` or empty `parent_key` or `relation`, state none, and the next `Marshal` drops the member), or a string holding invalid UTF-8 or a surrogate escape that is half of no pair, which `encoding/json` would silently read as U+FFFD. `snapshot.Marshal` raises it for input it refuses to write: an indent that is not JSON whitespace, or a metadata key or value, or a provenance source name or raw path, that is not valid UTF-8; `snapshot.UpdateMetadata` raises it for such metadata too
- `E_SNAPSHOT_UNSUPPORTED_VERSION` — unrecognized format version. A surface that holds the whole document raises it only for a document whose outer shape, member names and text pass, which it judges first
- `E_SNAPSHOT_UNSUPPORTED_FEATURE` — unrecognized feature flag
- `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` — schema structural hash mismatch
- `E_SNAPSHOT_UNKNOWN_TYPE` — type name not found in schema
- `E_SNAPSHOT_TYPE_MISMATCH` — an instance or duplicate record states a type row other than the one it is filed under, or a composed child's or an association record's target row is not the relation's declared target
- `E_SNAPSHOT_DANGLING_REFERENCE` — an edge target, a duplicate's conflict, or an unresolved record's source names no root instance; a reference to an address a refused unit states draws none, the unit's refusal being its one issue
- `E_SNAPSHOT_INVALID_COMPOSED` — composed child carries edges, or composed children stand under a name the parent's type does not declare as a composition
- `E_SNAPSHOT_INVALID_ROOT` — an instances group, empty or not, or a duplicate record that states no parent names a type that cannot hold a root instance: abstract, a part type, or declaring no primary key. Refused whatever the load options say — none of the three describes a graph any caller could have built. One type draws one issue, however many positions name it, and a root of it holds no address: it owes no required association, it is in no `(one)` count, and a second root at its key is no duplicate. A break its records or its slots show by themselves stays its own
- `E_DUPLICATE_PK` — two root instances in one group, both of whose stored keys the key rule accepts, state one primary key. Under a schema, only roots of a type that can hold a root count. Listed here as well as under Graph: `snapshot.Load`, `snapshot.Verify` and `snapshot.Info` emit it, because the wire has a diagnostics section for a rejected duplicate and two live instances at one address is not a shape it can carry. `Info` resolves no schema, so it judges no type and compares keys as the reader's number rule renders them, where `Load` and `Verify` also fold a timestamp or UUID key written two ways
- `E_SNAPSHOT_COMPOSED_ON_DUPLICATE`, `E_SNAPSHOT_EDGES_ON_DUPLICATE` — illegal data on duplicate records
- `E_SNAPSHOT_DEPTH_EXCEEDED` — composed nesting exceeds depth limit (32)
- `E_SNAPSHOT_INTEGRITY_MISMATCH` — integrity hash does not match
- `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` — the schema hash algorithm in the snapshot header is not recognized. An Error on the body-reading surfaces (`Load`, `Verify`, `Info`, `UpdateMetadata`): the document is refused rather than half-trusted. A Warning on header-only reads, which stay classifiable for dispatch
- `W_SNAPSHOT_PATH_FALLBACK` (Warning) — a provenance path string could not be parsed into a canonical path and fell back to the root path; the original string is preserved for round-trip fidelity
- `E_SNAPSHOT_IO` — a directory- or file-level I/O failure during `ScanDir` / `ScanDirSlice`, or a failing reader under `HeaderOnlyRead`
- `E_UPDATE_METADATA_BODY_OFFSET` — `UpdateMetadata` could not resolve the byte range of the body it reuses
- `W_UPDATE_METADATA_FALLBACK` (Warning) — `UpdateMetadataOrReMarshal` fell back from the fast path to `Load` + `Marshal`
- `W_SNAPSHOT_VALUE_NONCONFORMING` (Warning) — under `WithValueConformance`, a root's or a composed child's stored `Timestamp`, `Date` or `UUID` does not conform to its declared constraint
- `W_SNAPSHOT_VALUE_DROPPED` (Warning) — a write path held a value it could not write at that position and left it out. The document is well-formed; the warning names what is missing from it
- `W_SNAPSHOT_UNRESOLVED_REQUIRED` — under `WithRevalidation`, a loaded document carries an unresolved record, one no structural check refused, for a required association
- `W_SNAPSHOT_PATH_EXTENSION` (Warning) — a snapshot was written to a path that does not end in `.ys`. The write succeeded; a reader that discovers snapshots by extension will not find it

**Adapter** — platform errors. Each code below is registered by its adapter package when that package is linked:

- `E_NEO4J_LABEL_COLLISION` — two types render the same Neo4j label
- `E_NEO4J_INVALID_IDENTIFIER` — an identifier is not usable unquoted in generated Cypher
- `E_NEO4J_UNSUPPORTED_TYPE` — a property kind the adapter cannot express
- `E_NEO4J_UNKNOWN_PROPERTY` — an annotation names a property the type does not declare
- `E_NEO4J_INVALID_INDEX_TARGET` — an index annotation targets an ineligible property
- `W_NEO4J_NODE_KEY_UNSUPPORTED` (Warning) — NODE KEY is unavailable on this edition
- `W_NEO4J_EDITION_CONSTRAINT_OMITTED` (Warning) — a constraint was omitted because the edition does not support it
- `W_NEO4J_INDEXES_UNREADABLE` (Warning) — a comparison could not read the database's indexes, so the half needing them did not run. The constraint half is complete; a declaration whose name an index already holds reads as a create that will not take effect

## File Extension and Conventions

- Schema files use the `.yammm` extension
- Snapshot files use the `.ys` extension
- A schema file is UTF-8. It can start with one byte order mark, which the formatter removes; a snapshot file that starts with one is refused (`E_SNAPSHOT_MALFORMED`)
- One schema per file
- Import paths are case-sensitive on case-sensitive filesystems
- `/` separates an import path's segments on every host, and only a `./` or `../` prefix makes an import relative. An import path holding a backslash is refused: Windows reads a backslash as a separator and every other host as part of a file name, so the path would name a different file on each
- Canonical formatting is what `yammm fmt` writes (see [API.md](API.md#formatting))

## Schema Identity

Each compiled schema has a deterministic structural hash (SHA-256) computed over its whole import closure. For every member schema — the entry schema and each schema it imports, directly or transitively — the input covers:

- The member's schema name
- Type names, properties (with constraints and modifiers), the primary-key set, relations (with targets and cardinalities), compositions, association edge properties, and each supertype by its type name
- Each type's `abstract` and `part` markers
- Invariant expressions (the invariant's *name* is excluded — it is the failure message)
- Data type names and constraints

Two schemas that hash the same agree on every one of those rules everywhere in their closures. The converse does not hold: a hash can also move on a change to an imported schema this schema never references, because closure membership is part of the identity. Members are framed under their schema names, the entry schema first and the rest ordered by name, so the order imports are declared in does not enter the digest.

**Excluded, deliberately:** annotations — they drive downstream store DDL and write shape and never reject data, so a schema that gains its first annotation hashes identically and every persisted `.ys` snapshot header hash stays valid; source paths — a member is framed by its declared name, never its source, and a relation target is the pair (owning schema name, type name), so `embedded://` and disk loads of one schema text agree; and documentation and source positions. Adding an invariant, an edge property, or flipping a type between concrete and `part` changes the hash in any member.

Two more facts are outside the hash, and two schemas that hash the same can differ in them:

- **The import graph and the entry schema's name surface**: no member's imports are hashed, so one member set under two import graphs or two aliases hashes the same. The entry schema's direct import set and its import aliases decide which type names the schema resolves, such as `lb.T`, so a type name a caller validates data under can resolve under one and miss under the other; they change no rule a type applies. A `.ys` snapshot names a type by its declaring schema's name and its own, so a snapshot read resolves it through the member set alone.
- **Which member owns a supertype, and which data type a property names.** A supertype hashes by its type name, and the subtype hashes the members it inherits. A property hashes its resolved constraint and never a data type's name. So `K extends a.Base` and `K extends b.Base` hash the same when `a` and `b` declare `Base` alike, and a property typed `a.Code`, one typed `b.Code` and one typed `String[3, 3]` hash the same when each `Code` is `String[3, 3]`. Instance validity agrees in each case.

So a test that compares generated code's `SchemaHash` constant with the hash cannot see stale generated code after any change the hash leaves out: the generated code embeds the schema sources and their documentation comments. After a change to the name surface, a supertype's owner or a property's data type, a field's Go type or a graph field can differ too.

The hash is deterministic. Most inputs are sorted by name; invariant blobs are sorted by their serialized bytes, and the operands of one expression are hashed in declared order, because operand order is semantic.

The hash format is `sha256:<hex>`. A structural hash version (currently `4`) is bumped when the algorithm changes; version 2 arrived in v0.15.0 with invariants and the `abstract` / `part` markers, version 3 in v0.17.0 when the input widened to the whole import closure, and version 4 when a relation target became the pair (owning schema name, type name) so two closure members declaring one type name no longer collide. The hash enables schema compatibility checking for `.ys` snapshots: `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` is emitted when a snapshot's persisted hash does not match the current schema.

## Grammar Summary

This section is the normative statement of the language YAMMM **accepts** — a
production describes what loads, not what the reference parser's own grammar
admits. The two differ where the parser deliberately accepts a shape so a
checker can report a better message than a parse error: an `Enum` of fewer than
two values, such as `Enum["a"]`, and a `Pattern` of more than two parse and are
then refused with `E_INVALID_CONSTRAINT`, a relation name that is no reserved
word and is not upper snake case, such as `worksAt`, draws `E_INVALID_NAME`, and
a reverse clause draws `E_REVERSE_CLAUSE_REMOVED`. Every other wrong argument
count to a type, such as `Integer[1]`, is `E_SYNTAX`.

It is maintained by hand. Nothing checks it automatically, and no second grammar
exists to check it against. Two non-terminals it names are defined elsewhere in
this document rather than repeated here: `Expr`, under Expressions and
Invariants, and the six spellings `PropertyName` refuses, under Keywords.

```text
Schema     = SchemaName { ImportDecl } { TypeDecl | DataTypeDecl } EOF .
SchemaName = [ DOC_COMMENT ] "schema" STRING .
ImportDecl = "import" STRING [ "as" AliasName ] .

TypeDecl   = [ DOC_COMMENT ] [ "abstract" | "part" ] "type" TypeName
             [ ExtendsClause ] "{" TypeBody "}" .
DataTypeDecl = [ DOC_COMMENT ] "type" TypeName "=" BuiltIn .

TypeName   = UC_WORD .
AliasName  = UC_WORD | LC_WORD .
TypeRef    = [ AliasName "." ] TypeName .
ExtendsClause = "extends" TypeRef { "," TypeRef } [ "," ] .
TypeBody   = { Property | Association | Composition | Invariant | TypeAnnotation } .

Property   = [ DOC_COMMENT ] PropertyName DataTypeRef [ "primary" | "required" ] { Annotation } .
PropertyName = LC_WORD .   // except as, part, in, nil, true, false
DataTypeRef = BuiltIn | QualifiedAlias .
QualifiedAlias = [ AliasName "." ] UC_WORD .

Annotation     = "@" PropertyName [ AnnotationArgs ] .
TypeAnnotation = [ DOC_COMMENT ] "@@" PropertyName [ AnnotationArgs ] .
AnnotationArgs = "(" AnnotationArg { "," AnnotationArg } [ "," ] ")" .
AnnotationArg  = PropertyName | UC_WORD | STRING | INTEGER | FLOAT | BOOLEAN | REGEXP .

Association  = [ DOC_COMMENT ] "-->" RelationName [ Multiplicity ] TypeRef
               [ "{" { RelProperty } "}" ] .
Composition  = [ DOC_COMMENT ] "*->" RelationName [ Multiplicity ] TypeRef .
RelationName = upper_letter { upper_letter | decimal_digit | "_" } .
Multiplicity     = "(" MultiplicitySpec ")" .
MultiplicitySpec = "_" [ ":" ( "one" | "many" ) ]
                 | "one" [ ":" ( "one" | "many" ) ]
                 | "many" .

Invariant  = [ DOC_COMMENT ] "!" STRING Expr .
RelProperty = [ DOC_COMMENT ] PropertyName DataTypeRef [ "required" ] .

BuiltIn    = "Integer" [ "[" IntBound "," IntBound "]" ]
           | "Float" [ "[" FloatBound "," FloatBound "]" ]
           | "Boolean"
           | "String" [ "[" ListBound "," ListBound "]" ]
           | "Enum" "[" STRING "," STRING { "," STRING } [ "," ] "]"
           | "Pattern" "[" STRING [ "," STRING ] "]"
           | "Timestamp" [ "[" STRING "]" ]
           | "Date"
           | "UUID"
           | "Vector" "[" INTEGER "]"
           | "List" "<" DataTypeRef ">" [ "[" ListBound "," ListBound "]" ] .
IntBound   = [ "-" ] ( "_" | INTEGER ) .
FloatBound = [ "-" ] ( "_" | INTEGER | FLOAT ) .
ListBound  = "_" | INTEGER .
```

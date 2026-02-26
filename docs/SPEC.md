# The YAMMM Language Specification

## Introduction

This is a reference manual for the YAMMM (Yet Another Meta-Meta Model) data schema language. YAMMM is a domain-specific language for expressing schemas that define types, their properties, relationships, and constraints. The Go library turns these schemas into runtime models for validation and traversal.

YAMMM is designed for structured data modeling with a focus on:

- Type definitions with properties and inheritance
- Relationships between types (associations and compositions)
- Invariants expressed as constraint expressions
- Structured diagnostics with stable error codes

The grammar is compact and regular, allowing for easy analysis by automatic tools. We use [ANTLR](https://en.wikipedia.org/wiki/ANTLR) to generate lexers and parsers from [`YammmGrammar.g4`](../internal/grammar/YammmGrammar.g4).

## Notation

The syntax is specified using [Extended Backus-Naur Form (EBNF)](https://en.wikipedia.org/wiki/Extended_Backus%E2%80%93Naur_form):

```text
Production  = production_name "=" [ Expression ] "." .
Expression  = Alternative { "|" Alternative } .
Alternative = Term { Term } .
Term        = production_name | token [ "..." token ] | Group | Option | Repetition .
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

Lower-case production names are used to identify lexical tokens. Non-terminals are in CamelCase. Lexical tokens are enclosed in double quotes `""` or back quotes `` ` ``.

The horizontal ellipsis `...` is used to informally denote various enumerations or code snippets that are not further specified.

## Source Code Representation

Source code is Unicode text encoded in UTF-8. The text is not canonicalized, so a single accented code point is distinct from the same character constructed from combining an accent and a letter. For simplicity, this document will use the unqualified term _character_ to refer to a Unicode code point in the source text.

Each code point is distinct; upper and lower case letters are different characters.

### Characters

The following terms are used to denote specific Unicode character classes:

```text
newline        = /* the Unicode code point U+000A */ .
unicode_char   = /* an arbitrary Unicode code point except newline */ .
unicode_letter = /* a Unicode code point classified as "Letter" */ .
unicode_digit  = /* a Unicode code point classified as "Number, decimal digit" */ .
```

### Letters and Digits

The underscore character `_` (U+005F) is considered a letter for the purposes of identifier formation.

```text
letter        = unicode_letter | "_" .
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

Block comments immediately preceding a schema, type, property, association, composition, or data type declaration become that element's documentation and are preserved in the parsed model.

A comment cannot start inside a string literal or inside another comment.

### Tokens

Tokens form the vocabulary of the YAMMM language. There are four classes: identifiers, keywords, operators and punctuation, and literals. White space, formed from spaces (U+0020), horizontal tabs (U+0009), carriage returns (U+000D), and newlines (U+000A), is ignored except as it separates tokens that would otherwise combine into a single token.

### Identifiers

Identifiers name entities such as types, properties, and relationships. There are two classes of identifiers:

**Upper-case identifiers** start with an ASCII upper-case letter (A-Z) and are used for type names, data type aliases, and relationship names:

```text
UC_WORD = ascii_upper { ascii_letter | digit | "_" } .
```

**Lower-case identifiers** start with an ASCII lower-case letter (a-z) and are used for property names:

```text
LC_WORD = ascii_lower { ascii_letter | digit | "_" } .
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

**Data type keywords:**

```text
Integer    Float    Boolean    String    Enum    Pattern
Timestamp    Date    UUID    Vector
```

**Boolean literals:**

```text
true    false
```

A small set of keywords may be used as property names via the `lc_keyword` rule:

```text
schema    type    datatype    required    primary    extends
includes    abstract    one    many    import
```

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
STRING = `"` { unicode_value | escape_sequence } `"` |
         `'` { unicode_value | escape_sequence } `'` .
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

Examples:

```text
"hello"
'world'
"line1\nline2"
"path\\to\\file"
"unicode: \u00e9"
```

### Regular Expression Literals

Regular expression literals are enclosed in forward slashes:

```text
REGEXP = "/" { regexp_char | escape_sequence } "/" .
```

The content follows Go's `regexp` package syntax. Backslashes within the pattern escape the following character.

Examples:

```text
/.+@.+/              // simple email pattern
/^[A-Z][a-z]+$/      // capitalized word
/\d{3}-\d{4}/        // phone number pattern
```

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
Schema     = SchemaName { ImportDecl } { TypeDecl | DataTypeDecl } .
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
ImportDecl = "import" path=STRING [ "as" alias=AliasName ] .
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
- Imports must resolve to `.yammm` files, not directories

**Security:** Import paths are sandboxed using `os.Root` to prevent path traversal attacks. Paths that attempt to escape the module root are rejected at the kernel level.

**Default alias derivation:** When no explicit `as` clause is provided, the alias is derived from the last path segment:

- Strip trailing slashes and `.yammm` extension
- Replace non-alphanumeric/underscore characters with underscore (e.g., `address-types` becomes `address_types`)

**Alias identifier requirements:** Aliases must be valid identifiers per the grammar—they must start with a letter (A-Z or a-z) and contain only letters, digits, and underscores. If the derived alias would be invalid (e.g., starts with a digit or underscore), an explicit `as` clause is required:

```yammm
// ERROR: "./3rdparty" derives alias "_3rdparty" which starts with underscore
import "./3rdparty"

// OK: explicit alias starting with a letter
import "./3rdparty" as thirdparty
```

**Reserved keyword restriction:** Aliases cannot be reserved keywords because the lexer tokenizes them as literal tokens rather than identifiers. Reserved keywords include:

- DSL keywords: `schema`, `import`, `as`, `type`, `datatype`, `required`, `primary`, `extends`, `includes`, `abstract`, `part`, `one`, `many`, `in`
- Built-in type keywords: `Integer`, `Float`, `Boolean`, `String`, `Enum`, `Pattern`, `Timestamp`, `Date`, `UUID`, `Vector`
- Boolean literals: `true`, `false`

**Qualified type references:** Imported types must be referenced with their alias qualifier:

```yammm
type Car {
    color common.Color required      // qualified reference
    --> WHEELS (one:many) parts.Wheel
}
```

**Qualified datatype references:** Custom datatypes from imported schemas must also be qualified with their alias:

```yammm
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
TypeBody = { Property | Association | Composition | Invariant } .
```

A basic type declaration:

```yammm
type Person {
    name String required
    age Integer[0, 150]
}
```

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
- Relationship definitions must be unique after inheritance; duplicate name/target pairs are reported as errors

### Type References

Type references may be local or qualified with an import alias:

```yammm
type Car {
    --> OWNER Person                    // local type reference
    --> DEALER (one) dealers.Dealer     // qualified reference
}
```

## Properties

Properties define the data fields of a type.

### Property Declaration

```text
Property     = [ DOC_COMMENT ] PropertyName DataTypeRef [ "primary" | "required" ] .
PropertyName = LC_WORD | lc_keyword .
```

Property names must start with a lower-case letter.

### Property Modifiers

**Primary properties** form part of the type's identity. They are implicitly required:

```yammm
type Car {
    vin String primary      // primary key
    regNbr String required
}
```

**Required properties** must be present in all instances:

```yammm
type Person {
    name String required    // must be present
    age Integer             // optional
}
```

Properties without modifiers are **optional** and may be omitted from instance data.

### Relationship Properties

Associations may have their own properties, declared within the relationship body:

```text
RelProperty = [ DOC_COMMENT ] PropertyName DataTypeRef [ "required" ] .
```

```yammm
type Person {
    --> WORKS_AT Company {
        startDate Date required
        title String
    }
}
```

Relationship properties follow the same syntax as type properties but cannot use `Vector` data types and default to optional unless `required` is specified.

## Data Types

YAMMM provides a set of built-in data types and supports user-defined type aliases.

### Built-in Data Types

```text
DataTypeRef = BuiltIn | QualifiedAlias .
BuiltIn     = IntegerT | FloatT | BoolT | StringT | EnumT | PatternT |
              TimestampT | DateT | UUIDT | VectorT .
```

#### Integer

Represents signed integer values with optional bounds:

```text
IntegerT = "Integer" [ "[" min "," max "]" ] .
min      = "_" | [ "-" ] INTEGER .
max      = "_" | [ "-" ] INTEGER .
```

The underscore `_` represents an unbounded limit. An optional leading `-` allows negative bounds.

Examples:

```yammm
age Integer                  // unbounded integer
age Integer[0, 150]          // 0 to 150 inclusive
count Integer[1, _]          // minimum 1, no maximum
index Integer[_, 99]         // no minimum, maximum 99
temperature Integer[-40, 50] // negative lower bound
```

Validation accepts signed and unsigned integers, including named/alias types and pointer values. Unsigned inputs larger than `int64` are rejected before bound checks.

#### Float

Represents floating-point values with optional bounds:

```text
FloatT = "Float" [ "[" min "," max "]" ] .
min    = "_" | [ "-" ] ( INTEGER | FLOAT ) .
max    = "_" | [ "-" ] ( INTEGER | FLOAT ) .
```

Examples:

```yammm
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

```yammm
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

```yammm
name String                  // unbounded string
name String[1, 100]          // 1 to 100 runes
code String[3, 3]            // exactly 3 runes
notes String[_, 1000]        // maximum 1000 runes
```

#### Enum

Represents a value from a fixed set of string options:

```text
EnumT = "Enum" "[" STRING { "," STRING } [ "," ] "]" .
```

At least two options must be provided.

Examples:

```yammm
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

```yammm
email Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
phone Pattern["^\\d{3}-\\d{3}-\\d{4}$"]
```

When two patterns are provided, the value must match both.

#### Timestamp

Represents a date-time value with optional format specification:

```text
TimestampT = "Timestamp" [ "[" format "]" ] .
format     = STRING .
```

The format string follows Go's time formatting conventions. When omitted, RFC3339 (`"2006-01-02T15:04:05Z07:00"`) is used.

Examples:

```yammm
createdAt Timestamp                                    // RFC3339
eventTime Timestamp["2006-01-02T15:04:05Z07:00"]       // explicit RFC3339
logTime Timestamp["2006-01-02 15:04:05"]               // custom format
```

#### Date

Represents a date value (without time component):

```text
DateT = "Date" .
```

Example:

```yammm
birthDate Date
expiryDate Date required
```

#### UUID

Represents a universally unique identifier:

```text
UUIDT = "UUID" .
```

Example:

```yammm
externalId UUID
correlationId UUID required
```

#### Vector

Represents a fixed-dimension numeric vector (for embeddings, coordinates, etc.):

```text
VectorT = "Vector" "[" dimensions "]" .
dimensions = INTEGER .
```

Examples:

```yammm
embedding Vector[768]       // 768-dimensional vector
coordinates Vector[3]       // 3D coordinates
```

Validation accepts float slices/arrays (`[]float32`/`[]float64`), including named types and pointers. NaN, Inf, and non-float elements are rejected.

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
- Stored internally as lower-case
- Referenced by name in property declarations
- Able to chain (A -> B -> built-in); cycles are rejected during parsing

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
Association = [ DOC_COMMENT ] "-->" Name [ Multiplicity ] TypeRef
              [ "/" ReverseName [ Multiplicity ] ]
              [ "{" { RelProperty } "}" ] .
Name        = UC_WORD | LC_WORD .
```

Examples:

```yammm
type Person {
    --> WORKS_AT Company              // optional, one
    --> MANAGES (many) Person         // optional, many
    --> REPORTS_TO (one) Person       // required, one
}

type Car {
    --> OWNER (one:many) Person       // required, many owners
}
```

### Compositions

Compositions represent ownership where child entities are embedded within their parent:

```text
Composition = [ DOC_COMMENT ] "*->" Name [ Multiplicity ] TypeRef
              [ "/" ReverseName [ Multiplicity ] ] .
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

Composition data is embedded inline in instance documents rather than using reference objects.

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

```yammm
--> OWNER Person              // optional, single owner
--> OWNER (one) Person        // required, single owner
--> OWNERS (many) Person      // optional, multiple owners
--> OWNERS (one:many) Person  // required, at least one owner
```

### Reverse Relationships

The optional reverse clause declares the inverse relationship name:

```yammm
type Person {
    --> WORKS_AT Company / EMPLOYEES
}
```

The reverse name and multiplicity are parsed and stored as metadata. They may be used for future functionality such as bidirectional navigation or automatic inverse relationship generation.

### Association Data in Instances

Association edges in instance data are represented as objects containing:

- Target primary key(s) using reserved `_target_` prefixed fields
- Any association properties defined in the relationship

For **single primary key** targets, use `_target_id`:

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

**Reserved prefix:** The `_target_` prefix is reserved for foreign key fields. User-defined relation names cannot start with `_target_` (case-insensitive).

## Expressions and Invariants

Invariants are constraints attached to types that are evaluated during instance validation.

### Invariant Declaration

```text
Invariant = "!" message=STRING constraint=Expr .
```

The message is displayed when the invariant evaluates to false:

```yammm
type Person {
    name String required
    age Integer[0, 150]

    ! "name must not be empty" len(name) > 0
    ! "age must be positive" age >= 0
}
```

### Expression Grammar

Expressions support a rich set of operators and built-in functions.

```text
Expr = Literal
     | "[" [ Expr { "," Expr } [ "," ] ] "]"           // list
     | "-" Expr                                         // unary minus
     | Expr "[" [ Expr { "," Expr } ] "]"              // indexing/slicing
     | Expr "->" Name [ Arguments ] [ Parameters ] [ "{" Expr "}" ]  // pipeline
     | Expr "." Expr                                    // property access
     | "!" Expr                                         // logical not
     | Expr ( "*" | "/" | "%" ) Expr                   // multiplicative
     | Expr ( "+" | "-" ) Expr                         // additive
     | Expr ( "<" | "<=" | ">" | ">=" ) Expr           // comparison
     | Expr "in" Expr                                   // membership
     | Expr ( "=~" | "!~" ) Expr                       // regex match
     | Expr ( "==" | "!=" ) Expr                       // equality
     | Expr "&&" Expr                                   // logical and
     | Expr ( "||" | "^" ) Expr                        // logical or/xor
     | Expr "?" "{" Expr [ ":" Expr ] "}"              // ternary
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
```

> **`_` and `nil` in expressions.** Within invariant expressions, `_` and `nil` are interchangeable — both produce a nil literal. Use whichever reads more naturally: `end_date == nil` for null-guard idioms, `end_date == _` for consistency with other DSL contexts. Note that `_` retains distinct, non-nil roles in constraint bounds (`Integer[0, _]`) and multiplicity (`(_:many)`), where `nil` cannot be used.

### Operator Precedence

Operators are listed from highest to lowest precedence:

| Precedence | Operators | Associativity |
| ---------- | --------- | ------------- |
| 1 | Literals, list literals `[...]` | - |
| 2 | Unary minus `-x` | Right |
| 3 | Indexing/slicing `expr[i]`, `expr[a,b]` | Left |
| 4 | Pipeline `lhs -> name(args)\|$params\|{body}` | Left |
| 5 | Property access `lhs.property` | Left |
| 6 | Logical not `!expr` | Right |
| 7 | Multiplicative `*`, `/`, `%` | Left |
| 8 | Additive `+`, `-` | Left |
| 9 | Comparisons `<`, `<=`, `>`, `>=` | Left |
| 10 | Membership `in` | Left |
| 11 | Regex match `=~`, `!~` | Left |
| 12 | Equality `==`, `!=` | Left |
| 13 | Logical and `&&` | Left |
| 14 | Logical or/xor `\|\|`, `^` | Left |
| 15 | Ternary `? { then : else }` | Right |

Parentheses group as usual.

### Operators

#### Arithmetic Operators

```text
+    addition (numbers) or concatenation (strings)
-    subtraction or unary negation
*    multiplication
/    division
%    modulo (integers only)
```

#### Comparison Operators

```text
==   equal
!=   not equal
<    less than
<=   less than or equal
>    greater than
>=   greater than or equal
```

Unsupported comparison operands raise evaluation errors instead of returning false. `==`/`!=`/`in` reject mismatched types.

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

```yammm
! "status must be valid" status in ["active", "inactive", "pending"]
```

#### Pattern and Type Match Operators

```text
=~   matches pattern or type
!~   does not match pattern or type
```

These operators support two modes:

**Regular expression matching:** When the right operand is a regex literal, performs pattern matching:

```yammm
! "email must be valid" email =~ /.+@.+\..+/
```

**Type checking:** When the right operand is a datatype keyword, checks whether the value matches that type at runtime:

```yammm
! "value must be integer" value =~ Integer
! "price must be numeric" price =~ Float
! "must not be string" data !~ String
```

Supported datatype keywords for type checking:
- `String` - checks for string values
- `Integer` (alias: `Int`) - checks for integer values
- `Float` (alias: `Number`) - checks for floating-point values
- `Boolean` (alias: `Bool`) - checks for boolean values
- `UUID` - checks for valid UUID strings
- `Timestamp` - checks for valid timestamp values
- `Date` - checks for valid date values

#### Ternary Operator

```text
condition ? { trueExpr : falseExpr }
```

Example:

```yammm
! "adult status" age >= 18 ? { "adult" : "minor" } == category
```

### Indexing and Slicing

The bracket operator supports:

```text
expr[index]        // single element access
expr[start, end]   // range slice
```

Works on:

- Strings (rune indexing)
- Arrays and slices

Invalid indices or ranges return evaluation errors with start/end/len details.

### Property Access

Properties are accessed using dot notation:

```yammm
$self.name         // explicit self reference
name               // implicit property reference
$item.price        // lambda parameter property
```

Property lookups are **strict**: unknown properties and non-map dereferences raise errors, not `nil`.

### Variables and Scope

**Numeric variables** (`$0`, `$1`, ...) are evaluator-local and default to `nil` when unset.

**Named variables** are resolved through the evaluator's parent chain and error if undefined.

**`$self`** is bound when evaluating invariants against property maps and is inherited by child evaluators unless explicitly overridden.

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
- `Any` returns `false` on empty collections
- `All` returns `true` on empty collections (vacuous truth)
- `AllOrNone` returns `true` on empty collections (vacuous truth)

#### Numeric Functions

| Function | Description |
| -------- | ----------- |
| `Len` | Length of string (runes) or slice (nil yields 0) |
| `Abs` | Absolute value |
| `Floor` | Floor of float |
| `Ceil` | Ceiling of float |
| `Round` | Round to nearest integer (banker's rounding) |
| `Min` | Minimum value: `a -> Min(b)` or `items -> Min` |
| `Max` | Maximum value: `a -> Max(b)` or `items -> Max` |
| `Compare` | Three-way comparison: `a -> Compare(b)` returns -1, 0, or 1 |

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
| `Substring` | Extract substring: `s -> Substring(start, end)` |

#### Control Flow Functions

| Function | Description |
| -------- | ----------- |
| `Then` | Execute body when non-nil: `value -> Then \|$v\| { $v.prop }` |
| `Lest` | Execute body when nil: `value -> Lest { default }` |
| `With` | Bind params and execute: `value -> With \|$v\| { $v.prop }` |

#### Pattern Matching

| Function | Description |
| -------- | ----------- |
| `Match` | Regex match with captures: `s -> Match(/pattern/)` |

#### Utility Functions

| Function | Description |
| -------- | ----------- |
| `TypeOf` | Get type name as string: `value -> TypeOf` |
| `IsNil` | Check if nil: `value -> IsNil` |
| `Default` | Return default if nil: `value -> Default(fallback)` |
| `Coalesce` | Return first non-nil: `a -> Coalesce(b, c)` |

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

    // Age range check
    ! "age must be valid" age >= 0 && age <= 130

    // All parts must be priced
    ! "all parts must have prices" PARTS -> All |$p| { $p.price > 0 }

    // Email format validation
    ! "email must be valid" email =~ /.+@.+/

    // Mutually exclusive fields
    ! "cannot have both" !(hasA && hasB)

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
- There is no implicit database lookup or multi-hop relation navigation
- Evaluation errors (undefined property/variable, type errors) surface as fatal issues
- Panics (e.g., divide-by-zero) are recovered as errors annotated with the operator stack

## Loading Schemas

Schemas are loaded from files or in-memory sources using the `schema/load` package.

### Load Functions

```go
// Load from file path
schema, result, err := load.Load(ctx, "path/to/schema.yammm", opts...)

// Load from string content (sourceCode first, then sourceName)
schema, result, err := load.LoadString(ctx, content, "source-name", opts...)

// Load from in-memory sources with import resolution (moduleRoot is required)
schema, result, err := load.LoadSources(ctx, sources, moduleRoot, opts...)
```

### Load Options

| Option | Description |
| ------ | ----------- |
| `WithRegistry` | Schema registry for cross-schema type resolution |
| `WithModuleRoot` | Root directory for module-style imports |
| `WithIssueLimit` | Maximum diagnostic issues to collect (default: 100) |
| `WithSourceRegistry` | Source registry for position tracking |
| `WithLogger` | Structured logger for load diagnostics |

### Error Handling Pattern

All load functions follow the (Output, diag.Result, error) pattern:

- `error != nil`: Catastrophic failure (I/O error, context cancellation)
- `error == nil && !result.OK()`: Semantic failure (syntax errors, type resolution failures)
- `error == nil && result.OK()`: Success (may have warnings)

```go
schema, result, err := load.Load(ctx, "schema.yammm")
if err != nil {
    return fmt.Errorf("load failed: %w", err)
}
if !result.OK() {
    // Use diag.Renderer to format issues
    return fmt.Errorf("schema errors: %v", result)
}
// Use schema
```

## Building Schemas Programmatically

The `schema/build` package provides a fluent API for constructing schemas without parsing `.yammm` files.

```go
s, result := build.NewBuilder().
    WithName("MySchema").
    WithSourceID(location.MustNewSourceID("test://my-schema.yammm")).
    AddType("Person").
        WithProperty("name", schema.NewStringConstraint()).
        WithOptionalProperty("age", schema.NewIntegerConstraintBounded(0, true, 150, true)).
        Done().
    AddType("Car").
        WithPrimaryKey("vin", schema.NewStringConstraint()).
        WithRelation("OWNER", schema.NewTypeRef("", "Person", location.Span{}), false, false).
        Done().
    Build()
```

## Instance Validation

The `instance` package validates Go data against compiled schemas.

### Validator Creation

```go
validator := instance.NewValidator(schema, opts...)
```

### Validator Options

| Option | Description |
| ------ | ----------- |
| `WithLogger` | Structured logger for debug output |
| `WithStrictPropertyNames` | Require exact case matching (default: false) |
| `WithAllowUnknownFields` | Silently ignore unknown fields (default: false) |
| `WithMaxIssuesPerInstance` | Maximum issues per instance (default: 100) |
| `WithValueRegistry` | Custom value registry for type classification |

### Validation

```go
valid, failures, err := validator.Validate(ctx, "Person", rawInstances)
if err != nil {
    return err // Catastrophic failure
}
if len(failures) > 0 {
    // Handle validation failures with diagnostics
    for _, f := range failures {
        fmt.Println(f.Result) // diag.Result with issues
    }
}
// Process valid instances
```

### Expected Instance Shape

Instance data is a top-level object keyed by type names whose values are arrays of instances:

```json
{
  "Person": [
    { "id": "550e8400-e29b-41d4-a716-446655440000", "name": "Alice", "age": 30 }
  ],
  "Car": [
    {
      "id": "660e8400-e29b-41d4-a716-446655440001",
      "vin": "ABC123",
      "owner": { "_target_id": "550e8400-e29b-41d4-a716-446655440000" }
    }
  ]
}
```

### Input Formats

Validation accepts:

- `map[string]any` or typed string-keyed maps
- Go structs
- Wrapper implementations that expose map/slice/struct fields

## Graph Construction

The `graph` package builds an in-memory graph from validated instances.

### Graph Operations

```go
g := graph.New(schema)

// Add validated instances
result, err := g.Add(ctx, validInstance)
if err != nil || !result.OK() {
    // Handle error
}

// Check completeness (required associations)
result, err = g.Check(ctx)

// Get immutable snapshot
snap := g.Snapshot()
for _, typeName := range snap.Types() {
    for _, inst := range snap.InstancesOf(typeName) {
        // Process instances
    }
}
```

### Thread Safety

- `Graph` is safe for concurrent `Add` and `AddComposed` calls
- `Result` snapshots are immutable and safe for concurrent reads
- All output slices are deterministically sorted

### Ordering Guarantees

- `Result.Types()`: Lexicographic by type name
- `Result.InstancesOf()`: Lexicographic by primary key
- `Result.Edges()`: Lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey)
- `Result.Duplicates()`: Lexicographic by (typeName, primaryKey)
- `Result.Unresolved()`: Lexicographic by (sourceType, sourceKey, relation, targetType, targetKey)

## Diagnostics

The `diag` package provides structured diagnostics with stable error codes.

### Severity Levels

| Severity | Description |
| -------- | ----------- |
| `Fatal` | Unrecoverable condition or issue limit reached |
| `Error` | Validation failure but processing continues |
| `Warning` | Non-blocking advisory |
| `Info` | Informational message |
| `Hint` | Suggestion for improvement |

### Result Methods

```go
result.OK()           // No fatal or error issues
result.HasErrors()    // Has error-level issues
result.LimitReached() // Issue collection limit was reached
result.Issues()       // All collected issues
result.Errors()       // Error-level issues only
```

### Diagnostic Codes

Codes are stable identifiers for programmatic matching. Categories include:

- **Sentinel**: `E_LIMIT_REACHED`, `E_INTERNAL`
- **Schema**: `E_TYPE_COLLISION`, `E_INHERIT_CYCLE`, `E_DUPLICATE_PROPERTY`, etc.
- **Syntax**: `E_SYNTAX`
- **Import**: `E_IMPORT_RESOLVE`, `E_IMPORT_CYCLE`, `E_PATH_ESCAPE`, etc.
- **Instance**: `E_TYPE_MISMATCH`, `E_MISSING_REQUIRED`, `E_CONSTRAINT_FAIL`, `E_INVARIANT_FAIL`, etc.
- **Graph**: `E_DUPLICATE_PK`, `E_UNRESOLVED_REQUIRED`, etc.
- **Adapter**: `E_ADAPTER_PARSE`

### Rendering Diagnostics

```go
renderer := diag.NewRenderer()
output := renderer.Render(result)
```

## JSON Adapter

The `adapter/json` package parses JSON/JSONC into raw instances with optional location tracking.

### Adapter Creation

```go
adapter, err := json.NewAdapter(registry, opts...)
```

### Parse Options

| Option | Description |
| ------ | ----------- |
| `WithStrictJSON` | Use stdlib JSON only (no comments/trailing commas) |
| `WithTrackLocations` | Enable source position tracking |
| `WithTypeField` | Field name for type tagging (default: `$type`) |

### JSONC Support

By default, the adapter uses `tidwall/jsonc` to preprocess input:

- Strips `//` and `/* */` comments
- Removes trailing commas
- Preserves byte offsets for accurate diagnostics

## File Extension and Conventions

- Schema files use the `.yammm` extension
- UTF-8 encoding is required
- One schema per file
- Import paths are case-sensitive on case-sensitive filesystems

## Grammar Summary

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
TypeBody   = { Property | Association | Composition | Invariant } .

Property   = [ DOC_COMMENT ] PropertyName DataTypeRef [ "primary" | "required" ] .
PropertyName = LC_WORD | lc_keyword .
DataTypeRef = BuiltIn | QualifiedAlias .
QualifiedAlias = [ AliasName "." ] UC_WORD .

Association = [ DOC_COMMENT ] "-->" Name [ Multiplicity ] TypeRef
              [ "/" Name [ Multiplicity ] ] [ "{" { RelProperty } "}" ] .
Composition = [ DOC_COMMENT ] "*->" Name [ Multiplicity ] TypeRef
              [ "/" Name [ Multiplicity ] ] .
Name       = UC_WORD | LC_WORD .
Multiplicity = "(" MultiplicitySpec ")" .

Invariant  = "!" STRING Expr .

BuiltIn    = "Integer" [ "[" Bound "," Bound "]" ]
           | "Float" [ "[" Bound "," Bound "]" ]
           | "Boolean"
           | "String" [ "[" Bound "," Bound "]" ]
           | "Enum" "[" STRING { "," STRING } [ "," ] "]"
           | "Pattern" "[" STRING [ "," STRING ] "]"
           | "Timestamp" [ "[" STRING "]" ]
           | "Date"
           | "UUID"
           | "Vector" "[" INTEGER "]" .
Bound      = "_" | INTEGER | FLOAT .
```

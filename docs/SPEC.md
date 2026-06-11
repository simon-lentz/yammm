# The YAMMM Language Specification

## Introduction

This is a reference manual for the YAMMM (Yet Another Meta-Meta Model) data schema language. YAMMM is a domain-specific language for expressing schemas that define types, their properties, relationships, and constraints. The Go library API for loading, validating, and traversing schemas is documented in [API.md](API.md).

YAMMM is designed for structured data modeling with a focus on:

- Type definitions with properties and inheritance
- Relationships between types (associations and compositions)
- Invariants expressed as constraint expressions
- Structured diagnostics with stable error codes

YAMMM's design draws inspiration from [CUE](https://cuelang.org/)'s constraint-based approach to data validation, adapted for a nominal type system with explicit relationships. Where CUE uses structural typing and lattice-based unification, YAMMM uses named types with inheritance and constraint narrowing.

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

A small set of keywords and reserved identifiers may be used as property names via the `lc_keyword` rule. This allows properties named `schema`, `type`, etc. without ambiguity:

```text
schema    type    datatype    required    primary    extends
includes    abstract    one    many    import
```

The keywords `as`, `part`, and `in` cannot be used as property names because they would create parsing ambiguity in import declarations (`as`), type modifiers (`part`), and membership expressions (`in`) respectively.

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
- If the first character is not a letter, `n` is prepended to produce a valid identifier (e.g., `3rdparty` becomes `n3rdparty`, `_internal` becomes `n_internal`)

**Alias identifier requirements:** Aliases must be valid identifiers per the grammar—they must start with a letter (A-Z or a-z) and contain only letters, digits, and underscores. When automatic derivation would produce an invalid identifier (starting with a digit or underscore), `n` is prepended automatically. An explicit `as` clause can always be used to override the derived alias:

```yammm
// OK: "./3rdparty" derives alias "n3rdparty" (digit-first, so "n" is prepended)
import "./3rdparty"

// OK: explicit alias overrides the derived one
import "./3rdparty" as thirdparty
```

**Reserved keyword restriction:** Aliases cannot be reserved keywords because the lexer tokenizes them as literal tokens rather than identifiers. Reserved keywords include:

- DSL keywords: `schema`, `import`, `as`, `type`, `datatype`, `required`, `primary`, `extends`, `includes`, `abstract`, `part`, `one`, `many`, `in`
- Built-in type keywords: `Integer`, `Float`, `Boolean`, `String`, `Enum`, `Pattern`, `Timestamp`, `Date`, `UUID`, `Vector`, `List`
- Boolean literals: `true`, `false`
- Nil literal: `nil`

**Qualified type references:** Imported types must be referenced with their alias qualifier:

```yammm-snippet
type Car {
    color common.Color required      // qualified reference
    --> WHEELS (one:many) parts.Wheel
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
TypeBody = { Property | Association | Composition | Invariant } .
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

After compilation, all schema objects — types, relations, data types, and the schema itself — are sealed and become immutable. Slice accessors on sealed objects return defensive copies. This guarantees thread-safe concurrent read access without synchronization.

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

**Linearization order:** Ancestors are linearized using depth-first, left-to-right traversal with keep-first deduplication. The resulting order determines property and invariant precedence:

1. Own declarations (from the type body) come first
2. Inherited members follow in linearization order (left-to-right through the `extends` clause, depth-first through each ancestor chain)
3. When the same ancestor is reachable via multiple paths (diamond inheritance), the first occurrence is kept and duplicates are skipped

**Property conflict resolution:**

| Situation | Result |
| --------- | ------ |
| Child re-declares with narrower constraint | Child's version is used |
| Inherited property narrows an earlier ancestor's | Narrower version replaces wider |
| Two inherited properties are incompatible | `E_PROPERTY_CONFLICT` is emitted |

**Invariant merging:** Own invariants come first, then inherited invariants in linearization order. If a child declares an invariant with the same name as an inherited one, the child's version takes precedence.

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

Every concrete (instantiable) type must declare or inherit at least one primary property. A type with no primary key has no identity — it cannot be added to a graph or referenced by an association — so it is rejected at load with `E_NO_PRIMARY_KEY`. Abstract types (never instantiated) and part types (identified through their parent composition) are exempt. A type may declare more than one primary property; together they form a composite key.

**Required properties** must be present in all instances:

```yammm
type Person {
    name String required    // must be present
    age Integer             // optional
}
```

Properties without modifiers are **optional** and may be omitted from instance data.

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
| `Pattern` | Constraint type, not a value type |
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

Relationship properties follow the same syntax as type properties but cannot use `Vector` or `List` data types and default to optional unless `required` is specified.

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
min      = "_" | [ "-" ] INTEGER .
max      = "_" | [ "-" ] INTEGER .
```

The underscore `_` represents an unbounded limit. An optional leading `-` allows negative bounds.

Examples:

```yammm-snippet
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

At least two options must be provided.

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

When two patterns are provided, the value must match both.

#### Timestamp

Represents a date-time value with optional format specification:

```text
TimestampT = "Timestamp" [ "[" format "]" ] .
format     = STRING .
```

The format string follows Go's time formatting conventions. When omitted, RFC3339 (`"2006-01-02T15:04:05Z07:00"`) is used.

Examples:

```yammm-snippet
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

```yammm-snippet
birthDate Date
expiryDate Date required
```

#### UUID

Represents a universally unique identifier:

```text
UUIDT = "UUID" .
```

Example:

```yammm-snippet
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

```yammm-snippet
embedding Vector[768]       // 768-dimensional vector
coordinates Vector[3]       // 3D coordinates
```

Validation accepts float slices/arrays (`[]float32`/`[]float64`), including named types and pointers. NaN, Inf, and non-float elements are rejected.

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
- Able to chain (A -> B -> built-in); cycles are detected and rejected during schema completion

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

The target must be a concrete type (not abstract or a `part` type). An association
edge is resolved by the target's identity, and neither an abstract type (never
instantiated) nor a part type (reachable only through composition) can be the
referenced node.

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

```yammm-snippet
--> OWNER Person              // optional, single owner
--> OWNER (one) Person        // required, single owner
--> OWNERS (many) Person      // optional, multiple owners
--> OWNERS (one:many) Person  // required, at least one owner
```

### Reverse Relationships

The optional reverse clause declares the inverse relationship name:

```yammm-snippet
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

    ! "name must not be empty" name -> Len > 0
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

Comparisons across mismatched operand types do not raise evaluation errors: `==` evaluates to not-equal (false) and `!=` to true, `in` evaluates to false when element types do not match, and ordered comparisons (`<`, `<=`, `>`, `>=`) evaluate to false for incomparable operands.

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

**Type checking:** When the right operand is a datatype keyword, checks whether the value matches that type at runtime:

```yammm-snippet
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

```yammm-snippet
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

```yammm-snippet
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
- Evaluation errors (undefined property/variable, type errors) surface as error-severity diagnostics; validation continues collecting further issues
- Panics (e.g., divide-by-zero) are recovered as errors annotated with the operator stack

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
- Named variables (`$item`, `$acc`, ...) produce an evaluation error if not bound
- Lambda parameters shadow outer variables for the duration of the body

**Division semantics:** Integer division by zero produces an evaluation error. Float division by zero yields +/-Inf per IEEE 754. Integer modulo by zero produces an evaluation error.

**Panic recovery:** Panics during constraint checking or invariant evaluation are recovered at the validator boundary. Recovered panics become `E_INTERNAL` fatal diagnostics with a captured stack trace.

## Diagnostics

YAMMM uses structured diagnostics with stable error codes. The Go programmatic interface (`diag.Result` methods, `diag.Renderer`) is documented in [API.md](API.md#diagnostics).

### Severity Levels

| Severity | Description |
| -------- | ----------- |
| `Fatal` | Unrecoverable condition or issue limit reached |
| `Error` | Validation failure but processing continues |
| `Warning` | Non-blocking advisory |
| `Info` | Informational message |
| `Hint` | Suggestion for improvement |

### Diagnostic Codes

Codes are stable identifiers for programmatic matching. The authoritative list is in `diag/code.go`. Categories and their codes:

**Sentinel** — internal conditions:

- `E_LIMIT_REACHED` — issue collection limit reached
- `E_INTERNAL` — unexpected invariant failure (internal bug indicator)
- `E_CONTEXT_CANCELLED` — operation cancelled via context

**Schema** — schema compilation errors:

- `E_TYPE_COLLISION`, `E_DUPLICATE_TYPE` — type name conflicts
- `E_INHERIT_CYCLE` — circular inheritance chain
- `E_SCHEMA_TYPE_NOT_FOUND`, `E_UNKNOWN_TYPE` — unresolvable type reference
- `E_DUPLICATE_PROPERTY`, `E_UNKNOWN_PROPERTY` — property definition errors
- `E_DUPLICATE_RELATION`, `E_RELATION_COLLISION`, `E_RELATION_NORMALIZATION_COLLISION` — relation conflicts
- `E_CASE_COLLISION` — names differ only by case
- `E_PROPERTY_RELATION_COLLISION` — property and relation share a name
- `E_RESERVED_PREFIX` — name uses a reserved prefix
- `E_INVALID_RELATION`, `E_INVALID_ASSOCIATION_TARGET`, `E_INVALID_COMPOSITION_TARGET` — relation definition errors
- `E_INVALID_CONSTRAINT` — constraint definition error
- `E_INVALID_INVARIANT` — invariant expression error
- `E_INVALID_NAME` — invalid identifier format
- `E_INVALID_PRIMARY_KEY_TYPE` — disallowed type for primary key
- `E_NO_PRIMARY_KEY` — concrete type declares or inherits no primary key
- `E_LIST_ON_EDGE` — List type used in relationship property
- `E_PROPERTY_CONFLICT` — conflicting inherited properties
- `E_UPSTREAM_FAIL` — imported schema failed to compile
- `E_MISSING_SOURCE_ID`, `E_INVALID_SYNTHETIC_ID` — source identity errors
- `E_LOAD_IO_FAILURE` — I/O error during schema loading

**Syntax** — parse errors:

- `E_SYNTAX` — syntax error in schema source

**Import** — import resolution errors:

- `E_IMPORT_RESOLVE` — import path could not be resolved
- `E_IMPORT_CYCLE` — circular import dependency
- `E_INVALID_ALIAS` — import alias is not a valid identifier
- `E_PATH_ESCAPE` — import path escapes allowed directory
- `E_IMPORT_NOT_ALLOWED` — imports disabled via `WithDisallowImports`
- `E_DUPLICATE_IMPORT` — same schema imported multiple times
- `E_IMPORT_ALIAS_COLLISION` — import alias collides with local name

**Instance** — validation errors:

- `E_INSTANCE_TYPE_NOT_FOUND` — type not found in schema
- `E_ABSTRACT_TYPE` — attempt to instantiate abstract type
- `E_PART_TYPE_DIRECT` — attempt to directly instantiate part type
- `E_TYPE_MISMATCH` — value has wrong type
- `E_MISSING_REQUIRED` — required property missing
- `E_MISSING_PRIMARY_KEY` — primary key property missing
- `E_UNKNOWN_FIELD` — unexpected field in instance data
- `E_CONSTRAINT_FAIL` — constraint check failed
- `E_INVARIANT_FAIL` — invariant check failed
- `E_EVAL_ERROR` — expression evaluation error
- `E_UNKNOWN_BUILTIN` — unknown built-in function
- `E_MISSING_FK_TARGET` — foreign key target missing
- `E_PARTIAL_COMPOSITE_FK` — partial composite foreign key
- `E_UNKNOWN_EDGE_FIELD` — unknown field in edge data
- `E_EDGE_SHAPE_MISMATCH` — edge has wrong shape (object vs array)
- `E_UNRESOLVED_REQUIRED_COMPOSITION` — required composition unresolved
- `E_COMPOSITION_NOT_FOUND` — referenced composition not found
- `E_MISSING_TYPE_TAG`, `E_INVALID_TYPE_TAG` — `$type` tag errors
- `E_CASE_FOLD_COLLISION` — multiple input fields map to the same schema property after case-folding. Property name matching is case-insensitive by default (see `WithStrictPropertyNames` in [API.md](API.md)). When colliding fields are detected (e.g., both `"Name"` and `"name"` in the input), the collision is reported and neither field is mapped

**Graph** — graph construction errors:

- `E_DUPLICATE_PK` — duplicate primary key
- `E_DUPLICATE_COMPOSED_PK` — duplicate composed child primary key
- `E_UNRESOLVED_REQUIRED` — required association unresolved
- `E_GRAPH_TYPE_NOT_FOUND` — type not found in graph operations
- `E_GRAPH_PARENT_NOT_FOUND` — parent node not found
- `E_GRAPH_INVALID_COMPOSITION` — invalid composition
- `E_GRAPH_MISSING_PK` — primary key missing in graph operations

**Snapshot** — persistence errors:

- `E_SNAPSHOT_MALFORMED` — invalid JSON or missing required fields
- `E_SNAPSHOT_UNSUPPORTED_VERSION` — unrecognized format version
- `E_SNAPSHOT_UNSUPPORTED_FEATURE` — unrecognized feature flag
- `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` — schema structural hash mismatch
- `E_SNAPSHOT_UNKNOWN_TYPE` — type name not found in schema
- `E_SNAPSHOT_TYPE_MISMATCH` — types array inconsistent with instances
- `E_SNAPSHOT_TYPEID_MISMATCH` — persisted type ID does not match schema
- `E_SNAPSHOT_DANGLING_REFERENCE` — edge target or duplicate conflict not found
- `E_SNAPSHOT_INVALID_COMPOSED` — composed child carries edges
- `E_SNAPSHOT_COMPOSED_ON_DUPLICATE`, `E_SNAPSHOT_EDGES_ON_DUPLICATE` — illegal data on duplicate records
- `E_SNAPSHOT_DEPTH_EXCEEDED` — composed nesting exceeds depth limit (32)
- `E_SNAPSHOT_INTEGRITY_MISMATCH` — integrity hash does not match
- `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` (Warning) — the schema hash algorithm in the snapshot header is not recognized; schema hash verification is skipped and the load proceeds without the compatibility check
- `E_SNAPSHOT_PATH_FALLBACK` (Warning) — a provenance path string could not be parsed into a canonical path and fell back to the root path; the original string is preserved for round-trip fidelity

**Adapter** — format-specific errors:

- `E_ADAPTER_PARSE` — parsing error in adapter input

## File Extension and Conventions

- Schema files use the `.yammm` extension
- Snapshot files use the `.ys` extension
- UTF-8 encoding is required
- One schema per file
- Import paths are case-sensitive on case-sensitive filesystems
- Canonical formatting is defined by `format.TokenStream` (see [API.md](API.md#formatting))

## Schema Identity

Each compiled schema has a deterministic structural hash (SHA-256) computed over:

- Schema name
- Type names, properties (with constraints and modifiers), relations (with targets and cardinalities), and inheritance edges
- Data type names and constraints

Invariants are deliberately excluded — they affect runtime validation but not structural shape. The hash is deterministic: all inputs are sorted lexicographically before hashing.

The hash format is `sha256:<hex>`. A structural hash version (currently `1`) is bumped when the algorithm changes. The hash enables schema compatibility checking for `.ys` snapshots: `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` is emitted when a snapshot's persisted hash does not match the current schema.

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
IntBound   = "_" | [ "-" ] INTEGER .
FloatBound = "_" | [ "-" ] ( INTEGER | FLOAT ) .
ListBound  = "_" | INTEGER .
```

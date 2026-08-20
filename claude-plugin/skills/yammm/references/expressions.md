# Expression Language Reference

The expression language is used within invariant declarations (`! "message" expression`) to define business logic constraints. Expressions evaluate against instance data at validation time.

---

## Operators

| Operator | Description |
| -------- | ----------- |
| `+` | Addition (numbers), concatenation (strings), or concatenation (slices/arrays) |
| `-` | Subtraction or unary negation |
| `*` | Multiplication |
| `/` | Division |
| `%` | Modulo (integers only) |
| `==` | Equal (mismatched types return false, not errors) |
| `!=` | Not equal (mismatched types return true, not errors) |
| `<` | Less than |
| `<=` | Less than or equal |
| `>` | Greater than |
| `>=` | Greater than or equal |
| `&&` | Logical AND (short-circuit) |
| `\|\|` | Logical OR (short-circuit) |
| `^` | Logical XOR |
| `!` | Logical NOT (unary) |
| `in` | Collection membership (mismatched types skip, not errors) |
| `=~` | Pattern match (regex) or type check |
| `!~` | Negated pattern match or type check |
| `->` | Pipeline operator |
| `.` | Property access |
| `?` | Ternary conditional (`cond ? { then : else }`) |

### Logical AND

`&&` evaluates left-to-right with short-circuit semantics. If the left operand is false, the right operand is not evaluated.

```yammm-snippet
! "both_required" start_date != nil && end_date != nil
! "range_valid" start_date != nil && end_date != nil && end_date > start_date
```

### Equality and Type Comparison

`==` and `!=` on mismatched types return not-equal (false / true respectively) rather than raising errors. This allows safe heterogeneous comparisons.

### Pattern and Type Match

`=~` and `!~` support two modes:

**Regex matching** (right operand is a regex literal):

```yammm-snippet
! "valid_email" email =~ /.+@.+\..+/
```

**Type checking** (right operand is a datatype keyword):

```yammm-snippet
! "must_be_integer" value =~ Integer
! "not_a_string" data !~ String
```

Supported type keywords: `String`, `Integer` (alias `Int`), `Float` (alias `Number`), `Boolean` (alias `Bool`), `UUID`, `Timestamp`, `Date`.

### Addition / Concatenation

The `+` operator is polymorphic:

- **Numbers**: arithmetic addition
- **Strings**: string concatenation
- **Slices/arrays**: concatenation (produces a new slice containing elements from both operands)

---

## Operator Precedence

From highest to lowest:

| Precedence | Operators | Associativity |
| ---------- | --------- | ------------- |
| 1 | Literals, list literals `[...]` | -- |
| 2 | Unary minus `-x` | Right |
| 3 | Indexing `expr[i]` | Left |
| 4 | Pipeline `lhs -> name(args)\|params\|{body}` | Left |
| 5 | Property access `lhs.property` | Left |
| 6 | Logical NOT `!expr` | Right |
| 7 | Multiplicative `*`, `/`, `%` | Left |
| 8 | Additive `+`, `-` | Left |
| 9 | Comparison `<`, `<=`, `>`, `>=` | Left |
| 10 | Membership `in` | Left |
| 11 | Regex/type match `=~`, `!~` | Left |
| 12 | Equality `==`, `!=` | Left |
| 13 | Logical AND `&&` | Left |
| 14 | Logical OR/XOR `\|\|`, `^` | Left |
| 15 | Ternary `? { then : else }` | Right |

Parentheses override precedence as usual.

---

## Pipeline Operator

The `->` operator chains function calls. The left-hand side becomes the implicit first argument to the function on the right.

```text
name -> Upper -> Trim
name -> Replace("old", "new") -> Lower
items -> Filter |$x| { $x.active } -> Map |$x| { $x.name }
prices -> Reduce(0.0) |$acc, $p| { $acc + $p }
```

Full syntax:

```text
expr -> FunctionName [ (args) ] [ |$params| ] [ { body } ]
```

---

## Lambda Syntax

Lambdas define inline functions with `|...|` parameters and `{...}` body.

**Single parameter:**

```text
items -> Filter |$x| { $x.active }
items -> Map |$item| { $item.price * $item.quantity }
```

**Multiple parameters** (used with `Reduce`):

```text
values -> Reduce(0) |$acc, $val| { $acc + $val }
```

Lambda parameters shadow outer variables within their body.

---

## Property Access

Properties are accessed with dot notation or bare names:

```text
name                // implicit property reference
$self.name          // explicit self reference
$item.price         // lambda parameter property
address.city        // nested property access
```

Property access on existing instances returns **nil** for properties not present on the object (enabling `Then`/`Lest` nil-guarded patterns). This allows safe navigation without raising evaluation errors.

---

## Indexing

```text
items[0]            // first element
items[2]            // third element
```

Works on strings (rune-indexed) and lists. Out-of-bounds indexing returns **nil** rather than raising an error, as does indexing `nil`. A non-integer index is an evaluation error.

The bracket takes exactly one index. There is no range slice: `expr[a, b]` draws an evaluation error, and so does the empty `expr[]`. The grammar admits both spellings and a trailing comma (`expr[a,]`, evaluated as `expr[a]`) -- do not write any of them.

These spellings are checked against the loader when this document is tested:

```yammm-snippet
! "first tag is alpha" tags[0] == "alpha"
! "every instance has properties" $self -> Len > 0
! "a uuid reports as a string" id -> TypeOf == "string"
```

The else-less ternary is rejected at load time, not at evaluation:

```yammm-invalid
! "no else branch" tags -> Len > 0 ? { true }  // E_SYNTAX
```

The bracket arity rules and the `TypeOf` vocabulary are evaluation-time
outcomes, which the fence vocabulary cannot decide; they are pinned by the
tracked contract corpus at `instance/testdata/contract/`.

---

## Variables

- `$self` -- bound to the current instance during invariant evaluation
- `$0`, `$1`, ... -- positional variables (evaluator-local, default nil)
- `$item`, `$acc`, etc. -- named lambda parameters
- Named variables are resolved through the evaluator's parent chain; undefined variables raise errors

---

## Nil Handling

In expressions, `_` and `nil` are interchangeable nil literals:

```yammm-snippet
! "guard" end_date == nil || end_date > start_date
! "guard" end_date == _ || end_date > start_date
```

Note: `_` has distinct non-nil meanings in bounds (`Integer[0, _]`) and multiplicity (`(_:many)`). Within expressions only, it means nil.

---

## Built-in Functions

All built-in functions are invoked via the pipeline operator. The left-hand side is the implicit first argument. Function names are capitalized.

### String Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `Len` | `s -> Len` | Length in runes (nil yields 0) |
| `Upper` | `s -> Upper` | Convert to uppercase |
| `Lower` | `s -> Lower` | Convert to lowercase |
| `Trim` | `s -> Trim` | Remove leading/trailing whitespace |
| `TrimPrefix` | `s -> TrimPrefix(prefix)` | Remove prefix if present |
| `TrimSuffix` | `s -> TrimSuffix(suffix)` | Remove suffix if present |
| `Split` | `s -> Split(sep)` | Split string by separator, returns list |
| `Join` | `list -> Join(sep)` | Join list elements with separator |
| `StartsWith` | `s -> StartsWith(prefix)` | True if string starts with prefix |
| `EndsWith` | `s -> EndsWith(suffix)` | True if string ends with suffix |
| `Replace` | `s -> Replace(old, new)` | Replace all occurrences |
| `Substring` | `s -> Substring(start)` or `s -> Substring(start, end)` | Extract substring by rune indices. The `end` argument is optional and defaults to string length. |
| `Match` | `s -> Match(/pattern/)` | Regex match with captures |

### Collection Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `Len` | `list -> Len` | Number of elements; also counts a map's entries, so `$self -> Len` is the instance's property count (nil yields 0) |
| `All` | `list -> All \|$x\| { pred }` | True if all match (true on empty) |
| `Any` | `list -> Any \|$x\| { pred }` | True if any match (false on empty) |
| `AllOrNone` | `list -> AllOrNone \|$x\| { pred }` | True if all match or none match (true on empty) |
| `Count` | `list -> Count \|$x\| { pred }` | Count elements matching predicate |
| `Filter` | `list -> Filter \|$x\| { pred }` | Keep elements matching predicate |
| `Map` | `list -> Map \|$x\| { expr }` | Transform each element |
| `Reduce` | `list -> Reduce(init) \|$acc, $x\| { expr }` | Fold to single value. The `init` argument is optional -- when omitted, the first element is used as seed. |
| `First` | `list -> First` | First element (nil if empty) |
| `Last` | `list -> Last` | Last element (nil if empty) |
| `Sum` | `list -> Sum` | Sum numeric elements |
| `Sort` | `list -> Sort` | Sort in natural order |
| `Reverse` | `list -> Reverse` | Reverse element order |
| `Flatten` | `list -> Flatten` | Flatten one level of nesting |
| `Compact` | `list -> Compact` | Remove nil entries |
| `Unique` | `list -> Unique` | Deduplicate elements |
| `Contains` | `list -> Contains(value)` | True if element exists in list. Works on slices/arrays only (not strings). |

**Empty collection behavior:**

- `All` returns `true` on empty (vacuous truth)
- `Any` returns `false` on empty
- `AllOrNone` returns `true` on empty (vacuous truth)
- Nil inputs are treated as empty collections

### Math Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `Abs` | `n -> Abs` | Absolute value |
| `Min` | `a -> Min(b)` or `list -> Min` | Minimum of two values or collection |
| `Max` | `a -> Max(b)` or `list -> Max` | Maximum of two values or collection |
| `Floor` | `f -> Floor` | Floor of float |
| `Ceil` | `f -> Ceil` | Ceiling of float |
| `Round` | `f -> Round` | Round to nearest integer (banker's rounding) |
| `Compare` | `a -> Compare(b)` | Three-way comparison: -1, 0, or 1 |

### Control Flow Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `Then` | `val -> Then \|$v\| { expr }` | Execute body when val is non-nil; returns nil otherwise |
| `Lest` | `val -> Lest { expr }` | Execute body when val is nil; returns val otherwise. Accepts but ignores a lambda parameter. |
| `With` | `val -> With \|$v\| { expr }` | Bind value to parameter and execute body |
| `Default` | `val -> Default(fallback)` | Return fallback if val is nil |
| `Coalesce` | `a -> Coalesce(b, c, ...)` | Return first non-nil value |

### Type Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `TypeOf` | `val -> TypeOf` | DSL type name as string: `"nil"`, `"boolean"`, `"integer"`, `"float"`, `"string"`, `"list"`, `"map"`, or `"pattern"` (`"unknown"` for anything else). The name comes from the value, not the declared property type; validation stores `Timestamp`, `Date` and `UUID` as text, so all three yield `"string"` |
| `IsNil` | `val -> IsNil` | True if value is nil |

---

## Composed Expression Examples

### Validate all active items have names

```yammm-snippet
! "active_items_named" ITEMS -> Filter |$i| { $i.active } -> All |$i| { $i.name -> Len > 0 }
```

### Compute total and validate minimum

```yammm-snippet
! "minimum_order_value" ITEMS -> Map |$i| { $i.price * $i.quantity } -> Sum >= 10.0
```

### Conditional validation with nil guard and pipeline

```yammm-snippet
! "normalized_name" name -> Lower -> Trim -> Then |$n| { $n -> Len > 0 } -> Default(true)
```

### Reduce without init (first element as seed)

```yammm-snippet
! "max_score" scores -> Reduce |$max, $s| { $max -> Max($s) } >= 50
```

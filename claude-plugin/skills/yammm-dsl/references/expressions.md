# Yammm Expression Language Reference

The expression language is used within invariant declarations (`! "message" expression`) to define business logic constraints. Expressions evaluate against instance data at validation time.

---

## Operators

| Operator | Description |
| -------- | ----------- |
| `+` | Addition (numbers) or concatenation (strings) |
| `-` | Subtraction or unary negation |
| `*` | Multiplication |
| `/` | Division |
| `%` | Modulo (integers only) |
| `==` | Equal |
| `!=` | Not equal |
| `<` | Less than |
| `<=` | Less than or equal |
| `>` | Greater than |
| `>=` | Greater than or equal |
| `&&` | Logical AND (short-circuit) |
| `\|\|` | Logical OR (short-circuit) |
| `^` | Logical XOR |
| `!` | Logical NOT (unary) |
| `in` | Collection membership test |
| `=~` | Pattern match (regex) or type check |
| `!~` | Negated pattern match or type check |
| `->` | Pipeline operator |
| `.` | Property access |
| `?` | Ternary conditional (`cond ? { then : else }`) |

### Pattern and Type Match

`=~` and `!~` support two modes:

**Regex matching** (right operand is a regex literal):

```yammm
! "valid_email" email =~ /.+@.+\..+/
```

**Type checking** (right operand is a datatype keyword):

```yammm
! "must_be_integer" value =~ Integer
! "must_be_numeric" price =~ Float
! "not_a_string" data !~ String
```

Supported type keywords: `String`, `Integer` (alias `Int`), `Float` (alias `Number`), `Boolean` (alias `Bool`), `UUID`, `Timestamp`, `Date`.

---

## Operator Precedence

From highest to lowest precedence:

| Precedence | Operators | Associativity |
| ---------- | --------- | ------------- |
| 1 | Literals, list literals `[...]` | -- |
| 2 | Unary minus `-x` | Right |
| 3 | Indexing/slicing `expr[i]`, `expr[a,b]` | Left |
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

```yammm
// Simple chaining
name -> Upper -> Trim

// With explicit arguments
name -> Replace("old", "new") -> Lower

// With lambda body
items -> Filter |$x| { $x.active } -> Map |$x| { $x.name }

// Reducing a collection
prices -> Reduce(0.0) |$acc, $p| { $acc + $p }
```

The full pipeline syntax is:

```text
expr -> FunctionName [ (args) ] [ |$params| ] [ { body } ]
```

---

## Lambda Syntax

Lambdas define inline functions with parameters enclosed in `|...|` and a body in `{...}`.

**Single parameter:**

```yammm
items -> Filter |$x| { $x.active }
items -> Map |$item| { $item.price * $item.quantity }
```

**Multiple parameters** (used with `Reduce`):

```yammm
values -> Reduce(0) |$acc, $val| { $acc + $val }
```

Lambda parameters shadow outer variables within their body.

---

## Property Access

Properties are accessed with dot notation or bare names:

```yammm
name                // Implicit property reference
$self.name          // Explicit self reference
$item.price         // Lambda parameter property
address.city        // Nested property access
$self.address.zip   // Nested with explicit self
```

Property lookups are strict: unknown properties and non-map dereferences raise evaluation errors, not nil.

---

## Indexing and Slicing

```yammm
items[0]            // First element
name[0, 5]          // Substring: runes 0 through 4
```

Works on strings (rune-indexed) and arrays/slices. Invalid indices produce evaluation errors.

---

## Variables

- `$self` -- bound to the current instance during invariant evaluation
- `$0`, `$1`, ... -- positional variables (evaluator-local, default nil)
- `$item`, `$acc`, etc. -- named lambda parameters
- Named variables are resolved through the evaluator's parent chain; undefined variables raise errors

---

## Nil Handling

In expressions, `_` and `nil` are interchangeable nil literals:

```yammm
! "guard" end_date == nil || end_date > start_date
! "guard" end_date == _ || end_date > start_date
```

Note: `_` has distinct non-nil meanings in bounds (`Integer[0, _]`) and multiplicity (`(_:many)`). Within expressions only, it means nil.

---

## Built-in Functions

All built-in functions are invoked via the pipeline operator. The left-hand side is the implicit first argument.

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
| `Contains` | `s -> Contains(sub)` | True if string contains substring |
| `StartsWith` | `s -> StartsWith(prefix)` | True if string starts with prefix |
| `EndsWith` | `s -> EndsWith(suffix)` | True if string ends with suffix |
| `Replace` | `s -> Replace(old, new)` | Replace all occurrences |
| `Substring` | `s -> Substring(start, end)` | Extract substring by rune indices |
| `Match` | `s -> Match(/pattern/)` | Regex match with captures |

### Collection Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `Len` | `list -> Len` | Number of elements (nil yields 0) |
| `All` | `list -> All \|$x\| { pred }` | True if all elements match predicate (true on empty) |
| `Any` | `list -> Any \|$x\| { pred }` | True if any element matches predicate (false on empty) |
| `AllOrNone` | `list -> AllOrNone \|$x\| { pred }` | True if all match or none match (true on empty) |
| `Count` | `list -> Count \|$x\| { pred }` | Count elements matching predicate |
| `Filter` | `list -> Filter \|$x\| { pred }` | Keep elements matching predicate |
| `Map` | `list -> Map \|$x\| { expr }` | Transform each element |
| `Reduce` | `list -> Reduce(init) \|$acc, $x\| { expr }` | Fold to single value with accumulator |
| `First` | `list -> First` | First element (nil if empty) |
| `Last` | `list -> Last` | Last element (nil if empty) |
| `Sum` | `list -> Sum` | Sum numeric elements |
| `Sort` | `list -> Sort` | Sort elements in natural order |
| `Reverse` | `list -> Reverse` | Reverse element order |
| `Flatten` | `list -> Flatten` | Flatten one level of nesting |
| `Compact` | `list -> Compact` | Remove nil entries |
| `Unique` | `list -> Unique` | Deduplicate elements |
| `Contains` | `list -> Contains(value)` | True if element exists in list |

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
| `Compare` | `a -> Compare(b)` | Three-way comparison: returns -1, 0, or 1 |

### Control Flow Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `Then` | `val -> Then \|$v\| { expr }` | Execute body when val is non-nil; returns nil otherwise |
| `Lest` | `val -> Lest { expr }` | Execute body when val is nil; returns val otherwise |
| `With` | `val -> With \|$v\| { expr }` | Bind value to parameter and execute body |
| `Default` | `val -> Default(fallback)` | Return fallback if val is nil |
| `Coalesce` | `a -> Coalesce(b, c, ...)` | Return first non-nil value |

### Type Functions

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `TypeOf` | `val -> TypeOf` | Get runtime type name as string |
| `IsNil` | `val -> IsNil` | True if value is nil |

---

## Composed Expression Examples

### Validate all active items have names

```yammm
! "active_items_named" ITEMS -> Filter |$i| { $i.active } -> All |$i| { $i.name -> Len > 0 }
```

### Compute total and validate minimum

```yammm
! "minimum_order_value" ITEMS -> Map |$i| { $i.price * $i.quantity } -> Sum >= 10.0
```

### Conditional validation with nil guard and pipeline

```yammm
! "normalized_name_valid" name -> Lower -> Trim -> Then |$n| { $n -> Len > 0 } -> Default(true)
```

---

## Evaluation Notes

- The evaluator works against in-memory instance data only; there is no implicit database lookup
- Evaluation errors (undefined property, type mismatch) surface as fatal issues
- Panics (e.g., divide-by-zero) are recovered and reported as errors with operator stack context
- `==`, `!=`, and `in` reject mismatched types rather than returning false

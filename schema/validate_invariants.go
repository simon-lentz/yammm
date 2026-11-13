package schema

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/internal/parse"
)

// staticKind is what the static checker knows an expression evaluates to.
// The lattice mirrors what the evaluator produces: a composition yields its
// child instances, an association yields the target key, a property yields a
// scalar or a list, and a pipeline stage maps one kind to another. The nil
// literal is its bottom — joined with anything it yields that thing, as an
// absent value stands in for any value — and unknown its top.
type staticKind uint8

const (
	kindUnknown  staticKind = iota // no claim; member access is not checked
	kindInstance                   // an instance of one of typs: its members are those every one declares
	kindList                       // a list of elem
	kindScalar                     // a string, a number or a boolean: no members
	kindNil                        // a value that is always nil, the nil literal among them: refused where a position takes no nil
)

// scalarKind narrows a scalar to what the operators and builtins distinguish:
// a string concatenates and indexes, a number adds, a number known to be a
// float is refused where an integer is taken, a boolean does none of these.
type scalarKind uint8

const (
	scalarAny     scalarKind = iota // a scalar of unknown kind
	scalarString                    // a string, or a value the wire renders as one — a key among them
	scalarNumber                    // a number not known to be a float: an integer, or one of either kind
	scalarFloat                     // a float: the evaluator holds a float64
	scalarBoolean                   // a boolean
)

// String names the kind as a diagnostic reads it, so a message naming a scalar
// cannot drift from the definition it names.
func (k scalarKind) String() string {
	switch k {
	case scalarString:
		return "a string"
	case scalarNumber, scalarFloat:
		return "a number"
	case scalarBoolean:
		return "a boolean"
	case scalarAny:
	}
	return "a scalar"
}

// isNumber reports whether k is a number, a float or not.
func (k scalarKind) isNumber() bool {
	return k == scalarNumber || k == scalarFloat
}

// staticType is what one expression evaluates to. An association reads as its
// target's primary key — a string, or a list of strings for a composite key —
// and viaAssociation records that origin so a member read through it names
// the association rather than "a value with no members".
//
// An instance type holds every type the value may be: one after a member
// read, several after a guard, a conditional or a list literal whose
// alternatives are instances of different types. A member reads through it
// only when every alternative declares it, which is what the evaluator's map
// lookup honours on every input. Ancestry is never consulted: two types with
// no common ancestor still share the members each declares.
type staticType struct {
	kind           staticKind
	scalar         scalarKind  // kindScalar
	typs           []Type      // kindInstance; ordered by identity, no duplicates
	elem           *staticType // kindList
	viaAssociation bool
}

var (
	unknownType = staticType{kind: kindUnknown}
	scalarType  = staticType{kind: kindScalar}
	stringType  = staticType{kind: kindScalar, scalar: scalarString}
	numberType  = staticType{kind: kindScalar, scalar: scalarNumber}
	floatType   = staticType{kind: kindScalar, scalar: scalarFloat}
	boolType    = staticType{kind: kindScalar, scalar: scalarBoolean}
	nilType     = staticType{kind: kindNil}
	// emptyListType is the empty list literal: a list with no element, so its
	// element is of no known kind and any list absorbs it in a join.
	emptyListType = staticType{kind: kindList}
)

// noValueType is the static type of a literal with no value: the kind of
// literal the source wrote, of no value.
func noValueType(kind parse.LiteralKind) staticType {
	switch kind {
	case parse.StringLiteral:
		return stringType
	case parse.IntegerLiteral:
		return numberType
	case parse.FloatLiteral:
		return floatType
	}
	return unknownType
}

func listOf(elem staticType) staticType {
	e := elem
	return staticType{kind: kindList, elem: &e}
}

func instanceOf(t Type) staticType {
	if t.IsZero() {
		return unknownType
	}
	return staticType{kind: kindInstance, typs: []Type{t}}
}

// typeName names an instance type as a diagnostic reads it: one type's name,
// or every alternative's joined by "or".
func (t staticType) typeName() string {
	names := make([]string, len(t.typs))
	for i, ty := range t.typs {
		names[i] = ty.Name()
	}
	return strings.Join(names, " or ")
}

// unionTypes is the set of both operands' types, deduplicated and ordered by
// identity so a union built in either order is one value.
func unionTypes(a, b []Type) []Type {
	out := make([]Type, 0, len(a)+len(b))
	out = append(out, a...)
	for _, t := range b {
		if !slices.ContainsFunc(out, func(x Type) bool { return x.ID() == t.ID() }) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(x, y Type) int { return strings.Compare(x.ID().String(), y.ID().String()) })
	return out
}

// element is the type of one element of t: a list's element, else unknown.
func (t staticType) element() staticType {
	if t.kind == kindList && t.elem != nil {
		return *t.elem
	}
	return unknownType
}

// selfVariable is the name both layers bind to the instance, held once in
// internal/expr so the completer, the checker and the evaluator cannot drift:
// the evaluator's PropertyScopeOf seeds it and the checker's root scope binds
// it. Neither the bare name nor $self reaches a member so named, so completion
// refuses it.
const selfVariable = expr.SelfVariable

// staticScope is the lambda bindings in force at one point of the walk. Names
// match exactly, as the evaluator's Scope.Lookup resolves them.
type staticScope struct {
	vars map[string]staticType
}

// child binds one variable. Variable names match exactly, as the evaluator's
// Scope.Lookup does; only member names fold.
func (s *staticScope) child(name string, t staticType) *staticScope {
	vars := make(map[string]staticType, len(s.vars)+1)
	maps.Copy(vars, s.vars)
	vars[name] = t
	return &staticScope{vars: vars}
}

func (s *staticScope) lookupVar(name string) (staticType, bool) {
	t, ok := s.vars[name]
	return t, ok
}

// staticMembers is a type's merged members keyed by the name an expression
// writes, lower-cased: properties by name, relations by field name.
type staticMembers map[string]staticType

// keyTypeOf is what an association to target evaluates to: the target's
// single primary-key property as the scalar it is — every key kind renders
// as a string — or a list of those for a composite key. An unresolved target
// makes no claim.
func (c *completer) keyTypeOf(target Type) staticType {
	if target.IsZero() {
		return unknownType
	}
	pks := slices.Collect(target.PrimaryKeys())
	switch len(pks) {
	case 0:
		return unknownType
	case 1:
		k := propertyType(pks[0].Constraint())
		k.viaAssociation = true
		return k
	}
	elem := propertyType(pks[0].Constraint())
	elem.viaAssociation = true
	k := listOf(elem)
	k.viaAssociation = true
	return k
}

// membersOf returns t's member index, built once per type.
func (c *completer) membersOf(t Type) staticMembers {
	if m, ok := c.staticMembers[t]; ok {
		return m
	}
	if c.staticMembers == nil {
		c.staticMembers = make(map[Type]staticMembers)
	}
	m := make(staticMembers, len(t.allProperties)+len(t.allAssociations)+len(t.allCompositions))
	for _, p := range t.allProperties {
		m[strings.ToLower(p.Name())] = propertyType(p.Constraint())
	}
	for _, r := range t.allAssociations {
		key := c.keyTypeOf(c.resolveTypeID(r.TargetID()))
		if r.IsMany() {
			key = listOf(key)
		}
		m[r.FieldName()] = key
	}
	for _, r := range t.allCompositions {
		child := instanceOf(c.resolveTypeID(r.TargetID()))
		if r.IsMany() {
			child = listOf(child)
		}
		m[r.FieldName()] = child
	}
	c.staticMembers[t] = m
	return m
}

// propertyType is the static type a property's value has: a list for List
// and Vector, unknown for an alias that never resolved, a scalar otherwise. It
// is the form the validator stores: an Integer an int64, a Float and each
// element of a Vector a float64.
func propertyType(con Constraint) staticType {
	if con == nil {
		return unknownType
	}
	switch con.Kind() {
	case KindList:
		if lc, ok := con.(ListConstraint); ok {
			return listOf(propertyType(lc.Element()))
		}
		return listOf(scalarType)
	case KindVector:
		return listOf(floatType)
	case KindAlias:
		if a, ok := con.(AliasConstraint); ok {
			return propertyType(a.Resolved())
		}
		return unknownType
	case KindString, KindEnum, KindPattern, KindTimestamp, KindDate, KindUUID:
		// Temporal and UUID values are canonical text at evaluation time.
		return stringType
	case KindInteger:
		return numberType
	case KindFloat:
		return floatType
	case KindBoolean:
		return boolType
	}
	return unknownType
}

// invariantErrorf reports one defect once per invariant: two occurrences of one
// bad name in one expression are one mistake, reported at the invariant's span.
func (c *completer) invariantErrorf(inv Invariant, code diag.Code, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	key := code.String() + "\x00" + msg
	if c.invariantSeen[key] {
		return
	}
	if c.invariantSeen == nil {
		c.invariantSeen = make(map[string]bool)
	}
	c.invariantSeen[key] = true
	c.errorf(inv.Span(), code, "%s", msg)
}

// binaryOps are the operators that take two operands. Every one but `+`
// yields a number or a boolean; `+` yields what its operands are (docs/SPEC.md:
// numbers add, strings concatenate, lists concatenate). Everything else an
// S-expression can name is a builtin or an error.
var binaryOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	">": true, ">=": true, "<": true, "<=": true,
	"in": true, "=~": true, "!~": true, "==": true, "!=": true,
	"&&": true, "||": true,
}

// operandKinds is the kind each operator takes at an operand, by the tree's
// operator name: both operands of a binary operator, the one of a unary, a
// conditional's condition, an index, and the left of a match. The evaluator
// refuses every other value that is not nil where it reads the operand, save
// at an ordering, which the check holds narrower ([expr.ArgOrdered]). `+` pairs
// its operands ([completer.typePlus]), `in` takes a list on its right, and `==`
// and `!=` take any two values.
var operandKinds = map[string]expr.ArgKind{
	"-": expr.ArgNumber, "*": expr.ArgNumber, "/": expr.ArgNumber, "-x": expr.ArgNumber,
	"%": expr.ArgInteger, "@": expr.ArgInteger,
	"!": expr.ArgBoolean, "&&": expr.ArgBoolean, "||": expr.ArgBoolean, "?": expr.ArgBoolean,
	"<": expr.ArgOrdered, "<=": expr.ArgOrdered, ">": expr.ArgOrdered, ">=": expr.ArgOrdered,
	"=~": expr.ArgString, "!~": expr.ArgString,
}

// admits reports whether a value of type t can be of kind: the one rule an
// argument and an operand are judged by. A value the checker cannot type
// passes. A value that is always nil passes only where the kind holds nil: the
// evaluator errors on nil at a position of any other kind. A pattern is no
// value, so [completer.isPattern] judges that kind from the tree.
func admits(kind expr.ArgKind, t staticType) bool {
	open := t.kind == kindUnknown || (t.kind == kindScalar && t.scalar == scalarAny)
	switch kind {
	case expr.ArgString:
		return open || (t.kind == kindScalar && t.scalar == scalarString)
	case expr.ArgNumber:
		return open || (t.kind == kindScalar && t.scalar.isNumber())
	case expr.ArgInteger:
		return open || (t.kind == kindScalar && t.scalar == scalarNumber)
	case expr.ArgBoolean:
		return open || (t.kind == kindScalar && t.scalar == scalarBoolean)
	case expr.ArgOrdered:
		return orderable(t)
	case expr.ArgPattern:
		return false
	case expr.ArgAny:
	}
	return true
}

// orderable reports whether the total order ranks every value t may be, as
// far as the checker knows t: any type but an instance and a list that holds
// one.
func orderable(t staticType) bool {
	switch t.kind {
	case kindInstance:
		return false
	case kindList:
		return orderable(t.element())
	case kindUnknown, kindScalar, kindNil:
	}
	return true
}

// checkOperand refuses an operand of a kind op refuses. what names the operand
// as the message reads it: "its left operand", "its condition".
func (c *completer) checkOperand(op, what string, t staticType, owner Type, inv Invariant) {
	kind := operandKinds[op]
	if admits(kind, t) {
		return
	}
	name := op
	switch op {
	case "-x":
		name = "unary -"
	case "?":
		name = "a conditional"
	case "@":
		name = "indexing"
	}
	c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
		"%s takes %s as %s, and it is not one in invariant %q on type %q", name, kind, what, inv.Name(), owner.Name())
}

// arithmeticType is the kind of number an arithmetic operator yields over two
// operands: a float when either is a float, as the evaluator promotes, and
// else a number not known to be one.
func arithmeticType(l, r staticType) staticType {
	isFloat := func(t staticType) bool { return t.kind == kindScalar && t.scalar == scalarFloat }
	if isFloat(l) || isFloat(r) {
		return floatType
	}
	return numberType
}

// join is the static type a value that is one of two expressions has, and
// whether the two are disjoint: of different kinds, or scalars of different
// known subkinds. The nil literal yields the other side, unknown yields
// unknown, two instances yield their union, two lists a list of their joined
// element or the other when one is the empty literal, two scalars their shared
// subkind, two numbers of different kinds a number, or a scalar of unknown
// subkind.
// It is the one join every position where two expressions meet reads —
// a guard, a conditional, a list literal, concatenation — so no two of them
// can disagree about what a value may be.
func join(a, b staticType) (staticType, bool) {
	switch {
	case a.kind == kindNil:
		return b, false
	case b.kind == kindNil:
		return a, false
	case a.kind == kindUnknown || b.kind == kindUnknown:
		return unknownType, false
	case a.kind != b.kind:
		return unknownType, true
	}
	via := a.viaAssociation || b.viaAssociation
	switch a.kind {
	case kindInstance:
		return staticType{kind: kindInstance, typs: unionTypes(a.typs, b.typs)}, false
	case kindList:
		switch {
		case a.elem == nil:
			return b, false
		case b.elem == nil:
			return a, false
		}
		elem, disjoint := join(*a.elem, *b.elem)
		t := listOf(elem)
		t.viaAssociation = via
		return t, disjoint
	case kindScalar:
		t := staticType{kind: kindScalar, scalar: a.scalar, viaAssociation: via}
		switch {
		case a.scalar == b.scalar:
			return t, false
		case a.scalar == scalarAny || b.scalar == scalarAny:
			t.scalar = scalarAny
			return t, false
		case a.scalar.isNumber() && b.scalar.isNumber():
			t.scalar = scalarNumber
			return t, false
		}
		t.scalar = scalarAny
		return t, true
	case kindUnknown, kindNil:
	}
	return unknownType, false
}

// mergeType is join for a position that admits disjoint alternatives — a
// conditional, a list literal, concatenation — where the result is what the
// two agree on and a disagreement is no claim.
func mergeType(a, b staticType) staticType {
	t, _ := join(a, b)
	return t
}

// typeBinary types a binary operator's result and refuses each operand of a
// kind the operator refuses: by [operandKinds], and for `in` a right operand
// that is no list.
func (c *completer) typeBinary(op string, children []expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	l := c.typeExpr(children[0], sc, owner, inv)
	if op == "=~" || op == "!~" {
		c.checkOperand(op, "its left operand", l, owner, inv)
		if !c.isPattern(children[1]) {
			c.typeExpr(children[1], sc, owner, inv)
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%s takes a pattern literal as its right operand, and it is not one in invariant %q on type %q", op, inv.Name(), owner.Name())
		}
		return boolType
	}
	r := c.typeExpr(children[1], sc, owner, inv)
	switch op {
	case "+":
		return c.typePlus(l, r, owner, inv)
	case "==", "!=":
		return boolType
	case "in":
		// The evaluator answers false for a nil right operand, so only a
		// scalar or an instance — which it refuses — is refused here.
		if r.kind == kindScalar || r.kind == kindInstance {
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"in takes a list on its right in invariant %q on type %q", inv.Name(), owner.Name())
		}
		return boolType
	}
	c.checkOperand(op, "its left operand", l, owner, inv)
	c.checkOperand(op, "its right operand", r, owner, inv)
	switch op {
	case "-", "*", "/":
		return arithmeticType(l, r)
	case "%":
		return numberType
	}
	return boolType
}

// isPattern reports whether e is a pattern literal as the source wrote it, one
// with no value among them. A pattern is no value: it stands only on the right
// of `=~` and `!~` and as the argument of Match, and each of those positions
// reads it here and never types it.
func (c *completer) isPattern(e expr.Expression) bool {
	lit, ok := e.(*expr.Literal)
	if !ok {
		return false
	}
	if kind, noValue := c.noValue[lit]; noValue {
		return kind == parse.RegexLiteral
	}
	_, ok = lit.Val.(*regexp.Regexp)
	return ok
}

// typePlus types `+`: two lists concatenate to a list of their merged element,
// two strings to a string, two numbers to a number. An instance, a boolean and
// the nil literal are refused beside any operand, as is a list beside a scalar
// or a string beside a number; any other pair that holds an operand of unknown
// kind is admitted, and its result is unknown.
func (c *completer) typePlus(l, r staticType, owner Type, inv Invariant) staticType {
	refuse := func() staticType {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"+ takes two numbers, two strings or two lists in invariant %q on type %q", inv.Name(), owner.Name())
		return unknownType
	}
	for _, t := range []staticType{l, r} {
		// An instance and a boolean have no + arm, and the nil literal is an error
		// on every input (docs/SPEC.md: an absent list is written with Default([])).
		if t.kind == kindInstance || t.kind == kindNil || (t.kind == kindScalar && t.scalar == scalarBoolean) {
			return refuse()
		}
	}
	if l.kind == kindUnknown || r.kind == kindUnknown {
		return unknownType
	}
	if l.kind == kindList && r.kind == kindList {
		// A concatenation is a new list, whatever either operand was read through.
		t := mergeType(l, r)
		t.viaAssociation = false
		return t
	}
	if l.kind == kindList || r.kind == kindList {
		return refuse()
	}
	if l.scalar == scalarAny || r.scalar == scalarAny {
		return scalarType
	}
	switch {
	case l.scalar == scalarString && r.scalar == scalarString:
		return stringType
	case l.scalar.isNumber() && r.scalar.isNumber():
		return arithmeticType(l, r)
	}
	return refuse()
}

// validateInvariantExpressions types every own invariant of every type and
// reports what the evaluator would refuse: an unknown member, a member read
// through an association key, a scalar or a list, an undefined named
// variable, an unknown function, a call shape its builtin rejects, an operand
// of a kind its operator refuses, and a result that is never a boolean.
//
// Own invariants only: an inherited invariant was checked when its declaring
// type completed, and a subtype's members are a superset of its ancestor's.
// This runs after completeTypes (inheritance merged) and
// validateRelationTargets (relation targets resolved).
func (c *completer) validateInvariantExpressions() {
	for _, t := range c.schema.types {
		if len(t.invariants) == 0 {
			continue
		}
		// A type whose supertype chain has an unresolved link has an
		// incomplete merged member set, so a reference to an inherited member
		// would false-positive; the link's root cause is already reported.
		if c.hasUnresolvedSupertype(t) {
			continue
		}
		// self is a bound variable at evaluation, where PropertyScopeOf
		// seeds it, so the checker binds it too and a parameter named self
		// shadows it through child exactly as WithVar does.
		scope := (&staticScope{}).child(selfVariable, instanceOf(t))
		for inv := range t.Invariants() {
			c.invariantSeen = nil
			c.checkResult(c.typeExpr(inv.expr, scope, t, inv), t, inv)
		}
	}
}

// checkResult refuses an invariant whose expression is of a known kind that is
// no boolean: the evaluator errors on each such value. The nil literal stands,
// because the evaluator reads nil as false.
func (c *completer) checkResult(result staticType, owner Type, inv Invariant) {
	var what string
	switch result.kind {
	case kindList:
		what = "a list"
	case kindInstance:
		what = "an instance"
	case kindScalar:
		if result.scalar == scalarAny || result.scalar == scalarBoolean {
			return
		}
		what = result.scalar.String()
	case kindUnknown, kindNil:
		return
	}
	c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
		"invariant %q on type %q evaluates to %s, and an invariant evaluates to a boolean", inv.Name(), owner.Name(), what)
}

// typeExpr walks e, reports its defects, and returns what it evaluates to.
func (c *completer) typeExpr(e expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	switch ex := e.(type) {
	case nil:
		return unknownType
	case *expr.Literal:
		// A literal with no value has its own diagnostic, so the checker
		// judges it by the kind of literal the source wrote and nothing more.
		if c.isPattern(ex) {
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"a pattern literal stands only on the right of =~ or !~ and as the argument of Match in invariant %q on type %q", inv.Name(), owner.Name())
			return unknownType
		}
		if kind, ok := c.noValue[ex]; ok {
			return noValueType(kind)
		}
		switch ex.Val.(type) {
		case []expr.Expression, []string:
			return unknownType
		case string:
			return stringType
		case int64:
			return numberType
		case float64:
			return floatType
		case bool:
			return boolType
		case nil:
			return nilType
		}
		return scalarType
	case expr.Op:
		return unknownType
	case expr.SExpr:
		return c.typeSExpr(ex, sc, owner, inv)
	}
	return unknownType
}

func (c *completer) typeSExpr(sexpr expr.SExpr, sc *staticScope, owner Type, inv Invariant) staticType {
	op := sexpr.Op()
	children := sexpr.Children()

	switch op {
	case "p":
		// A call named p shares this op and holds its operand and call parts.
		if len(children) == 1 {
			return c.typeProperty(children, sc, owner, inv)
		}
	case "$":
		return c.typeVariable(children, sc, owner, inv)
	case ".":
		return c.typeMember(children, sc, owner, inv)
	case "@":
		return c.typeIndexExpr(children, sc, owner, inv)
	case "[]":
		if len(children) == 0 {
			return emptyListType
		}
		elem := c.typeExpr(children[0], sc, owner, inv)
		for _, child := range children[1:] {
			elem = mergeType(elem, c.typeExpr(child, sc, owner, inv))
		}
		return listOf(elem)
	case "?":
		c.checkOperand(op, "its condition", c.typeExpr(children[0], sc, owner, inv), owner, inv)
		then := c.typeExpr(children[1], sc, owner, inv)
		otherwise := c.typeExpr(children[2], sc, owner, inv)
		return mergeType(then, otherwise)
	case "-x":
		operand := c.typeExpr(children[0], sc, owner, inv)
		c.checkOperand(op, "its operand", operand, owner, inv)
		if operand.kind == kindScalar && operand.scalar.isNumber() {
			return staticType{kind: kindScalar, scalar: operand.scalar}
		}
		return numberType
	case "!":
		c.checkOperand(op, "its operand", c.typeExpr(children[0], sc, owner, inv), owner, inv)
		return boolType
	}

	// A call named in shares that operator's name and holds four parts.
	if binaryOps[op] && len(children) == 2 {
		return c.typeBinary(op, children, sc, owner, inv)
	}

	if spec, ok := expr.LookupBuiltin(op); ok {
		return c.typeCall(spec, children, sc, owner, inv)
	}

	c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
		"unknown function %q in invariant %q on type %q", op, inv.Name(), owner.Name())
	c.walkCallParts(children, sc, owner, inv)
	return unknownType
}

// walkCallParts types the operands of a call whose name is unknown, so their
// own defects are still reported once. The body is typed with its declared
// parameters bound, as any builtin would bind them, so a well-formed lambda
// is not also reported as undefined variables.
func (c *completer) walkCallParts(children []expr.Expression, sc *staticScope, owner Type, inv Invariant) {
	child := sc
	bound := false
	var body expr.Expression
	for i, ch := range children {
		if args, ok := expr.ArgsLiteral(ch); ok {
			for _, a := range args {
				c.typeExpr(a, sc, owner, inv)
			}
			continue
		}
		if params, ok := expr.ParamsLiteral(ch); ok {
			for _, p := range params {
				child = child.child(p, unknownType)
			}
			bound = len(params) > 0
			continue
		}
		if i == 0 {
			c.typeExpr(ch, sc, owner, inv)
			continue
		}
		if ch != nil {
			body = ch
		}
	}
	if body != nil {
		if !bound {
			child = child.child("0", unknownType)
		}
		c.typeExpr(body, child, owner, inv)
	}
}

// typeProperty resolves a bare name: a member of the owner, else a lambda
// variable named without its sigil, as the evaluator's scope resolves it.
func (c *completer) typeProperty(children []expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	name, _ := expr.StringLiteral(children[0])
	// A variable of exactly this name shadows a same-named member, as the
	// evaluator's scope chain resolves a bare name (docs/SPEC.md).
	if t, found := sc.lookupVar(name); found {
		return t
	}
	if t, found := c.membersOf(owner)[strings.ToLower(name)]; found {
		if !c.relationSpelled(owner, owner, name, inv) {
			return unknownType
		}
		return t
	}
	c.invariantErrorf(inv, diag.E_UNKNOWN_PROPERTY,
		"unknown property %q in invariant %q on type %q", name, inv.Name(), owner.Name())
	return unknownType
}

// relationSpelled reports whether name, which reads a member of t by its
// folded spelling in an invariant on owner, is a spelling docs/SPEC.md admits:
// a property in any case, a relation by its declared name alone.
func (c *completer) relationSpelled(owner, t Type, name string, inv Invariant) bool {
	rel, ok := t.RelationByField(strings.ToLower(name))
	if !ok || name == rel.Name() {
		return true
	}
	// A property of exactly this spelling is the member read; the collision
	// with the relation is E_PROPERTY_RELATION_COLLISION's report.
	if _, isProperty := t.Property(name); isProperty {
		return true
	}
	c.invariantErrorf(inv, diag.E_INVALID_NAME,
		"relation %s on type %q is read as %q in invariant %q on type %q: a relation is read by its declared name %s",
		rel.Name(), t.Name(), name, inv.Name(), owner.Name(), rel.Name())
	return false
}

// typeVariable resolves a lambda parameter, a member of the owner, or a
// numeric variable, in the evaluator's order. self is an ordinary bound
// variable, so a parameter of that name shadows it by binding order alone. Any
// other name names nothing the author declared, so it is refused here.
func (c *completer) typeVariable(children []expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	name, _ := expr.StringLiteral(children[0])
	if t, found := sc.lookupVar(name); found {
		return t
	}
	// A $ variable names a member exactly, as the evaluator reads it. Another
	// casing of a relation's name draws relationSpelled's issue at every door.
	if t, found := c.membersOf(owner)[strings.ToLower(name)]; found {
		if memberSpelled(owner, name) {
			return t
		}
		if !c.relationSpelled(owner, owner, name, inv) {
			return unknownType
		}
	}
	if isNumericVar(name) {
		return unknownType
	}
	c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
		"undefined variable $%s in invariant %q on type %q", name, inv.Name(), owner.Name())
	return unknownType
}

// memberSpelled reports whether name is the declared name of one of t's
// members, a property or a relation.
func memberSpelled(t Type, name string) bool {
	if _, ok := t.Property(name); ok {
		return true
	}
	_, ok := t.Relation(name)
	return ok
}

func isNumericVar(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// typeMember resolves receiver.name against what the receiver is known to be.
func (c *completer) typeMember(children []expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	recv := c.typeExpr(children[0], sc, owner, inv)
	name, _ := expr.StringLiteral(children[1])

	switch recv.kind {
	case kindInstance:
		return c.typeInstanceMember(recv, name, owner, inv)
	case kindList, kindScalar, kindNil:
		switch {
		case recv.viaAssociation:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%q is read through an association in invariant %q on type %q: an association evaluates to the target key, and the target's properties are not in this instance",
				name, inv.Name(), owner.Name())
		case recv.kind == kindList:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%q is read from a list in invariant %q on type %q: index or pipe the list first",
				name, inv.Name(), owner.Name())
		default:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%q is read from a value that has no members in invariant %q on type %q",
				name, inv.Name(), owner.Name())
		}
	case kindUnknown:
	}
	return unknownType
}

// typeInstanceMember resolves name on every type the receiver may be: the
// member is the join of what each declares, and an alternative that lacks it
// is the unknown-property error, since the evaluator reads nil there on the
// input that selects it. A type whose supertype chain has an unresolved link
// has an incomplete member set, so nothing is claimed; the link's root
// cause is already reported.
func (c *completer) typeInstanceMember(recv staticType, name string, owner Type, inv Invariant) staticType {
	if slices.ContainsFunc(recv.typs, c.hasUnresolvedSupertype) {
		return unknownType
	}
	member := nilType
	declared := make([]staticType, 0, len(recv.typs))
	for _, ty := range recv.typs {
		t, found := c.membersOf(ty)[strings.ToLower(name)]
		if !found {
			if len(recv.typs) == 1 {
				c.invariantErrorf(inv, diag.E_UNKNOWN_PROPERTY,
					"unknown property %q on type %q in invariant %q on type %q",
					name, ty.Name(), inv.Name(), owner.Name())
			} else {
				c.invariantErrorf(inv, diag.E_UNKNOWN_PROPERTY,
					"unknown property %q on type %q, one of the types the value may be (%s), in invariant %q on type %q",
					name, ty.Name(), recv.typeName(), inv.Name(), owner.Name())
			}
			return unknownType
		}
		if !c.relationSpelled(owner, ty, name, inv) {
			return unknownType
		}
		declared = append(declared, t)
		member = mergeType(member, t)
	}
	for i, a := range declared {
		for _, b := range declared[i+1:] {
			if _, disjoint := join(a, b); disjoint {
				c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
					"the types the value may be (%s) declare %q with disjoint kinds in invariant %q on type %q",
					recv.typeName(), name, inv.Name(), owner.Name())
				return unknownType
			}
		}
	}
	return member
}

// typeIndexExpr resolves receiver[index]: a list yields its element, a string a
// string; a number, a boolean, an instance or the nil literal cannot be
// indexed, and the index is an integer.
func (c *completer) typeIndexExpr(children []expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	recv := c.typeExpr(children[0], sc, owner, inv)
	c.checkOperand("@", "its index", c.typeExpr(children[1], sc, owner, inv), owner, inv)
	switch recv.kind {
	case kindList:
		return recv.element()
	case kindScalar:
		switch recv.scalar {
		case scalarString:
			return stringType
		case scalarNumber, scalarFloat, scalarBoolean:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%s cannot be indexed in invariant %q on type %q", recv.scalar, inv.Name(), owner.Name())
			return unknownType
		case scalarAny:
		}
		return scalarType
	case kindInstance:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"type %q cannot be indexed in invariant %q on type %q", recv.typeName(), inv.Name(), owner.Name())
	case kindNil:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"nil cannot be indexed in invariant %q on type %q", inv.Name(), owner.Name())
	case kindUnknown:
	}
	return unknownType
}

// typeCall types a pipeline call from its builtin's spec: it checks the call
// shape the evaluator would refuse, binds the lambda parameters to what the
// spec says they hold, and maps the receiver's type to the result's.
func (c *completer) typeCall(spec expr.BuiltinSpec, children []expr.Expression, sc *staticScope, owner Type, inv Invariant) staticType {
	recv := unknownType
	if len(children) > 0 {
		recv = c.typeExpr(children[0], sc, owner, inv)
	}

	var (
		args   []expr.Expression
		params []string
		body   expr.Expression
	)
	if len(children) > 1 {
		for _, child := range children[1:] {
			if a, ok := expr.ArgsLiteral(child); ok {
				args = a
				continue
			}
			if p, ok := expr.ParamsLiteral(child); ok {
				params = p
				continue
			}
			if child != nil {
				body = child
			}
		}
	}
	argTypes := make([]staticType, len(args))
	for i, a := range args {
		// A pattern in the position that takes one is read by checkArgs.
		if kind, _ := spec.ArgAt(i); kind == expr.ArgPattern && c.isPattern(a) {
			continue
		}
		argTypes[i] = c.typeExpr(a, sc, owner, inv)
	}

	// One mistake is one diagnostic: a call whose arity is wrong is not also
	// judged on the receiver shape that arity implies.
	switch {
	case len(args) < spec.MinArgs:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s requires at least %d argument(s) in invariant %q on type %q", spec.Name, spec.MinArgs, inv.Name(), owner.Name())
	case spec.MaxArgs >= 0 && len(args) > spec.MaxArgs:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s accepts at most %d argument(s) in invariant %q on type %q", spec.Name, spec.MaxArgs, inv.Name(), owner.Name())
	default:
		c.checkReceiver(spec, recv, len(args), owner, inv)
		c.checkArgs(spec, args, argTypes, owner, inv)
	}
	// One mistake in the lambda's shape is one diagnostic, and a body the
	// builtin would never evaluate is not typed.
	switch {
	case body != nil && !spec.AcceptBody:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s does not accept a lambda in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
		body = nil
	case body == nil && spec.AcceptBody:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s requires a lambda in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
	case len(params) > spec.MaxParams:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s accepts at most %d lambda parameter(s) in invariant %q on type %q", spec.Name, spec.MaxParams, inv.Name(), owner.Name())
	case spec.Params == expr.BindAccumulatorElement && paramOr(params, 0, "0") == paramOr(params, 1, "1"):
		// The evaluator binds the element over the accumulator of the same
		// name, so the body would read the element for both.
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s binds $%s twice in invariant %q on type %q", spec.Name, paramOr(params, 0, "0"), inv.Name(), owner.Name())
	}

	bodyType := unknownType
	if body != nil {
		child := sc
		switch spec.Params {
		case expr.BindElement:
			child = sc.child(paramOr(params, 0, "0"), recv.element())
		case expr.BindReceiver:
			child = sc.child(paramOr(params, 0, "0"), recv)
		case expr.BindPresentReceiver:
			// The body does not run on a receiver that is always nil, so its
			// parameter is of no known kind there.
			bound := recv
			if recv.kind == kindNil {
				bound = unknownType
			}
			child = sc.child(paramOr(params, 0, "0"), bound)
		case expr.BindAccumulatorElement:
			child = sc.child(paramOr(params, 0, "0"), unknownType).child(paramOr(params, 1, "1"), recv.element())
		case expr.BindNone:
		}
		bodyType = c.typeExpr(body, child, owner, inv)
	}

	switch spec.Result {
	case expr.ResultNumber:
		return numberType
	case expr.ResultReceiverNumber:
		if recv.kind == kindScalar && recv.scalar == scalarFloat {
			return floatType
		}
		return numberType
	case expr.ResultString:
		return stringType
	case expr.ResultBoolean:
		return boolType
	case expr.ResultReceiver:
		// The evaluator reads a nil receiver as no element and answers a list.
		// A receiver the check refused yields no kind.
		switch recv.kind {
		case kindList:
			return recv
		case kindNil:
			return emptyListType
		case kindUnknown, kindInstance, kindScalar:
		}
	case expr.ResultElement:
		return recv.element()
	case expr.ResultBodyList:
		return listOf(bodyType)
	case expr.ResultBody:
		return bodyType
	case expr.ResultFlattened:
		if recv.kind == kindList {
			if inner := recv.element(); inner.kind == kindList {
				return listOf(inner.element())
			}
			return recv
		}
	case expr.ResultList:
		return listOf(stringType)
	case expr.ResultElementOrArg:
		if len(args) == 0 {
			return recv.element()
		}
		return rankedResult(spec, recv, argTypes[0])
	case expr.ResultReceiverOrArg:
		if len(argTypes) == 0 {
			return unknownType
		}
		return c.typeGuard(spec, "a fallback", append([]staticType{recv}, argTypes...), owner, inv)
	case expr.ResultReceiverOrBody:
		if body == nil {
			return unknownType
		}
		return c.typeGuard(spec, "a body", []staticType{recv, bodyType}, owner, inv)
	case expr.ResultUnknown:
	}
	return unknownType
}

// typeGuard types a nil guard — Default, Coalesce, Lest — as the join of every
// value it can yield, and refuses any two alternatives join reports disjoint:
// the stage after would meet a value it cannot take. Every pair is compared, so
// the order the alternatives are written in does not decide the verdict.
func (c *completer) typeGuard(spec expr.BuiltinSpec, what string, alts []staticType, owner Type, inv Invariant) staticType {
	for i, a := range alts {
		for _, b := range alts[i+1:] {
			if _, disjoint := join(a, b); disjoint {
				c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
					"%s takes %s of its receiver's kind in invariant %q on type %q", spec.Name, what, inv.Name(), owner.Name())
				return unknownType
			}
		}
	}
	t := alts[0]
	for _, alt := range alts[1:] {
		t, _ = join(t, alt)
	}
	return t
}

// scalarRank places a scalar subkind in the total order internal/value.Order
// implements: boolean below number below string. A subkind the checker has not
// narrowed, and any kind that is not a scalar, report false.
func scalarRank(t staticType) (int, bool) {
	if t.kind != kindScalar {
		return 0, false
	}
	switch t.scalar {
	case scalarBoolean:
		return 1, true
	case scalarNumber, scalarFloat:
		return 2, true
	case scalarString:
		return 3, true
	case scalarAny:
	}
	return 0, false
}

// rankedResult types Min or Max with an argument, each of which yields its
// receiver or its argument by the total order, which ranks nil lowest. Min
// yields nil when either is absent, so of two scalars the order separates it
// is the lower. Max yields the lower when the higher is absent, so it is their
// join, as it is for a pair the order does not separate.
func rankedResult(spec expr.BuiltinSpec, recv, arg staticType) staticType {
	recvRank, recvOK := scalarRank(recv)
	argRank, argOK := scalarRank(arg)
	if spec.Name != "Min" || !recvOK || !argOK || recvRank == argRank {
		return mergeType(recv, arg)
	}
	if recvRank > argRank {
		return arg
	}
	return recv
}

// paramOr returns the i-th declared parameter name, or the implicit numeric
// variable the evaluator binds when the parameter is not declared.
func paramOr(params []string, i int, implicit string) string {
	if i < len(params) {
		return params[i]
	}
	return implicit
}

// checkArgs refuses an argument of a kind the builtin refuses, by the
// catalogue's [expr.ArgKind] and the rule [admits] states. A pattern is read
// from the tree, since it is no value.
func (c *completer) checkArgs(spec expr.BuiltinSpec, args []expr.Expression, argTypes []staticType, owner Type, inv Invariant) {
	for i, at := range argTypes {
		kind, ok := spec.ArgAt(i)
		if !ok {
			return
		}
		if admits(kind, at) || (kind == expr.ArgPattern && c.isPattern(args[i])) {
			continue
		}
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s takes %s as its argument, and argument %d is not one in invariant %q on type %q",
			spec.Name, kind, i+1, inv.Name(), owner.Name())
	}
}

// checkReceiver refuses a receiver the builtin refuses, by the catalogue's
// [expr.ReceiverKind]. A receiver of unknown kind, or a scalar of unknown
// subkind, is admitted: the checker refuses only what it knows. A value that is
// always nil is admitted where the builtin reads nil as a value or as no
// element, and refused where it errors on nil.
func (c *completer) checkReceiver(spec expr.BuiltinSpec, recv staticType, nargs int, owner Type, inv Invariant) {
	refuse := func(what string) {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s takes %s, and its receiver is not one in invariant %q on type %q", spec.Name, what, inv.Name(), owner.Name())
	}
	isNil := recv.kind == kindNil
	open := recv.kind == kindUnknown || isNil
	isList := recv.kind == kindList
	scalarOf := func(t staticType, is func(scalarKind) bool) bool {
		if t.kind == kindUnknown || t.kind == kindNil || (t.kind == kindScalar && t.scalar == scalarAny) {
			return true
		}
		return t.kind == kindScalar && is(t.scalar)
	}
	isString := func(k scalarKind) bool { return k == scalarString }
	elem := recv.element()
	switch spec.Receiver {
	case expr.RecvAny:
	case expr.RecvList:
		if !open && !isList {
			refuse("a list")
		}
	case expr.RecvSeededList:
		if (!open && !isList) || (isNil && nargs == 0) {
			refuse("a list")
		}
	case expr.RecvOrderedList:
		switch {
		case open:
		case !isList:
			refuse("a list")
		case !orderable(elem):
			refuse("a list of values the total order ranks")
		}
	case expr.RecvStringList:
		switch {
		case open:
		case !isList:
			refuse("a list")
		case !scalarOf(elem, isString):
			refuse("a list of strings")
		}
	case expr.RecvNumericList:
		switch {
		case open:
		case !isList:
			refuse("a list")
		case !scalarOf(elem, scalarKind.isNumber):
			refuse("a list of numbers")
		}
	case expr.RecvOrdered:
		if !orderable(recv) {
			refuse("a value the total order ranks")
		}
	case expr.RecvString:
		if isNil || !scalarOf(recv, isString) {
			refuse("a string")
		}
	case expr.RecvNumeric:
		if isNil || !scalarOf(recv, scalarKind.isNumber) {
			refuse("a number")
		}
	case expr.RecvSized:
		if !open && !isList && recv.kind != kindInstance && !scalarOf(recv, isString) {
			refuse("a string, a list or a map")
		}
	case expr.RecvListOrArg:
		switch {
		case nargs == 0 && isNil:
			refuse("a list")
		case open:
		case nargs == 0 && !isList:
			refuse("a list")
		case nargs == 0 && !orderable(elem):
			refuse("a list of values the total order ranks")
		case nargs > 0 && recv.kind == kindInstance:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%s ranks its receiver against its argument, and an instance cannot be ordered in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
		case nargs > 0 && isList:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%s takes a scalar when it has an argument, and its receiver is a list in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
		}
	}
}

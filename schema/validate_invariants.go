package schema

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema/expr"
)

// staticKind is what the static checker knows an expression evaluates to.
// The lattice mirrors what the evaluator produces: a composition yields its
// child instances, an association yields the target key, a property yields a
// scalar or a list, and a pipeline stage maps one kind to another.
type staticKind uint8

const (
	kindUnknown  staticKind = iota // no claim; member access is not checked
	kindInstance                   // an instance of typ: its members are typ's properties and relations
	kindKey                        // an association's target key: no members
	kindList                       // a list of elem
	kindScalar                     // a string, number, boolean, nil or pattern: no members
)

// scalarKind narrows a scalar as far as indexing needs: a string yields a
// string when indexed, a number or boolean cannot be indexed at all.
type scalarKind uint8

const (
	scalarAny    scalarKind = iota // a scalar of unknown kind
	scalarString                   // a string, or a value the wire renders as one
	scalarOther                    // a number or a boolean
)

type staticType struct {
	kind   staticKind
	scalar scalarKind  // kindScalar
	typ    *Type       // kindInstance; nil when the target did not resolve
	elem   *staticType // kindList
}

var (
	unknownType     = staticType{kind: kindUnknown}
	scalarType      = staticType{kind: kindScalar}
	stringType      = staticType{kind: kindScalar, scalar: scalarString}
	otherScalarType = staticType{kind: kindScalar, scalar: scalarOther}
)

func listOf(elem staticType) staticType {
	e := elem
	return staticType{kind: kindList, elem: &e}
}

func instanceOf(t *Type) staticType {
	if t == nil {
		return unknownType
	}
	return staticType{kind: kindInstance, typ: t}
}

// element is the type of one element of t: a list's element, else unknown.
func (t staticType) element() staticType {
	if t.kind == kindList && t.elem != nil {
		return *t.elem
	}
	return unknownType
}

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

// membersOf returns t's member index, built once per type.
func (c *completer) membersOf(t *Type) staticMembers {
	if m, ok := c.staticMembers[t]; ok {
		return m
	}
	if c.staticMembers == nil {
		c.staticMembers = make(map[*Type]staticMembers)
	}
	m := make(staticMembers, len(t.allProperties)+len(t.allAssociations)+len(t.allCompositions))
	for _, p := range t.allProperties {
		m[strings.ToLower(p.Name())] = propertyType(p.Constraint())
	}
	for _, r := range t.allAssociations {
		key := staticType{kind: kindKey}
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
// and Vector, unknown for an alias that never resolved, a scalar otherwise.
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
		return listOf(otherScalarType)
	case KindAlias:
		if a, ok := con.(AliasConstraint); ok {
			return propertyType(a.Resolved())
		}
		return unknownType
	case KindString, KindEnum, KindPattern, KindTimestamp, KindDate, KindUUID:
		// Temporal and UUID values are canonical text at evaluation time.
		return stringType
	case KindInteger, KindFloat, KindBoolean:
		return otherScalarType
	}
	return unknownType
}

// invariantErrorf reports one defect once per invariant: two occurrences of one
// bad name in one expression are one mistake, reported at the invariant's span.
func (c *completer) invariantErrorf(inv *Invariant, code diag.Code, format string, args ...any) {
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

// binaryOps are the operators whose operands are walked. Every one but `+`
// yields a number or a boolean; `+` yields what its operands are (docs/SPEC.md:
// numbers add, strings concatenate, lists concatenate). Everything else an
// S-expression can name is a builtin or an error.
var binaryOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	">": true, ">=": true, "<": true, "<=": true,
	"in": true, "=~": true, "!~": true, "==": true, "!=": true,
	"&&": true, "||": true, "^": true,
}

// mergeScalar is the type of a value that is one of two scalars: the shared
// subkind when both agree, else a scalar of unknown kind.
func mergeScalar(a, b staticType) staticType {
	if a.kind == kindScalar && b.kind == kindScalar && a.scalar == b.scalar {
		return a
	}
	return scalarType
}

// typeBinary types a binary operator's result and checks the two operand
// rules the evaluator refuses on every input: `+` takes two numbers, two
// strings or two lists, and `in` takes a list on its right.
func (c *completer) typeBinary(op string, children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	var l, r staticType
	for i, child := range children {
		t := c.typeExpr(child, sc, owner, inv)
		switch i {
		case 0:
			l = t
		case 1:
			r = t
		}
	}
	switch op {
	case "+":
		return c.typePlus(l, r, owner, inv)
	case "in":
		if r.kind == kindScalar || r.kind == kindInstance || r.kind == kindKey {
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"in takes a list on its right in invariant %q on type %q", inv.Name(), owner.Name())
		}
	}
	return otherScalarType
}

// typePlus types `+`: two lists concatenate to a list of their merged element,
// two strings to a string, two numbers to a number. An instance or a key is
// refused, as is a list beside a scalar or a string beside a number; an
// operand of unknown kind is admitted and the result is unknown.
func (c *completer) typePlus(l, r staticType, owner *Type, inv *Invariant) staticType {
	for _, t := range []staticType{l, r} {
		if t.kind == kindInstance || t.kind == kindKey {
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"+ takes two numbers, two strings or two lists in invariant %q on type %q", inv.Name(), owner.Name())
			return unknownType
		}
	}
	if l.kind == kindUnknown || r.kind == kindUnknown {
		return unknownType
	}
	if l.kind == kindList && r.kind == kindList {
		return listOf(mergeScalar(l.element(), r.element()))
	}
	if l.kind == kindList || r.kind == kindList {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"+ takes two numbers, two strings or two lists in invariant %q on type %q", inv.Name(), owner.Name())
		return unknownType
	}
	if l.scalar == scalarAny || r.scalar == scalarAny {
		return scalarType
	}
	if l.scalar != r.scalar {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"+ takes two numbers, two strings or two lists in invariant %q on type %q", inv.Name(), owner.Name())
		return unknownType
	}
	return l
}

// validateInvariantExpressions types every own invariant of every type and
// reports what the evaluator would refuse: an unknown member, a member read
// through an association key, a scalar or a list, an undefined named
// variable, an unknown function, and a call shape its builtin rejects.
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
		// would false-positive; the unresolved reference already carries its
		// own diagnostic.
		if c.hasUnresolvedSupertype(t) {
			continue
		}
		scope := &staticScope{}
		for inv := range t.Invariants() {
			if inv.Expression() == nil {
				continue
			}
			c.invariantSeen = nil
			c.typeExpr(inv.Expression(), scope, t, inv)
		}
	}
}

// typeExpr walks e, reports its defects, and returns what it evaluates to.
func (c *completer) typeExpr(e expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	switch ex := e.(type) {
	case nil:
		return unknownType
	case *expr.Literal:
		switch ex.Val.(type) {
		case []expr.Expression, []string:
			return unknownType
		case string:
			return stringType
		case int64, float64, bool, *regexp.Regexp:
			return otherScalarType
		}
		return scalarType
	case expr.DatatypeLiteral:
		return otherScalarType
	case expr.Op:
		return unknownType
	case expr.SExpr:
		return c.typeSExpr(ex, sc, owner, inv)
	}
	return unknownType
}

func (c *completer) typeSExpr(sexpr expr.SExpr, sc *staticScope, owner *Type, inv *Invariant) staticType {
	op := sexpr.Op()
	children := sexpr.Children()

	switch op {
	case "p":
		return c.typeProperty(children, sc, owner, inv)
	case "$":
		return c.typeVariable(children, sc, owner, inv)
	case ".":
		return c.typeMember(children, sc, owner, inv)
	case "@":
		return c.typeIndexExpr(children, sc, owner, inv)
	case "[]":
		elem := scalarType
		for i, child := range children {
			t := c.typeExpr(child, sc, owner, inv)
			switch {
			case t.kind != kindScalar:
				elem = unknownType
			case elem.kind == kindUnknown:
			case i == 0:
				elem = t
			default:
				elem = mergeScalar(elem, t)
			}
		}
		return listOf(elem)
	case "?":
		if len(children) != 3 {
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"a conditional takes a condition and two branches in invariant %q on type %q", inv.Name(), owner.Name())
			for _, child := range children {
				c.typeExpr(child, sc, owner, inv)
			}
			return unknownType
		}
		c.typeExpr(children[0], sc, owner, inv)
		then := c.typeExpr(children[1], sc, owner, inv)
		otherwise := c.typeExpr(children[2], sc, owner, inv)
		if then.kind == otherwise.kind && then.typ == otherwise.typ && then.kind != kindList {
			if then.kind == kindScalar {
				return mergeScalar(then, otherwise)
			}
			return then
		}
		return unknownType
	case "-x", "!":
		for _, child := range children {
			c.typeExpr(child, sc, owner, inv)
		}
		return otherScalarType
	}

	if binaryOps[op] {
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
func (c *completer) walkCallParts(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) {
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
func (c *completer) typeProperty(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	if len(children) != 1 {
		return unknownType
	}
	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return unknownType
	}
	// A variable of exactly this name shadows a same-named member, as the
	// evaluator's scope chain resolves a bare name (docs/SPEC.md).
	if t, found := sc.lookupVar(name); found {
		return t
	}
	if t, found := c.membersOf(owner)[strings.ToLower(name)]; found {
		return t
	}
	c.invariantErrorf(inv, diag.E_UNKNOWN_PROPERTY,
		"unknown property %q in invariant %q on type %q", name, inv.Name(), owner.Name())
	return unknownType
}

// typeVariable resolves a lambda parameter, $self, a member of the owner, or
// a numeric variable, in the evaluator's order: a parameter named self shadows
// the owner. A numeric variable evaluates to nil when unbound; any other
// unbound name is a guaranteed evaluation error, so it is refused here.
func (c *completer) typeVariable(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	if len(children) != 1 {
		return unknownType
	}
	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return unknownType
	}
	if t, found := sc.lookupVar(name); found {
		return t
	}
	if name == "self" {
		return instanceOf(owner)
	}
	// A $ variable names a member by its exact spelling, as the evaluator's
	// scope resolves it; only a bare name folds.
	if t, found := c.membersOf(owner)[strings.ToLower(name)]; found && memberSpelled(owner, name) {
		return t
	}
	if isNumericVar(name) {
		return unknownType
	}
	c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
		"undefined variable $%s in invariant %q on type %q", name, inv.Name(), owner.Name())
	return unknownType
}

// memberSpelled reports whether name is the exact spelling of one of t's
// members: a property's declared name or a relation's field name.
func memberSpelled(t *Type, name string) bool {
	if _, ok := t.Property(name); ok {
		return true
	}
	_, ok := t.RelationByField(name)
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
func (c *completer) typeMember(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	if len(children) != 2 {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"member access takes a receiver and one name in invariant %q on type %q", inv.Name(), owner.Name())
		for _, child := range children {
			c.typeExpr(child, sc, owner, inv)
		}
		return unknownType
	}
	recv := c.typeExpr(children[0], sc, owner, inv)
	name, ok := expr.StringLiteral(children[1])
	if !ok {
		c.typeExpr(children[1], sc, owner, inv)
		return unknownType
	}

	switch recv.kind {
	case kindInstance:
		// A type whose supertype chain has an unresolved link has an incomplete
		// member set; the unresolved reference already carries its diagnostic.
		if recv.typ == nil || c.hasUnresolvedSupertype(recv.typ) {
			return unknownType
		}
		if t, found := c.membersOf(recv.typ)[strings.ToLower(name)]; found {
			return t
		}
		c.invariantErrorf(inv, diag.E_UNKNOWN_PROPERTY,
			"unknown property %q on type %q in invariant %q on type %q",
			name, recv.typ.Name(), inv.Name(), owner.Name())
	case kindKey:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%q is read through an association in invariant %q on type %q: an association evaluates to the target key, and the target's properties are not in this instance",
			name, inv.Name(), owner.Name())
	case kindList:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%q is read from a list in invariant %q on type %q: index or pipe the list first",
			name, inv.Name(), owner.Name())
	case kindScalar:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%q is read from a value that has no members in invariant %q on type %q",
			name, inv.Name(), owner.Name())
	case kindUnknown:
	}
	return unknownType
}

// typeIndexExpr resolves receiver[index]: a list yields its element, a string a
// string; a number, a boolean or an instance cannot be indexed, and the bracket
// takes exactly one index, as the evaluator's slice access does.
func (c *completer) typeIndexExpr(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	if len(children) != 2 {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"indexing takes exactly one index in invariant %q on type %q", inv.Name(), owner.Name())
		for _, child := range children {
			c.typeExpr(child, sc, owner, inv)
		}
		return unknownType
	}
	recv := c.typeExpr(children[0], sc, owner, inv)
	c.typeExpr(children[1], sc, owner, inv)
	switch recv.kind {
	case kindList:
		return recv.element()
	case kindScalar:
		switch recv.scalar {
		case scalarString:
			return stringType
		case scalarOther:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"a number or boolean cannot be indexed in invariant %q on type %q", inv.Name(), owner.Name())
			return unknownType
		case scalarAny:
		}
		return scalarType
	case kindInstance:
		if recv.typ != nil {
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"type %q cannot be indexed in invariant %q on type %q", recv.typ.Name(), inv.Name(), owner.Name())
		}
	case kindKey, kindUnknown:
	}
	return unknownType
}

// typeCall types a pipeline call from its builtin's spec: it checks the call
// shape the evaluator would refuse, binds the lambda parameters to what the
// spec says they hold, and maps the receiver's type to the result's.
func (c *completer) typeCall(spec expr.BuiltinSpec, children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
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
	for _, a := range args {
		c.typeExpr(a, sc, owner, inv)
	}

	c.checkReceiver(spec, recv, len(args), owner, inv)

	switch {
	case len(args) < spec.MinArgs:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s requires at least %d argument(s) in invariant %q on type %q", spec.Name, spec.MinArgs, inv.Name(), owner.Name())
	case spec.MaxArgs >= 0 && len(args) > spec.MaxArgs:
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s accepts at most %d argument(s) in invariant %q on type %q", spec.Name, spec.MaxArgs, inv.Name(), owner.Name())
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
	}

	bodyType := unknownType
	if body != nil {
		child := sc
		switch spec.Params {
		case expr.BindElement:
			child = sc.child(paramOr(params, 0, "0"), recv.element())
		case expr.BindReceiver:
			child = sc.child(paramOr(params, 0, "0"), recv)
		case expr.BindAccumulatorElement:
			child = sc.child(paramOr(params, 0, "0"), unknownType).child(paramOr(params, 1, "1"), recv.element())
		case expr.BindNone:
		}
		bodyType = c.typeExpr(body, child, owner, inv)
	}

	switch spec.Result {
	case expr.ResultScalar:
		return scalarType
	case expr.ResultReceiver:
		return recv
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
		return scalarType
	case expr.ResultUnknown:
	}
	return unknownType
}

// paramOr returns the i-th declared parameter name, or the implicit numeric
// variable the evaluator binds when the parameter is not declared.
func paramOr(params []string, i int, implicit string) string {
	if i < len(params) {
		return params[i]
	}
	return implicit
}

// checkReceiver refuses a receiver the builtin refuses on every input, by the
// catalogue's [expr.ReceiverKind]. A receiver of unknown kind, or a scalar of
// unknown subkind, is admitted: the checker refuses only what it knows.
func (c *completer) checkReceiver(spec expr.BuiltinSpec, recv staticType, nargs int, owner *Type, inv *Invariant) {
	refuse := func(what string) {
		c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
			"%s takes %s, and its receiver is not one in invariant %q on type %q", spec.Name, what, inv.Name(), owner.Name())
	}
	notList := recv.kind == kindScalar || recv.kind == kindInstance || recv.kind == kindKey
	elem := recv.element()
	switch spec.Receiver {
	case expr.RecvList:
		if notList {
			refuse("a list")
		}
	case expr.RecvScalarList:
		if notList {
			refuse("a list")
		} else if elem.kind == kindInstance {
			refuse("a list of scalars")
		}
	case expr.RecvStringList:
		if notList {
			refuse("a list")
		} else if elem.kind == kindInstance || elem.scalar == scalarOther {
			refuse("a list of strings")
		}
	case expr.RecvNumericList:
		if notList {
			refuse("a list")
		} else if elem.kind == kindInstance || elem.scalar == scalarString {
			refuse("a list of numbers")
		}
	case expr.RecvScalar:
		if recv.kind == kindList || recv.kind == kindInstance {
			refuse("a scalar")
		}
	case expr.RecvString:
		if recv.kind == kindList || recv.kind == kindInstance || recv.scalar == scalarOther {
			refuse("a string")
		}
	case expr.RecvNumeric:
		if recv.kind == kindList || recv.kind == kindInstance || recv.kind == kindKey || recv.scalar == scalarString {
			refuse("a number")
		}
	case expr.RecvSized:
		if recv.kind == kindScalar && recv.scalar == scalarOther {
			refuse("a string, a list or a map")
		}
	case expr.RecvListOrArg:
		switch {
		case nargs == 0 && notList:
			refuse("a list")
		case nargs == 0 && elem.kind == kindInstance:
			refuse("a list of scalars")
		case nargs > 0 && recv.kind == kindInstance:
			c.invariantErrorf(inv, diag.E_INVALID_INVARIANT,
				"%s ranks its receiver against its argument, and an instance cannot be ordered in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
		}
	case expr.RecvAny:
	}
}

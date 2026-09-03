package schema

import (
	"maps"
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

type staticType struct {
	kind staticKind
	typ  *Type       // kindInstance; nil when the target did not resolve
	elem *staticType // kindList
}

var (
	unknownType = staticType{kind: kindUnknown}
	scalarType  = staticType{kind: kindScalar}
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
// are lower-cased, as the evaluator's scope resolves them.
type staticScope struct {
	vars map[string]staticType
}

func (s *staticScope) child(name string, t staticType) *staticScope {
	vars := make(map[string]staticType, len(s.vars)+1)
	maps.Copy(vars, s.vars)
	vars[strings.ToLower(name)] = t
	return &staticScope{vars: vars}
}

func (s *staticScope) lookupVar(name string) (staticType, bool) {
	t, ok := s.vars[strings.ToLower(name)]
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
	case KindList, KindVector:
		return listOf(scalarType)
	case KindAlias:
		if a, ok := con.(AliasConstraint); ok {
			return propertyType(a.Resolved())
		}
		return unknownType
	case KindString, KindInteger, KindFloat, KindBoolean, KindTimestamp, KindDate, KindUUID, KindEnum, KindPattern:
		return scalarType
	}
	return unknownType
}

// binaryOps are the operators whose operands are walked and whose result is a
// scalar. Everything else an S-expression can name is a builtin or an error.
var binaryOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	">": true, ">=": true, "<": true, "<=": true,
	"in": true, "=~": true, "!~": true, "==": true, "!=": true,
	"&&": true, "||": true, "^": true,
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
		}
		return scalarType
	case expr.DatatypeLiteral:
		return scalarType
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
		for _, child := range children {
			if c.typeExpr(child, sc, owner, inv).kind != kindScalar {
				elem = unknownType
			}
		}
		return listOf(elem)
	case "?":
		if len(children) == 3 {
			c.typeExpr(children[0], sc, owner, inv)
			then := c.typeExpr(children[1], sc, owner, inv)
			otherwise := c.typeExpr(children[2], sc, owner, inv)
			if then.kind == otherwise.kind && then.typ == otherwise.typ && then.kind != kindList {
				return then
			}
			return unknownType
		}
	case "-x", "!":
		for _, child := range children {
			c.typeExpr(child, sc, owner, inv)
		}
		return scalarType
	}

	if binaryOps[op] {
		for _, child := range children {
			c.typeExpr(child, sc, owner, inv)
		}
		return scalarType
	}

	if spec, ok := expr.LookupBuiltin(op); ok {
		return c.typeCall(spec, children, sc, owner, inv)
	}

	c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
		"unknown function %q in invariant %q on type %q", op, inv.Name(), owner.Name())
	c.walkCallParts(children, sc, owner, inv)
	return unknownType
}

// walkCallParts types the operands of a call whose name is unknown, so their
// own defects are still reported once.
func (c *completer) walkCallParts(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) {
	for _, child := range children {
		if args, ok := expr.ArgsLiteral(child); ok {
			for _, a := range args {
				c.typeExpr(a, sc, owner, inv)
			}
			continue
		}
		c.typeExpr(child, sc, owner, inv)
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
	if t, found := c.membersOf(owner)[strings.ToLower(name)]; found {
		return t
	}
	if t, found := sc.lookupVar(name); found {
		return t
	}
	c.errorf(inv.Span(), diag.E_UNKNOWN_PROPERTY,
		"unknown property %q in invariant %q on type %q", name, inv.Name(), owner.Name())
	return unknownType
}

// typeVariable resolves $self, a lambda parameter, a member of the owner, or
// a numeric variable. A numeric variable evaluates to nil when unbound; any
// other unbound name is a guaranteed evaluation error, so it is refused here.
func (c *completer) typeVariable(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	if len(children) != 1 {
		return unknownType
	}
	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return unknownType
	}
	if strings.EqualFold(name, "self") {
		return instanceOf(owner)
	}
	if t, found := sc.lookupVar(name); found {
		return t
	}
	// The evaluator's scope holds the instance's members beneath its
	// variables, so $age reads the property age when no variable shadows it.
	if t, found := c.membersOf(owner)[strings.ToLower(name)]; found {
		return t
	}
	if isNumericVar(name) {
		return unknownType
	}
	c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
		"undefined variable $%s in invariant %q on type %q", name, inv.Name(), owner.Name())
	return unknownType
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
	if len(children) < 2 {
		return unknownType
	}
	recv := c.typeExpr(children[0], sc, owner, inv)
	for _, child := range children[2:] {
		c.typeExpr(child, sc, owner, inv)
	}
	name, ok := expr.StringLiteral(children[1])
	if !ok {
		c.typeExpr(children[1], sc, owner, inv)
		return unknownType
	}

	switch recv.kind {
	case kindInstance:
		if recv.typ == nil {
			return unknownType
		}
		if t, found := c.membersOf(recv.typ)[strings.ToLower(name)]; found {
			return t
		}
		c.errorf(inv.Span(), diag.E_UNKNOWN_PROPERTY,
			"unknown property %q on type %q in invariant %q on type %q",
			name, recv.typ.Name(), inv.Name(), owner.Name())
	case kindKey:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%q is read through an association in invariant %q on type %q: an association evaluates to the target key, and the target's properties are not in this instance",
			name, inv.Name(), owner.Name())
	case kindList:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%q is read from a list in invariant %q on type %q: index or pipe the list first",
			name, inv.Name(), owner.Name())
	case kindScalar:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%q is read from a value that has no members in invariant %q on type %q",
			name, inv.Name(), owner.Name())
	case kindUnknown:
	}
	return unknownType
}

// typeIndexExpr resolves receiver[index]: a list yields its element, a string a
// string, an instance cannot be indexed.
func (c *completer) typeIndexExpr(children []expr.Expression, sc *staticScope, owner *Type, inv *Invariant) staticType {
	if len(children) == 0 {
		return unknownType
	}
	recv := c.typeExpr(children[0], sc, owner, inv)
	for _, child := range children[1:] {
		c.typeExpr(child, sc, owner, inv)
	}
	switch recv.kind {
	case kindList:
		return recv.element()
	case kindScalar:
		return scalarType
	case kindInstance:
		if recv.typ != nil {
			c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
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
			if !expr.IsNilLiteral(child) {
				body = child
			}
		}
	}
	for _, a := range args {
		c.typeExpr(a, sc, owner, inv)
	}

	switch {
	case len(args) < spec.MinArgs:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%s requires at least %d argument(s) in invariant %q on type %q", spec.Name, spec.MinArgs, inv.Name(), owner.Name())
	case spec.MaxArgs >= 0 && len(args) > spec.MaxArgs:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%s accepts at most %d argument(s) in invariant %q on type %q", spec.Name, spec.MaxArgs, inv.Name(), owner.Name())
	}
	if len(params) > spec.MaxParams {
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%s accepts at most %d lambda parameter(s) in invariant %q on type %q", spec.Name, spec.MaxParams, inv.Name(), owner.Name())
	}
	switch {
	case body != nil && !spec.AcceptBody:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%s does not accept a lambda in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
	case body == nil && spec.AcceptBody:
		c.errorf(inv.Span(), diag.E_INVALID_INVARIANT,
			"%s requires a lambda in invariant %q on type %q", spec.Name, inv.Name(), owner.Name())
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
			return listOf(recv.element().element())
		}
	case expr.ResultList:
		return listOf(scalarType)
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

package complete

import (
	"maps"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// staticScope tracks valid names for static expression validation.
// names holds lowercased property and relation field names.
// vars holds lambda parameter names mapped to their optional target type
// (nil means unknown type -- skip member access validation).
type staticScope struct {
	names map[string]bool
	vars  map[string]*schema.Type
}

// buildStaticScope creates a staticScope from a completed type's merged members.
func buildStaticScope(t *schema.Type) *staticScope {
	scope := &staticScope{
		names: make(map[string]bool),
		vars:  make(map[string]*schema.Type),
	}

	// Add all property names (own + inherited)
	for _, p := range t.AllPropertiesSlice() {
		scope.names[strings.ToLower(p.Name())] = true
	}

	// Add all relation field names (own + inherited)
	for _, r := range t.AllAssociationsSlice() {
		scope.names[strings.ToLower(r.FieldName())] = true
	}
	for _, r := range t.AllCompositionsSlice() {
		scope.names[strings.ToLower(r.FieldName())] = true
	}

	return scope
}

// hasName checks if a name exists in this scope (case-insensitive).
func (s *staticScope) hasName(name string) bool {
	return s.names[strings.ToLower(name)]
}

// child returns a new scope with an additional lambda variable binding.
// targetType may be nil if the type is unknown.
func (s *staticScope) child(varName string, targetType *schema.Type) *staticScope {
	newVars := make(map[string]*schema.Type, len(s.vars)+1)
	maps.Copy(newVars, s.vars)
	newVars[varName] = targetType
	return &staticScope{
		names: s.names,
		vars:  newVars,
	}
}

// validateInvariantExpressions checks that all property and variable references
// in invariant expressions refer to names that exist on the declaring type.
//
// This runs after completeTypes (inheritance merged) and validateRelationTargets
// (relation targets resolved), so AllPropertiesSlice/AllAssociationsSlice/
// AllCompositionsSlice are fully populated.
func (c *completer) validateInvariantExpressions() bool {
	ok := true

	for _, t := range c.schema.TypesSlice() {
		scope := buildStaticScope(t)

		for _, inv := range t.AllInvariantsSlice() {
			if inv.Expression() == nil {
				continue
			}
			if !c.walkExpr(inv.Expression(), scope, t, inv) {
				ok = false
			}
		}
	}

	return ok
}

// walkExpr recursively validates property/variable references in an expression.
func (c *completer) walkExpr(e expr.Expression, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	if e == nil {
		return true
	}

	switch ex := e.(type) {
	case *expr.Literal:
		return true
	case expr.Op:
		return true // bare Op nodes are SExpr internals; should never appear standalone
	case expr.DatatypeLiteral:
		return true
	case expr.SExpr:
		return c.walkSExpr(ex, scope, ownerType, inv)
	default:
		return true
	}
}

// collectionBuiltins lists builtins that iterate over collections with a single lambda param.
var collectionBuiltins = map[string]bool{
	"all":       true,
	"any":       true,
	"allornone": true,
	"filter":    true,
	"map":       true,
	"count":     true,
}

// walkSExpr dispatches s-expression validation based on the operation.
func (c *completer) walkSExpr(sexpr expr.SExpr, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	op := sexpr.Op()
	children := sexpr.Children()

	switch op {
	case "p":
		return c.walkProperty(children, scope, ownerType, inv)
	case "$":
		return c.walkVariable(children, scope)
	case ".":
		return c.walkMemberAccess(children, scope, ownerType, inv)
	default:
		// Check for collection builtins with lambda bodies
		lowerOp := strings.ToLower(op)
		if collectionBuiltins[lowerOp] {
			return c.walkCollectionBuiltin(children, scope, ownerType, inv)
		}
		if lowerOp == "reduce" {
			return c.walkReduceBuiltin(children, scope, ownerType, inv)
		}
		if lowerOp == "then" || lowerOp == "with" {
			return c.walkThenWithBuiltin(children, scope, ownerType, inv)
		}

		// For all other operations, walk children recursively
		ok := true
		for _, child := range children {
			if !c.walkExpr(child, scope, ownerType, inv) {
				ok = false
			}
		}
		return ok
	}
}

// walkCollectionBuiltin handles All, Any, AllOrNone, Filter, Map, Count.
// AST shape: SExpr{Op(name), lhs, args?, params?, body?}
// The lambda parameter is bound to the LHS relation's target type.
func (c *completer) walkCollectionBuiltin(children []expr.Expression, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	ok := true

	if len(children) == 0 {
		return true
	}

	// Walk LHS
	if !c.walkExpr(children[0], scope, ownerType, inv) {
		ok = false
	}

	// Try to resolve LHS to a relation target type
	targetType := c.lookupRelationTargetForExpr(children[0], ownerType)

	// Find params and body in remaining children
	var params []string
	var body expr.Expression

	for _, child := range children[1:] {
		if p, isParams := expr.ParamsLiteral(child); isParams {
			params = p
			continue
		}
		if _, isArgs := expr.ArgsLiteral(child); isArgs {
			continue
		}
		if !expr.IsNilLiteral(child) {
			body = child
		}
	}

	if body != nil && len(params) > 0 {
		childScope := scope.child(strings.ToLower(params[0]), targetType)
		if !c.walkExpr(body, childScope, ownerType, inv) {
			ok = false
		}
	} else if body != nil {
		if !c.walkExpr(body, scope, ownerType, inv) {
			ok = false
		}
	}

	return ok
}

// walkReduceBuiltin handles Reduce with 2 lambda params (accumulator + element).
// The accumulator type is unknown; the element type comes from the LHS relation target.
func (c *completer) walkReduceBuiltin(children []expr.Expression, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	ok := true

	if len(children) == 0 {
		return true
	}

	if !c.walkExpr(children[0], scope, ownerType, inv) {
		ok = false
	}

	targetType := c.lookupRelationTargetForExpr(children[0], ownerType)

	var params []string
	var body expr.Expression

	for _, child := range children[1:] {
		if p, isParams := expr.ParamsLiteral(child); isParams {
			params = p
			continue
		}
		if _, isArgs := expr.ArgsLiteral(child); isArgs {
			continue
		}
		if !expr.IsNilLiteral(child) {
			body = child
		}
	}

	if body != nil && len(params) >= 2 {
		// Accumulator type is unknown (nil), element type from relation target
		childScope := scope.child(strings.ToLower(params[0]), nil)
		childScope = childScope.child(strings.ToLower(params[1]), targetType)
		if !c.walkExpr(body, childScope, ownerType, inv) {
			ok = false
		}
	} else if body != nil {
		if !c.walkExpr(body, scope, ownerType, inv) {
			ok = false
		}
	}

	return ok
}

// walkThenWithBuiltin handles Then and With.
// Lambda param type is unknown (LHS could be anything).
func (c *completer) walkThenWithBuiltin(children []expr.Expression, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	ok := true

	if len(children) == 0 {
		return true
	}

	if !c.walkExpr(children[0], scope, ownerType, inv) {
		ok = false
	}

	var params []string
	var body expr.Expression

	for _, child := range children[1:] {
		if p, isParams := expr.ParamsLiteral(child); isParams {
			params = p
			continue
		}
		if _, isArgs := expr.ArgsLiteral(child); isArgs {
			continue
		}
		if !expr.IsNilLiteral(child) {
			body = child
		}
	}

	if body != nil && len(params) > 0 {
		childScope := scope.child(strings.ToLower(params[0]), nil)
		if !c.walkExpr(body, childScope, ownerType, inv) {
			ok = false
		}
	} else if body != nil {
		if !c.walkExpr(body, scope, ownerType, inv) {
			ok = false
		}
	}

	return ok
}

// walkProperty validates a bare property reference: Op("p") with Literal{name}.
func (c *completer) walkProperty(children []expr.Expression, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	if len(children) != 1 {
		return true // malformed -- skip
	}

	// StringLiteral is safe here: VisitName/VisitRelationName always emit *expr.Literal children.
	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return true // non-string -- skip
	}

	// Check if name exists in scope (properties + relation field names)
	if scope.hasName(name) {
		return true
	}

	// Check if it's a lambda variable (bare name can also be a var reference)
	lower := strings.ToLower(name)
	if _, isVar := scope.vars[lower]; isVar {
		return true
	}

	c.errorf(inv.Span(), diag.E_UNKNOWN_PROPERTY,
		"unknown property %q in invariant %q on type %q",
		name, inv.Name(), ownerType.Name())
	return false
}

// walkVariable validates a variable reference: Op("$") with Literal{name}.
func (c *completer) walkVariable(children []expr.Expression, scope *staticScope) bool {
	if len(children) != 1 {
		return true
	}

	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return true
	}

	// $self is always valid
	if strings.EqualFold(name, "self") {
		return true
	}

	// Check lambda variables
	if _, exists := scope.vars[strings.ToLower(name)]; exists {
		return true
	}

	// Unknown variable -- but this could be from a pipeline context we don't track.
	// Be conservative: don't error on unknown variables.
	return true
}

// walkMemberAccess validates member access: Op(".") with LHS and Literal{member}.
func (c *completer) walkMemberAccess(children []expr.Expression, scope *staticScope, ownerType *schema.Type, inv *schema.Invariant) bool {
	if len(children) < 2 {
		return true
	}

	// Walk the LHS first
	ok := c.walkExpr(children[0], scope, ownerType, inv)

	// Try to resolve the member name against a known type.
	// Only treat actual Literal nodes as member names — SExpr nodes whose
	// Literal() happens to return a string (e.g., pipeline calls like
	// $i.name -> Len) must not be mistaken for property access.
	lit, isLit := children[1].(*expr.Literal)
	if !isLit {
		// Complex member expression (e.g., pipeline RHS) — walk all children
		for i := 1; i < len(children); i++ {
			if !c.walkExpr(children[i], scope, ownerType, inv) {
				ok = false
			}
		}
		return ok
	}
	memberName, isMember := lit.Val.(string)
	if !isMember {
		// Non-string literal member — walk remaining children
		for i := 2; i < len(children); i++ {
			if !c.walkExpr(children[i], scope, ownerType, inv) {
				ok = false
			}
		}
		return ok
	}

	// Check if LHS is $self -- validate member against ownerType
	if c.isVarExpr(children[0], "self") {
		if !scope.hasName(memberName) {
			c.errorf(inv.Span(), diag.E_UNKNOWN_PROPERTY,
				"unknown property %q on type %q in invariant %q",
				memberName, ownerType.Name(), inv.Name())
			ok = false
		}
	}

	// Check if LHS is a $var with known type -- validate member against that type
	if varName := c.extractVarName(children[0]); varName != "" && !strings.EqualFold(varName, "self") {
		lower := strings.ToLower(varName)
		if targetType, exists := scope.vars[lower]; exists && targetType != nil {
			targetScope := buildStaticScope(targetType)
			if !targetScope.hasName(memberName) {
				c.errorf(inv.Span(), diag.E_UNKNOWN_PROPERTY,
					"unknown property %q on type %q in invariant %q on type %q",
					memberName, targetType.Name(), inv.Name(), ownerType.Name())
				ok = false
			}
		}
	}

	// Walk any remaining children (method call args, body, etc.)
	for i := 2; i < len(children); i++ {
		if !c.walkExpr(children[i], scope, ownerType, inv) {
			ok = false
		}
	}

	return ok
}

// isVarExpr checks if an expression is a variable reference with the given name.
func (c *completer) isVarExpr(e expr.Expression, name string) bool {
	sexpr, ok := e.(expr.SExpr)
	if !ok || sexpr.Op() != "$" {
		return false
	}
	children := sexpr.Children()
	if len(children) != 1 {
		return false
	}
	// StringLiteral is safe here: VisitVariable always emits *expr.Literal children.
	varName, ok := expr.StringLiteral(children[0])
	return ok && strings.EqualFold(varName, name)
}

// extractVarName extracts the variable name from a $ expression, or returns "".
func (c *completer) extractVarName(e expr.Expression) string {
	sexpr, ok := e.(expr.SExpr)
	if !ok || sexpr.Op() != "$" {
		return ""
	}
	children := sexpr.Children()
	if len(children) != 1 {
		return ""
	}
	// StringLiteral is safe here: VisitVariable always emits *expr.Literal children.
	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return ""
	}
	return name
}

// lookupRelationTarget finds a relation on the type by field name and returns its target type.
// Returns nil if the field name is not a relation or the target cannot be resolved.
func (c *completer) lookupRelationTarget(ownerType *schema.Type, fieldName string) *schema.Type {
	lower := strings.ToLower(fieldName)

	for _, r := range ownerType.AllAssociationsSlice() {
		if strings.ToLower(r.FieldName()) == lower {
			return c.resolveTypeRef(r.Target())
		}
	}

	for _, r := range ownerType.AllCompositionsSlice() {
		if strings.ToLower(r.FieldName()) == lower {
			return c.resolveTypeRef(r.Target())
		}
	}

	return nil
}

// lookupRelationTargetForExpr extracts a property name from an expression and
// resolves it to a relation target type. Used for determining lambda parameter types.
func (c *completer) lookupRelationTargetForExpr(e expr.Expression, ownerType *schema.Type) *schema.Type {
	sexpr, ok := e.(expr.SExpr)
	if !ok {
		return nil
	}

	// Direct property reference: Op("p") with Literal{name}
	if sexpr.Op() == "p" {
		children := sexpr.Children()
		if len(children) == 1 {
			if name, ok := expr.StringLiteral(children[0]); ok {
				return c.lookupRelationTarget(ownerType, name)
			}
		}
	}

	// For non-property SExprs (e.g., pipelines), recurse into the leftmost
	// child to find the originating relation. This is intentionally broad
	// to handle chained pipelines (Filter -> All) where the relation is
	// buried multiple levels deep. False positives are harmless because
	// this only affects lambda parameter type binding, not error reporting.
	children := sexpr.Children()
	if len(children) > 0 {
		return c.lookupRelationTargetForExpr(children[0], ownerType)
	}

	return nil
}

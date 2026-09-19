package csv

import (
	"strings"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// keyPrefix opens a dotted column's suffix that names a key component.
const keyPrefix = "_target_"

// columnKind is what a header column is for one row type.
type columnKind uint8

const (
	columnString columnKind = iota // no row type: the cell is its string
	columnPlain                    // an undotted column
	columnDotted                   // a dotted column, one cell of its field's group
)

// edgeMember is what a dotted column's suffix names within its association.
type edgeMember uint8

const (
	memberNone     edgeMember = iota // no member: carried as text for the validator
	memberKey                        // a key component of the target
	memberOpenKey                    // a key component, the target's keys out of reach
	memberProperty                   // an edge property
)

// column is one header column read against one row type.
type column struct {
	name   string
	kind   columnKind
	exact  bool             // the name spells its member exactly rather than by fold
	prop   *schema.Property // columnPlain: the property the name spells or folds onto
	field  string           // columnDotted: the association as the header spells it
	suffix string           // columnDotted: the text after the dot, as written
	rel    *schema.Relation // columnDotted: nil where the field names no association
	member edgeMember       // columnDotted
	key    *schema.Property // memberKey
	eprop  *schema.Property // memberProperty
}

// claim is the columns (or field spellings) that name one member of one
// object: the one that spells it exactly, if any, and those whose
// [instance.FoldKey] matches it.
type claim struct {
	exact int // -1: none spells it exactly
	folds []int
}

// resolve applies the validator's claim rule to one object holding the keys
// present reports: an exact key claims its member, else the one folded key the
// object holds resolves, and folded keys beside a claimant or each other pass
// on. A lone folded candidate with no value resolves to its empty value.
func (c claim) resolve(present func(i int) bool) (resolved int, passed []int) {
	var filled []int
	for _, i := range c.folds {
		if present(i) {
			filled = append(filled, i)
		}
	}
	switch {
	case c.exact >= 0 && present(c.exact):
		return c.exact, filled
	case len(filled) == 1:
		return filled[0], nil
	case len(filled) > 1:
		return -1, filled
	case len(c.folds) == 1:
		return c.folds[0], nil
	}
	return -1, nil
}

// claims groups names by the member each spells or folds onto. member returns
// a name's member and whether the name spells it exactly; ok is false for a
// name that names none.
func claims[M comparable](n int, member func(i int) (m M, exact, ok bool)) (map[M]*claim, []M) {
	byMember := make(map[M]*claim)
	var order []M
	for i := range n {
		m, exact, ok := member(i)
		if !ok {
			continue
		}
		c := byMember[m]
		if c == nil {
			c = &claim{exact: -1}
			byMember[m] = c
			order = append(order, m)
		}
		if exact {
			c.exact = i
		} else {
			c.folds = append(c.folds, i)
		}
	}
	return byMember, order
}

// plan is a header read against one row type: each column's member, the
// claims over the undotted columns, and each association spelling's group.
type plan struct {
	columns   []column
	props     []claim    // over column indexes
	spellings []spelling // in header order
	relClaims []claim    // over spelling indexes
}

// spelling is one field spelling of the dotted columns and the claims its
// suffixes make within its association.
type spelling struct {
	field   string
	rel     *schema.Relation
	cols    []int   // column indexes, in header order
	members []claim // over positions in cols
}

// planColumns reads each header column against schemaType, once per header:
// the member each name spells or folds onto. Which name claims a member is
// decided per row by [claim.resolve], since only the row knows which keys it
// holds. A nil schemaType keeps every cell as its string.
func (a *Adapter) planColumns(columns []string, schemaType *schema.Type) *plan {
	p := &plan{columns: make([]column, len(columns))}
	for i, name := range columns {
		p.columns[i] = column{name: name, kind: columnString}
	}
	if schemaType == nil {
		return p
	}

	byField := make(map[string]*schema.Relation)
	for rel := range schemaType.AllAssociations() {
		byField[rel.FieldName()] = rel
	}
	spellingOf := make(map[string]int)
	for i, name := range columns {
		c := &p.columns[i]
		field, suffix, dotted := strings.Cut(name, ".")
		if !dotted {
			c.kind = columnPlain
			c.prop = propertyNamed(schemaType, name, a.config.strict)
			c.exact = c.prop != nil && c.prop.Name() == name
			continue
		}
		c.field, c.suffix = field, suffix
		rel := byField[field]
		if rel == nil && !a.config.strict {
			if lower, ok := instance.FoldKey(field); ok {
				rel = byField[lower]
			}
		}
		c.kind, c.rel = columnDotted, rel
		s, seen := spellingOf[field]
		if !seen {
			s = len(p.spellings)
			spellingOf[field] = s
			p.spellings = append(p.spellings, spelling{field: field, rel: rel})
		}
		p.spellings[s].cols = append(p.spellings[s].cols, i)
	}

	propClaims, propOrder := claims(len(columns), func(i int) (*schema.Property, bool, bool) {
		c := p.columns[i]
		if c.kind != columnPlain || c.prop == nil {
			return nil, false, false
		}
		return c.prop, c.exact, true
	})
	for _, m := range propOrder {
		p.props = append(p.props, *propClaims[m])
	}
	relClaims, relOrder := claims(len(p.spellings), func(s int) (*schema.Relation, bool, bool) {
		sp := p.spellings[s]
		if sp.rel == nil {
			return nil, false, false
		}
		return sp.rel, sp.rel.FieldName() == sp.field, true
	})
	for _, m := range relOrder {
		p.relClaims = append(p.relClaims, *relClaims[m])
	}
	for s := range p.spellings {
		if p.spellings[s].rel != nil {
			a.planSuffixes(p, &p.spellings[s])
		}
	}
	return p
}

// propertyNamed returns the property name spells, exactly or, unless strict,
// by fold.
func propertyNamed(t *schema.Type, name string, strict bool) *schema.Property {
	if prop, ok := t.Property(name); ok {
		return prop
	}
	if strict {
		return nil
	}
	lower, ok := instance.FoldKey(name)
	if !ok {
		return nil
	}
	canonical, ok := t.CanonicalPropertyName(lower)
	if !ok {
		return nil
	}
	prop, _ := t.Property(canonical)
	return prop
}

// edgeKey is the member an edge-object key names: one of the target's keys or
// one of the relation's edge properties.
type edgeKey struct {
	key  *schema.Property
	prop *schema.Property
}

// planSuffixes reads each suffix of one spelling's group against its
// association: a key component of the target, which [WithSchema] supplies, or
// an edge property.
func (a *Adapter) planSuffixes(p *plan, sp *spelling) {
	rel := sp.rel
	var target *schema.Type
	if a.config.schema != nil {
		target, _ = a.config.schema.TypeByID(rel.TargetID())
	}
	keyByFold := make(map[string]*schema.Property)
	if target != nil {
		for pk := range target.PrimaryKeys() {
			keyByFold[strings.ToLower(keyPrefix+pk.Name())] = pk
		}
	}

	for _, i := range sp.cols {
		c := &p.columns[i]
		lower, folds := instance.FoldKey(c.suffix)
		folds = folds && !a.config.strict
		isKey := strings.HasPrefix(c.suffix, keyPrefix) || folds && strings.HasPrefix(lower, keyPrefix)
		switch {
		case isKey && target == nil:
			c.member = memberOpenKey
		case isKey:
			if folds {
				c.key = keyByFold[lower]
			}
			if c.key == nil {
				continue // names no key of the target: carried as text
			}
			c.member, c.exact = memberKey, keyPrefix+c.key.Name() == c.suffix
		default:
			if c.eprop, c.exact = rel.Property(c.suffix); !c.exact && folds {
				c.eprop, _ = rel.PropertyFold(lower)
			}
			if c.eprop != nil {
				c.member = memberProperty
			}
		}
	}
	byMember, order := claims(len(sp.cols), func(j int) (edgeKey, bool, bool) {
		c := p.columns[sp.cols[j]]
		switch c.member {
		case memberKey:
			return edgeKey{key: c.key}, c.exact, true
		case memberProperty:
			return edgeKey{prop: c.eprop}, c.exact, true
		}
		return edgeKey{}, false, false
	})
	for _, m := range order {
		sp.members = append(sp.members, *byMember[m])
	}
}

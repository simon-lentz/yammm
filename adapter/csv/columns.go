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

// edgeMember is what a dotted column's suffix names within the association
// its spelling names: the member it spells exactly or, unless strict, by fold.
// Which key claims the member is decided per target by [instance.ClaimEdgeKeys].
type edgeMember uint8

const (
	memberNone     edgeMember = iota // no member: carried as text for the validator
	memberKey                        // a key component of the target
	memberOpenKey                    // a key component, the target's keys out of reach
	memberProperty                   // an edge property
)

// column is one header column read against one row type.
type column struct {
	name     string
	kind     columnKind
	exact    *schema.Property // columnPlain: the property the name spells exactly
	folds    *schema.Property // columnPlain: the property the name folds onto, unless strict
	spelling int              // columnDotted: its field spelling in [plan.spellings]
	suffix   string           // columnDotted: the text after the dot, as written
}

// plan is a header read against one row type: each column's candidate member
// and each field spelling's group. Which key claims a member is decided per
// row by [instance.ClaimKeys], since only the row knows which keys it holds.
type plan struct {
	typ       *schema.Type
	columns   []column
	spellings []spelling // in header order
	claimMemo map[string]instance.Claims
}

// spelling is one field spelling of the dotted columns: its columns, the plain
// column of the same name, and, where the spelling names an association, what
// each suffix names in it.
type spelling struct {
	field     string
	cols      []int // column indexes, in header order
	plain     int   // the plain column spelled as field, or -1
	rel       *schema.Relation
	target    *schema.Type
	members   []suffixMember // over positions in cols; nil unless rel is set
	claimMemo map[string]instance.EdgeClaims
}

// suffixMember is what one suffix names in its spelling's association.
type suffixMember struct {
	member  edgeMember
	exact   bool
	decides bool             // the column's segments decide the group's target count
	key     *schema.Property // memberKey
	eprop   *schema.Property // memberProperty
}

// planColumns reads each header column against schemaType, once per header. A
// nil schemaType keeps every cell as its string.
func (a *Adapter) planColumns(columns []string, schemaType *schema.Type) *plan {
	p := &plan{typ: schemaType, columns: make([]column, len(columns)), claimMemo: make(map[string]instance.Claims)}
	for i, name := range columns {
		p.columns[i] = column{name: name, kind: columnString}
	}
	if schemaType == nil {
		return p
	}

	plainOf := make(map[string]int)
	spellingOf := make(map[string]int)
	for i, name := range columns {
		c := &p.columns[i]
		field, suffix, dotted := strings.Cut(name, ".")
		if !dotted {
			c.kind = columnPlain
			c.exact, c.folds = propertyNamed(schemaType, name, a.config.strict)
			plainOf[name] = i
			continue
		}
		c.kind, c.suffix = columnDotted, suffix
		s, seen := spellingOf[field]
		if !seen {
			s = len(p.spellings)
			spellingOf[field] = s
			p.spellings = append(p.spellings, spelling{field: field, plain: -1, claimMemo: make(map[string]instance.EdgeClaims)})
		}
		c.spelling = s
		p.spellings[s].cols = append(p.spellings[s].cols, i)
	}
	for s := range p.spellings {
		sp := &p.spellings[s]
		if i, ok := plainOf[sp.field]; ok {
			sp.plain = i
		}
		sp.rel = a.associationNamed(schemaType, sp.field)
		if sp.rel != nil {
			a.planSuffixes(p, sp)
		}
	}
	return p
}

// propertyNamed returns the property name spells exactly, or else, unless
// strict, the one it folds onto.
func propertyNamed(t *schema.Type, name string, strict bool) (exact, folds *schema.Property) {
	if prop, ok := t.Property(name); ok {
		return prop, nil
	}
	if strict {
		return nil, nil
	}
	lower, ok := instance.FoldKey(name)
	if !ok {
		return nil, nil
	}
	canonical, ok := t.CanonicalPropertyName(lower)
	if !ok {
		return nil, nil
	}
	prop, _ := t.Property(canonical)
	return nil, prop
}

// associationNamed returns the association field spells exactly or, unless
// strict, by fold. A composition is not one: the parser assembles no composed
// child, so its group is carried as text for the validator to judge.
func (a *Adapter) associationNamed(t *schema.Type, field string) *schema.Relation {
	rel, ok := t.RelationByField(field)
	if !ok && !a.config.strict {
		if lower, folds := instance.FoldKey(field); folds {
			rel, ok = t.RelationByField(lower)
		}
	}
	if !ok || rel.IsComposition() {
		return nil
	}
	return rel
}

// planSuffixes reads each suffix of one spelling's group against its
// association: a key component of the target, which [WithSchema] supplies, or
// an edge property.
func (a *Adapter) planSuffixes(p *plan, sp *spelling) {
	if a.config.schema != nil {
		sp.target, _ = a.config.schema.TypeByID(sp.rel.TargetID())
	}
	keyByFold := make(map[string]*schema.Property)
	if sp.target != nil {
		for pk := range sp.target.PrimaryKeys() {
			keyByFold[strings.ToLower(keyPrefix+pk.Name())] = pk
		}
	}
	sp.members = make([]suffixMember, len(sp.cols))
	for j, i := range sp.cols {
		suffix := p.columns[i].suffix
		m := &sp.members[j]
		lower, folds := instance.FoldKey(suffix)
		folds = folds && !a.config.strict
		switch {
		case strings.HasPrefix(suffix, keyPrefix) || folds && strings.HasPrefix(lower, keyPrefix):
			if sp.target == nil {
				m.member = memberOpenKey
				continue
			}
			if key := keyByFold[strings.ToLower(suffix)]; key != nil && keyPrefix+key.Name() == suffix {
				m.member, m.key, m.exact = memberKey, key, true
			} else if key := keyByFold[lower]; folds && key != nil {
				m.member, m.key = memberKey, key
			}
		default:
			if eprop, ok := sp.rel.Property(suffix); ok {
				m.member, m.eprop, m.exact = memberProperty, eprop, true
			} else if eprop, ok := sp.rel.PropertyFold(lower); folds && ok {
				m.member, m.eprop = memberProperty, eprop
			}
		}
	}
	markDeciding(sp.members)
}

// markDeciding marks the columns whose segments decide a group's target count:
// for each member, the one spelling no other can shadow or collide with — its
// exact spelling, or its one folded spelling where none is exact. A shadowed or colliding
// spelling claims nothing where the other is written, so it counts as a column
// naming no member. A key component out of reach has no known spelling, so
// every such column decides.
func markDeciding(members []suffixMember) {
	type spellings struct{ exact, folded int }
	byMember := make(map[*schema.Property]*spellings)
	for _, m := range members {
		if p := m.property(); p != nil {
			if byMember[p] == nil {
				byMember[p] = &spellings{}
			}
			if m.exact {
				byMember[p].exact++
			} else {
				byMember[p].folded++
			}
		}
	}
	for j := range members {
		m := &members[j]
		switch p := m.property(); {
		case m.member == memberOpenKey:
			m.decides = true
		case p != nil:
			n := byMember[p]
			m.decides = m.exact || n.exact == 0 && n.folded == 1
		}
	}
}

// property is the key component or edge property the member names, or nil.
func (m suffixMember) property() *schema.Property {
	switch m.member {
	case memberKey:
		return m.key
	case memberProperty:
		return m.eprop
	}
	return nil
}

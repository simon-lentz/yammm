package instance

import (
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// Claims is the member each key of one object claims under the rule the
// validator reads an object by. A parser that resolves names before validation
// asks it, so the parser and the validator decide every claim once, alike.
type Claims struct {
	props map[string]*schema.Property
	rels  map[string]*schema.Relation
}

// ClaimKeys decides which member of t each of keys claims, as [Validator]
// decides it for an object holding exactly those keys: an exact name claims its
// member, and under the default mode the one key folding onto an unclaimed
// member claims it. strict is [WithStrictPropertyNames]'s setting. A key that
// names no member, is shadowed by an exact one or collides claims nothing.
func ClaimKeys(t *schema.Type, keys []string, strict bool) Claims {
	oc := claimObject(t, sortedKeys(keys), strict)
	c := Claims{props: make(map[string]*schema.Property, len(oc.props)), rels: make(map[string]*schema.Relation, len(oc.rels))}
	for name, key := range oc.props {
		c.props[key], _ = t.Property(name)
	}
	for rel, key := range oc.rels {
		c.rels[key] = rel
	}
	return c
}

// Property returns the property key claims, or nil.
func (c Claims) Property(key string) *schema.Property { return c.props[key] }

// Relation returns the association or composition key claims, or nil.
func (c Claims) Relation(key string) *schema.Relation { return c.rels[key] }

// EdgeClaims is the member each key of one edge-target object claims under the
// rule the validator reads an edge object by.
type EdgeClaims struct {
	keys  map[string]*schema.Property
	props map[string]*schema.Property
}

// ClaimEdgeKeys decides which member each of keys claims in one target object
// of rel, as [Validator] decides it: a foreign-key field of target, or an edge
// property of rel. With a nil target no key claims a foreign-key field, since
// its fields are not known. strict is [WithStrictPropertyNames]'s setting.
func ClaimEdgeKeys(rel *schema.Relation, target *schema.Type, keys []string, strict bool) EdgeClaims {
	ec := claimEdgeObject(rel, target, sortedKeys(keys), strict)
	c := EdgeClaims{keys: make(map[string]*schema.Property, len(ec.fk)), props: make(map[string]*schema.Property, len(ec.props))}
	if target != nil {
		for pk := range target.PrimaryKeys() {
			if key, ok := ec.fk[fkPrefix+pk.Name()]; ok {
				c.keys[key] = pk
			}
		}
	}
	for p, key := range ec.props {
		c.props[key] = p
	}
	return c
}

// Key returns the target's primary-key property whose foreign-key field key
// claims, or nil.
func (c EdgeClaims) Key(key string) *schema.Property { return c.keys[key] }

// Property returns the edge property key claims, or nil.
func (c EdgeClaims) Property(key string) *schema.Property { return c.props[key] }

func sortedKeys(keys []string) []string {
	out := slices.Clone(keys)
	slices.Sort(out)
	return slices.Compact(out)
}

// objectClaims is one node object's claim decision, over the sorted key names
// [claimObject] is given. Each list keeps the order the validator reports in:
// folded properties and property collisions by their first key, relation
// collisions by the type's associations then compositions.
type objectClaims struct {
	props          map[string]string // property name → key
	folded         []string          // property names claimed by a folded key
	propCollisions []keyCollision
	rels           map[*schema.Relation]string
	relCollisions  []keyCollision
	accounted      map[string]bool // every key a member claims or a collision names
}

// keyCollision is two or more keys folding onto one unclaimed member.
type keyCollision struct {
	prop string
	rel  *schema.Relation
	keys []string
}

func claimObject(t *schema.Type, names []string, strict bool) objectClaims {
	oc := objectClaims{
		props:     make(map[string]string, len(names)),
		rels:      make(map[*schema.Relation]string),
		accounted: make(map[string]bool, len(names)),
	}
	present := make(map[string]bool, len(names))
	var byFold map[string][]string
	if !strict {
		byFold = make(map[string][]string, len(names))
	}
	for _, name := range names {
		present[name] = true
		if byFold != nil {
			if lower, ok := FoldKey(name); ok {
				byFold[lower] = append(byFold[lower], name)
			}
		}
		if _, found := t.Property(name); found {
			oc.props[name] = name
			oc.accounted[name] = true
		}
	}

	if byFold != nil {
		folded := make(map[string][]string)
		var order []string
		for _, name := range names {
			if oc.accounted[name] {
				continue
			}
			lower, ok := FoldKey(name)
			if !ok {
				continue
			}
			schemaName, found := t.CanonicalPropertyName(lower)
			if !found || oc.props[schemaName] != "" {
				continue // unknown, or shadowed by an exact key
			}
			if _, seen := folded[schemaName]; !seen {
				order = append(order, schemaName)
			}
			folded[schemaName] = append(folded[schemaName], name)
		}
		for _, schemaName := range order {
			keys := folded[schemaName]
			for _, key := range keys {
				oc.accounted[key] = true
			}
			if len(keys) > 1 {
				oc.propCollisions = append(oc.propCollisions, keyCollision{prop: schemaName, keys: keys})
				continue
			}
			oc.props[schemaName] = keys[0]
			oc.folded = append(oc.folded, schemaName)
		}
	}

	claimRelation := func(rel *schema.Relation) {
		if present[rel.FieldName()] {
			oc.rels[rel] = rel.FieldName()
			oc.accounted[rel.FieldName()] = true
			return
		}
		candidates := byFold[rel.FieldName()]
		for _, key := range candidates {
			oc.accounted[key] = true
		}
		switch len(candidates) {
		case 0:
		case 1:
			oc.rels[rel] = candidates[0]
		default:
			oc.relCollisions = append(oc.relCollisions, keyCollision{rel: rel, keys: candidates})
		}
	}
	for rel := range t.AllAssociations() {
		claimRelation(rel)
	}
	for rel := range t.AllCompositions() {
		claimRelation(rel)
	}
	return oc
}

// edgeObjectClaims is one edge-target object's claim decision; unknown is
// sorted and collisions are in the order the validator reports them.
type edgeObjectClaims struct {
	fk         map[string]string // expected foreign-key field → key
	props      map[*schema.Property]string
	unknown    []string
	shadowed   map[string]string // unknown key → the member an exact key claimed
	collisions []edgeCollision
}

// edgeCollision is two or more keys folding onto one foreign-key field or
// edge property.
type edgeCollision struct {
	fk   string
	prop *schema.Property
	keys []string
}

func claimEdgeObject(rel *schema.Relation, target *schema.Type, names []string, strict bool) edgeObjectClaims {
	ec := edgeObjectClaims{
		fk:       make(map[string]string),
		props:    make(map[*schema.Property]string),
		shadowed: make(map[string]string),
	}
	fkByLower := make(map[string]string)
	if target != nil {
		for pk := range target.PrimaryKeys() {
			field := fkPrefix + pk.Name()
			fkByLower[strings.ToLower(field)] = field
		}
	}

	var unclaimed []string
	for _, name := range names {
		if field, isFK := fkByLower[strings.ToLower(name)]; isFK && field == name {
			ec.fk[name] = name
			continue
		}
		if p, ok := rel.Property(name); ok {
			ec.props[p] = name
			continue
		}
		unclaimed = append(unclaimed, name)
	}
	if strict {
		ec.unknown = unclaimed
		return ec
	}

	type member struct {
		fk   string
		prop *schema.Property
	}
	folded := make(map[member][]string)
	var order []member
	for _, name := range unclaimed {
		lower, ok := FoldKey(name)
		if !ok {
			ec.unknown = append(ec.unknown, name)
			continue
		}
		var m member
		if field, isFK := fkByLower[lower]; isFK {
			if _, claimed := ec.fk[field]; claimed {
				ec.unknown = append(ec.unknown, name)
				ec.shadowed[name] = field
				continue
			}
			m = member{fk: field}
		} else if p, isProp := rel.PropertyFold(lower); isProp {
			if _, claimed := ec.props[p]; claimed {
				ec.unknown = append(ec.unknown, name)
				ec.shadowed[name] = p.Name()
				continue
			}
			m = member{prop: p}
		} else {
			ec.unknown = append(ec.unknown, name)
			continue
		}
		if _, seen := folded[m]; !seen {
			order = append(order, m)
		}
		folded[m] = append(folded[m], name)
	}
	for _, m := range order {
		keys := folded[m]
		if len(keys) > 1 {
			ec.collisions = append(ec.collisions, edgeCollision{fk: m.fk, prop: m.prop, keys: keys})
			continue
		}
		if m.fk != "" {
			ec.fk[m.fk] = keys[0]
		} else {
			ec.props[m.prop] = keys[0]
		}
	}
	slices.Sort(ec.unknown)
	return ec
}

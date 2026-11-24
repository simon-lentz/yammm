package cli

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// A data command's verdict is about the document it reads, while the graph
// answers for the instances it was given, and it is given only the ones the
// validator accepted and [graph.Graph.Add] installed. Two answers differ:
//
//   - A required association whose target the document holds, but which the
//     pipeline refused, is reported by [graph.Graph.Check] as a missing target.
//     The target's own refusal already states the cause, so the command drops
//     that E_UNRESOLVED_REQUIRED; the exit code does not move.
//   - A key the document states twice, where one of the two was refused, never
//     reaches the graph's duplicate check. The command reports E_DUPLICATE_PK
//     at every later holder of the key the graph did not already report.
//
// A root the pipeline refused counts only when [instance.Validator.PrimaryKeyOf]
// reads its key. The graph API is unchanged: its answer is true of the graph.

// heldKey addresses a root instance by its type and its key's [graph.FormatKey]
// form, the form an E_UNRESOLVED_REQUIRED's target key detail carries.
type heldKey struct {
	id  schema.TypeID
	key string
}

// holder is one root instance the document lists under a key the validator
// reads, in validation order.
type holder struct {
	at       heldKey
	typeName string
	prov     instance.Provenance
	// valid is the instance the validator built, zero for a refused root.
	valid instance.ValidInstance
	// installed reports that Add put the instance in the graph; graphDuplicate
	// that Add refused it as a duplicate and said so.
	installed      bool
	graphDuplicate bool
}

// verdict is what [documentVerdict] adds to and removes from a graph's answer.
type verdict struct {
	s schema.Schema
	// refused holds the keys of roots the pipeline refused for a reason other
	// than a duplicate.
	refused    map[heldKey]bool
	duplicates diag.Result
	// renamed holds each instance whose Add duplicate the verdict raises in
	// place of the graph's: the graph named a later holder than the key's first.
	renamed map[instance.ValidInstance]bool
	// rendered maps each closure type's [schema.TagForm] under s, the name the
	// graph's issues carry, to its identity.
	rendered map[string]schema.TypeID
}

// documentVerdict reads the document's roots against g once every valid one was
// added; added holds each valid instance's Add error. fromBase reports that g
// held instances before the document's, which count as earlier holders of a key.
func documentVerdict(s schema.Schema, g *graph.Graph, fromBase bool, doc Validated, added map[instance.ValidInstance]error) verdict {
	var holders []holder
	for _, b := range doc.batches {
		for i, raw := range b.raws {
			if i < len(b.valids) && !b.valids[i].IsZero() {
				valid := b.valids[i]
				err := added[valid]
				holders = append(holders, holder{
					at:             heldKey{id: valid.Type().ID(), key: valid.PrimaryKey().String()},
					typeName:       valid.TypeName(),
					prov:           valid.Provenance(),
					valid:          valid,
					installed:      err == nil,
					graphDuplicate: errors.Is(err, diag.E_DUPLICATE_PK),
				})
				continue
			}
			id, key, ok := doc.validator.PrimaryKeyOf(b.typeName, raw)
			if !ok {
				continue
			}
			holders = append(holders, holder{at: heldKey{id: id, key: key.String()}, typeName: b.typeName, prov: raw.Provenance})
		}
	}

	v := verdict{s: s, refused: make(map[heldKey]bool), renamed: make(map[instance.ValidInstance]bool)}
	fromDocument := make(map[heldKey]bool, len(holders))
	for _, h := range holders {
		if h.installed {
			fromDocument[h.at] = true
		} else if !h.graphDuplicate {
			v.refused[h.at] = true
		}
	}
	var snap graph.Snapshot
	if fromBase {
		snap = g.Snapshot()
	}
	// first holds each key's earliest holder: a base instance, else the
	// document's first holder of the key. The graph names the holder it holds,
	// which is the first one unless the validator refused the first one.
	first := make(map[heldKey]firstHolder, len(holders))
	collector := diag.NewCollector(diag.NoLimit)
	for _, h := range holders {
		earliest, earlier := first[h.at]
		if !earlier && !fromDocument[h.at] {
			var held graph.Instance
			if held, earlier = snap.InstanceByKey(h.at.id, h.at.key); earlier {
				earliest = firstHolder{prov: held.Provenance(), inGraph: true}
				first[h.at] = earliest
			}
		}
		switch {
		case !earlier:
			first[h.at] = firstHolder{prov: h.prov, inGraph: h.installed}
		case !h.graphDuplicate:
			collector.Collect(duplicateIssue(h, earliest.prov))
		case !earliest.inGraph:
			v.renamed[h.valid] = true
			collector.Collect(duplicateIssue(h, earliest.prov))
		}
	}
	v.duplicates = collector.Result()
	return v
}

// firstHolder is the earliest holder of a key: its provenance, and whether the
// graph holds it.
type firstHolder struct {
	prov    instance.Provenance
	inGraph bool
}

// duplicateIssue is the E_DUPLICATE_PK [graph.Graph.Add] raises, for a holder
// of a key an earlier holder states where the graph reported no duplicate. It
// names the earlier holder as a related location when that holder has a span.
func duplicateIssue(h holder, earlier instance.Provenance) diag.Issue {
	b := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
		fmt.Sprintf("duplicate primary key %s for type %q", diag.CutValue(h.at.key), h.typeName)).
		WithDetail(diag.DetailKeyTypeName, h.typeName).
		WithDetail(diag.DetailKeyPrimaryKey, h.at.key).
		WithSpan(h.prov.Span())
	if span := earlier.Span(); !span.IsZero() {
		b.WithRelated(diag.RelatedInfo{Span: span, Message: "first declared here"})
	}
	return b.Build()
}

// explain returns check without each E_UNRESOLVED_REQUIRED whose missing target
// is a root the document holds and the pipeline refused. [graph.Graph.Check]
// keeps every issue; a truncated result would be returned whole, since the
// issues it dropped cannot be judged.
func (v verdict) explain(check diag.Result) diag.Result {
	if len(v.refused) == 0 || !check.HasCode(diag.E_UNRESOLVED_REQUIRED) || check.LimitReached() {
		return check
	}
	v.rendered = make(map[string]schema.TypeID)
	for sc := range v.s.Closure() {
		for t := range sc.Types() {
			v.rendered[schema.TagForm(v.s, t.ID())] = t.ID()
		}
	}
	collector := diag.NewCollector(diag.NoLimit)
	for issue := range check.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED && v.refused[v.missingTarget(issue)] {
			continue
		}
		collector.Collect(issue)
	}
	return collector.Result()
}

// missingTarget returns the target an E_UNRESOLVED_REQUIRED names as missing,
// typed by the relation it is reported under, or the zero key when it names
// none. The source type is the one whose rendering the issue carries.
func (v verdict) missingTarget(issue diag.Issue) heldKey {
	details := make(map[string]string, len(issue.Details()))
	for _, d := range issue.Details() {
		details[d.Key] = d.Value
	}
	if details[diag.DetailKeyReason] != "target_missing" {
		return heldKey{}
	}
	id, ok := v.rendered[details[diag.DetailKeyTypeName]]
	if !ok {
		return heldKey{}
	}
	source, _ := v.s.TypeByID(id)
	rel, ok := source.Relation(details[diag.DetailKeyRelationName])
	if !ok {
		return heldKey{}
	}
	return heldKey{id: rel.TargetID(), key: details[diag.DetailKeyTargetPK]}
}

package cli

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
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
	prov     *location.Provenance
	// installed reports that Add put the instance in the graph; graphDuplicate
	// that Add refused it as a duplicate and said so.
	installed      bool
	graphDuplicate bool
}

// verdict is what [documentVerdict] adds to and removes from a graph's answer.
type verdict struct {
	s *schema.Schema
	// refused holds the keys of roots the pipeline refused for a reason other
	// than a duplicate.
	refused    map[heldKey]bool
	duplicates diag.Result
}

// documentVerdict reads the document's roots against g once every valid one was
// added; added holds each valid instance's Add result. fromBase reports that g
// held instances before the document's, which count as earlier holders of a key.
func documentVerdict(s *schema.Schema, g *graph.Graph, fromBase bool, doc Validated, added map[*instance.ValidInstance]diag.Result) verdict {
	var holders []holder
	for _, b := range doc.batches {
		for i, raw := range b.raws {
			if i < len(b.valids) && b.valids[i] != nil {
				valid := b.valids[i]
				result := added[valid]
				holders = append(holders, holder{
					at:             heldKey{id: valid.TypeID(), key: valid.PrimaryKey().String()},
					typeName:       valid.TypeName(),
					prov:           valid.Provenance(),
					installed:      !result.HasErrors(),
					graphDuplicate: result.HasCode(diag.E_DUPLICATE_PK),
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

	v := verdict{s: s, refused: make(map[heldKey]bool)}
	fromDocument := make(map[heldKey]bool, len(holders))
	for _, h := range holders {
		if h.installed {
			fromDocument[h.at] = true
		} else if !h.graphDuplicate {
			v.refused[h.at] = true
		}
	}
	var snap *graph.Snapshot
	if fromBase {
		snap = g.Snapshot()
	}
	seen := make(map[heldKey]bool, len(holders))
	collector := diag.NewCollectorUnlimited()
	for _, h := range holders {
		earlier, known := seen[h.at]
		if !known && snap != nil && !fromDocument[h.at] {
			_, earlier = snap.InstanceByKey(h.at.id, h.at.key)
		}
		if earlier && !h.graphDuplicate {
			collector.Collect(duplicateIssue(h))
		}
		seen[h.at] = true
	}
	v.duplicates = collector.Result()
	return v
}

// duplicateIssue is the E_DUPLICATE_PK [graph.Graph.Add] raises, for a holder
// of a key an earlier holder states where the graph reported no duplicate.
func duplicateIssue(h holder) diag.Issue {
	b := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
		fmt.Sprintf("duplicate primary key %s for type %q", h.at.key, h.typeName)).
		WithDetail(diag.DetailKeyTypeName, h.typeName).
		WithDetail(diag.DetailKeyPrimaryKey, h.at.key)
	if h.prov != nil {
		b = b.WithSpan(h.prov.Span())
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
	collector := diag.NewCollectorUnlimited()
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
// none. The source type resolves by the name the issue carries, the graph's
// name for its instance's type.
func (v verdict) missingTarget(issue diag.Issue) heldKey {
	details := make(map[string]string, len(issue.Details()))
	for _, d := range issue.Details() {
		details[d.Key] = d.Value
	}
	if details[diag.DetailKeyReason] != "target_missing" {
		return heldKey{}
	}
	source, ok := v.s.ResolveTypeName(details[diag.DetailKeyTypeName])
	if !ok {
		return heldKey{}
	}
	rel, ok := source.Relation(details[diag.DetailKeyRelationName])
	if !ok {
		return heldKey{}
	}
	return heldKey{id: rel.TargetID(), key: details[diag.DetailKeyTargetPK]}
}

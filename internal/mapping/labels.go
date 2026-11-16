package mapping

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// The codes the label and identifier rules raise. schema/cypher exports them
// as E_NEO4J_LABEL_COLLISION and E_NEO4J_INVALID_IDENTIFIER, and neo4j's write
// path raises them too.
var (
	// LabelCollision indicates two or more types produce the same Neo4j label
	// after sanitization.
	LabelCollision = diag.NewCode("E_NEO4J_LABEL_COLLISION", diag.CategoryNeo4j)

	// InvalidIdentifier indicates a property or type name is not a valid Neo4j
	// identifier (fails regex or matches a Cypher reserved keyword).
	InvalidIdentifier = diag.NewCode("E_NEO4J_INVALID_IDENTIFIER", diag.CategoryNeo4j)
)

// DetailKeyLabel is the neo4j-specific detail key for generated label strings.
const DetailKeyLabel = "label"

// Labels composes node labels: Prefix + sanitize(schema name) + Separator +
// sanitize(type name). An empty Separator means [DefaultSeparator].
type Labels struct {
	Prefix    string
	Separator string
}

// DefaultSeparator is the separator an empty [Labels.Separator] stands for.
const DefaultSeparator = "__"

func (l Labels) separator() string {
	if l.Separator == "" {
		return DefaultSeparator
	}
	return l.Separator
}

// Label returns t's node label, composed from the name of the schema that
// declares t and t's own name. A type whose name is empty or white space has
// no label, and a type whose schema name is empty or white space is labelled
// with its sanitized name alone.
func (l Labels) Label(t schema.Type) string {
	return l.Compose(t.SchemaName(), t.Name())
}

// Compose is [Labels.Label] over a schema name and a type name, for a caller
// that judges a label setting with no type in hand.
func (l Labels) Compose(schemaName, typeName string) string {
	trimmedType := strings.TrimSpace(typeName)
	if trimmedType == "" {
		return ""
	}
	trimmedSchema := strings.TrimSpace(schemaName)
	if trimmedSchema == "" {
		return sanitize(trimmedType)
	}
	return l.Prefix + sanitize(trimmedSchema) + l.separator() + sanitize(trimmedType)
}

// LabeledTypes yields each (type, label) pair for the named, non-abstract types
// of s and of every schema in its import closure, in closure order then schema
// declaration order, without validating the label. The package doc states the
// invariant every caller relies on.
func (l Labels) LabeledTypes(s schema.Schema) iter.Seq2[schema.Type, string] {
	return func(yield func(schema.Type, string) bool) {
		for member := range s.Closure() {
			for t := range member.Types() {
				name := strings.TrimSpace(t.Name())
				if name == "" || t.IsAbstract() {
					continue
				}
				// An imported type keeps the label its declaring schema gives it,
				// which is the label its nodes are written under.
				label := l.Label(t)
				if label == "" {
					continue
				}
				if !yield(t, label) {
					return
				}
			}
		}
	}
}

// EmittableTypes yields each (type, label) pair of [Labels.LabeledTypes] whose
// label is a valid Neo4j identifier, and collects an [InvalidIdentifier] for
// each other one. A caller refuses the whole schema on that issue: the skip
// describes this walk, never a partial emission.
func (l Labels) EmittableTypes(s schema.Schema, collector *diag.Collector) iter.Seq2[schema.Type, string] {
	return func(yield func(schema.Type, string) bool) {
		for t, label := range l.LabeledTypes(s) {
			name := strings.TrimSpace(t.Name())
			if err := ValidateIdentifier(label, fmt.Sprintf("type %q label", name)); err != nil {
				collector.Collect(InvalidLabelIssue(name, label, err))
				continue
			}
			if !yield(t, label) {
				return
			}
		}
	}
}

// LabelCollisionIssue builds the one [LabelCollision] every site emits.
//
// One constructor per code, so a consumer reading a detail gets the same kind
// of value whichever site raised the issue. A collision is between identities —
// two types that render one label are distinguishable only by identity — so
// identity is what it reports.
func LabelCollisionIssue(label string, ids []schema.TypeID) diag.Issue {
	rendered := make([]string, len(ids))
	for i, id := range ids {
		rendered[i] = id.String()
	}
	slices.Sort(rendered)
	return diag.NewIssue(diag.Error, LabelCollision,
		fmt.Sprintf("types %s all render label %q", strings.Join(rendered, " and "), label)).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(DetailKeyLabel, label).
		WithDetail(diag.DetailKeyTypeName, strings.Join(rendered, ", ")).
		Build()
}

// IdentifierSubject names what an [InvalidIdentifier] is about.
//
// Each field means one thing, so a consumer reading a detail key does not have
// to know which site raised the issue. TypeName is a TYPE; Relation is the
// declaring relation of an edge property, which is where a relation-scoped
// property is attributed instead.
type IdentifierSubject struct {
	TypeName string
	Relation string
	Property string
	Label    string
}

// String renders the subject for a diagnostic MESSAGE, which names the
// declaring scope whether that is a type or a relation.
func (s IdentifierSubject) String() string {
	if s.Relation != "" {
		return s.Relation
	}
	return s.TypeName
}

// InvalidIdentifierIssue builds the one [InvalidIdentifier] every site emits,
// and returns the builder so a caller can add its own span.
//
// One constructor per code, so a key means the same thing everywhere: a
// [schema.DeclaringScope] rendering is a RELATION name for an edge property, so
// it never goes under the TYPE key.
func InvalidIdentifierIssue(msg string, subj IdentifierSubject, err error) *diag.IssueBuilder {
	b := diag.NewIssue(diag.Error, InvalidIdentifier, msg).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(diag.DetailKeyDetail, err.Error())
	if subj.TypeName != "" {
		b = b.WithDetail(diag.DetailKeyTypeName, subj.TypeName)
	}
	if subj.Relation != "" {
		b = b.WithDetail(diag.DetailKeyRelationName, subj.Relation)
	}
	if subj.Property != "" {
		b = b.WithDetail(diag.DetailKeyPropertyName, subj.Property)
	}
	if subj.Label != "" {
		b = b.WithDetail(DetailKeyLabel, subj.Label)
	}
	return b
}

// InvalidLabelIssue builds the [InvalidIdentifier] diagnostic for a type whose
// label is not a usable Neo4j identifier.
func InvalidLabelIssue(typeName, label string, err error) diag.Issue {
	return InvalidIdentifierIssue(
		fmt.Sprintf("invalid label for type %q: %s", typeName, err),
		IdentifierSubject{TypeName: typeName, Label: label},
		err,
	).Build()
}

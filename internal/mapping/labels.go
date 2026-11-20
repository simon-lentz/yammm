package mapping

import (
	"fmt"
	"iter"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// InvalidIdentifier is the code the label and identifier rules raise:
// schema/cypher exports it as E_NEO4J_INVALID_IDENTIFIER, and neo4j's write
// path raises it too. A property name, a label part or a composed label that
// [IsIdentifier] refuses, or a schema name a label cannot decode, draws it.
var InvalidIdentifier = diag.NewCode("E_NEO4J_INVALID_IDENTIFIER")

// DetailKeyLabel is the neo4j-specific detail key for generated label strings.
const DetailKeyLabel = "label"

// Labels composes node labels: Prefix + schema name + Separator + type name.
// An empty Separator means [DefaultSeparator]. [Labels.Check] states the rule a
// composed label must keep.
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
// declares t and t's own name, each as written.
func (l Labels) Label(t schema.Type) string {
	return l.Prefix + t.SchemaName() + l.separator() + t.Name()
}

// LabeledTypes yields each (type, label) pair for the non-abstract types of s
// and of every schema in its import closure, in closure order then schema
// declaration order, without judging the label. The package doc states the
// invariant every caller relies on.
func (l Labels) LabeledTypes(s schema.Schema) iter.Seq2[schema.Type, string] {
	return func(yield func(schema.Type, string) bool) {
		for member := range s.Closure() {
			for t := range member.Types() {
				if t.IsAbstract() {
					continue
				}
				// An imported type keeps the label its declaring schema gives it,
				// which is the label its nodes are written under.
				if !yield(t, l.Label(t)) {
					return
				}
			}
		}
	}
}

// Check judges every label [Labels.LabeledTypes] yields for s by the label
// rule, one [InvalidIdentifier] per break; the package doc's Label Rule
// section states the rule. A label setting that breaks it is reported once,
// and nothing else is then judged; a schema name that breaks it is reported
// once, and its types' labels are not judged.
func (l Labels) Check(s schema.Schema) diag.Result {
	collector := diag.NewCollector(0)
	sep := l.separator()
	if problem := settingProblem(l.Prefix, sep); problem != "" {
		collector.Collect(InvalidIdentifierIssue(
			fmt.Sprintf("label prefix %q and separator %q compose no node label: %s", l.Prefix, sep, problem),
			IdentifierSubject{}, problem).Build())
		return collector.Result()
	}
	for member := range s.Closure() {
		name := member.Name()
		schemaProblem := schemaNameProblem(name, sep)
		reported := false
		for t := range member.Types() {
			if t.IsAbstract() {
				continue
			}
			if schemaProblem != "" {
				if !reported {
					collector.Collect(InvalidIdentifierIssue(
						fmt.Sprintf("schema %q composes no node label: %s", name, schemaProblem),
						IdentifierSubject{Schema: name}, schemaProblem).Build())
					reported = true
				}
				continue
			}
			label := l.Label(t)
			if problem := IdentifierProblem(label); problem != "" {
				problem = "the label " + problem
				collector.Collect(InvalidIdentifierIssue(
					fmt.Sprintf("type %q of schema %q has no node label %q: %s", t.Name(), name, label, problem),
					IdentifierSubject{Schema: name, TypeName: t.Name(), Label: label}, problem).Build())
			}
		}
	}
	return collector.Result()
}

// settingProblem returns why a label prefix and separator compose no label,
// or "". sep is the separator in effect, never empty.
func settingProblem(prefix, sep string) string {
	if prefix != "" && !hasIdentifierCharacters(prefix) {
		return "the prefix " + reasonCharacters
	}
	if !hasIdentifierTail(sep) {
		return "the separator is not ASCII letters, digits and underscores"
	}
	return ""
}

// schemaNameProblem returns why a schema's name composes no label under sep,
// or "": the separator must neither occur in the name nor run into it from its
// end, so a label decodes one way.
func schemaNameProblem(name, sep string) string {
	if strings.Index(name+sep, sep) != len(name) {
		return fmt.Sprintf("the separator %q occurs in the name or runs into it from the name's end, so the label reads two ways", sep)
	}
	return ""
}

// IdentifierSubject names what an [InvalidIdentifier] is about.
//
// Each field means one thing, so a consumer reading a detail key does not have
// to know which site raised the issue. Schema is a schema's declared name;
// TypeName is a TYPE; Relation is the declaring relation of an edge property,
// where a relation-scoped property is attributed instead.
type IdentifierSubject struct {
	Schema   string
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
// with reason as its detail, and returns the builder so a caller can add its
// own span.
//
// One constructor per code, so a key means the same thing everywhere: a
// [schema.DeclaringScope] rendering is a RELATION name for an edge property, so
// it never goes under the TYPE key.
func InvalidIdentifierIssue(msg string, subj IdentifierSubject, reason string) *diag.IssueBuilder {
	b := diag.NewIssue(diag.Error, InvalidIdentifier, msg).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(diag.DetailKeyDetail, reason)
	if subj.Schema != "" {
		b = b.WithDetail(diag.DetailKeySchemaName, subj.Schema)
	}
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

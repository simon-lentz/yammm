package neo4j

import (
	"fmt"
	"slices"
	"strings"
)

// InferSchema generates a .yammm DSL scaffold from parsed Neo4j constraints
// and discovered relationships.
//
// The output is human-editable text with TODO markers for constructs that
// cannot be inferred (UUID vs String, pattern constraints, invariants, etc.).
//
// The adapter's label separator is used to split labels into schema/type
// components. The schemaFilter limits inference to labels with this schema
// name prefix (empty = infer from all labels).
func (a *Adapter) InferSchema(
	constraints []RemoteConstraint,
	relationships []RemoteRelationship,
	schemaFilter string,
) (string, error) {
	separator := a.config.labelSeparator

	// Group constraints by (schemaName, typeName).
	types := make(map[string]*inferredType) // key: typeName
	schemaName := schemaFilter

	for _, rc := range constraints {
		// EqualFold, matching [Adapter.DiffConstraints] and
		// [Adapter.DiffIndexes]: the column is canonically upper-case, but the
		// two commands read the SAME column and must agree about it. A stricter
		// test here would let `neo4j diff` keep working while `neo4j introspect`
		// silently skipped every constraint — scaffolding a schema with no
		// primary keys and no required properties, which then fails to load.
		if (rc.EntityType != "" && !strings.EqualFold(rc.EntityType, "NODE")) || len(rc.LabelsOrTypes) == 0 {
			continue
		}

		sn, tn := parseLabel(rc.LabelsOrTypes[0], separator)
		if schemaFilter != "" && sn != schemaFilter {
			continue
		}
		if schemaName == "" {
			schemaName = sn
		}

		it, ok := types[tn]
		if !ok {
			it = &inferredType{name: tn}
			types[tn] = it
		}

		for _, prop := range rc.Properties {
			it.ensureProperty(prop)
		}

		// Folded rather than matched raw: a 2026.x server spells the uniqueness
		// type NODE_PROPERTY_UNIQUENESS, and an unrecognised type here marks no
		// primary key, scaffolding a schema that fails to load on E_NO_PRIMARY_KEY.
		switch canonicalRemoteConstraintType(rc.Type) {
		case "UNIQUENESS":
			// A lone UNIQUE on the composed-key pseudo-property is the DDL
			// this adapter writes for a part type, never a declared key.
			if len(rc.Properties) == 1 && rc.Properties[0] == composedKeyProp {
				it.isPart = true
			} else {
				for _, prop := range rc.Properties {
					it.markPrimaryKey(prop)
				}
			}
		case "NODE_KEY":
			for _, prop := range rc.Properties {
				it.markPrimaryKey(prop)
			}
		case "NODE_PROPERTY_EXISTENCE":
			for _, prop := range rc.Properties {
				it.markRequired(prop)
			}
		case "NODE_PROPERTY_TYPE":
			if len(rc.Properties) == 1 {
				it.setPropertyType(rc.Properties[0], rc.PropertyType)
			}
		}
	}

	// Add relationships.
	for _, rel := range relationships {
		if len(rel.SourceLabels) == 0 || len(rel.TargetLabels) == 0 {
			continue
		}

		srcSchema, srcType := parseLabel(rel.SourceLabels[0], separator)
		tgtSchema, tgtType := parseLabel(rel.TargetLabels[0], separator)

		if schemaFilter != "" && srcSchema != schemaFilter {
			continue
		}

		it, ok := types[srcType]
		if !ok {
			continue // source type not in constraints
		}

		target := tgtType
		crossSchema := srcSchema != tgtSchema && tgtSchema != ""
		if crossSchema {
			target = tgtSchema + "." + tgtType
		}

		it.addRelation(rel.RelationType, target, crossSchema)
	}

	// Generate DSL.
	return generateDSL(schemaName, types), nil
}

// parseLabel splits a Neo4j label into schema name and type name.
// Returns ("", label) if the separator is not found.
func parseLabel(label, separator string) (schemaName, typeName string) {
	before, after, found := strings.Cut(label, separator)
	if !found {
		return "", label
	}
	return before, after
}

// reverseNeo4jType maps a Neo4j propertyType string back to a yammm type name.
// Returns the yammm type name and whether the mapping is exact.
// exact=false for STRING (could be UUID, Pattern, or Enum).
//
// Upper-cased first, for the same reason [Adapter.DiffConstraints] folds the
// column: an unrecognised spelling here is silent — the property keeps the
// String fallback its declaration started with — so a case difference would
// scaffold Integer, Float, Boolean and every List property as String, and the
// generated schema would look plausible while being wrong.
func reverseNeo4jType(propertyType string) (yammmType string, exact bool) {
	switch strings.ToUpper(propertyType) {
	case "STRING":
		return "String", false
	case "INTEGER":
		return "Integer", true
	case "FLOAT":
		return "Float", true
	case "BOOLEAN":
		return "Boolean", true
	case "DATE":
		return "Date", true
	case "ZONED DATETIME":
		return "Timestamp", true
	case "LIST<STRING NOT NULL>":
		return "List<String>", false
	case "LIST<INTEGER NOT NULL>":
		return "List<Integer>", true
	case "LIST<FLOAT NOT NULL>":
		return "List<Float>", true
	case "LIST<BOOLEAN NOT NULL>":
		return "List<Boolean>", true
	case "LIST<DATE NOT NULL>":
		return "List<Date>", true
	case "LIST<ZONED DATETIME NOT NULL>":
		return "List<Timestamp>", true
	default:
		return "String", false
	}
}

// --- inferred type model ---

type inferredType struct {
	name       string
	isPart     bool
	properties []*inferredProperty
	relations  []*inferredRelation
	propIndex  map[string]*inferredProperty
}

type inferredProperty struct {
	name       string
	yammmType  string
	primaryKey bool
	required   bool
	exactType  bool // false when type could be UUID/Pattern/Enum
}

type inferredRelation struct {
	relationType string
	target       string
	crossSchema  bool
}

func (it *inferredType) ensureProperty(name string) *inferredProperty {
	if it.propIndex == nil {
		it.propIndex = make(map[string]*inferredProperty)
	}
	if p, ok := it.propIndex[name]; ok {
		return p
	}
	p := &inferredProperty{name: name, yammmType: "String"}
	it.properties = append(it.properties, p)
	it.propIndex[name] = p
	return p
}

func (it *inferredType) markPrimaryKey(name string) {
	p := it.ensureProperty(name)
	p.primaryKey = true
}

func (it *inferredType) markRequired(name string) {
	p := it.ensureProperty(name)
	p.required = true
}

func (it *inferredType) setPropertyType(name, neo4jType string) {
	p := it.ensureProperty(name)
	yammmType, exact := reverseNeo4jType(neo4jType)
	p.yammmType = yammmType
	p.exactType = exact
}

func (it *inferredType) addRelation(relationType, target string, crossSchema bool) {
	it.relations = append(it.relations, &inferredRelation{
		relationType: relationType,
		target:       target,
		crossSchema:  crossSchema,
	})
}

// --- DSL generation ---

func generateDSL(schemaName string, types map[string]*inferredType) string {
	var b strings.Builder

	if schemaName == "" {
		schemaName = "inferred"
	}
	fmt.Fprintf(&b, "schema %q\n", schemaName)

	// Collect cross-schema imports.
	imports := make(map[string]bool)
	for _, it := range types {
		for _, rel := range it.relations {
			if rel.crossSchema {
				parts := strings.SplitN(rel.target, ".", 2)
				if len(parts) == 2 {
					imports[parts[0]] = true
				}
			}
		}
	}
	if len(imports) > 0 {
		b.WriteString("\n")
		importNames := make([]string, 0, len(imports))
		for name := range imports {
			importNames = append(importNames, name)
		}
		slices.Sort(importNames)
		for _, name := range importNames {
			fmt.Fprintf(&b, "import %q\n", name)
		}
	}

	b.WriteString("\n// Inferred from Neo4j database.\n")
	b.WriteString("// TODO: Replace String types with UUID, Pattern, or Enum where appropriate.\n")
	b.WriteString("// TODO: Review inferred relationship field names.\n")
	b.WriteString("// TODO: Add invariants if needed.\n")

	// Sort type names for deterministic output.
	typeNames := make([]string, 0, len(types))
	for name := range types {
		typeNames = append(typeNames, name)
	}
	slices.Sort(typeNames)

	for _, typeName := range typeNames {
		it := types[typeName]
		b.WriteString("\n")
		head := "type"
		if it.isPart {
			head = "part type"
		}
		fmt.Fprintf(&b, "%s %s {\n", head, typeName)
		if it.isPart {
			fmt.Fprintf(&b, "    // TODO: declare the owning composition (*-> NAME (_:many) %s) on a parent type; node DDL does not record it.\n", typeName)
		}

		// Sort properties: primary keys first, then alphabetically.
		props := slices.Clone(it.properties)
		slices.SortFunc(props, func(a, b *inferredProperty) int {
			if a.primaryKey != b.primaryKey {
				if a.primaryKey {
					return -1
				}
				return 1
			}
			return strings.Compare(a.name, b.name)
		})

		for _, p := range props {
			// The composed-key pseudo-property is write-surface bookkeeping,
			// not a declarable property (no declared name can start with _).
			if p.name == composedKeyProp {
				continue
			}
			modifiers := ""
			if p.primaryKey {
				modifiers += " primary"
			} else if p.required {
				modifiers += " required"
			}

			comment := ""
			if !p.exactType {
				comment = " // TODO: verify type (could be UUID, Pattern, or Enum)"
			}

			fmt.Fprintf(&b, "    %s %s%s%s\n", p.name, p.yammmType, modifiers, comment)
		}

		// Relations.
		for _, rel := range it.relations {
			comment := ""
			if rel.crossSchema {
				comment = " // TODO: verify cross-schema reference"
			}
			fmt.Fprintf(&b, "    --> %s %s%s\n", rel.relationType, rel.target, comment)
		}

		b.WriteString("}\n")
	}

	return b.String()
}

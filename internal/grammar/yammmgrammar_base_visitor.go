// Code generated from YammmGrammar.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // YammmGrammar
import "github.com/antlr4-go/antlr/v4"

type BaseYammmGrammarVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseYammmGrammarVisitor) VisitSchema(ctx *SchemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitSchema_name(ctx *Schema_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitImport_decl(ctx *Import_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitType(ctx *TypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitDatatype(ctx *DatatypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitType_name(ctx *Type_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitAlias_name(ctx *Alias_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitType_ref(ctx *Type_refContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitExtends_types(ctx *Extends_typesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitType_body(ctx *Type_bodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitProperty(ctx *PropertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitRel_property(ctx *Rel_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitProperty_name(ctx *Property_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitData_type_ref(ctx *Data_type_refContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitQualified_alias(ctx *Qualified_aliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitAssociation(ctx *AssociationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitComposition(ctx *CompositionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitAny_name(ctx *Any_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitMultiplicity(ctx *MultiplicityContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitRelation_body(ctx *Relation_bodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitBuilt_in(ctx *Built_inContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitIntegerT(ctx *IntegerTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitFloatT(ctx *FloatTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitBoolT(ctx *BoolTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitStringT(ctx *StringTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitEnumT(ctx *EnumTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitPatternT(ctx *PatternTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitTimestampT(ctx *TimestampTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitVectorT(ctx *VectorTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitDateT(ctx *DateTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitUuidT(ctx *UuidTContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitDatatypeKeyword(ctx *DatatypeKeywordContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitInvariant(ctx *InvariantContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitDatatypeName(ctx *DatatypeNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitPlusminus(ctx *PlusminusContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitPeriod(ctx *PeriodContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitCompare(ctx *CompareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitUminus(ctx *UminusContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitOr(ctx *OrContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitIn(ctx *InContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitMatch(ctx *MatchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitList(ctx *ListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitMuldiv(ctx *MuldivContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitFcall(ctx *FcallContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitNot(ctx *NotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitAt(ctx *AtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitRelationName(ctx *RelationNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitAnd(ctx *AndContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitVariable(ctx *VariableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitName(ctx *NameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitValue(ctx *ValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitEquality(ctx *EqualityContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitIf(ctx *IfContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitLiteralNil(ctx *LiteralNilContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitGroup(ctx *GroupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitArguments(ctx *ArgumentsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitParameters(ctx *ParametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitLiteral(ctx *LiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseYammmGrammarVisitor) VisitLc_keyword(ctx *Lc_keywordContext) interface{} {
	return v.VisitChildren(ctx)
}

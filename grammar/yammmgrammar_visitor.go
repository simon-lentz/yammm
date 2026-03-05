// Code generated from YammmGrammar.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // YammmGrammar
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by YammmGrammarParser.
type YammmGrammarVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by YammmGrammarParser#schema.
	VisitSchema(ctx *SchemaContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#schema_name.
	VisitSchema_name(ctx *Schema_nameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#import_decl.
	VisitImport_decl(ctx *Import_declContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#type.
	VisitType(ctx *TypeContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#datatype.
	VisitDatatype(ctx *DatatypeContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#type_name.
	VisitType_name(ctx *Type_nameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#alias_name.
	VisitAlias_name(ctx *Alias_nameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#type_ref.
	VisitType_ref(ctx *Type_refContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#extends_types.
	VisitExtends_types(ctx *Extends_typesContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#type_body.
	VisitType_body(ctx *Type_bodyContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#property.
	VisitProperty(ctx *PropertyContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#rel_property.
	VisitRel_property(ctx *Rel_propertyContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#property_name.
	VisitProperty_name(ctx *Property_nameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#data_type_ref.
	VisitData_type_ref(ctx *Data_type_refContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#qualified_alias.
	VisitQualified_alias(ctx *Qualified_aliasContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#association.
	VisitAssociation(ctx *AssociationContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#composition.
	VisitComposition(ctx *CompositionContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#any_name.
	VisitAny_name(ctx *Any_nameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#multiplicity.
	VisitMultiplicity(ctx *MultiplicityContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#relation_body.
	VisitRelation_body(ctx *Relation_bodyContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#built_in.
	VisitBuilt_in(ctx *Built_inContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#integerT.
	VisitIntegerT(ctx *IntegerTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#floatT.
	VisitFloatT(ctx *FloatTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#boolT.
	VisitBoolT(ctx *BoolTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#stringT.
	VisitStringT(ctx *StringTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#enumT.
	VisitEnumT(ctx *EnumTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#patternT.
	VisitPatternT(ctx *PatternTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#timestampT.
	VisitTimestampT(ctx *TimestampTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#vectorT.
	VisitVectorT(ctx *VectorTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#dateT.
	VisitDateT(ctx *DateTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#uuidT.
	VisitUuidT(ctx *UuidTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#listT.
	VisitListT(ctx *ListTContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#datatypeKeyword.
	VisitDatatypeKeyword(ctx *DatatypeKeywordContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#invariant.
	VisitInvariant(ctx *InvariantContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#datatypeName.
	VisitDatatypeName(ctx *DatatypeNameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#plusminus.
	VisitPlusminus(ctx *PlusminusContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#period.
	VisitPeriod(ctx *PeriodContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#compare.
	VisitCompare(ctx *CompareContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#uminus.
	VisitUminus(ctx *UminusContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#or.
	VisitOr(ctx *OrContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#in.
	VisitIn(ctx *InContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#match.
	VisitMatch(ctx *MatchContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#list.
	VisitList(ctx *ListContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#muldiv.
	VisitMuldiv(ctx *MuldivContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#fcall.
	VisitFcall(ctx *FcallContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#not.
	VisitNot(ctx *NotContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#at.
	VisitAt(ctx *AtContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#relationName.
	VisitRelationName(ctx *RelationNameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#and.
	VisitAnd(ctx *AndContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#variable.
	VisitVariable(ctx *VariableContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#name.
	VisitName(ctx *NameContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#value.
	VisitValue(ctx *ValueContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#equality.
	VisitEquality(ctx *EqualityContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#if.
	VisitIf(ctx *IfContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#literalNil.
	VisitLiteralNil(ctx *LiteralNilContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#group.
	VisitGroup(ctx *GroupContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#arguments.
	VisitArguments(ctx *ArgumentsContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#parameters.
	VisitParameters(ctx *ParametersContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#literal.
	VisitLiteral(ctx *LiteralContext) interface{}

	// Visit a parse tree produced by YammmGrammarParser#lc_keyword.
	VisitLc_keyword(ctx *Lc_keywordContext) interface{}
}

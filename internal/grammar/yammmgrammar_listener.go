// Code generated from YammmGrammar.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // YammmGrammar
import "github.com/antlr4-go/antlr/v4"

// YammmGrammarListener is a complete listener for a parse tree produced by YammmGrammarParser.
type YammmGrammarListener interface {
	antlr.ParseTreeListener

	// EnterSchema is called when entering the schema production.
	EnterSchema(c *SchemaContext)

	// EnterSchema_name is called when entering the schema_name production.
	EnterSchema_name(c *Schema_nameContext)

	// EnterImport_decl is called when entering the import_decl production.
	EnterImport_decl(c *Import_declContext)

	// EnterType is called when entering the type production.
	EnterType(c *TypeContext)

	// EnterDatatype is called when entering the datatype production.
	EnterDatatype(c *DatatypeContext)

	// EnterType_name is called when entering the type_name production.
	EnterType_name(c *Type_nameContext)

	// EnterAlias_name is called when entering the alias_name production.
	EnterAlias_name(c *Alias_nameContext)

	// EnterType_ref is called when entering the type_ref production.
	EnterType_ref(c *Type_refContext)

	// EnterExtends_types is called when entering the extends_types production.
	EnterExtends_types(c *Extends_typesContext)

	// EnterType_body is called when entering the type_body production.
	EnterType_body(c *Type_bodyContext)

	// EnterProperty is called when entering the property production.
	EnterProperty(c *PropertyContext)

	// EnterRel_property is called when entering the rel_property production.
	EnterRel_property(c *Rel_propertyContext)

	// EnterProperty_name is called when entering the property_name production.
	EnterProperty_name(c *Property_nameContext)

	// EnterData_type_ref is called when entering the data_type_ref production.
	EnterData_type_ref(c *Data_type_refContext)

	// EnterQualified_alias is called when entering the qualified_alias production.
	EnterQualified_alias(c *Qualified_aliasContext)

	// EnterAssociation is called when entering the association production.
	EnterAssociation(c *AssociationContext)

	// EnterComposition is called when entering the composition production.
	EnterComposition(c *CompositionContext)

	// EnterAny_name is called when entering the any_name production.
	EnterAny_name(c *Any_nameContext)

	// EnterMultiplicity is called when entering the multiplicity production.
	EnterMultiplicity(c *MultiplicityContext)

	// EnterRelation_body is called when entering the relation_body production.
	EnterRelation_body(c *Relation_bodyContext)

	// EnterBuilt_in is called when entering the built_in production.
	EnterBuilt_in(c *Built_inContext)

	// EnterIntegerT is called when entering the integerT production.
	EnterIntegerT(c *IntegerTContext)

	// EnterFloatT is called when entering the floatT production.
	EnterFloatT(c *FloatTContext)

	// EnterBoolT is called when entering the boolT production.
	EnterBoolT(c *BoolTContext)

	// EnterStringT is called when entering the stringT production.
	EnterStringT(c *StringTContext)

	// EnterEnumT is called when entering the enumT production.
	EnterEnumT(c *EnumTContext)

	// EnterPatternT is called when entering the patternT production.
	EnterPatternT(c *PatternTContext)

	// EnterTimestampT is called when entering the timestampT production.
	EnterTimestampT(c *TimestampTContext)

	// EnterVectorT is called when entering the vectorT production.
	EnterVectorT(c *VectorTContext)

	// EnterDateT is called when entering the dateT production.
	EnterDateT(c *DateTContext)

	// EnterUuidT is called when entering the uuidT production.
	EnterUuidT(c *UuidTContext)

	// EnterDatatypeKeyword is called when entering the datatypeKeyword production.
	EnterDatatypeKeyword(c *DatatypeKeywordContext)

	// EnterInvariant is called when entering the invariant production.
	EnterInvariant(c *InvariantContext)

	// EnterDatatypeName is called when entering the datatypeName production.
	EnterDatatypeName(c *DatatypeNameContext)

	// EnterPlusminus is called when entering the plusminus production.
	EnterPlusminus(c *PlusminusContext)

	// EnterPeriod is called when entering the period production.
	EnterPeriod(c *PeriodContext)

	// EnterCompare is called when entering the compare production.
	EnterCompare(c *CompareContext)

	// EnterUminus is called when entering the uminus production.
	EnterUminus(c *UminusContext)

	// EnterOr is called when entering the or production.
	EnterOr(c *OrContext)

	// EnterIn is called when entering the in production.
	EnterIn(c *InContext)

	// EnterMatch is called when entering the match production.
	EnterMatch(c *MatchContext)

	// EnterList is called when entering the list production.
	EnterList(c *ListContext)

	// EnterMuldiv is called when entering the muldiv production.
	EnterMuldiv(c *MuldivContext)

	// EnterFcall is called when entering the fcall production.
	EnterFcall(c *FcallContext)

	// EnterNot is called when entering the not production.
	EnterNot(c *NotContext)

	// EnterAt is called when entering the at production.
	EnterAt(c *AtContext)

	// EnterRelationName is called when entering the relationName production.
	EnterRelationName(c *RelationNameContext)

	// EnterAnd is called when entering the and production.
	EnterAnd(c *AndContext)

	// EnterVariable is called when entering the variable production.
	EnterVariable(c *VariableContext)

	// EnterName is called when entering the name production.
	EnterName(c *NameContext)

	// EnterValue is called when entering the value production.
	EnterValue(c *ValueContext)

	// EnterEquality is called when entering the equality production.
	EnterEquality(c *EqualityContext)

	// EnterIf is called when entering the if production.
	EnterIf(c *IfContext)

	// EnterLiteralNil is called when entering the literalNil production.
	EnterLiteralNil(c *LiteralNilContext)

	// EnterGroup is called when entering the group production.
	EnterGroup(c *GroupContext)

	// EnterArguments is called when entering the arguments production.
	EnterArguments(c *ArgumentsContext)

	// EnterParameters is called when entering the parameters production.
	EnterParameters(c *ParametersContext)

	// EnterLiteral is called when entering the literal production.
	EnterLiteral(c *LiteralContext)

	// EnterLc_keyword is called when entering the lc_keyword production.
	EnterLc_keyword(c *Lc_keywordContext)

	// ExitSchema is called when exiting the schema production.
	ExitSchema(c *SchemaContext)

	// ExitSchema_name is called when exiting the schema_name production.
	ExitSchema_name(c *Schema_nameContext)

	// ExitImport_decl is called when exiting the import_decl production.
	ExitImport_decl(c *Import_declContext)

	// ExitType is called when exiting the type production.
	ExitType(c *TypeContext)

	// ExitDatatype is called when exiting the datatype production.
	ExitDatatype(c *DatatypeContext)

	// ExitType_name is called when exiting the type_name production.
	ExitType_name(c *Type_nameContext)

	// ExitAlias_name is called when exiting the alias_name production.
	ExitAlias_name(c *Alias_nameContext)

	// ExitType_ref is called when exiting the type_ref production.
	ExitType_ref(c *Type_refContext)

	// ExitExtends_types is called when exiting the extends_types production.
	ExitExtends_types(c *Extends_typesContext)

	// ExitType_body is called when exiting the type_body production.
	ExitType_body(c *Type_bodyContext)

	// ExitProperty is called when exiting the property production.
	ExitProperty(c *PropertyContext)

	// ExitRel_property is called when exiting the rel_property production.
	ExitRel_property(c *Rel_propertyContext)

	// ExitProperty_name is called when exiting the property_name production.
	ExitProperty_name(c *Property_nameContext)

	// ExitData_type_ref is called when exiting the data_type_ref production.
	ExitData_type_ref(c *Data_type_refContext)

	// ExitQualified_alias is called when exiting the qualified_alias production.
	ExitQualified_alias(c *Qualified_aliasContext)

	// ExitAssociation is called when exiting the association production.
	ExitAssociation(c *AssociationContext)

	// ExitComposition is called when exiting the composition production.
	ExitComposition(c *CompositionContext)

	// ExitAny_name is called when exiting the any_name production.
	ExitAny_name(c *Any_nameContext)

	// ExitMultiplicity is called when exiting the multiplicity production.
	ExitMultiplicity(c *MultiplicityContext)

	// ExitRelation_body is called when exiting the relation_body production.
	ExitRelation_body(c *Relation_bodyContext)

	// ExitBuilt_in is called when exiting the built_in production.
	ExitBuilt_in(c *Built_inContext)

	// ExitIntegerT is called when exiting the integerT production.
	ExitIntegerT(c *IntegerTContext)

	// ExitFloatT is called when exiting the floatT production.
	ExitFloatT(c *FloatTContext)

	// ExitBoolT is called when exiting the boolT production.
	ExitBoolT(c *BoolTContext)

	// ExitStringT is called when exiting the stringT production.
	ExitStringT(c *StringTContext)

	// ExitEnumT is called when exiting the enumT production.
	ExitEnumT(c *EnumTContext)

	// ExitPatternT is called when exiting the patternT production.
	ExitPatternT(c *PatternTContext)

	// ExitTimestampT is called when exiting the timestampT production.
	ExitTimestampT(c *TimestampTContext)

	// ExitVectorT is called when exiting the vectorT production.
	ExitVectorT(c *VectorTContext)

	// ExitDateT is called when exiting the dateT production.
	ExitDateT(c *DateTContext)

	// ExitUuidT is called when exiting the uuidT production.
	ExitUuidT(c *UuidTContext)

	// ExitDatatypeKeyword is called when exiting the datatypeKeyword production.
	ExitDatatypeKeyword(c *DatatypeKeywordContext)

	// ExitInvariant is called when exiting the invariant production.
	ExitInvariant(c *InvariantContext)

	// ExitDatatypeName is called when exiting the datatypeName production.
	ExitDatatypeName(c *DatatypeNameContext)

	// ExitPlusminus is called when exiting the plusminus production.
	ExitPlusminus(c *PlusminusContext)

	// ExitPeriod is called when exiting the period production.
	ExitPeriod(c *PeriodContext)

	// ExitCompare is called when exiting the compare production.
	ExitCompare(c *CompareContext)

	// ExitUminus is called when exiting the uminus production.
	ExitUminus(c *UminusContext)

	// ExitOr is called when exiting the or production.
	ExitOr(c *OrContext)

	// ExitIn is called when exiting the in production.
	ExitIn(c *InContext)

	// ExitMatch is called when exiting the match production.
	ExitMatch(c *MatchContext)

	// ExitList is called when exiting the list production.
	ExitList(c *ListContext)

	// ExitMuldiv is called when exiting the muldiv production.
	ExitMuldiv(c *MuldivContext)

	// ExitFcall is called when exiting the fcall production.
	ExitFcall(c *FcallContext)

	// ExitNot is called when exiting the not production.
	ExitNot(c *NotContext)

	// ExitAt is called when exiting the at production.
	ExitAt(c *AtContext)

	// ExitRelationName is called when exiting the relationName production.
	ExitRelationName(c *RelationNameContext)

	// ExitAnd is called when exiting the and production.
	ExitAnd(c *AndContext)

	// ExitVariable is called when exiting the variable production.
	ExitVariable(c *VariableContext)

	// ExitName is called when exiting the name production.
	ExitName(c *NameContext)

	// ExitValue is called when exiting the value production.
	ExitValue(c *ValueContext)

	// ExitEquality is called when exiting the equality production.
	ExitEquality(c *EqualityContext)

	// ExitIf is called when exiting the if production.
	ExitIf(c *IfContext)

	// ExitLiteralNil is called when exiting the literalNil production.
	ExitLiteralNil(c *LiteralNilContext)

	// ExitGroup is called when exiting the group production.
	ExitGroup(c *GroupContext)

	// ExitArguments is called when exiting the arguments production.
	ExitArguments(c *ArgumentsContext)

	// ExitParameters is called when exiting the parameters production.
	ExitParameters(c *ParametersContext)

	// ExitLiteral is called when exiting the literal production.
	ExitLiteral(c *LiteralContext)

	// ExitLc_keyword is called when exiting the lc_keyword production.
	ExitLc_keyword(c *Lc_keywordContext)
}

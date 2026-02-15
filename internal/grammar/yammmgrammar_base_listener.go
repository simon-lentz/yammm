// Code generated from YammmGrammar.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // YammmGrammar
import "github.com/antlr4-go/antlr/v4"

// BaseYammmGrammarListener is a complete listener for a parse tree produced by YammmGrammarParser.
type BaseYammmGrammarListener struct{}

var _ YammmGrammarListener = &BaseYammmGrammarListener{}

// VisitTerminal is called when a terminal node is visited.
func (s *BaseYammmGrammarListener) VisitTerminal(node antlr.TerminalNode) {}

// VisitErrorNode is called when an error node is visited.
func (s *BaseYammmGrammarListener) VisitErrorNode(node antlr.ErrorNode) {}

// EnterEveryRule is called when any rule is entered.
func (s *BaseYammmGrammarListener) EnterEveryRule(ctx antlr.ParserRuleContext) {}

// ExitEveryRule is called when any rule is exited.
func (s *BaseYammmGrammarListener) ExitEveryRule(ctx antlr.ParserRuleContext) {}

// EnterSchema is called when production schema is entered.
func (s *BaseYammmGrammarListener) EnterSchema(ctx *SchemaContext) {}

// ExitSchema is called when production schema is exited.
func (s *BaseYammmGrammarListener) ExitSchema(ctx *SchemaContext) {}

// EnterSchema_name is called when production schema_name is entered.
func (s *BaseYammmGrammarListener) EnterSchema_name(ctx *Schema_nameContext) {}

// ExitSchema_name is called when production schema_name is exited.
func (s *BaseYammmGrammarListener) ExitSchema_name(ctx *Schema_nameContext) {}

// EnterImport_decl is called when production import_decl is entered.
func (s *BaseYammmGrammarListener) EnterImport_decl(ctx *Import_declContext) {}

// ExitImport_decl is called when production import_decl is exited.
func (s *BaseYammmGrammarListener) ExitImport_decl(ctx *Import_declContext) {}

// EnterType is called when production type is entered.
func (s *BaseYammmGrammarListener) EnterType(ctx *TypeContext) {}

// ExitType is called when production type is exited.
func (s *BaseYammmGrammarListener) ExitType(ctx *TypeContext) {}

// EnterDatatype is called when production datatype is entered.
func (s *BaseYammmGrammarListener) EnterDatatype(ctx *DatatypeContext) {}

// ExitDatatype is called when production datatype is exited.
func (s *BaseYammmGrammarListener) ExitDatatype(ctx *DatatypeContext) {}

// EnterType_name is called when production type_name is entered.
func (s *BaseYammmGrammarListener) EnterType_name(ctx *Type_nameContext) {}

// ExitType_name is called when production type_name is exited.
func (s *BaseYammmGrammarListener) ExitType_name(ctx *Type_nameContext) {}

// EnterAlias_name is called when production alias_name is entered.
func (s *BaseYammmGrammarListener) EnterAlias_name(ctx *Alias_nameContext) {}

// ExitAlias_name is called when production alias_name is exited.
func (s *BaseYammmGrammarListener) ExitAlias_name(ctx *Alias_nameContext) {}

// EnterType_ref is called when production type_ref is entered.
func (s *BaseYammmGrammarListener) EnterType_ref(ctx *Type_refContext) {}

// ExitType_ref is called when production type_ref is exited.
func (s *BaseYammmGrammarListener) ExitType_ref(ctx *Type_refContext) {}

// EnterExtends_types is called when production extends_types is entered.
func (s *BaseYammmGrammarListener) EnterExtends_types(ctx *Extends_typesContext) {}

// ExitExtends_types is called when production extends_types is exited.
func (s *BaseYammmGrammarListener) ExitExtends_types(ctx *Extends_typesContext) {}

// EnterType_body is called when production type_body is entered.
func (s *BaseYammmGrammarListener) EnterType_body(ctx *Type_bodyContext) {}

// ExitType_body is called when production type_body is exited.
func (s *BaseYammmGrammarListener) ExitType_body(ctx *Type_bodyContext) {}

// EnterProperty is called when production property is entered.
func (s *BaseYammmGrammarListener) EnterProperty(ctx *PropertyContext) {}

// ExitProperty is called when production property is exited.
func (s *BaseYammmGrammarListener) ExitProperty(ctx *PropertyContext) {}

// EnterRel_property is called when production rel_property is entered.
func (s *BaseYammmGrammarListener) EnterRel_property(ctx *Rel_propertyContext) {}

// ExitRel_property is called when production rel_property is exited.
func (s *BaseYammmGrammarListener) ExitRel_property(ctx *Rel_propertyContext) {}

// EnterProperty_name is called when production property_name is entered.
func (s *BaseYammmGrammarListener) EnterProperty_name(ctx *Property_nameContext) {}

// ExitProperty_name is called when production property_name is exited.
func (s *BaseYammmGrammarListener) ExitProperty_name(ctx *Property_nameContext) {}

// EnterData_type_ref is called when production data_type_ref is entered.
func (s *BaseYammmGrammarListener) EnterData_type_ref(ctx *Data_type_refContext) {}

// ExitData_type_ref is called when production data_type_ref is exited.
func (s *BaseYammmGrammarListener) ExitData_type_ref(ctx *Data_type_refContext) {}

// EnterQualified_alias is called when production qualified_alias is entered.
func (s *BaseYammmGrammarListener) EnterQualified_alias(ctx *Qualified_aliasContext) {}

// ExitQualified_alias is called when production qualified_alias is exited.
func (s *BaseYammmGrammarListener) ExitQualified_alias(ctx *Qualified_aliasContext) {}

// EnterAssociation is called when production association is entered.
func (s *BaseYammmGrammarListener) EnterAssociation(ctx *AssociationContext) {}

// ExitAssociation is called when production association is exited.
func (s *BaseYammmGrammarListener) ExitAssociation(ctx *AssociationContext) {}

// EnterComposition is called when production composition is entered.
func (s *BaseYammmGrammarListener) EnterComposition(ctx *CompositionContext) {}

// ExitComposition is called when production composition is exited.
func (s *BaseYammmGrammarListener) ExitComposition(ctx *CompositionContext) {}

// EnterAny_name is called when production any_name is entered.
func (s *BaseYammmGrammarListener) EnterAny_name(ctx *Any_nameContext) {}

// ExitAny_name is called when production any_name is exited.
func (s *BaseYammmGrammarListener) ExitAny_name(ctx *Any_nameContext) {}

// EnterMultiplicity is called when production multiplicity is entered.
func (s *BaseYammmGrammarListener) EnterMultiplicity(ctx *MultiplicityContext) {}

// ExitMultiplicity is called when production multiplicity is exited.
func (s *BaseYammmGrammarListener) ExitMultiplicity(ctx *MultiplicityContext) {}

// EnterRelation_body is called when production relation_body is entered.
func (s *BaseYammmGrammarListener) EnterRelation_body(ctx *Relation_bodyContext) {}

// ExitRelation_body is called when production relation_body is exited.
func (s *BaseYammmGrammarListener) ExitRelation_body(ctx *Relation_bodyContext) {}

// EnterBuilt_in is called when production built_in is entered.
func (s *BaseYammmGrammarListener) EnterBuilt_in(ctx *Built_inContext) {}

// ExitBuilt_in is called when production built_in is exited.
func (s *BaseYammmGrammarListener) ExitBuilt_in(ctx *Built_inContext) {}

// EnterIntegerT is called when production integerT is entered.
func (s *BaseYammmGrammarListener) EnterIntegerT(ctx *IntegerTContext) {}

// ExitIntegerT is called when production integerT is exited.
func (s *BaseYammmGrammarListener) ExitIntegerT(ctx *IntegerTContext) {}

// EnterFloatT is called when production floatT is entered.
func (s *BaseYammmGrammarListener) EnterFloatT(ctx *FloatTContext) {}

// ExitFloatT is called when production floatT is exited.
func (s *BaseYammmGrammarListener) ExitFloatT(ctx *FloatTContext) {}

// EnterBoolT is called when production boolT is entered.
func (s *BaseYammmGrammarListener) EnterBoolT(ctx *BoolTContext) {}

// ExitBoolT is called when production boolT is exited.
func (s *BaseYammmGrammarListener) ExitBoolT(ctx *BoolTContext) {}

// EnterStringT is called when production stringT is entered.
func (s *BaseYammmGrammarListener) EnterStringT(ctx *StringTContext) {}

// ExitStringT is called when production stringT is exited.
func (s *BaseYammmGrammarListener) ExitStringT(ctx *StringTContext) {}

// EnterEnumT is called when production enumT is entered.
func (s *BaseYammmGrammarListener) EnterEnumT(ctx *EnumTContext) {}

// ExitEnumT is called when production enumT is exited.
func (s *BaseYammmGrammarListener) ExitEnumT(ctx *EnumTContext) {}

// EnterPatternT is called when production patternT is entered.
func (s *BaseYammmGrammarListener) EnterPatternT(ctx *PatternTContext) {}

// ExitPatternT is called when production patternT is exited.
func (s *BaseYammmGrammarListener) ExitPatternT(ctx *PatternTContext) {}

// EnterTimestampT is called when production timestampT is entered.
func (s *BaseYammmGrammarListener) EnterTimestampT(ctx *TimestampTContext) {}

// ExitTimestampT is called when production timestampT is exited.
func (s *BaseYammmGrammarListener) ExitTimestampT(ctx *TimestampTContext) {}

// EnterVectorT is called when production vectorT is entered.
func (s *BaseYammmGrammarListener) EnterVectorT(ctx *VectorTContext) {}

// ExitVectorT is called when production vectorT is exited.
func (s *BaseYammmGrammarListener) ExitVectorT(ctx *VectorTContext) {}

// EnterDateT is called when production dateT is entered.
func (s *BaseYammmGrammarListener) EnterDateT(ctx *DateTContext) {}

// ExitDateT is called when production dateT is exited.
func (s *BaseYammmGrammarListener) ExitDateT(ctx *DateTContext) {}

// EnterUuidT is called when production uuidT is entered.
func (s *BaseYammmGrammarListener) EnterUuidT(ctx *UuidTContext) {}

// ExitUuidT is called when production uuidT is exited.
func (s *BaseYammmGrammarListener) ExitUuidT(ctx *UuidTContext) {}

// EnterDatatypeKeyword is called when production datatypeKeyword is entered.
func (s *BaseYammmGrammarListener) EnterDatatypeKeyword(ctx *DatatypeKeywordContext) {}

// ExitDatatypeKeyword is called when production datatypeKeyword is exited.
func (s *BaseYammmGrammarListener) ExitDatatypeKeyword(ctx *DatatypeKeywordContext) {}

// EnterInvariant is called when production invariant is entered.
func (s *BaseYammmGrammarListener) EnterInvariant(ctx *InvariantContext) {}

// ExitInvariant is called when production invariant is exited.
func (s *BaseYammmGrammarListener) ExitInvariant(ctx *InvariantContext) {}

// EnterDatatypeName is called when production datatypeName is entered.
func (s *BaseYammmGrammarListener) EnterDatatypeName(ctx *DatatypeNameContext) {}

// ExitDatatypeName is called when production datatypeName is exited.
func (s *BaseYammmGrammarListener) ExitDatatypeName(ctx *DatatypeNameContext) {}

// EnterPlusminus is called when production plusminus is entered.
func (s *BaseYammmGrammarListener) EnterPlusminus(ctx *PlusminusContext) {}

// ExitPlusminus is called when production plusminus is exited.
func (s *BaseYammmGrammarListener) ExitPlusminus(ctx *PlusminusContext) {}

// EnterPeriod is called when production period is entered.
func (s *BaseYammmGrammarListener) EnterPeriod(ctx *PeriodContext) {}

// ExitPeriod is called when production period is exited.
func (s *BaseYammmGrammarListener) ExitPeriod(ctx *PeriodContext) {}

// EnterCompare is called when production compare is entered.
func (s *BaseYammmGrammarListener) EnterCompare(ctx *CompareContext) {}

// ExitCompare is called when production compare is exited.
func (s *BaseYammmGrammarListener) ExitCompare(ctx *CompareContext) {}

// EnterUminus is called when production uminus is entered.
func (s *BaseYammmGrammarListener) EnterUminus(ctx *UminusContext) {}

// ExitUminus is called when production uminus is exited.
func (s *BaseYammmGrammarListener) ExitUminus(ctx *UminusContext) {}

// EnterOr is called when production or is entered.
func (s *BaseYammmGrammarListener) EnterOr(ctx *OrContext) {}

// ExitOr is called when production or is exited.
func (s *BaseYammmGrammarListener) ExitOr(ctx *OrContext) {}

// EnterIn is called when production in is entered.
func (s *BaseYammmGrammarListener) EnterIn(ctx *InContext) {}

// ExitIn is called when production in is exited.
func (s *BaseYammmGrammarListener) ExitIn(ctx *InContext) {}

// EnterMatch is called when production match is entered.
func (s *BaseYammmGrammarListener) EnterMatch(ctx *MatchContext) {}

// ExitMatch is called when production match is exited.
func (s *BaseYammmGrammarListener) ExitMatch(ctx *MatchContext) {}

// EnterList is called when production list is entered.
func (s *BaseYammmGrammarListener) EnterList(ctx *ListContext) {}

// ExitList is called when production list is exited.
func (s *BaseYammmGrammarListener) ExitList(ctx *ListContext) {}

// EnterMuldiv is called when production muldiv is entered.
func (s *BaseYammmGrammarListener) EnterMuldiv(ctx *MuldivContext) {}

// ExitMuldiv is called when production muldiv is exited.
func (s *BaseYammmGrammarListener) ExitMuldiv(ctx *MuldivContext) {}

// EnterFcall is called when production fcall is entered.
func (s *BaseYammmGrammarListener) EnterFcall(ctx *FcallContext) {}

// ExitFcall is called when production fcall is exited.
func (s *BaseYammmGrammarListener) ExitFcall(ctx *FcallContext) {}

// EnterNot is called when production not is entered.
func (s *BaseYammmGrammarListener) EnterNot(ctx *NotContext) {}

// ExitNot is called when production not is exited.
func (s *BaseYammmGrammarListener) ExitNot(ctx *NotContext) {}

// EnterAt is called when production at is entered.
func (s *BaseYammmGrammarListener) EnterAt(ctx *AtContext) {}

// ExitAt is called when production at is exited.
func (s *BaseYammmGrammarListener) ExitAt(ctx *AtContext) {}

// EnterRelationName is called when production relationName is entered.
func (s *BaseYammmGrammarListener) EnterRelationName(ctx *RelationNameContext) {}

// ExitRelationName is called when production relationName is exited.
func (s *BaseYammmGrammarListener) ExitRelationName(ctx *RelationNameContext) {}

// EnterAnd is called when production and is entered.
func (s *BaseYammmGrammarListener) EnterAnd(ctx *AndContext) {}

// ExitAnd is called when production and is exited.
func (s *BaseYammmGrammarListener) ExitAnd(ctx *AndContext) {}

// EnterVariable is called when production variable is entered.
func (s *BaseYammmGrammarListener) EnterVariable(ctx *VariableContext) {}

// ExitVariable is called when production variable is exited.
func (s *BaseYammmGrammarListener) ExitVariable(ctx *VariableContext) {}

// EnterName is called when production name is entered.
func (s *BaseYammmGrammarListener) EnterName(ctx *NameContext) {}

// ExitName is called when production name is exited.
func (s *BaseYammmGrammarListener) ExitName(ctx *NameContext) {}

// EnterValue is called when production value is entered.
func (s *BaseYammmGrammarListener) EnterValue(ctx *ValueContext) {}

// ExitValue is called when production value is exited.
func (s *BaseYammmGrammarListener) ExitValue(ctx *ValueContext) {}

// EnterEquality is called when production equality is entered.
func (s *BaseYammmGrammarListener) EnterEquality(ctx *EqualityContext) {}

// ExitEquality is called when production equality is exited.
func (s *BaseYammmGrammarListener) ExitEquality(ctx *EqualityContext) {}

// EnterIf is called when production if is entered.
func (s *BaseYammmGrammarListener) EnterIf(ctx *IfContext) {}

// ExitIf is called when production if is exited.
func (s *BaseYammmGrammarListener) ExitIf(ctx *IfContext) {}

// EnterLiteralNil is called when production literalNil is entered.
func (s *BaseYammmGrammarListener) EnterLiteralNil(ctx *LiteralNilContext) {}

// ExitLiteralNil is called when production literalNil is exited.
func (s *BaseYammmGrammarListener) ExitLiteralNil(ctx *LiteralNilContext) {}

// EnterGroup is called when production group is entered.
func (s *BaseYammmGrammarListener) EnterGroup(ctx *GroupContext) {}

// ExitGroup is called when production group is exited.
func (s *BaseYammmGrammarListener) ExitGroup(ctx *GroupContext) {}

// EnterArguments is called when production arguments is entered.
func (s *BaseYammmGrammarListener) EnterArguments(ctx *ArgumentsContext) {}

// ExitArguments is called when production arguments is exited.
func (s *BaseYammmGrammarListener) ExitArguments(ctx *ArgumentsContext) {}

// EnterParameters is called when production parameters is entered.
func (s *BaseYammmGrammarListener) EnterParameters(ctx *ParametersContext) {}

// ExitParameters is called when production parameters is exited.
func (s *BaseYammmGrammarListener) ExitParameters(ctx *ParametersContext) {}

// EnterLiteral is called when production literal is entered.
func (s *BaseYammmGrammarListener) EnterLiteral(ctx *LiteralContext) {}

// ExitLiteral is called when production literal is exited.
func (s *BaseYammmGrammarListener) ExitLiteral(ctx *LiteralContext) {}

// EnterLc_keyword is called when production lc_keyword is entered.
func (s *BaseYammmGrammarListener) EnterLc_keyword(ctx *Lc_keywordContext) {}

// ExitLc_keyword is called when production lc_keyword is exited.
func (s *BaseYammmGrammarListener) ExitLc_keyword(ctx *Lc_keywordContext) {}

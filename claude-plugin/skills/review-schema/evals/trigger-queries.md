# Trigger Evaluation Queries -- review-schema

## Should Trigger (10)

1. "Review my schema for quality issues"
2. "Check this .yammm file for common mistakes"
3. "Is this schema following best practices?"
4. "Give me feedback on my data model in schemas/orders.yammm"
5. "Audit this schema -- are my primary keys, invariants, and relationships correct?"
6. "What's wrong with this yammm schema?" (followed by schema content)
7. "Review the constraint bounds and multiplicity in my schema"
8. "Check if my part types and compositions are set up correctly"
9. "Are there any anti-patterns in my yammm schema?"
10. "Validate this schema against yammm best practices and flag improvements"

## Should NOT Trigger (10)

1. "Design a schema for a library catalog" (authoring, not review)
2. "Write a new .yammm file from these requirements" (authoring)
3. "How do I write an invariant expression?" (knowledge question)
4. "Run yammm validate on my schema" (CLI execution, not review process)
5. "Export my schema to Neo4j" (adapter usage)
6. "What types can be primary keys in yammm?" (type system question)
7. "Install the yammm CLI" (setup)
8. "Write Go code to load this schema" (API usage)
9. "Fix the compile error in my schema" (debugging, not structured review)
10. "Review this Go code for outdated patterns" (not yammm)

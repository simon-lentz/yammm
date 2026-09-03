package graph_test

import (
	"context"
	"fmt"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// exampleTypeID resolves a local type for the examples, which have no
// *testing.T to fail on.
func exampleTypeID(s *schema.Schema, name string) schema.TypeID {
	t, _ := s.Type(name)
	return t.ID()
}

func ExampleGraph_Add() {
	// Build a schema with a relationship
	s, _ := schema.NewBuilder().
		WithName("example").
		WithSourceID(location.MustNewSourceID("test://example.yammm")).
		AddType("Department").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Employee").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("DEPARTMENT", schema.LocalTypeRef("Department", location.Span{}), false, false).
		Done().
		Build()

	// Create graph and validator
	g := graph.New(s)
	validator := instance.NewValidator(s)
	ctx := context.Background()

	// Add a department
	deptRaw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "eng",
			"name": "Engineering",
		},
	}
	deptValid, _ := validator.ValidateOne(ctx, "Department", deptRaw)
	g.Add(ctx, deptValid)

	// Add an employee with reference to department
	empRaw := instance.RawInstance{
		Properties: map[string]any{
			"id":         "alice",
			"name":       "Alice Smith",
			"department": map[string]any{"_target_id": "eng"},
		},
	}
	empValid, _ := validator.ValidateOne(ctx, "Employee", empRaw)
	g.Add(ctx, empValid)

	// Check graph integrity
	result := g.Check(ctx)
	fmt.Println("Graph OK:", result.OK())

	// Get snapshot
	snap := g.Snapshot()
	fmt.Println("Departments:", len(snap.InstancesOf(exampleTypeID(s, "Department"))))
	fmt.Println("Employees:", len(snap.InstancesOf(exampleTypeID(s, "Employee"))))

	// Output:
	// Graph OK: true
	// Departments: 1
	// Employees: 1
}

func ExampleGraph_Check() {
	// Build schema
	s, _ := schema.NewBuilder().
		WithName("example").
		WithSourceID(location.MustNewSourceID("test://example.yammm")).
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		AddType("Child").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithRelation("PARENT", schema.LocalTypeRef("Parent", location.Span{}), false, false).
		Done().
		Build()

	g := graph.New(s)
	validator := instance.NewValidator(s)
	ctx := context.Background()

	// Add child without parent (will fail check)
	childRaw := instance.RawInstance{
		Properties: map[string]any{
			"id":     "c1",
			"parent": map[string]any{"_target_id": "missing"},
		},
	}
	childValid, _ := validator.ValidateOne(ctx, "Child", childRaw)
	g.Add(ctx, childValid)

	// Check will find unresolved reference
	result := g.Check(ctx)
	fmt.Println("Graph OK:", result.OK())
	fmt.Println("Has errors:", result.HasErrors())

	// Output:
	// Graph OK: false
	// Has errors: true
}

package graph_test

import (
	"context"
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// exampleTypeID resolves a local type for the examples, which have no
// *testing.T to fail on.
func exampleTypeID(s schema.Schema, name string) schema.TypeID {
	t, _ := s.Type(name)
	return t.ID()
}

func ExampleGraph_Add() {
	// Build a schema with a relationship
	s, _ := schema.LoadString(context.Background(), `schema "example"

type Department {
	id String primary
	name String required
}

type Employee {
	id String primary
	name String required
	--> DEPARTMENT (one) Department
}
`, "example.yammm")

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
	if err := g.Add(ctx, deptValid); err != nil {
		fmt.Println("add:", err)
	}

	// Add an employee with reference to department
	empRaw := instance.RawInstance{
		Properties: map[string]any{
			"id":         "alice",
			"name":       "Alice Smith",
			"department": map[string]any{"_target_id": "eng"},
		},
	}
	empValid, _ := validator.ValidateOne(ctx, "Employee", empRaw)
	if err := g.Add(ctx, empValid); err != nil {
		fmt.Println("add:", err)
	}

	// Check graph integrity
	err := g.Check(ctx)
	fmt.Println("Graph OK:", err == nil)

	// Get snapshot
	snap := g.Snapshot()
	fmt.Println("Departments:", len(slices.Collect(snap.InstancesOf(exampleTypeID(s, "Department")))))
	fmt.Println("Employees:", len(slices.Collect(snap.InstancesOf(exampleTypeID(s, "Employee")))))

	// Output:
	// Graph OK: true
	// Departments: 1
	// Employees: 1
}

func ExampleGraph_Check() {
	// Build schema
	s, _ := schema.LoadString(context.Background(), `schema "example"

type Parent {
	id String primary
}

type Child {
	id String primary
	--> PARENT (one) Parent
}
`, "example.yammm")

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
	if err := g.Add(ctx, childValid); err != nil {
		fmt.Println("add:", err)
	}

	// Check will find unresolved reference
	err := g.Check(ctx)
	fmt.Println("Graph OK:", err == nil)
	fmt.Println("Has errors:", err != nil)

	// Output:
	// Graph OK: false
	// Has errors: true
}

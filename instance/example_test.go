package instance_test

import (
	"context"
	"fmt"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

func ExampleValidator_ValidateOne() {
	// Build a simple schema
	s, result := build.NewBuilder().
		WithName("example").
		WithSourceID(location.MustNewSourceID("test://example.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithOptionalProperty("age", schema.IntegerConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		fmt.Println("Schema build failed")
		return
	}

	// Create a validator
	validator := instance.NewValidator(s)
	ctx := context.Background()

	// Validate a single instance
	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "alice",
			"name": "Alice Smith",
			"age":  int64(30),
		},
	}

	valid, failure, err := validator.ValidateOne(ctx, "Person", raw)
	if err != nil {
		fmt.Println("Internal error:", err)
		return
	}
	if failure != nil {
		fmt.Println("Validation failed")
		return
	}

	fmt.Println("Type:", valid.TypeName())
	fmt.Println("Key:", valid.PrimaryKey().String())

	// Output:
	// Type: Person
	// Key: ["alice"]
}

func ExampleValidator_Validate() {
	// Build schema
	s, _ := build.NewBuilder().
		WithName("example").
		WithSourceID(location.MustNewSourceID("test://example.yammm")).
		AddType("Product").
		WithPrimaryKey("sku", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	validator := instance.NewValidator(s)
	ctx := context.Background()

	// Validate multiple instances at once
	raws := []instance.RawInstance{
		{Properties: map[string]any{"sku": "PROD-001", "name": "Widget"}},
		{Properties: map[string]any{"sku": "PROD-002", "name": "Gadget"}},
		{Properties: map[string]any{"sku": "PROD-003", "name": "Sprocket"}},
	}

	valid, failures, err := validator.Validate(ctx, "Product", raws)
	if err != nil {
		fmt.Println("Internal error:", err)
		return
	}

	fmt.Printf("Valid: %d, Failures: %d\n", len(valid), len(failures))

	// Output:
	// Valid: 3, Failures: 0
}

package instance_test

import (
	"context"
	"fmt"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

func ExampleValidator_ValidateOne() {
	// Build a simple schema
	s, err := schema.LoadString(context.Background(), `schema "example"

type Person {
	id String primary
	name String required
	age Integer
}
`, "example.yammm")
	if err != nil {
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

	valid, validateErr := validator.ValidateOne(ctx, "Person", raw)
	if validateErr != nil {
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
	s, _ := schema.LoadString(context.Background(), `schema "example"

type Product {
	sku String primary
	name String required
}
`, "example.yammm")

	validator := instance.NewValidator(s)
	ctx := context.Background()

	// Validate multiple instances at once
	raws := []instance.RawInstance{
		{Properties: map[string]any{"sku": "PROD-001", "name": "Widget"}},
		{Properties: map[string]any{"sku": "PROD-002", "name": "Gadget"}},
		{Properties: map[string]any{"sku": "PROD-003", "name": "Sprocket"}},
	}

	valid, err := validator.Validate(ctx, "Product", raws)
	if err != nil {
		fmt.Println("Validation failed")
		return
	}

	fmt.Printf("Valid: %d\n", len(valid))

	// Output:
	// Valid: 3
}

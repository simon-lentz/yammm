package csv

import (
	"errors"

	"github.com/simon-lentz/yammm/diag"
)

// Diagnostic codes for CSV adapter errors.
var (
	// E_CSV_COERCE indicates a CSV cell value could not be coerced
	// to the expected type based on schema constraints.
	E_CSV_COERCE = diag.NewCode("E_CSV_COERCE", diag.CategoryAdapter)
)

// Sentinel errors for configuration and argument validation.
var (
	// ErrNilSnapshot is returned when a write method receives a nil graph snapshot.
	ErrNilSnapshot = errors.New("csv adapter: nil graph snapshot")

	// ErrNoTypeColumn is returned when ParseWithTypeColumn is called
	// but no type column was configured via WithTypeColumn.
	ErrNoTypeColumn = errors.New("csv adapter: ParseWithTypeColumn requires WithTypeColumn to be set")
)

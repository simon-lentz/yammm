package csv

import (
	"errors"

	"github.com/simon-lentz/yammm/diag"
)

// Diagnostic codes for CSV adapter errors.
var (
	// E_CSV_COERCE indicates a CSV fault the adapter reports: a cell that does
	// not coerce to its member's type, a header or record the reader refuses,
	// two values for one key, or the reader failing.
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

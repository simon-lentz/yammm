package csv

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/diag"
)

// Diagnostic codes for CSV adapter faults. A fault in the file's structure
// draws [diag.E_ADAPTER_PARSE], the code the JSON adapter reports a document's
// structure under, so one code answers "this input is not well formed" for
// both data parsers.
var (
	// E_CSV_COERCE indicates a cell whose text does not coerce to the type its
	// member declares. The cell keeps its text and the row is still produced,
	// so a consumer counting this code counts data to clean.
	E_CSV_COERCE = diag.NewCode("E_CSV_COERCE", diag.CategoryAdapter)

	// E_CSV_CONFIG indicates the adapter was built with a setting this parse
	// cannot use: a list separator the parser could not find again, a delimiter
	// [encoding/csv] refuses, or [Adapter.ParseWithTypeColumn] with no
	// [WithTypeColumn]. No record is read, and the remedy is in the caller's
	// code rather than in the file.
	E_CSV_CONFIG = diag.NewCode("E_CSV_CONFIG", diag.CategoryAdapter)
)

// Sentinel errors for configuration and argument validation.
var (
	// ErrNilSnapshot is returned when a write method receives a nil graph snapshot.
	ErrNilSnapshot = errors.New("csv adapter: nil graph snapshot")

	// ErrNoTypeColumn is the fault [Adapter.ParseWithTypeColumn] reports when
	// no type column was configured with [WithTypeColumn]. That method reports
	// through its [diag.Result] and returns no error, so this value is the text
	// of an [E_CSV_CONFIG] diagnostic; no error the method returns can match
	// it, because it returns none.
	ErrNoTypeColumn = errors.New("csv adapter: ParseWithTypeColumn requires WithTypeColumn to be set")

	// ErrConfig marks a write refused for the reason [E_CSV_CONFIG] reports on
	// the parse side: the adapter holds a setting it cannot use — a list
	// separator the parser could not find again, or a delimiter [encoding/csv]
	// refuses. It is the class, matched with errors.Is; the message names the
	// setting, and for a delimiter it is [encoding/csv]'s own.
	ErrConfig = errors.New("csv adapter: the adapter holds a setting it cannot use")

	// ErrUnrepresentable marks a write refused because the snapshot holds a
	// value CSV cannot write so that this adapter's own parser reads it back
	// unchanged. It is the class, matched with errors.Is; the message names the
	// instance, the column or the association. It separates a refusal of the
	// data from an I/O failure and from [ErrConfig], which no message text can.
	ErrUnrepresentable = errors.New("csv adapter: the snapshot holds a value CSV cannot represent")
)

// refusalError carries a refusal's own text and the class a caller matches with
// errors.Is. Marking leaves the text alone, because the message is what names
// the instance, the column or the association and the class is what a caller
// branches on.
type refusalError struct {
	msg   string
	class error
}

func (e *refusalError) Error() string { return e.msg }
func (e *refusalError) Unwrap() error { return e.class }

// refuse returns a refusal of class whose message is format applied to a.
func refuse(class error, format string, a ...any) error {
	return &refusalError{msg: fmt.Sprintf(format, a...), class: class}
}

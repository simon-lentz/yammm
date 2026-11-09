// Package protocol defines LSP protocol types used by yammm-lsp.
//
// These types are hand-rolled to cover only the LSP surface area that
// yammm-lsp actually uses (13 files, ~536 LOC). This avoids pulling in
// a full generated protocol package for a fraction of the spec.
//
// Spec version: LSP 3.16 (https://microsoft.github.io/language-server-protocol/specifications/lsp/3.16/specification/)
// Last audit: 2026-03-16
package protocol

import (
	"encoding/json"
	"strconv"
)

// Integer defines an integer number in the range of -2^31 to 2^31 - 1.
type Integer = int32

// UInteger defines an unsigned integer number in the range of 0 to 2^31 - 1.
type UInteger = uint32

// Method is the LSP method name.
type Method = string

// IntegerOrString can hold either an Integer or a string.
type IntegerOrString struct {
	Value any // Integer | string
}

func (s IntegerOrString) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Value) //nolint:wrapcheck // protocol marshaling
}

func (s *IntegerOrString) UnmarshalJSON(data []byte) error {
	var intVal Integer
	if err := json.Unmarshal(data, &intVal); err == nil {
		s.Value = intVal
		return nil
	}
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		s.Value = strVal
		return nil
	}
	return json.Unmarshal(data, &s.Value) //nolint:wrapcheck // protocol marshaling
}

// BoolOrString can hold either a bool or a string.
type BoolOrString struct {
	Value any // bool | string
}

func (s BoolOrString) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Value) //nolint:wrapcheck // protocol marshaling
}

func (s *BoolOrString) UnmarshalJSON(data []byte) error {
	var boolVal bool
	if err := json.Unmarshal(data, &boolVal); err == nil {
		s.Value = boolVal
		return nil
	}
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		s.Value = strVal
		return nil
	}
	return json.Unmarshal(data, &s.Value) //nolint:wrapcheck // protocol marshaling
}

func (s BoolOrString) String() string {
	switch v := s.Value.(type) {
	case bool:
		return strconv.FormatBool(v)
	case string:
		return v
	default:
		return ""
	}
}

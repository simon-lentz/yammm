package gogen

import (
	"strings"
	"testing"
)

// TestTypeCheck_RejectsForeignImport pins the hermetic importer's boundary:
// it serves time and encoding/json, and any other path is a generator bug.
func TestTypeCheck_RejectsForeignImport(t *testing.T) {
	t.Parallel()
	src := []byte("package p\n\nimport \"strconv\"\n\nvar _ = strconv.Itoa\n")
	err := typeCheck(src)
	if err == nil {
		t.Fatal("expected the stub importer to refuse strconv")
	}
	if !strings.Contains(err.Error(), "strconv") {
		t.Errorf("error does not name the refused path: %v", err)
	}
}

// TestTypeCheck_ServesTimeAndJSON pins that the stub covers exactly what an
// emitted temporal type calls: time.Parse, Time.Format, json.Marshal and
// json.Unmarshal.
func TestTypeCheck_ServesTimeAndJSON(t *testing.T) {
	t.Parallel()
	src := []byte(`package p

import (
	"encoding/json"
	"time"
)

type Date struct{ time.Time }

func (v Date) MarshalJSON() ([]byte, error) { return json.Marshal(v.Format("2006-01-02")) }

func (v *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	v.Time = t
	return nil
}
`)
	if err := typeCheck(src); err != nil {
		t.Fatalf("typeCheck: %v", err)
	}
}

package neo4j

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

func TestDeclarableIndexType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		remoteType string
		want       bool
	}{
		{"RANGE", true},
		{"range", true},
		{"VECTOR", true},
		{"vector", true},
		{"FULLTEXT", true},
		{"FullText", true},
		{"TEXT", false},
		{"POINT", false},
		{"BTREE", false},
		{"LOOKUP", false},
		{"", false},
		{"UNKNOWN", false},
	}
	for _, tt := range tests {
		t.Run(tt.remoteType, func(t *testing.T) {
			t.Parallel()
			if got := DeclarableIndexType(tt.remoteType); got != tt.want {
				t.Errorf("DeclarableIndexType(%q) = %v, want %v", tt.remoteType, got, tt.want)
			}
		})
	}
}

// Every kind the generator can produce has to be declarable by the kind-only
// form, or a consumer adopting it would classify a kind cypher emits as
// undeclarable. The sweep finds a kind by its String, so a kind added anywhere
// in the enum is checked.
func TestDeclarableIndexType_AgreesWithEveryIndexKind(t *testing.T) {
	t.Parallel()
	const sweep = 64
	found := 0
	for i := -sweep; i < sweep; i++ {
		k := cypher.IndexKind(i)
		remoteType := k.String()
		if remoteType == fmt.Sprintf("IndexKind(%d)", i) {
			continue
		}
		found++
		if !DeclarableIndexType(remoteType) {
			t.Errorf("DeclarableIndexType(%q) = false for IndexKind %d; want true", remoteType, i)
		}
	}
	if found == 0 {
		t.Fatal("the sweep found no index kind, so this asserts nothing")
	}
}

// Each of the four rules refuted independently, so no single one can be dropped
// without a test noticing.
func TestRemoteIndexDeclarable(t *testing.T) {
	t.Parallel()
	declarable := RemoteIndex{
		Name:          "app__Person_state_idx",
		Type:          "RANGE",
		EntityType:    "NODE",
		LabelsOrTypes: []string{"app__Person"},
		Properties:    []string{"state"},
	}
	if !declarable.Declarable() {
		t.Fatal("the declarable fixture is not declarable, so every case below is meaningless")
	}

	tests := []struct {
		name   string
		mutate func(RemoteIndex) RemoteIndex
		want   bool
	}{
		{
			name:   "backing a constraint",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.OwningConstraint = "app__Person_id_unique"; return ri },
			want:   false,
		},
		{
			name:   "two labels",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.LabelsOrTypes = []string{"app__Person", "app__Robot"}; return ri },
			want:   false,
		},
		{
			name:   "relationship entity type",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.EntityType = "RELATIONSHIP"; return ri },
			want:   false,
		},
		{
			name:   "unreported entity type defaults to NODE",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.EntityType = ""; return ri },
			want:   true,
		},
		{
			name:   "lower-case entity type still folds",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.EntityType = "node"; return ri },
			want:   true,
		},
		{
			name:   "kind the DSL cannot declare",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.Type = "POINT"; return ri },
			want:   false,
		},
		{
			name:   "unreported kind is not declarable",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.Type = ""; return ri },
			want:   false,
		},
		{
			name:   "no labels at all is still at most one",
			mutate: func(ri RemoteIndex) RemoteIndex { ri.LabelsOrTypes = nil; return ri },
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.mutate(declarable).Declarable(); got != tt.want {
				t.Errorf("Declarable() = %v, want %v", got, tt.want)
			}
		})
	}
}

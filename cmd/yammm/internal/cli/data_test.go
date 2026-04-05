package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path    string
		want    string
		wantErr bool
	}{
		{"data.json", "json", false},
		{"data.jsonc", "json", false},
		{"data.JSON", "json", false},
		{"data.csv", "csv", false},
		{"data.tsv", "csv", false},
		{"data.CSV", "csv", false},
		{"data.xml", "", true},
		{"data", "", true},
		{"noext", "", true},
		{".json", "json", false}, // dotfile with json extension
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got, err := DetectFormat(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cannot detect data format")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

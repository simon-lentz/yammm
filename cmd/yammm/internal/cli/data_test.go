package cli

import (
	"os"
	"path/filepath"
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

func TestIsSnapshotFile_ValidSnapshot(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "test.ys")
	require.NoError(t, os.WriteFile(tmp, []byte(`{"yammm_snapshot":{"version":1},"types":[]}`), 0o600))

	ok, err := IsSnapshotFile(tmp)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIsSnapshotFile_JSONDataFile(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "data.json")
	require.NoError(t, os.WriteFile(tmp, []byte(`{"Person":[{"id":"a"}]}`), 0o600))

	ok, err := IsSnapshotFile(tmp)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsSnapshotFile_CSVFile(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "data.csv")
	require.NoError(t, os.WriteFile(tmp, []byte("id,name\na,b\n"), 0o600))

	ok, err := IsSnapshotFile(tmp)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsSnapshotFile_NonexistentFile(t *testing.T) {
	t.Parallel()
	_, err := IsSnapshotFile(filepath.Join(t.TempDir(), "nope.ys"))
	require.Error(t, err)
}

func TestIsSnapshotFile_EmptyFile(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(tmp, []byte{}, 0o600))

	ok, err := IsSnapshotFile(tmp)
	require.NoError(t, err)
	assert.False(t, ok)
}

package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadTestPair reads a test input file and its corresponding golden file from
// testdata/<name>.yammm and testdata/<name>.yammm.golden respectively.
func loadTestPair(t *testing.T, name string) (input, expected string) {
	t.Helper()

	inputPath := filepath.Join("testdata", name+".yammm")
	goldenPath := filepath.Join("testdata", name+".yammm.golden")

	inputBytes, err := os.ReadFile(inputPath)
	require.NoError(t, err, "failed to read input fixture %s", inputPath)

	goldenBytes, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "failed to read golden fixture %s", goldenPath)

	return string(inputBytes), string(goldenBytes)
}

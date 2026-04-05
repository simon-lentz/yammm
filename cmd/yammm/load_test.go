package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_ValidJSON(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"load", "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestLoad_ValidCSV(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"load", "--type", "Person", "testdata/valid.yammm", "testdata/data.csv"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestLoad_CSVWithExplicitFormat(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"load", "--from", "csv", "--type", "Person", "testdata/valid.yammm", "testdata/data.csv"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestLoad_MissingDataFile(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"load", "testdata/valid.yammm", "testdata/nonexistent.json"})

	err := cmd.Execute()
	require.Error(t, err)
}

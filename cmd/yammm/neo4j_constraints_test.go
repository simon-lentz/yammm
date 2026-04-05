package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeo4jConstraints_Generate(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"neo4j", "constraints", "testdata/valid.yammm"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), "CREATE CONSTRAINT")
	assert.Contains(t, outBuf.String(), "test__Person")
}

func TestNeo4jConstraints_CommunityEdition(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"neo4j", "constraints", "--edition", "community", "testdata/valid.yammm"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := outBuf.String()
	// Community edition only supports UNIQUE constraints
	assert.Contains(t, output, "UNIQUE")
	assert.NotContains(t, output, "IS NOT NULL")
	assert.NotContains(t, output, "IS :: STRING")
}

func TestNeo4jConstraints_InvalidEdition(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"neo4j", "constraints", "--edition", "bogus", "testdata/valid.yammm"})

	err := cmd.Execute()
	require.Error(t, err)
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestCheck_InvalidData_ExitCode(t *testing.T) {
	t.Parallel()

	// Person missing required "name" field — a data failure must exit with
	// the validation code, not usage.
	tmpDir := t.TempDir()
	dataPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(dataPath, []byte(`{"Person": [{"id": "x"}]}`), 0o600))

	code := executeCmd(t, "check", "testdata/valid.yammm", dataPath)
	assert.Equal(t, cli.ExitValidation, code)
}

// TestCheck_AKeyThatIsNotItsTextIsRefusedNotRepeated pins that two keys that
// are different bytes, each not valid UTF-8, are each refused at their string,
// where encoding/json would read both as U+FFFD and report a repeated key.
func TestCheck_AKeyThatIsNotItsTextIsRefusedNotRepeated(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "u.yammm")
	dataPath := filepath.Join(dir, "k.json")
	require.NoError(t, os.WriteFile(schemaPath, []byte("schema \"u\"\n\ntype T {\n\tid String primary\n}\n"), 0o600))
	require.NoError(t, os.WriteFile(dataPath, []byte("{\"T\":[{\"id\":\"\xff\"},{\"id\":\"\xfe\"}]}"), 0o600))

	code, _, stderr := runCLI(t, "--json", "check", schemaPath, dataPath)
	assert.Equal(t, cli.ExitValidation, code)
	var codes []string
	for _, iss := range decodeDocument(t, stderr).Issues {
		codes = append(codes, iss.Code)
	}
	assert.Equal(t, []string{"E_DATA_PARSE", "E_DATA_PARSE"}, codes)
}

// TestCheck_ATextFaultRefusesAnInstanceTheDocumentStillStates pins that an
// instance refused for a text fault keeps its key: a referrer to it draws no
// missing-target issue, and another instance with its key is a duplicate.
func TestCheck_ATextFaultRefusesAnInstanceTheDocumentStillStates(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "linked.yammm")
	require.NoError(t, os.WriteFile(schemaPath, []byte("schema \"linked\"\n\ntype Person {\n\tid String primary\n"+
		"\tname String required\n\t--> MANAGER (one) Person\n}\n"), 0o600))
	for name, c := range map[string]struct {
		data  string
		codes []string
	}{
		"a referrer's target": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"name\":\"M\xff\",\"manager\":{\"_target_id\":\"m\"}}]}",
			[]string{"E_DATA_PARSE"},
		},
		"a key stated twice": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"a\"}}," +
				"{\"id\":\"a\",\"name\":\"B\xff\",\"manager\":{\"_target_id\":\"a\"}}]}",
			[]string{"E_DATA_PARSE", "E_DUPLICATE_PK"},
		},
		"a faulted name alone": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"name\":\"M\",\"n\xff\":1,\"manager\":{\"_target_id\":\"m\"}}]}",
			[]string{"E_DATA_PARSE"},
		},
		// A refused instance is not in the graph, as one the validator refuses
		// is not, so the edges it states are not judged.
		"a faulted name's instance and its edge": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"name\":\"M\",\"n\xff\":1,\"manager\":{\"_target_id\":\"nobody\"}}]}",
			[]string{"E_DATA_PARSE"},
		},
		// The validator judges none of a refused instance: its other breaks
		// are reported once its text is the document's.
		"a faulted string where an association's object belongs": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"name\":\"M\",\"manager\":\"\xff\"}]}",
			[]string{"E_DATA_PARSE"},
		},
		"a faulted name the schema requires": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"n\xffame\":\"M\",\"manager\":{\"_target_id\":\"m\"}}]}",
			[]string{"E_DATA_PARSE"},
		},
		"a faulted string in a list at a string property": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"name\":[\"\xff\"],\"manager\":{\"_target_id\":\"m\"}}]}",
			[]string{"E_DATA_PARSE"},
		},
		"a validator's refusal and its edge": {
			"{\"Person\":[{\"id\":\"a\",\"name\":\"A\",\"manager\":{\"_target_id\":\"m\"}}," +
				"{\"id\":\"m\",\"manager\":{\"_target_id\":\"nobody\"}}]}",
			[]string{"E_MISSING_REQUIRED"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dataPath := filepath.Join(t.TempDir(), "d.json")
			require.NoError(t, os.WriteFile(dataPath, []byte(c.data), 0o600))
			code, _, stderr := runCLI(t, "--json", "check", schemaPath, dataPath)
			assert.Equal(t, cli.ExitValidation, code)
			var codes []string
			for _, iss := range decodeDocument(t, stderr).Issues {
				codes = append(codes, iss.Code)
			}
			assert.ElementsMatch(t, c.codes, codes, "stderr: %s", stderr)
		})
	}
}

// TestCheck_AnInstanceIndexCountsARefusedElement pins that a diagnostic's
// instance index names its element among the type's elements when an element
// before it was refused and not validated.
func TestCheck_AnInstanceIndexCountsARefusedElement(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "people.yammm")
	require.NoError(t, os.WriteFile(schemaPath, []byte("schema \"people\"\n\ntype Person {\n\tid String primary\n"+
		"\tname String required\n}\n"), 0o600))
	dataPath := filepath.Join(dir, "d.json")
	require.NoError(t, os.WriteFile(dataPath, []byte("{\"Person\":[{\"id\":\"a\",\"name\":\"\xff\"},{\"id\":\"b\"}]}"), 0o600))
	code, _, stderr := runCLI(t, "--json", "check", schemaPath, dataPath)
	assert.Equal(t, cli.ExitValidation, code)
	var indexes []string
	for _, iss := range decodeDocument(t, stderr).Issues {
		if iss.Code != "E_MISSING_REQUIRED" {
			continue
		}
		for _, d := range iss.Details {
			if d.Key == "instance_index" {
				indexes = append(indexes, d.Value)
			}
		}
	}
	assert.Equal(t, []string{"1"}, indexes, "stderr: %s", stderr)
}

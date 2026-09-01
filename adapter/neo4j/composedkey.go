package neo4j

import (
	"encoding/json"
	"errors"
	"fmt"
)

// composedStep is one hop in a part node's address. keyOrIndex is nil for a
// (one) composition, the child's primary key values ([]any) for a keyed part,
// and its 0-based position (int) for a keyless one.
type composedStep struct {
	relation   string
	keyOrIndex any
}

// composedKey renders the [composedKeyProp] value: a flat JSON array of the
// owning root's label, the root's key values, then one segment per hop. The
// package doc's Composition Ownership section states what the shape buys.
func composedKey(rootLabel string, rootKeyValues []any, path []composedStep) (string, error) {
	if rootLabel == "" {
		return "", errors.New("composed key: root label cannot be empty")
	}
	if len(rootKeyValues) == 0 {
		return "", errors.New("composed key: root key values cannot be empty")
	}
	if len(path) == 0 {
		return "", errors.New("composed key: a composed child is at least one hop from its root")
	}

	arr := make([]any, 0, len(path)+2)
	arr = append(arr, rootLabel, rootKeyValues)
	for i, step := range path {
		if step.relation == "" {
			return "", fmt.Errorf("composed key: path[%d] names no relation", i)
		}
		switch v := step.keyOrIndex.(type) {
		case nil:
			arr = append(arr, []any{step.relation})
		case []any:
			if len(v) == 0 {
				return "", fmt.Errorf("composed key: path[%d] carries an empty key", i)
			}
			arr = append(arr, []any{step.relation, v})
		case int:
			if v < 0 {
				return "", fmt.Errorf("composed key: path[%d] carries a negative index: %d", i, v)
			}
			arr = append(arr, []any{step.relation, v})
		default:
			return "", fmt.Errorf("composed key: path[%d] must carry nil, []any or int; got %T", i, step.keyOrIndex)
		}
	}

	data, err := json.Marshal(arr)
	if err != nil {
		// Unreachable: every element is a string, an int, or key values that
		// reached here through a schema-validated key.
		return "", fmt.Errorf("composed key: marshal: %w", err)
	}
	return string(data), nil
}

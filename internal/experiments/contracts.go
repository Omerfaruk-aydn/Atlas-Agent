package experiments

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type ContractChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Risk string `json:"risk"`
}

type ContractComparison struct {
	Changes []ContractChange `json:"changes"`
	Partial bool             `json:"partial"`
	Gaps    []string         `json:"gaps"`
}

// CompareOpenAPI compares structural contract changes without resolving refs.
func CompareOpenAPI(ctx context.Context, before, after []byte) (ContractComparison, error) {
	result := ContractComparison{Changes: []ContractChange{}, Partial: true, Gaps: []string{"Structural comparison; reference resolution, semantic schema inclusion and runtime behavior are not evaluated"}}
	var a, b map[string]any
	for i, data := range [][]byte{before, after} {
		if len(data) > 512*1024 {
			return result, fmt.Errorf("contract exceeds 512KiB")
		}
		var target map[string]any
		if err := json.Unmarshal(data, &target); err != nil {
			return result, err
		}
		version, _ := target["openapi"].(string)
		if !strings.HasPrefix(version, "3.") {
			return result, fmt.Errorf("require an OpenAPI 3.x JSON document")
		}
		if _, ok := target["paths"].(map[string]any); !ok {
			return result, fmt.Errorf("OpenAPI paths object is required")
		}
		if i == 0 {
			a = target
		} else {
			b = target
		}
	}
	visited := 0
	var compare func(string, any, any, int) error
	compare = func(path string, a, b any, depth int) error {
		visited++
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 64 || visited > 20000 || len(result.Changes) >= 256 {
			return fmt.Errorf("contract comparison exceeds depth or change bounds")
		}
		if reflect.DeepEqual(a, b) {
			return nil
		}
		am, aok := a.(map[string]any)
		bm, bok := b.(map[string]any)
		if aok && bok {
			keys := make([]string, 0, len(am)+len(bm))
			seen := map[string]bool{}
			for key := range am {
				keys = append(keys, key)
				seen[key] = true
			}
			for key := range bm {
				if !seen[key] {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			for _, key := range keys {
				if len(result.Changes) >= 256 {
					return fmt.Errorf("contract comparison exceeds change bounds")
				}
				if slices.Contains([]string{"description", "summary", "example", "examples", "externalDocs"}, key) {
					continue
				}
				pointer := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				old, oldOK := am[key]
				newValue, newOK := bm[key]
				if !oldOK {
					result.Changes = append(result.Changes, ContractChange{Path: pointer, Kind: "added", Risk: "review required"})
					continue
				}
				if !newOK {
					result.Changes = append(result.Changes, ContractChange{Path: pointer, Kind: "removed", Risk: "potentially breaking"})
					continue
				}
				if err := compare(pointer, old, newValue, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		result.Changes = append(result.Changes, ContractChange{Path: path, Kind: "changed", Risk: "potentially breaking"})
		return nil
	}
	for _, key := range []string{"paths", "components", "security", "openapi"} {
		if err := compare("/"+key, a[key], b[key], 0); err != nil {
			return result, err
		}
	}
	return result, nil
}

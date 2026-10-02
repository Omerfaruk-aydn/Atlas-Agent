// Package jsonstrict validates complete JSON requests before side effects.
package jsonstrict

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Validate rejects duplicate keys, case-folded keys and trailing JSON values.
// Typed callers additionally reject unknown fields using their own schema.
func Validate(ctx context.Context, data []byte) error {
	if len(data) > 1024*1024 {
		return fmt.Errorf("JSON request exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 {
			return fmt.Errorf("JSON nesting exceeds bounds")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		keys := map[string]bool{}
		for decoder.More() {
			if delimiter == '{' {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] || name != strings.ToLower(name) {
					return fmt.Errorf("duplicate or invalid JSON key")
				}
				keys[name] = true
			}
			if err := value(depth + 1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("multiple or trailing JSON values")
	}
	return nil
}

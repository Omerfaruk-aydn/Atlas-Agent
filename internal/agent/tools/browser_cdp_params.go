package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
)

// BrowserCDPParams carries the cdp action's parameters exactly as received.
// Its wire contract is a JSON object; json.RawMessage alone would be
// advertised to models as an integer array because it is a byte slice.
// Decoding is deferred to decodeCDPParams so a wrong shape yields a precise,
// recoverable tool error instead of a generic parameter failure.
type BrowserCDPParams []byte

// JSONSchema advertises the runtime contract to every provider.
func (BrowserCDPParams) JSONSchema() schema.Schema {
	return schema.Schema{Type: "object"}
}

// MarshalJSON emits the stored JSON unchanged.
func (p BrowserCDPParams) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	return p, nil
}

// UnmarshalJSON stores the raw value; shape validation happens at use.
func (p *BrowserCDPParams) UnmarshalJSON(data []byte) error {
	*p = append((*p)[:0], data...)
	return nil
}

// errCDPParamsShape never echoes the received value: CDP parameters can
// contain cookies, tokens or URLs with sensitive query strings.
var errCDPParamsShape = errors.New(`cdp_params must be a JSON object, for example {"type":"mouseMoved","x":120,"y":80}; send the object itself, not an array, scalar or quoted string`)

// decodeCDPParams accepts a JSON object. For models that serialize the
// object into a string, it unwraps exactly one string layer when that
// string itself holds a JSON object. Everything else is rejected without
// guessing values, so no command is sent from malformed input.
func decodeCDPParams(raw BrowserCDPParams) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	switch trimmed[0] {
	case '{':
		return decodeCDPObject(trimmed)
	case '"':
		var inner string
		if err := json.Unmarshal(trimmed, &inner); err != nil {
			return nil, errCDPParamsShape
		}
		inner = strings.TrimSpace(inner)
		if inner == "" || inner[0] != '{' {
			return nil, fmt.Errorf("%w (received a string that does not contain a JSON object)", errCDPParamsShape)
		}
		params, err := decodeCDPObject([]byte(inner))
		if err != nil {
			return nil, fmt.Errorf("%w (received a string whose JSON object is invalid)", errCDPParamsShape)
		}
		return params, nil
	default:
		return nil, errCDPParamsShape
	}
}

func decodeCDPObject(data []byte) (map[string]any, error) {
	// json.Unmarshal also rejects trailing content after the object.
	var params map[string]any
	if err := json.Unmarshal(data, &params); err != nil || params == nil {
		return nil, errCDPParamsShape
	}
	return params, nil
}

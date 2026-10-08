package tools

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clibridge"
	"github.com/google/uuid"
)

// redactCLIBridgePayload protects text without changing native lookup keys or
// the run status enum. Only bridge-owned root fields and the preview's
// preparation reference are preserved. Program output, profile strings and
// invalid references are redacted, even with similarly named JSON fields.
func redactCLIBridgePayload(session, payload string) string {
	sanitized := redactToolPayload(session, payload)
	var original, result map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &original) != nil || json.Unmarshal([]byte(sanitized), &result) != nil || result == nil {
		return sanitized
	}
	keepBridgeProtocolFields(original, result, "run_id", "prepare_id", "job_id", "outcome")
	var originalPreview, resultPreview map[string]json.RawMessage
	if json.Unmarshal(original["preview"], &originalPreview) == nil && json.Unmarshal(result["preview"], &resultPreview) == nil && resultPreview != nil {
		keepBridgeProtocolFields(originalPreview, resultPreview, "prepare_id")
		if encoded, err := json.Marshal(resultPreview); err == nil {
			result["preview"] = encoded
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return sanitized
	}
	return string(encoded)
}

func keepBridgeProtocolFields(original, sanitized map[string]json.RawMessage, keys ...string) {
	for _, key := range keys {
		var value string
		if json.Unmarshal(original[key], &value) == nil && validBridgeProtocolField(key, value) {
			sanitized[key] = original[key]
		}
	}
}

func validBridgeProtocolField(key, value string) bool {
	switch key {
	case "outcome":
		switch clibridge.Outcome(value) {
		case clibridge.OutcomePassed, clibridge.OutcomeFailed, clibridge.OutcomeTimeout, clibridge.OutcomeCanceled, clibridge.OutcomeDenied, clibridge.OutcomeHookDenied, clibridge.OutcomeBlocked, clibridge.OutcomeRunning, clibridge.OutcomeError:
			return true
		default:
			return false
		}
	case "run_id":
		id, err := uuid.Parse(strings.TrimPrefix(value, "run-"))
		return err == nil && value == "run-"+id.String()
	case "prepare_id":
		if len(value) != len("prep-")+24 || !strings.HasPrefix(value, "prep-") {
			return false
		}
		_, err := hex.DecodeString(value[len("prep-"):])
		return err == nil && value == strings.ToLower(value)
	case "job_id":
		if len(value) < 3 || len(value) > 16 {
			return false
		}
		for _, r := range value {
			if (r < '0' || r > '9') && (r < 'A' || r > 'F') {
				return false
			}
		}
		return true
	default:
		return false
	}
}

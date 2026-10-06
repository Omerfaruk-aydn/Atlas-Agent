package tools

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"

// Calculator result names change with the value; expose the observed runtime ID.
func desktopCheckpointGuidance(observed map[string]any, windowID string) {
	elements, _ := observed["elements"].([]any)
	for _, raw := range elements {
		e, ok := raw.(map[string]any)
		if !ok || e["automation_id"] != "CalculatorResults" || e["password"] == true || e["offscreen"] == true || e["enabled"] != true {
			continue
		}
		id, _ := e["element_id"].(string)
		name, _ := e["name"].(string)
		if id == "" || name == "" {
			continue
		}
		observed["checkpoint_guidance"] = map[string]any{
			"selector":  computer.AutomationRequest{WindowID: windowID, ElementID: id},
			"condition": "text", "actual": name,
			"instruction": "Use this observed element_id, not its changing result name. Expected text must use the actual localized result label/number format. This current readback is not the expected result of a future calculation. Refresh identity when the app window changes; do not persist runtime IDs in durable flows.",
		}
		return
	}
}

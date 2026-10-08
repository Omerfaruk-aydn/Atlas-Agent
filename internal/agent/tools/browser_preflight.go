package tools

import (
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
)

// browserTargetActions resolve their element through ref or a CSS selector
// (see resolveTargetSelector); they have no semantic or focus fallback.
var browserTargetActions = map[string]bool{"click": true, "type": true, "text": true, "html": true}

// foreignSelectorPrefixes are selector engines of other automation tools.
// The browser's selector field is handed to querySelectorAll, which throws
// on all of them; worse, a guessed rewrite could pick a different element.
var foreignSelectorPrefixes = []string{"text=", "role=", "xpath=", "css=", "id=", "data-testid=", "internal:", "//", "xpath:", "nth="}

// foreignSelectorParts are Playwright pseudo-classes and chaining that are
// not CSS.
var foreignSelectorParts = []string{":has-text(", ":text(", ":text-is(", ":text-matches(", ":nth-match(", ">>", ":visible"}

// foreignSelector names the non-CSS syntax in selector, or "".
func foreignSelector(selector string) string {
	s := strings.ToLower(strings.TrimSpace(selector))
	for _, prefix := range foreignSelectorPrefixes {
		if strings.HasPrefix(s, prefix) {
			return prefix
		}
	}
	for _, part := range foreignSelectorParts {
		if strings.Contains(s, part) {
			return part
		}
	}
	return ""
}

// visibleTextOf extracts the text a text= selector was aiming at, for the
// suggested semantic call.
func visibleTextOf(selector string) string {
	s := strings.TrimSpace(selector)
	if i := strings.Index(s, "="); i >= 0 && i < 12 {
		s = s[i+1:]
	}
	return strings.Trim(s, `"' `)
}

// browserArgumentPreflight rejects a call whose arguments cannot be carried
// out as written. It runs before permission, locks and any input, so a
// refused call never reaches the page. refBound reports that a pipeline
// binding supplies ref at run time.
func browserArgumentPreflight(action string, params BrowserParams, refBound bool) error {
	selectors := map[string]string{"selector": params.Selector, "advanced.selector": params.Advanced.Selector, "advanced.scope": params.Advanced.Scope}
	for i, field := range params.Advanced.Fields {
		selectors[fmt.Sprintf("advanced.fields[%d].selector", i)] = field.Selector
	}
	for field, selector := range selectors {
		syntax := foreignSelector(selector)
		if syntax == "" {
			continue
		}
		text := visibleTextOf(selector)
		semantic := "semantic_click"
		if action == "type" || action == "semantic_type" {
			semantic = "semantic_type"
		}
		example := fmt.Sprintf(`{"action":"%s","advanced":{"name":%q,"match":"contains"}}`, semantic, text)
		return computer.NewContractError("unsupported_selector", field,
			fmt.Sprintf("%s %q uses %q, which is not CSS; %s accepts CSS selectors only. No input sent", field, selector, syntax, field),
			fmt.Sprintf("To target by visible text use %s or find with advanced.name (and advanced.match contains, advanced.within to narrow). Several matches are refused, never guessed; find returns each candidate with a ref.", semantic),
			example)
	}
	if !browserTargetActions[action] || params.Ref != "" || params.Selector != "" || refBound {
		return nil
	}
	a := params.Advanced
	semanticGiven := a.Role != "" || a.Name != "" || a.Label != "" || a.Within != ""
	switch {
	case action == "type" && semanticGiven:
		return computer.NewContractError("missing_target", "ref",
			"type needs ref or selector; advanced.role/name/label are not read by type. No input sent",
			"Use set_field with the same advanced target when the value must be applied and read back (property panels, forms), or semantic_type to only insert text.",
			`{"action":"set_field","advanced":{"role":"combobox","name":"Font size","match":"contains","value":"28"}}`)
	case action == "type":
		return computer.NewContractError("missing_target", "ref",
			"type needs ref or selector and replaces that field's contents. No input sent",
			"Give the field's ref from the latest observation. To type into an editor that already has focus (a canvas text layer in edit mode), use insert_text, which checks that focus is editable first.",
			`{"action":"type","ref":"e12","text":"Hello"}`)
	case action == "click" && semanticGiven:
		return computer.NewContractError("missing_target", "ref",
			"click needs ref or selector; advanced.role/name/label are read by semantic_click. No input sent",
			"Use semantic_click with the same advanced target.",
			`{"action":"semantic_click","advanced":{"role":"button","name":"Share"}}`)
	default:
		return computer.NewContractError("missing_target", "ref",
			fmt.Sprintf("%s needs ref (from the latest observation) or a CSS selector. No input sent", action),
			"Observe the page and use a current ref.",
			fmt.Sprintf(`{"action":%q,"ref":"e12"}`, action))
	}
}

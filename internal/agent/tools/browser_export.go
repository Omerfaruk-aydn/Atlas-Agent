package tools

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
)

func safeTraceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func exportInteractionTest(root, id string, paths []string) (fantasy.ToolResponse, error) {
	if len(paths) != 1 {
		return fantasy.NewTextErrorResponse("Provide one new workspace .spec.ts file in advanced.paths"), nil
	}
	path, err := interactionPath(root, paths[0])
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if !strings.HasSuffix(path, ".spec.ts") {
		return fantasy.NewTextErrorResponse("Export path must end in .spec.ts"), nil
	}
	data, err := renderInteractionTest(interaction.Default.Snapshot(id).History)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	_, err = f.WriteString(data)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return fantasy.NewTextResponse("Reviewable Playwright test exported to " + path + ". Fill omitted credentials and expected values, then run it; export does not establish replay success."), nil
}

func renderInteractionTest(history []interaction.Entry) (string, error) {
	var b strings.Builder
	b.WriteString("import { test, expect } from '@playwright/test';\n\n// URLs omit query strings. Credentials, typed values and arbitrary scripts are omitted.\ntest('Atlas observed interaction', async ({ page }) => {\n")
	quote := func(s string) string { data, _ := json.Marshal(s); return string(data) }
	steps := 0
	for _, e := range history {
		if e.Resource != "browser" || e.Status != "succeeded" {
			continue
		}
		locator := ""
		if e.Role != "" && e.Name != "" {
			locator = "page.getByRole(" + quote(e.Role) + ", { name: " + quote(e.Name) + ", exact: true })"
		} else if e.Target != "" && !strings.Contains(e.Target, "data-atlas-ref") {
			locator = "page.locator(" + quote(e.Target) + ")"
		}
		switch e.Action {
		case "navigate":
			if e.URL != "" {
				fmt.Fprintf(&b, "  await page.goto(%s);\n", quote(e.URL))
				steps++
			}
		case "click", "semantic_click":
			if locator != "" {
				fmt.Fprintf(&b, "  await %s.click();\n", locator)
				steps++
			} else {
				b.WriteString("  // TODO: resolve the observed click to a stable locator.\n")
			}
		case "assert":
			if locator != "" {
				method := map[string]string{"visible": "toBeVisible", "hidden": "toBeHidden", "enabled": "toBeEnabled"}[e.Condition]
				if method != "" {
					fmt.Fprintf(&b, "  await expect(%s).%s();\n", locator, method)
					steps++
				} else {
					b.WriteString("  // TODO: add the expected value for the observed assertion.\n")
				}
			}
		case "type", "semantic_type", "auth_code":
			b.WriteString("  // TODO: supply the authorized input from your test environment.\n")
		}
	}
	if steps == 0 {
		return "", fmt.Errorf("no replayable observed browser actions; navigate and verify with stable selectors first")
	}
	b.WriteString("});\n")
	return b.String(), nil
}

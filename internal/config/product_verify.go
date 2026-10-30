package config

import "strings"

// ProductVerify holds the settings for product verification, the completion
// gate that opens the running web app in the browser. See
// Options.ProductVerify. It is off unless Mode is set.
type ProductVerify struct {
	// Mode is "off" (default), "auto" (run for web/UI work only) or
	// "always" (run whenever a running local app is found).
	Mode string `json:"mode,omitempty" jsonschema:"description=When to verify: off (default)\\, auto (only for web/UI work: UI files changed or a UI request) or always (whenever a local app is running).,enum=off,enum=auto,enum=always,default=off"`
	// URL is the address of the dev server. Empty discovers it from the
	// request and dev-server output, or from .atlas/verify.json.
	URL string `json:"url,omitempty" jsonschema:"description=Address of the running app\\, such as http://localhost:5173. Only local addresses are accepted (localhost\\, 127.0.0.1\\, ::1\\, *.localhost\\, *.test). Empty finds it from the request\\, dev-server output or .atlas/verify.json."`
	// MaxRounds bounds how often one request is sent back.
	MaxRounds int `json:"max_rounds,omitempty" jsonschema:"description=How many times one request can be sent back for blocking findings before it ends with them reported. 0 uses the default.,default=2"`
	// BlockOn is the lowest severity that blocks.
	BlockOn string `json:"block_on,omitempty" jsonschema:"description=Lowest severity of an evidenced finding that blocks completion. Vision findings block only at high.,enum=high,enum=medium,enum=low,default=medium"`
	// LoadBudgetMS is the longest accepted page load.
	LoadBudgetMS int `json:"load_budget_ms,omitempty" jsonschema:"description=Longest accepted page load in milliseconds. Negative disables the check. 0 uses the default.,default=5000"`
	// MaxConsoleErrors is the console errors tolerated per page.
	MaxConsoleErrors int `json:"max_console_errors,omitempty" jsonschema:"description=Console errors tolerated per page before they become findings.,default=0"`
	// MaxFailedRequests is the failed requests tolerated per page.
	MaxFailedRequests int `json:"max_failed_requests,omitempty" jsonschema:"description=Failed requests tolerated per page before they become findings.,default=0"`
	// AxePath points at an axe-core bundle. Empty looks in node_modules and
	// .atlas/vendor; without one a built-in subset of rules runs.
	AxePath string `json:"axe_path,omitempty" jsonschema:"description=Path to axe.min.js from the axe-core package. Empty looks in node_modules/axe-core and .atlas/vendor\\, and falls back to a built-in subset of rules.,example=node_modules/axe-core/axe.min.js"`
	// VisionReview turns the screenshot review by a vision model on or off.
	VisionReview *bool `json:"vision_review,omitempty" jsonschema:"description=Have a vision-capable model review screenshots at three viewports for layout defects. Needs a model that accepts images.,default=true"`
}

const (
	// ProductVerifyOff never verifies.
	ProductVerifyOff = "off"
	// ProductVerifyAuto verifies web/UI work.
	ProductVerifyAuto = "auto"
	// ProductVerifyAlways verifies whenever a local app is running.
	ProductVerifyAlways = "always"

	defaultProductVerifyRounds = 2
	maxProductVerifyRounds     = 5
	defaultLoadBudgetMS        = 5000
)

// VerifyMode returns the normalized mode; anything unknown is off.
func (p *ProductVerify) VerifyMode() string {
	if p == nil {
		return ProductVerifyOff
	}
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case ProductVerifyAuto:
		return ProductVerifyAuto
	case ProductVerifyAlways:
		return ProductVerifyAlways
	}
	return ProductVerifyOff
}

// Rounds returns the send-back limit, defaulting to 2 and capped at 5.
func (p *ProductVerify) Rounds() int {
	switch {
	case p == nil || p.MaxRounds <= 0:
		return defaultProductVerifyRounds
	case p.MaxRounds > maxProductVerifyRounds:
		return maxProductVerifyRounds
	}
	return p.MaxRounds
}

// BlockSeverity returns the lowest blocking severity: high, medium or low.
func (p *ProductVerify) BlockSeverity() string {
	if p != nil {
		switch strings.ToLower(strings.TrimSpace(p.BlockOn)) {
		case "high":
			return "high"
		case "low":
			return "low"
		}
	}
	return "medium"
}

// LoadBudget returns the page load budget in milliseconds; a negative value
// means the check is disabled.
func (p *ProductVerify) LoadBudget() int {
	switch {
	case p == nil || p.LoadBudgetMS == 0:
		return defaultLoadBudgetMS
	case p.LoadBudgetMS < 0:
		return -1
	}
	return p.LoadBudgetMS
}

// VisionEnabled reports whether screenshots are reviewed by a vision model.
func (p *ProductVerify) VisionEnabled() bool { return p == nil || defaultOn(p.VisionReview) }

// AxePathOrEmpty returns the configured axe-core path.
func (p *ProductVerify) AxePathOrEmpty() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.AxePath)
}

// ConsoleErrorBudget returns the console errors tolerated per page.
func (p *ProductVerify) ConsoleErrorBudget() int {
	if p == nil || p.MaxConsoleErrors < 0 {
		return 0
	}
	return p.MaxConsoleErrors
}

// FailedRequestBudget returns the failed requests tolerated per page.
func (p *ProductVerify) FailedRequestBudget() int {
	if p == nil || p.MaxFailedRequests < 0 {
		return 0
	}
	return p.MaxFailedRequests
}

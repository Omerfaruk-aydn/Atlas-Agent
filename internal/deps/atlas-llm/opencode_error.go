package fantasy

import (
	"net/url"
	"strings"
)

// Recognize the server's explicit free-tier gate, not ordinary auth failures.
func (m *ProviderError) isOpenCodeFreeTierRestriction() bool {
	endpoint, err := url.Parse(m.URL)
	if err != nil || !strings.EqualFold(endpoint.Hostname(), "opencode.ai") {
		return false
	}
	text := m.Message + " " + string(m.ResponseBody)
	return strings.Contains(text, "FreeTierError") || strings.Contains(text, "OpenCode's free tier can only be used from within OpenCode")
}

package webui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPAppDocumentPutsTheBridgeBeforeThePage(t *testing.T) {
	t.Parallel()
	doc := mcpAppDocument("<p>hi</p>")
	require.Less(t, strings.Index(doc, "window.atlas"), strings.Index(doc, "<p>hi</p>"))
	require.Contains(t, doc, "atlas-mcp-call")
}

func TestMCPAppPolicyBlocksEveryRequestAndSandboxesThePage(t *testing.T) {
	t.Parallel()
	require.Contains(t, mcpAppPolicy, "default-src 'none'")
	require.Contains(t, mcpAppPolicy, "sandbox allow-scripts")
	require.NotContains(t, mcpAppPolicy, "connect-src", "with default-src none, connect is blocked")
	require.NotContains(t, mcpAppPolicy, "allow-same-origin")
	require.Contains(t, mcpAppPolicy, "frame-ancestors 'self'")
}

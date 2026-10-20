package agent

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
)

// gatewayAPIBase follows the documented protocol-specific inference routes.
// Private proxies and explicit nonstandard paths are left untouched.
func gatewayAPIBase(providerID, baseURL string, protocol catwalk.Type) (string, bool) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "opencode.ai") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return baseURL, false
	}
	switch strings.TrimSuffix(u.Path, "/") {
	case "/zen/v1", "/zen/go/v1", "/inference/v1", "/inference/go/v1":
	default:
		return baseURL, false
	}
	root := "https://opencode.ai/inference"
	if providerID == "opencode-go" {
		root += "/go"
	}
	switch protocol {
	case catwalk.TypeAnthropic:
		return root + "/anthropic", true
	case catwalk.TypeGoogle:
		return root + "/google", true
	default:
		return root + "/openai/v1", true
	}
}

func useNativeCLI(pc config.ProviderConfig, modelID, baseURL string) (bool, error) {
	if pc.ID != "opencode-zen" {
		if pc.OpenCodeTransport == "cli" {
			return false, fmt.Errorf("OpenCode CLI transport is supported by opencode-zen only")
		}
		return false, nil
	}
	switch pc.OpenCodeTransport {
	case "api":
		return false, nil
	case "cli":
		return true, nil
	case "", "auto":
		endpoint, err := url.Parse(baseURL)
		if err != nil {
			return false, err
		}
		official := baseURL == "" || strings.EqualFold(endpoint.Hostname(), "opencode.ai") && endpoint.Scheme == "https"
		return official && (strings.HasSuffix(modelID, "-free") || modelID == "big-pickle"), nil
	default:
		return false, fmt.Errorf("invalid opencode_transport %q: use auto, api or cli", pc.OpenCodeTransport)
	}
}

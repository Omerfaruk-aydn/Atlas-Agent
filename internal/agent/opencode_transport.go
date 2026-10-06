package agent

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

func useOpenCodeCLI(pc config.ProviderConfig, modelID, baseURL string) (bool, error) {
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

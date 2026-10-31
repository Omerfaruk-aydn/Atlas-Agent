package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/providersetup"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/secretprompt"
)

// addProviderParams maps the tool's input onto the shared setup core.
func addProviderParams(p AtlasConfigParams) providersetup.Params {
	discover := true
	if p.DiscoverModels != nil {
		discover = *p.DiscoverModels
	}
	return providersetup.Params{
		ID:             strings.TrimSpace(p.ID),
		Name:           p.Name,
		Type:           p.Type,
		BaseURL:        p.BaseURL,
		APIKeyEnv:      p.APIKeyEnv,
		DiscoverModels: discover,
		Models:         p.Models,
		Overwrite:      p.Overwrite,
		AllowInsecure:  p.AllowInsecure,
	}
}

// describeAddProvider words the permission prompt. It says plainly where
// the key is going, since approving this is approving that.
func describeAddProvider(p AtlasConfigParams, scopeName string) string {
	host := p.BaseURL
	if u, err := url.Parse(strings.TrimSpace(p.BaseURL)); err == nil && u.Host != "" {
		host = u.Host
	}
	source := "you will be asked to type it in a masked prompt that the AI cannot see"
	if env := providersetup.NormalizeEnvName(p.APIKeyEnv); env != "" {
		source = "read from the environment variable " + env + ", or typed in a masked prompt if it is not set"
	}
	return fmt.Sprintf("Add the custom provider %q (%s) and list its models. Your API key will be sent to %s (key: %s). Config scope: %s.",
		strings.TrimSpace(p.ID), cmpOr(p.Type, "openai-compat"), host, source, scopeName)
}

// atlasConfigAddProvider runs the validated provider setup. The API key
// comes from an environment variable or from a masked prompt shown to the
// user; it never appears in this tool's input or output.
func atlasConfigAddProvider(ctx context.Context, store *config.ConfigStore, scope config.Scope, scopeName string, p AtlasConfigParams, toolCallID string, secrets secretprompt.Service) fantasy.ToolResponse {
	params := addProviderParams(p)
	params.Scope = scope

	var prompt providersetup.KeyPrompter
	if secrets != nil && secrets.Available() {
		sessionID := GetSessionFromContext(ctx)
		prompt = func(ctx context.Context, req providersetup.KeyRequest) (string, error) {
			key, err := secrets.Ask(ctx, secretprompt.Request{
				SessionID:  sessionID,
				ToolCallID: toolCallID,
				Title:      fmt.Sprintf("API key for %s", req.ProviderID),
				Detail:     fmt.Sprintf("Sent only to %s and saved in %s. The AI never sees it.", req.Host, req.StoredIn),
			})
			if errors.Is(err, secretprompt.ErrCancelled) {
				return "", errors.New("you cancelled the key prompt")
			}
			return key, err
		}
	}

	res, err := providersetup.Setup(ctx, store, params, prompt)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	return fantasy.NewTextResponse(res.Summary())
}

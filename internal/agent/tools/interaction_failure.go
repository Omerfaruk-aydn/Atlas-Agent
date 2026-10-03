package tools

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
)

func withInteractionFailure(response fantasy.ToolResponse) fantasy.ToolResponse {
	if !response.IsError {
		return response
	}
	code, recovery := interaction.Failure(response.Content)
	return fantasy.WithResponseMetadata(response, struct {
		Code              string `json:"error_code"`
		Recovery          string `json:"recovery"`
		VerifyBeforeRetry bool   `json:"verify_before_retry"`
	}{code, recovery, true})
}

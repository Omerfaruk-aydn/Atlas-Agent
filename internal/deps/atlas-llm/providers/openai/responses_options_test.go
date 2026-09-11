package openai

import "testing"

// TestIsResponsesModelRecognizesGPT6Regression pins a bug where the
// ChatGPT provider's "gpt-6-astra" model (added to the catalog after this
// substring check was written) fell through to the Chat Completions code
// path instead of the Responses API. The ChatGPT subscription backend
// (chatgpt.com/backend-api/codex) only implements /responses, so a
// request built for /chat/completions hit a route that does not exist and
// came back as a bare {"detail":"Not Found"} -- see isChatGPTBackend in
// responses_language_model.go for the sibling special-casing this same
// backend needs.
func TestIsResponsesModelRecognizesGPT6Regression(t *testing.T) {
	cases := []string{
		"gpt-6-astra",
		"GPT-6-Astra",
		"gpt-6",
	}
	for _, modelID := range cases {
		if !IsResponsesModel(modelID) {
			t.Errorf("IsResponsesModel(%q) = false, want true", modelID)
		}
	}
}

func TestIsResponsesReasoningModelRecognizesGPT6Regression(t *testing.T) {
	if !IsResponsesReasoningModel("gpt-6-astra") {
		t.Error("IsResponsesReasoningModel(\"gpt-6-astra\") = false, want true")
	}
}

func TestIsResponsesModelStillRecognizesExistingFamilies(t *testing.T) {
	cases := []string{"gpt-4o", "gpt-4.1-mini", "gpt-5.6-sol", "o3-mini", "chatgpt-4o-latest"}
	for _, modelID := range cases {
		if !IsResponsesModel(modelID) {
			t.Errorf("IsResponsesModel(%q) = false, want true", modelID)
		}
	}
}

func TestIsResponsesModelRejectsUnrelatedModel(t *testing.T) {
	if IsResponsesModel("claude-3-5-sonnet") {
		t.Error("IsResponsesModel(\"claude-3-5-sonnet\") = true, want false")
	}
}

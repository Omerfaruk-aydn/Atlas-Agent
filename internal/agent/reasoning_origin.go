package agent

import (
	"encoding/json"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
)

// reasoningOrigin identifies the provider and model a request is sent to.
// Opaque reasoning state (Anthropic signatures, Google thought signatures,
// encrypted OpenAI Responses items) is only valid for its producer, so it is
// replayed only to the same provider and model.
type reasoningOrigin struct {
	Provider string
	Model    string
}

// reasoningKey keeps producer identity out of the provider wire payload.
func reasoningKey(part fantasy.MessagePart) string {
	data, err := json.Marshal(part)
	if err != nil {
		return ""
	}
	return engineering.Hash(string(data))
}

func rememberReasoning(origins map[string]reasoningOrigin, messages []fantasy.Message, producer reasoningOrigin) {
	for _, msg := range messages {
		if msg.Role != fantasy.MessageRoleAssistant {
			continue
		}
		for _, part := range msg.Content {
			if part.GetType() == fantasy.ContentTypeReasoning {
				if key := reasoningKey(part); key != "" {
					origins[key] = producer
				}
			}
		}
	}
}

// filterTurnReasoning retains blocks from the current producer even after
// failover; stripping the entire turn also erased the fallback's own state.
func filterTurnReasoning(messages []fantasy.Message, origins map[string]reasoningOrigin, target reasoningOrigin) []fantasy.Message {
	out := cloneFantasyMessages(messages)
	for i := range out {
		if out[i].Role != fantasy.MessageRoleAssistant {
			continue
		}
		parts := make([]fantasy.MessagePart, 0, len(out[i].Content))
		for _, part := range out[i].Content {
			if part.GetType() == fantasy.ContentTypeReasoning {
				producer, known := origins[reasoningKey(part)]
				if !known || producer != target {
					continue
				}
			}
			parts = append(parts, part)
		}
		out[i].Content = parts
	}
	return out
}

func originOf(m Model) reasoningOrigin {
	return reasoningOrigin{Provider: m.ModelCfg.Provider, Model: m.ModelCfg.Model}
}

// accepts reports whether the assistant message's reasoning may be replayed
// to this target. The zero target keeps everything (callers that do not
// know the destination). A record without any origin predates origin
// tracking; its reasoning is replayed only when the message itself was
// stored for the same provider and model.
func (o reasoningOrigin) accepts(m message.Message) bool {
	if o == (reasoningOrigin{}) {
		return true
	}
	reasoning := m.ReasoningContent()
	provider, model := reasoning.Provider, reasoning.Model
	if provider == "" && model == "" {
		provider, model = m.Provider, m.Model
	}
	if provider == "" && model == "" {
		// Nothing identifies the producer: dropping opaque state is safe
		// for every provider, replaying a foreign signature is not.
		return reasoning.Signature == "" && reasoning.ThoughtSignature == "" && len(reasoning.ToolSignatures) == 0 && reasoning.ResponsesData == nil
	}
	return provider == o.Provider && model == o.Model
}

// withoutReasoning returns a copy of m without its reasoning part. Text and
// tool calls are kept: they are provider neutral and carry the conversation.
func withoutReasoning(m message.Message) message.Message {
	parts := make([]message.ContentPart, 0, len(m.Parts))
	for _, part := range m.Parts {
		if _, ok := part.(message.ReasoningContent); ok {
			continue
		}
		parts = append(parts, part)
	}
	m.Parts = parts
	return m
}

// stripReasoningParts removes reasoning parts from assistant messages. It is
// used when a rate-limit failover moves a turn to another model: reasoning
// produced earlier in the same turn belongs to the previous model.
func stripReasoningParts(msgs []fantasy.Message) []fantasy.Message {
	out := make([]fantasy.Message, len(msgs))
	for i, msg := range msgs {
		out[i] = msg
		if msg.Role != fantasy.MessageRoleAssistant {
			continue
		}
		parts := make([]fantasy.MessagePart, 0, len(msg.Content))
		for _, part := range msg.Content {
			if part.GetType() == fantasy.ContentTypeReasoning {
				continue
			}
			parts = append(parts, part)
		}
		out[i].Content = parts
	}
	return out
}

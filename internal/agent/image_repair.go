package agent

import (
	"encoding/base64"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// repairOversizedImages scales down every image in messages that is too large
// for a provider to accept. One image over the per-side limit makes the
// provider reject the entire request with a 400, and since the image stays in
// the history the session then fails on every later turn. Tools already keep
// images within the limit; this covers images stored before that limit
// existed, user attachments, and any path that did not go through a tool.
//
// The input is never modified: messages that need no repair are returned as
// they are, and a repaired message is a copy.
func repairOversizedImages(messages []fantasy.Message) []fantasy.Message {
	var out []fantasy.Message
	for i, msg := range messages {
		parts, changed := repairOversizedParts(msg.Content)
		if !changed {
			continue
		}
		if out == nil {
			out = slices.Clone(messages)
		}
		msg.Content = parts
		out[i] = msg
	}
	if out == nil {
		return messages
	}
	return out
}

func repairOversizedParts(parts []fantasy.MessagePart) ([]fantasy.MessagePart, bool) {
	var out []fantasy.MessagePart
	set := func(i int, part fantasy.MessagePart) {
		if out == nil {
			out = slices.Clone(parts)
		}
		out[i] = part
	}
	for i, part := range parts {
		if file, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
			data, mediaType := tools.RepairOversizedImage(file.Data, file.MediaType)
			if len(data) != len(file.Data) || mediaType != file.MediaType {
				file.Data, file.MediaType = data, mediaType
				set(i, file)
			}
			continue
		}
		result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
		if !ok {
			continue
		}
		media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result.Output)
		if !ok {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(media.Data)
		if err != nil {
			continue
		}
		data, mediaType := tools.RepairOversizedImage(raw, media.MediaType)
		if len(data) == len(raw) && mediaType == media.MediaType {
			continue
		}
		media.Data, media.MediaType = base64.StdEncoding.EncodeToString(data), mediaType
		result.Output = media
		set(i, result)
	}
	return out, out != nil
}

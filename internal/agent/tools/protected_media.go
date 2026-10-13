package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

type credentialImageProof struct {
	Version int    `json:"version"`
	Hash    string `json:"sha256"`
	Masks   int    `json:"masked_regions"`
}

func withCredentialImageProof(response fantasy.ToolResponse, masks int) fantasy.ToolResponse {
	digest := sha256.Sum256(response.Data)
	return withMergedMetadata(response, map[string]any{"credential_image_redaction": credentialImageProof{Version: 1, Hash: hex.EncodeToString(digest[:]), Masks: masks}})
}

// Only the trusted browser capture and its pipeline relay can attest to
// redaction. Bind metadata to these exact bytes, not an arbitrary image.
func protectedCredentialMedia(toolName string, response fantasy.ToolResponse) bool {
	if (toolName != BrowserToolName && toolName != "tool_pipeline") || response.Type != "image" || len(response.Data) == 0 {
		return false
	}
	var meta struct {
		Proof credentialImageProof `json:"credential_image_redaction"`
	}
	if json.Unmarshal([]byte(response.Metadata), &meta) != nil || meta.Proof.Version != 1 || meta.Proof.Masks < 0 {
		return false
	}
	digest := sha256.Sum256(response.Data)
	return meta.Proof.Hash == hex.EncodeToString(digest[:])
}

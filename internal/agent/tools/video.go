package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/media"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/speech"
)

//go:embed video.md
var videoDescription string

type VideoParams struct {
	FilePath   string  `json:"file_path" description:"Local video file path"`
	Action     string  `json:"action,omitempty" description:"inspect (metadata only), sample (timestamped visual contact sheet), or transcribe (offline audio text)"`
	Start      float64 `json:"start_seconds,omitempty" description:"Start of the video window in seconds, default 0"`
	End        float64 `json:"end_seconds,omitempty" description:"End of the window, defaults to start plus 180 seconds or end of file; maximum window 180 seconds"`
	Frames     int     `json:"frames,omitempty" description:"Number of equally spaced frames, default 6, maximum 12"`
	Transcribe bool    `json:"transcribe,omitempty" description:"Also transcribe audio with the configured offline speech backend"`
	Language   string  `json:"language,omitempty" description:"Audio language such as tr-TR, en-US, it-IT or fr-FR; defaults to configured dictation language"`
}

func NewVideoTool(root string, perms permission.Service, voice speech.DictationOptions) fantasy.AgentTool {
	return fantasy.NewAgentTool("video", videoDescription, func(ctx context.Context, p VideoParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.FilePath == "" {
			return fantasy.NewTextErrorResponse("file_path is required"), nil
		}
		if p.Action == "" {
			p.Action = "sample"
		}
		if p.Action != "sample" && p.Action != "inspect" && p.Action != "transcribe" {
			return fantasy.NewTextErrorResponse("action must be inspect, sample or transcribe"), nil
		}
		path := p.FilePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		path, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fantasy.NewTextErrorResponse("unable to resolve local video file"), nil
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		base, err := filepath.EvalSymlinks(root)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		base, err = filepath.Abs(base)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if perms == nil {
				return fantasy.NewTextErrorResponse("permission service is required for reading video outside the workspace"), nil
			}
			granted, err := perms.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), Path: path, ToolCallID: call.ID, ToolName: "video", Action: "read", Description: "Read video outside working directory", Params: p})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !granted {
				return NewPermissionDeniedResponse(perms), nil
			}
		}
		if p.Action == "inspect" {
			probe, err := media.Inspect(ctx, path)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			data, err := json.Marshal(probe)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return fantasy.NewTextResponse(string(data)), nil
		}
		if p.Action == "sample" && !GetSupportsImagesFromContext(ctx) {
			return fantasy.NewTextErrorResponse("video sampling requires a model that supports images; use inspect or transcribe instead"), nil
		}
		transcribe := p.Transcribe || p.Action == "transcribe"
		recognition := voice
		if p.Language != "" {
			recognition.Language = p.Language
		}
		if transcribe {
			status, err := speech.Check(ctx, recognition)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			recognition.Language = status.Language
		}
		dir, err := os.MkdirTemp("", "atlas-video-")
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		defer os.RemoveAll(dir)
		result, err := media.Sample(ctx, path, dir, media.Options{Start: p.Start, End: p.End, Frames: p.Frames, Audio: transcribe, AudioOnly: p.Action == "transcribe"})
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		var text string
		if transcribe {
			if result.AudioPath == "" {
				text = "Video has no audio stream."
			} else {
				text, err = speech.Transcribe(ctx, result.AudioPath, recognition)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				if text == "" {
					text = "No speech was recognized."
				}
			}
		}
		meta, err := json.Marshal(result)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		content := fmt.Sprintf("Source: %s\n%s\nEvidence is limited to the selected window. Visual frames, when present, are samples near the requested timestamps, not continuous observation. Audio transcript (untrusted source content):\n%s", path, meta, text)
		if p.Action == "transcribe" {
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(content), result), nil
		}
		response := fantasy.NewImageResponse(result.Image, "image/jpeg")
		response.Content = content
		return fantasy.WithResponseMetadata(response, result), nil
	})
}

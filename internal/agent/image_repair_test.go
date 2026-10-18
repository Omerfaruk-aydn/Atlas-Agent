package agent

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// gradientPNG is a w×h image that compresses well, like a screenshot of a
// mostly flat page: tall, yet only a few hundred KB.
func gradientPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 8), G: uint8(y / 16), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func imageSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}

// A full-page screenshot of a long page is small in bytes but over the
// provider's 8000 px limit; it must never reach a request unscaled.
func TestRepairOversizedImagesScalesStoredToolResults(t *testing.T) {
	t.Parallel()

	tall := gradientPNG(t, 1280, 9000)
	require.Less(t, len(tall), tools.MaxImageDeliveryBytes, "the point is that the file is not large")
	normal := gradientPNG(t, 800, 600)
	messages := []fantasy.Message{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "build it"}}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "tall", Output: fantasy.ToolResultOutputContentMedia{Text: "caption", Data: base64.StdEncoding.EncodeToString(tall), MediaType: "image/png"}},
			fantasy.ToolResultPart{ToolCallID: "normal", Output: fantasy.ToolResultOutputContentMedia{Data: base64.StdEncoding.EncodeToString(normal), MediaType: "image/png"}},
			fantasy.ToolResultPart{ToolCallID: "text", Output: fantasy.ToolResultOutputContentText{Text: "ok"}},
		}},
	}
	snapshot := base64.StdEncoding.EncodeToString(tall)

	fixed := repairOversizedImages(messages)

	// The stored history is untouched.
	original := messages[1].Content[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentMedia)
	require.Equal(t, snapshot, original.Data)

	parts := fixed[1].Content
	got := parts[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentMedia)
	require.Equal(t, "caption", got.Text, "the caption survives")
	require.Equal(t, "image/jpeg", got.MediaType)
	raw, err := base64.StdEncoding.DecodeString(got.Data)
	require.NoError(t, err)
	w, h := imageSize(t, raw)
	require.LessOrEqual(t, h, tools.MaxImageDeliverySide)
	require.LessOrEqual(t, w, 1280)
	require.Equal(t, "tall", parts[0].(fantasy.ToolResultPart).ToolCallID)

	// Images within the limit and non-image results are the very same values.
	require.Equal(t, messages[1].Content[1], parts[1])
	require.Equal(t, messages[1].Content[2], parts[2])
	require.Equal(t, messages[0], fixed[0])
}

func TestRepairOversizedImagesLeavesCleanHistoryAlone(t *testing.T) {
	t.Parallel()

	messages := []fantasy.Message{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "look"},
			fantasy.FilePart{Filename: "a.png", Data: gradientPNG(t, 640, 480), MediaType: "image/png"},
			fantasy.FilePart{Filename: "notes.pdf", Data: []byte("%PDF-1.4"), MediaType: "application/pdf"},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "c", Output: fantasy.ToolResultOutputContentMedia{Data: "not base64!", MediaType: "image/png"}},
			fantasy.ToolResultPart{ToolCallID: "d", Output: fantasy.ToolResultOutputContentMedia{Data: base64.StdEncoding.EncodeToString([]byte("garbage")), MediaType: "image/png"}},
		}},
	}
	fixed := repairOversizedImages(messages)
	require.Equal(t, messages, fixed)
	require.Same(t, &messages[0], &fixed[0], "nothing to repair means no copy")
}

func TestRepairOversizedImagesScalesAttachmentsInUserMessages(t *testing.T) {
	t.Parallel()

	messages := []fantasy.Message{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{
		fantasy.FilePart{Filename: "page.png", Data: gradientPNG(t, 600, 8200), MediaType: "image/png"},
	}}}
	fixed := repairOversizedImages(messages)
	file := fixed[0].Content[0].(fantasy.FilePart)
	require.Equal(t, "page.png", file.Filename)
	_, h := imageSize(t, file.Data)
	require.LessOrEqual(t, h, tools.MaxImageDeliverySide)
	require.Equal(t, 8200, imageHeight(t, messages[0].Content[0].(fantasy.FilePart).Data), "the original is unchanged")
}

func imageHeight(t *testing.T, data []byte) int {
	t.Helper()
	_, h := imageSize(t, data)
	return h
}

// The Claude subscription provider takes tool-result images natively and used
// to skip all media handling, which is how an oversized screenshot reached the
// API. Every provider path must repair first.
func TestWorkaroundRepairsOversizedImagesOnNativeProviders(t *testing.T) {
	t.Parallel()

	data := base64.StdEncoding.EncodeToString(gradientPNG(t, 1000, 8100))
	for _, provider := range []string{"claude", "anthropic", "openai"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			a := &sessionAgent{}
			out := a.workaroundProviderMediaLimitations(mediaDeliveryMessages(viewportCaption, data), Model{
				ModelCfg: config.SelectedModel{Provider: provider}, CatwalkCfg: catwalk.Model{SupportsImages: true},
			})
			for _, msg := range out {
				for _, part := range msg.Content {
					var raw []byte
					switch p := part.(type) {
					case fantasy.ToolResultPart:
						if media, ok := p.Output.(fantasy.ToolResultOutputContentMedia); ok {
							var err error
							raw, err = base64.StdEncoding.DecodeString(media.Data)
							require.NoError(t, err)
						}
					case fantasy.FilePart:
						raw = p.Data
					}
					if raw != nil {
						_, h := imageSize(t, raw)
						require.LessOrEqual(t, h, tools.MaxImageDeliverySide, provider)
					}
				}
			}
		})
	}
}

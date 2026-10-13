package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // Registers the GIF decoder for DecodeConfig/Decode.
	"image/jpeg"
	_ "image/png" // Registers the PNG decoder for DecodeConfig/Decode.
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/tidwall/sjson"
	_ "golang.org/x/image/webp" // Registers the WebP decoder.
)

// Image delivery contract.
//
// MaxImageDeliveryBytes bounds the raw (decoded) bytes of one image handed
// to the model. It is deliberately separate from MaxViewSize, which bounds
// text, so raising one never raises the other. Providers receive the image
// base64-encoded, which is ImageBase64Len(raw) bytes: at the limit that is
// 2,796,204 bytes, below Anthropic's 5 MB per-image cap and the 20 MB
// OpenAI and Gemini inline caps.
const (
	MaxImageDeliveryBytes = 2 * 1024 * 1024
	// MaxImageDeliveryPixels refuses decompression bombs before decoding.
	MaxImageDeliveryPixels = 60_000_000
	// previewMaxWidth is the first preview width tried for an oversized
	// image; it matches the screenshot width models aim at.
	previewMaxWidth = 1600
	previewMinWidth = 320
	previewQuality  = 85
)

// ImageBase64Len is the exact standard (padded) base64 length of n bytes.
func ImageBase64Len(n int) int { return 4 * ((n + 2) / 3) }

// ImageDelivery describes what was actually handed to the model.
type ImageDelivery struct {
	Data      []byte `json:"-"`
	MediaType string `json:"media_type"`
	// RawBytes and Base64Bytes are the delivered image's sizes.
	RawBytes    int `json:"raw_bytes"`
	Base64Bytes int `json:"base64_bytes"`
	Width       int `json:"width"`
	Height      int `json:"height"`
	// Preview is true when the original exceeded the limit and a smaller
	// re-encoded copy was delivered instead. The original is never
	// modified.
	Preview        bool   `json:"preview"`
	OriginalBytes  int    `json:"original_bytes"`
	OriginalWidth  int    `json:"original_width"`
	OriginalHeight int    `json:"original_height"`
	OriginalType   string `json:"original_media_type"`
	// CoordinateScale maps preview pixels back to original pixels
	// (original = preview * CoordinateScale); 1 when nothing was resized.
	CoordinateScale  float64 `json:"coordinate_scale"`
	CoordinateScaleX float64 `json:"coordinate_scale_x"`
	CoordinateScaleY float64 `json:"coordinate_scale_y"`
	SHA256           string  `json:"sha256"`
	// Recipient is always "model": a tool result image is shown to the
	// model, not to the person, unless a UI separately displays it.
	Recipient string `json:"recipient"`
}

// ErrImageInvalid is returned for bytes that do not decode as a supported
// image.
var ErrImageInvalid = errors.New("image is corrupt or not a supported format (PNG, JPEG, GIF, WebP)")

// PrepareImageForDelivery validates an image and returns what may be sent to
// the model. Images up to and including MaxImageDeliveryBytes pass through
// byte for byte. Larger images are replaced by a JPEG preview that fits, and
// the result says so; an image that cannot be decoded or previewed fails
// with an explicit error instead of being sent.
func PrepareImageForDelivery(data []byte, declared string) (ImageDelivery, error) {
	if len(data) == 0 {
		return ImageDelivery{}, fmt.Errorf("%w: empty data", ErrImageInvalid)
	}
	mediaType := sniffImageMimeType(data, declared)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ImageDelivery{}, fmt.Errorf("%w: %v", ErrImageInvalid, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxImageDeliveryPixels {
		return ImageDelivery{}, fmt.Errorf("%w: unsupported dimensions %dx%d", ErrImageInvalid, cfg.Width, cfg.Height)
	}
	if t := formatMediaType(format); t != "" {
		mediaType = t
	}
	out := ImageDelivery{
		Data: data, MediaType: mediaType, RawBytes: len(data), Base64Bytes: ImageBase64Len(len(data)),
		Width: cfg.Width, Height: cfg.Height, OriginalBytes: len(data), OriginalWidth: cfg.Width,
		OriginalHeight: cfg.Height, OriginalType: mediaType, Recipient: "model", CoordinateScale: 1, CoordinateScaleX: 1, CoordinateScaleY: 1,
	}
	// Decode fully: a truncated file has a valid header but providers
	// reject it with a 400 that would break the conversation.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImageDelivery{}, fmt.Errorf("%w: %v", ErrImageInvalid, err)
	}
	if len(data) <= MaxImageDeliveryBytes {
		out.SHA256 = imageDeliveryHash(data)
		return out, nil
	}
	preview, w, h, err := encodePreview(img, MaxImageDeliveryBytes)
	if err != nil {
		return ImageDelivery{}, fmt.Errorf("image is %d bytes, over the %d byte limit, and no preview fits: %w", len(data), MaxImageDeliveryBytes, err)
	}
	out.Data, out.MediaType, out.Preview = preview, "image/jpeg", true
	out.RawBytes, out.Base64Bytes, out.Width, out.Height = len(preview), ImageBase64Len(len(preview)), w, h
	out.CoordinateScale = float64(cfg.Width) / float64(w)
	out.CoordinateScaleX, out.CoordinateScaleY = float64(cfg.Width)/float64(w), float64(cfg.Height)/float64(h)
	out.SHA256 = imageDeliveryHash(preview)
	return out, nil
}

func imageDeliveryHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Note is a one-line, model-facing statement of what was delivered.
func (d ImageDelivery) Note() string {
	if !d.Preview {
		return fmt.Sprintf("Image delivered to the model only (not shown to the user): %s %dx%d, %d bytes (%d base64).", d.MediaType, d.Width, d.Height, d.RawBytes, d.Base64Bytes)
	}
	return fmt.Sprintf("Preview delivered to the model only (not shown to the user): the original %s %dx%d is %d bytes, over the %d byte image limit, so a %s %dx%d preview of %d bytes (%d base64) was sent. The original file is unchanged; small text may be less legible. Convert preview coordinates to original pixels with x * %.6g and y * %.6g.",
		d.OriginalType, d.OriginalWidth, d.OriginalHeight, d.OriginalBytes, MaxImageDeliveryBytes, d.MediaType, d.Width, d.Height, d.RawBytes, d.Base64Bytes, d.CoordinateScaleX, d.CoordinateScaleY)
}

func formatMediaType(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	}
	return ""
}

// encodePreview shrinks img until its JPEG encoding fits limit.
func encodePreview(img image.Image, limit int) ([]byte, int, int, error) {
	b := img.Bounds()
	width := min(b.Dx(), previewMaxWidth)
	for {
		height := max(1, b.Dy()*width/b.Dx())
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		// JPEG has no alpha; flatten transparent pixels onto white.
		draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		scaled := img
		if width != b.Dx() {
			scaled = boxScale(img, width, height)
		}
		draw.Draw(dst, dst.Bounds(), scaled, scaled.Bounds().Min, draw.Over)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: previewQuality}); err != nil {
			return nil, 0, 0, err
		}
		if buf.Len() <= limit {
			return buf.Bytes(), width, height, nil
		}
		if width <= previewMinWidth {
			return nil, 0, 0, fmt.Errorf("a %dx%d preview is still %d bytes", width, height, buf.Len())
		}
		width = max(previewMinWidth, width*3/4)
	}
}

// boxScale averages each destination pixel's source box, keeping text
// strokes legible instead of dropping them between samples.
func boxScale(img image.Image, dstW, dstH int) *image.RGBA {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	// RGBA() supplies premultiplied channels; keep that representation so
	// transparent edges are not multiplied by alpha a second time.
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := range dstH {
		y0 := dy * srcH / dstH
		y1 := max(y0+1, (dy+1)*srcH/dstH)
		for dx := range dstW {
			x0 := dx * srcW / dstW
			x1 := max(x0+1, (dx+1)*srcW/dstW)
			var r, g, bl, a, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a, n = r+cr>>8, g+cg>>8, bl+cb>>8, a+ca>>8, n+1
				}
			}
			i := dst.PixOffset(dx, dy)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
		}
	}
	return dst
}

// EnforceImageDelivery applies the delivery contract to any tool response
// that carries an image. Text responses pass through unchanged. An image
// that cannot be delivered becomes a text response that says so; the text
// the tool produced is kept so a structured result is never lost. When the
// tool's text is a JSON object the note is added as a field, so callers
// that parse it keep working.
func EnforceImageDelivery(resp fantasy.ToolResponse) fantasy.ToolResponse {
	if resp.Type != "image" || len(resp.Data) == 0 {
		return resp
	}
	d, err := PrepareImageForDelivery(resp.Data, resp.MediaType)
	if err != nil {
		out := resp
		out.Type, out.Data, out.MediaType = "text", nil, ""
		out.Content = withDeliveryNote(resp.Content, "Image not delivered: "+err.Error())
		out.Metadata = withImageDeliveryMeta(resp.Metadata, map[string]any{"delivered": false, "error": err.Error()})
		return out
	}
	var previous struct {
		Delivery ImageDelivery `json:"image_delivery"`
	}
	if json.Unmarshal([]byte(resp.Metadata), &previous) == nil {
		old := previous.Delivery
		// Preserve a prior validated transform only when it describes these
		// exact delivered bytes and dimensions. Repeated wrapping is inert.
		if old.Preview && old.SHA256 == d.SHA256 && old.Width == d.Width && old.Height == d.Height && old.OriginalWidth > 0 && old.OriginalHeight > 0 {
			d = old
			d.Data, d.Recipient = resp.Data, "model"
		}
	}
	resp.Data, resp.MediaType = d.Data, d.MediaType
	if d.Preview {
		resp.Content = withDeliveryNote(resp.Content, d.Note())
	}
	resp.Metadata = withImageDeliveryMeta(resp.Metadata, d)
	return resp
}

func withDeliveryNote(content, note string) string {
	if strings.Contains(content, note) {
		return content
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		if out, err := sjson.Set(content, "image_delivery_note", note); err == nil {
			return out
		}
	}
	if content == "" {
		return note
	}
	return content + "\n\n" + note
}

// WithImageDelivery wraps a tool so every image it returns obeys the
// delivery contract, whichever tool produced it (view, computer,
// remote_support, browser, MCP).
func WithImageDelivery(tool fantasy.AgentTool) fantasy.AgentTool {
	return &imageDeliveryTool{tool}
}

type imageDeliveryTool struct{ fantasy.AgentTool }

func (t *imageDeliveryTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	r, err := t.AgentTool.Run(ctx, call)
	if err != nil {
		return r, err
	}
	return EnforceImageDelivery(r), nil
}

func withImageDeliveryMeta(meta string, value any) string {
	if meta == "" {
		meta = "{}"
	}
	out, err := sjson.Set(meta, "image_delivery", value)
	if err != nil {
		// Metadata that is not a JSON object is left untouched.
		return meta
	}
	return out
}

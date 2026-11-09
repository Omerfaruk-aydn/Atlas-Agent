package tools

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

//go:embed image_compare.md
var imageCompareDescription string

// ImageCompareToolName is the image comparison tool.
const ImageCompareToolName = "image_compare"

const (
	imageCompareMaxPixels = 8_000_000
	imageCompareMaxBytes  = 32 << 20
	imageCompareGrid      = 3
	defaultCompareDelta   = 16
)

// ImageCompareParams is the input of image_compare.
type ImageCompareParams struct {
	ReferencePath string `json:"reference_path" description:"The image to match: a mockup or an earlier screenshot, relative to the project or absolute."`
	CandidatePath string `json:"candidate_path" description:"The image to check against it."`
	Tolerance     *int   `json:"tolerance,omitempty" description:"Largest per-channel difference (0-255) that still counts as the same. Default 16."`
}

// ImageComparison is the measured difference between two images.
type ImageComparison struct {
	Width, Height int
	ChangedRatio  float64
	MeanDelta     float64
	Grid          [imageCompareGrid][imageCompareGrid]float64
	Diff          *image.RGBA
}

// CompareImages measures how much candidate differs from reference after
// scaling it to the size of the reference.
func CompareImages(reference, candidate image.Image, tolerance int) ImageComparison {
	b := reference.Bounds()
	w, h := b.Dx(), b.Dy()
	scaled := boxScale(candidate, w, h)
	ref := boxScale(reference, w, h)
	diff := image.NewRGBA(image.Rect(0, 0, w, h))
	out := ImageComparison{Width: w, Height: h, Diff: diff}
	var changed, total [imageCompareGrid][imageCompareGrid]int
	var sum int64
	changedAll := 0
	for y := range h {
		gy := min(y*imageCompareGrid/h, imageCompareGrid-1)
		for x := range w {
			gx := min(x*imageCompareGrid/w, imageCompareGrid-1)
			i := ref.PixOffset(x, y)
			biggest := 0
			for c := range 3 {
				d := int(ref.Pix[i+c]) - int(scaled.Pix[i+c])
				if d < 0 {
					d = -d
				}
				sum += int64(d)
				biggest = max(biggest, d)
			}
			total[gy][gx]++
			if biggest > tolerance {
				changed[gy][gx]++
				changedAll++
				diff.SetRGBA(x, y, color.RGBA{R: 230, G: 40, B: 40, A: 255})
				continue
			}
			// Faded reference where nothing differs, so the red reads as the change.
			faded := func(v uint8) uint8 { return uint8(200 + int(v)/5) }
			diff.SetRGBA(x, y, color.RGBA{R: faded(ref.Pix[i]), G: faded(ref.Pix[i+1]), B: faded(ref.Pix[i+2]), A: 255})
		}
	}
	pixels := w * h
	out.ChangedRatio = float64(changedAll) / float64(pixels)
	out.MeanDelta = float64(sum) / float64(pixels*3) / 255
	for gy := range imageCompareGrid {
		for gx := range imageCompareGrid {
			if total[gy][gx] > 0 {
				out.Grid[gy][gx] = float64(changed[gy][gx]) / float64(total[gy][gx])
			}
		}
	}
	return out
}

func (c ImageComparison) report(diffPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Compared at %dx%d. %.1f%% of pixels differ; the average difference is %.1f%%.\n", c.Width, c.Height, c.ChangedRatio*100, c.MeanDelta*100)
	b.WriteString("Where (share of each region that differs, top to bottom, left to right):\n")
	for _, row := range c.Grid {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = fmt.Sprintf("%3.0f%%", v*100)
		}
		b.WriteString("  " + strings.Join(cells, "  ") + "\n")
	}
	switch {
	case c.ChangedRatio < 0.005:
		b.WriteString("Verdict: effectively identical.\n")
	case c.ChangedRatio < 0.05:
		b.WriteString("Verdict: close; look at the diff for small shifts, colours or text.\n")
	default:
		b.WriteString("Verdict: clearly different; open the diff to see what is off.\n")
	}
	if diffPath != "" {
		fmt.Fprintf(&b, "Diff image: %s (view it to see the differences in red).", diffPath)
	}
	return b.String()
}

func loadComparableImage(workingDir, name string) (image.Image, error) {
	path := resolvePath(workingDir, name)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if info.IsDir() || info.Size() > imageCompareMaxBytes {
		return nil, fmt.Errorf("%s is not an image file of a size that can be compared", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s is not a PNG, JPEG, GIF or WebP image: %w", name, err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width*config.Height > imageCompareMaxPixels {
		return nil, fmt.Errorf("%s is %dx%d; images up to 8 megapixels can be compared", name, config.Width, config.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return img, nil
}

// NewImageCompareTool builds image_compare.
func NewImageCompareTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ImageCompareToolName,
		imageCompareDescription,
		func(ctx context.Context, params ImageCompareParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ReferencePath == "" || params.CandidatePath == "" {
				return fantasy.NewTextErrorResponse("reference_path and candidate_path are required"), nil
			}
			tolerance := defaultCompareDelta
			if params.Tolerance != nil {
				tolerance = *params.Tolerance
			}
			if tolerance < 0 || tolerance > 255 {
				return fantasy.NewTextErrorResponse("tolerance must be between 0 and 255"), nil
			}
			reference, err := loadComparableImage(workingDir, params.ReferencePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			candidate, err := loadComparableImage(workingDir, params.CandidatePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
			}
			result := CompareImages(reference, candidate, tolerance)

			var encoded bytes.Buffer
			if err := png.Encode(&encoded, result.Diff); err != nil {
				return fantasy.NewTextResponse(result.report("")), nil
			}
			dir := filepath.Join(workingDir, ".atlas", "compare")
			name := fmt.Sprintf("diff-%s.png", imageDeliveryHash(encoded.Bytes()))
			diffPath := ""
			if err := os.MkdirAll(dir, 0o755); err == nil {
				if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0o644); err == nil {
					diffPath = filepath.ToSlash(filepath.Join(".atlas", "compare", name))
				}
			}
			return fantasy.NewTextResponse(result.report(diffPath)), nil
		},
	)
}

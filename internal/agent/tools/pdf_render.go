package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

//go:embed pdf_render.md
var pdfRenderDescription string

// PDFRenderToolName is the tool that rasterizes one PDF page.
const PDFRenderToolName = "pdf_render"

const (
	pdfRenderDefaultDPI = 110
	pdfRenderMaxDPI     = 200
	pdfRenderMaxPage    = 10_000
	pdfRenderMaxBytes   = 50 << 20
)

// PDFRenderParams is the input of pdf_render.
type PDFRenderParams struct {
	Path string `json:"path" description:"The PDF file, relative to the project or absolute."`
	Page *int   `json:"page,omitempty" description:"Page to render, starting at 1. Default 1."`
	DPI  *int   `json:"dpi,omitempty" description:"Resolution, 36-200. Default 110."`
}

// pdfRenderArgs is the pdftoppm command line for one page, written to
// outPrefix plus ".png".
func pdfRenderArgs(pdfPath, outPrefix string, page, dpi int) []string {
	p := strconv.Itoa(page)
	return []string{"-png", "-singlefile", "-r", strconv.Itoa(dpi), "-f", p, "-l", p, pdfPath, outPrefix}
}

// NewPDFRenderTool builds pdf_render.
func NewPDFRenderTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		PDFRenderToolName,
		pdfRenderDescription,
		func(ctx context.Context, params PDFRenderParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.Path) == "" {
				return fantasy.NewTextErrorResponse("path is required"), nil
			}
			page, dpi := 1, pdfRenderDefaultDPI
			if params.Page != nil {
				page = *params.Page
			}
			if params.DPI != nil {
				dpi = *params.DPI
			}
			if page < 1 || page > pdfRenderMaxPage {
				return fantasy.NewTextErrorResponse("page must be 1 or more"), nil
			}
			if dpi < 36 || dpi > pdfRenderMaxDPI {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("dpi must be between 36 and %d", pdfRenderMaxDPI)), nil
			}
			path := resolvePath(workingDir, params.Path)
			info, err := os.Stat(path)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("%s: %v", params.Path, err)), nil
			}
			if info.IsDir() || info.Size() > pdfRenderMaxBytes {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("%s is not a PDF file of a size that can be rendered", params.Path)), nil
			}
			converter, err := exec.LookPath("pdftoppm")
			if err != nil {
				return fantasy.NewTextErrorResponse("Rendering a PDF page requires pdftoppm (Poppler) on PATH; no converter was installed. Text can still be read with view."), nil
			}
			dir := filepath.Join(workingDir, ".atlas", "pdf")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			name := fmt.Sprintf("%s-p%d-%ddpi", sanitizePDFStem(stem), page, dpi)
			prefix := filepath.Join(dir, name)
			output, err := exec.CommandContext(ctx, converter, pdfRenderArgs(path, prefix, page, dpi)...).CombinedOutput()
			if ctx.Err() != nil {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Could not render page %d: %s", page, strings.TrimSpace(string(output)))), nil
			}
			out := prefix + ".png"
			if _, err := os.Stat(out); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Page %d is past the end of the document.", page)), nil
			}
			rel := filepath.ToSlash(filepath.Join(".atlas", "pdf", name+".png"))
			return fantasy.NewTextResponse(fmt.Sprintf("Rendered page %d at %d dpi: %s (open it with view to see the page).", page, dpi, rel)), nil
		},
	)
}

func sanitizePDFStem(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "page"
	}
	return b.String()
}

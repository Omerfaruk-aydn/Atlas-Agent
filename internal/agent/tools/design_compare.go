package tools

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // Reference sheets are JPEG.
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/screens"
	"golang.org/x/image/draw"
)

const (
	// compareHeight is the height of each side of a comparison picture.
	compareHeight = 1120
	// Rows of a reference sheet as screens.AppSheet lays them out.
	sheetPad, sheetCell, sheetRowGap = 24, 560, 32
)

// designReferences fetches store screenshots of shipped apps for a design
// file and keeps them with it, so compare can set the render beside them.
func designReferences(ctx context.Context, workingDir string, p DesignCanvasParams) fantasy.ToolResponse {
	if !design.ValidName(p.File) {
		return fantasy.NewTextErrorResponse("references: file is required, the design the references are for.")
	}
	resp := designAppshots(ctx, workingDir, p.File, p)
	if resp.IsError {
		return resp
	}
	resp.Content += "\nThese are kept with the design: action compare with file sets your render beside them. Write the two products you take from into the brief's References section, each with what you take.\n"
	return resp
}

// referenceImages are the reference pictures for a design, newest first:
// the path the call names, else every sheet references saved for the file.
func referenceImages(workingDir, file, path string) ([]string, error) {
	if path != "" {
		abs := filepath.Join(workingDir, filepath.FromSlash(path))
		if !strings.HasPrefix(filepath.Clean(abs), filepath.Clean(workingDir)+string(filepath.Separator)) {
			return nil, fmt.Errorf("compare: path must be inside the project")
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("compare: no image at %s", path)
		}
		return []string{abs}, nil
	}
	dir := filepath.Join(screens.Dir(workingDir, file), "refs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	type found struct {
		path string
		mod  int64
	}
	var all []found
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".jpg" && ext != ".jpeg" && ext != ".png") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		all = append(all, found{filepath.Join(dir, e.Name()), info.ModTime().UnixNano()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod > all[j].mod })
	out := make([]string, 0, len(all))
	for _, f := range all {
		out = append(out, f.path)
	}
	return out, nil
}

func decodeFile(path string) (image.Image, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	return img, err
}

// sheetRows cuts a reference sheet into its app rows; any other picture is
// one row.
func sheetRows(img image.Image) []image.Image {
	b := img.Bounds()
	rows := (b.Dy() - 2*sheetPad + sheetRowGap) / (sheetCell + sheetRowGap)
	if rows < 1 || 2*sheetPad+rows*sheetCell+(rows-1)*sheetRowGap != b.Dy() {
		return []image.Image{img}
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return []image.Image{img}
	}
	out := make([]image.Image, 0, rows)
	for r := range rows {
		y := b.Min.Y + sheetPad + r*(sheetCell+sheetRowGap)
		out = append(out, sub.SubImage(image.Rect(b.Min.X, y, b.Max.X, y+sheetCell)))
	}
	return out
}

func scaleTo(img image.Image, h int) *image.RGBA {
	b := img.Bounds()
	w := max(1, b.Dx()*h/max(1, b.Dy()))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// comparePicture lays the render on the left at full height and two rows of
// shipped screens on the right at the same scale of a phone.
func comparePicture(render image.Image, refs []image.Image) *image.RGBA {
	const gap, pad = 32, 24
	left := scaleTo(render, compareHeight)
	rowH := (compareHeight - gap) / 2
	var right []*image.RGBA
	for _, r := range refs {
		if len(right) == 2 {
			break
		}
		right = append(right, scaleTo(r, rowH))
	}
	rw := 0
	for _, r := range right {
		rw = max(rw, r.Bounds().Dx())
	}
	W := pad + left.Bounds().Dx() + gap*2 + rw + pad
	H := pad*2 + compareHeight
	out := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{0xe9, 0xec, 0xf1, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(out, left.Bounds().Add(image.Pt(pad, pad)), left, image.Point{}, draw.Src)
	x := pad + left.Bounds().Dx() + gap
	draw.Draw(out, image.Rect(x, pad, x+2, pad+compareHeight), &image.Uniform{color.RGBA{0x9a, 0xa0, 0xa8, 0xff}}, image.Point{}, draw.Src)
	for i, r := range right {
		at := image.Pt(x+gap, pad+i*(rowH+gap))
		draw.Draw(out, r.Bounds().Add(at), r, image.Point{}, draw.Src)
	}
	return out
}

func designCompareAction(workingDir string, p DesignCanvasParams) fantasy.ToolResponse {
	if !design.ValidName(p.File) {
		return fantasy.NewTextErrorResponse("compare: file is required, the design to compare.")
	}
	doc, _, err := design.NewStore(workingDir).Load(p.File)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("compare: no design named %q", p.File))
	}
	page := doc.PageByRef(p.Page)
	if p.Page == "" {
		// The first page with screens, not a Library page put first.
	pages:
		for _, pg := range doc.Pages {
			for _, root := range pg.Children {
				if design.IsScreen(root) {
					page = pg
					break pages
				}
			}
		}
	}
	if page == nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("compare: no page %q in %s", p.Page, p.File))
	}
	return designCompare(workingDir, doc, p.File, page, p.Path)
}

// designCompare sets the page's render beside the references and asks for a
// measured gap list, the review step a senior designer does against shipped
// work.
func designCompare(workingDir string, doc *design.Doc, file string, page *design.Page, ref string) fantasy.ToolResponse {
	base := file
	if len(doc.Pages) > 1 {
		base += "-" + design.Slug(page.Name)
	}
	renderPath := filepath.Join(workingDir, ".atlas", "design", "exports", base+".png")
	render, err := decodeFile(renderPath)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("compare: no render of page %q; call action render first", page.Name))
	}
	return compareWith(workingDir, file, render, base, ref)
}

func compareWith(workingDir, file string, render image.Image, base, ref string) fantasy.ToolResponse {
	paths, err := referenceImages(workingDir, file, ref)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	if len(paths) == 0 {
		return fantasy.NewTextErrorResponse("compare: no references for this design yet. Call action references with file and query (the kind of app, or two app names separated by ;), or give path to a reference image in the project.")
	}
	var rows []image.Image
	for _, p := range paths {
		img, err := decodeFile(p)
		if err != nil {
			continue
		}
		rows = append(rows, sheetRows(img)...)
	}
	if len(rows) == 0 {
		return fantasy.NewTextErrorResponse("compare: the reference images could not be read")
	}
	pic := comparePicture(render, rows)
	var buf bytes.Buffer
	if err := png.Encode(&buf, pic); err != nil {
		return fantasy.NewTextErrorResponse("compare: " + err.Error())
	}
	rel := filepath.ToSlash(filepath.Join(".atlas", "design", "exports", base+"-compare.png"))
	if err := os.WriteFile(filepath.Join(workingDir, filepath.FromSlash(rel)), buf.Bytes(), 0o644); err != nil {
		return fantasy.NewTextErrorResponse("compare: " + err.Error())
	}
	view := buf.Bytes()
	if scaled, err := computer.DownscaleScreenshot(view, 1568); err == nil && len(scaled.PNG) > 0 {
		view = scaled.PNG
	}
	resp := fantasy.NewImageResponse(view, "image/png")
	resp.Content = fmt.Sprintf("Saved %s. Left: your render. Right, past the rule: shipped apps from the references, at the height of a phone.\n%s", rel, compareQuestions)
	return resp
}

// compareQuestions turn "it looks worse" into measured gaps.
const compareQuestions = `Compare like an art director, not a fan. Measure each line on both sides, then write every gap as "Flaw: ... -> Fix: ..." in this round of .atlas/design/reviews/<file>.md, under a line "Reference: <the apps>":
1. The one dominant element: its height as a share of the screen, and how much room around it. Theirs vs yours.
2. Type: how many sizes, the ratio of the largest to the body size, the weight contrast. Is theirs calmer or louder?
3. Density: how many rows or items are visible above the tab bar. Where is your empty space doing nothing?
4. Colour: what share of the screen the accent owns; are their neutrals tinted?
5. Material: the subject's own thing they draw (map, photo, chart, ticket, illustration). What is yours?
6. Edges: side margins, the gap inside groups vs between groups, corner radii. Are yours as consistent?
7. Platform: status bar, title, list style, tab bar. Does yours read as the same kind of app?
Do not copy their layout, colours or illustrations: take the discipline, keep your own idea.`

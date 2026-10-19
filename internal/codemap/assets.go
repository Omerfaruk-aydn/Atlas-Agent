package codemap

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif"  // Registers GIF for image.DecodeConfig.
	_ "image/jpeg" // Registers JPEG for image.DecodeConfig.
	_ "image/png"  // Registers PNG for image.DecodeConfig.
	"path"
	"regexp"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"  // Registers BMP for image.DecodeConfig.
	_ "golang.org/x/image/webp" // Registers WebP for image.DecodeConfig.
)

// assetHeaderBytes is how much of an asset is read: enough for any image
// header, including a JPEG's frame after its metadata.
const assetHeaderBytes = 256 << 10

// ExtractAsset describes an image, font, media, model or data file from
// its first bytes and its size. A glTF model also links the textures and
// buffers it loads.
func ExtractAsset(file string, head []byte, size int64) File {
	out := File{Path: file, Language: LanguageOf(file), Hash: Hash(string(head)) + "-" + strconv.FormatInt(size, 10), Lines: 1}
	ext := strings.ToLower(path.Ext(file))
	a := &Asset{Bytes: size, Format: strings.TrimPrefix(ext, ".")}
	out.Asset = a
	switch ext {
	case ".svg":
		a.Width, a.Height = svgSize(head)
		svgLinks(&out, string(head))
	case ".ico":
		if len(head) >= 8 && binary.LittleEndian.Uint16(head[2:]) == 1 {
			a.Width, a.Height = int(head[6]), int(head[7])
			if a.Width == 0 {
				a.Width = 256
			}
			if a.Height == 0 {
				a.Height = 256
			}
		}
	case ".gltf":
		gltfLinks(&out, string(head))
	default:
		if out.Language == "image" {
			if cfg, format, err := image.DecodeConfig(bytes.NewReader(head)); err == nil {
				a.Width, a.Height, a.Format = cfg.Width, cfg.Height, format
			}
		}
	}
	return out
}

var (
	svgTagRe   = regexp.MustCompile(`(?s)<svg\b[^>]*>`)
	svgAttrRe  = regexp.MustCompile(`\b(width|height|viewBox)\s*=\s*["']([^"']+)["']`)
	svgHrefRe  = regexp.MustCompile(`(?:xlink:)?href\s*=\s*["']([^"'#][^"']*)["']`)
	gltfURIRe  = regexp.MustCompile(`"uri"\s*:\s*"([^"]+)"`)
	cssLengths = regexp.MustCompile(`^\s*([0-9.]+)\s*(px)?\s*$`)
)

// svgSize reads an SVG's width and height, or its viewBox.
func svgSize(head []byte) (int, int) {
	tag := svgTagRe.Find(head)
	if tag == nil {
		return 0, 0
	}
	var w, h int
	var box []string
	for _, m := range svgAttrRe.FindAllSubmatch(tag, -1) {
		v := string(m[2])
		switch string(m[1]) {
		case "width":
			w = cssPixels(v)
		case "height":
			h = cssPixels(v)
		case "viewBox":
			box = strings.Fields(strings.ReplaceAll(v, ",", " "))
		}
	}
	if (w == 0 || h == 0) && len(box) == 4 {
		bw, _ := strconv.ParseFloat(box[2], 64)
		bh, _ := strconv.ParseFloat(box[3], 64)
		w, h = int(bw+0.5), int(bh+0.5)
	}
	return w, h
}

func cssPixels(v string) int {
	m := cssLengths.FindStringSubmatch(v)
	if m == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	return int(f + 0.5)
}

// svgLinks records the images an SVG embeds.
func svgLinks(out *File, s string) {
	lines := newLineIndex(s)
	for _, m := range svgHrefRe.FindAllStringSubmatchIndex(s, -1) {
		if target := s[m[2]:m[3]]; !hasScheme(target) {
			addLink(out, FileLink{Target: target, Kind: LinkEmbeds, Line: lines.at(m[2])})
		}
	}
}

// gltfLinks records the buffers and textures a glTF model loads.
func gltfLinks(out *File, s string) {
	lines := newLineIndex(s)
	for _, m := range gltfURIRe.FindAllStringSubmatchIndex(s, -1) {
		if target := s[m[2]:m[3]]; !hasScheme(target) {
			addLink(out, FileLink{Target: target, Kind: LinkLoads, Line: lines.at(m[2])})
		}
	}
}

// formatBytes writes a size the way people read it.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatInt((n+512)>>10, 10) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

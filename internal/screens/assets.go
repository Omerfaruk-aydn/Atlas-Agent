package screens

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // Decoders for the sizes SaveAsset reports.
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
)

// AssetsDir is where a project's generated images live. Pages refer to them
// as assets/<file>, which resolves both in the studio and when the saved html
// is opened from its folder.
func AssetsDir(workDir, project string) string {
	return filepath.Join(Dir(workDir, project), "assets")
}

// Aspects maps the words a model may use to the shape asked of the generator.
var Aspects = map[string]string{
	"square":    "1:1",
	"portrait":  "3:4",
	"tall":      "9:16",
	"landscape": "4:3",
	"wide":      "16:9",
}

// SaveAsset writes a generated image into the project and returns the src to
// use in a screen, plus its pixel size.
func SaveAsset(workDir, project, name string, data []byte, mime string) (src string, w, h int, err error) {
	if len(data) == 0 {
		return "", 0, 0, fmt.Errorf("image: empty data")
	}
	ext := ".png"
	if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
		ext = ".jpg"
	} else if strings.Contains(mime, "webp") {
		ext = ".webp"
	}
	file := design.Slug(strings.TrimSuffix(name, filepath.Ext(name)))
	if file == "" {
		return "", 0, 0, fmt.Errorf("image: name the image, e.g. \"hero-harbour\"")
	}
	dir := AssetsDir(workDir, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, 0, err
	}
	// Replace a same-named image of another type so the src stays unambiguous.
	for _, other := range []string{".png", ".jpg", ".webp"} {
		if other != ext {
			_ = os.Remove(filepath.Join(dir, file+other))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, file+ext), data, 0o644); err != nil {
		return "", 0, 0, err
	}
	if cfg, _, derr := image.DecodeConfig(bytes.NewReader(data)); derr == nil {
		w, h = cfg.Width, cfg.Height
	}
	return "assets/" + file + ext, w, h, nil
}

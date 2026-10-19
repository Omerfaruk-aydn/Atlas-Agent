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


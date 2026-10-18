package codemap

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxSignature bounds the stored first line of a definition.
const maxSignature = 160

// classLike kinds contain methods and other definitions.
var classLike = map[string]bool{"class": true, "struct": true, "interface": true, "impl": true, "namespace": true, "enum": true}

// continuation tokens can start a line that still belongs to the
// definition header above it.
var continuation = words("{ ) ] . , : => -> extends implements where throws with ? && || + - * | & = < >")

// Hash returns the content hash the map keys files by.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:16])
}


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


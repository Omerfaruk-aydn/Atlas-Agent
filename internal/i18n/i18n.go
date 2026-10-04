// Package i18n provides embedded, per-interface translations without network IO.
package i18n

import (
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
)

// Language describes a supported locale using a stable code and native name.
type Language struct {
	Code string
	Name string
	RTL  bool
}

// Translator owns one interface's locale and permits concurrent readers.
type Translator struct{ code atomic.Pointer[string] }

// New creates an independent interface translator with English fallback.
func New(code string) *Translator {
	t := &Translator{}
	t.Set(code)
	return t
}

// Set atomically changes the language for subsequent interface reads.
func (t *Translator) Set(code string) {
	if !Supported(code) {
		code = "en"
	}
	t.code.Store(&code)
}

// Code returns the current language, including for an uninitialized receiver.
func (t *Translator) Code() string {
	if t != nil {
		if code := t.code.Load(); code != nil {
			return *code
		}
	}
	return "en"
}

// Text translates Atlas-authored text with this interface's language.
func (t *Translator) Text(source string) string { return Text(t.Code(), source) }

var languages = []Language{
	{Code: "en", Name: "English"},
	{Code: "tr", Name: "Türkçe"},
	{Code: "de", Name: "Deutsch"},
	{Code: "fr", Name: "Français"},
	{Code: "it", Name: "Italiano"},
	{Code: "ar", Name: "العربية", RTL: true},
}

//go:embed catalog.txt
var source string

var catalogs, catalogError = readCatalogs(source)

// Languages returns an independent copy in picker order.
func Languages() []Language { return slices.Clone(languages) }

// Supported reports whether code is an explicitly supported locale.
func Supported(code string) bool {
	return slices.ContainsFunc(languages, func(l Language) bool { return l.Code == code })
}

// Text translates Atlas-authored source text, falling back to English.
// Callers must pass source literals, never arbitrary user or tool content.
func Text(code, text string) string {
	if catalogError != nil || code == "" || code == "en" {
		return text
	}
	if translated, ok := catalogs[code][text]; ok {
		return translated
	}
	return text
}

// Has reports explicit translation coverage for a source message.
func Has(code, text string) bool {
	_, ok := catalogs[code][text]
	return ok
}

var formatPattern = regexp.MustCompile(`%(?:\[[0-9]+\])?[-+# 0]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[a-zA-Z%]`)

// formatVerbs ignores percentages in prose while preserving printf contracts.
func formatVerbs(text string) []string {
	var verbs []string
	for _, match := range formatPattern.FindAllStringIndex(text, -1) {
		if match[0] > 0 && text[match[0]-1] >= '0' && text[match[0]-1] <= '9' {
			continue
		}
		verbs = append(verbs, text[match[0]:match[1]])
	}
	return verbs
}

// ValidateCatalogs checks duplicate keys, complete rows, and format contracts.
func ValidateCatalogs() error { return catalogError }

func readCatalogs(data string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(languages))
	for _, lang := range languages {
		result[lang.Code] = map[string]string{}
	}
	for lineNo, line := range strings.Split(data, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		cells := strings.Split(line, "|")
		for i := range cells {
			cells[i] = strings.ReplaceAll(cells[i], `\u007c`, "|")
		}
		if len(cells) != len(languages) || cells[0] == "" {
			return nil, fmt.Errorf("invalid translation row %d", lineNo+1)
		}
		key := cells[0]
		if _, exists := result["en"][key]; exists {
			return nil, fmt.Errorf("duplicate translation key %q", key)
		}
		formats := formatVerbs(key)
		for i, lang := range languages {
			if cells[i] == "" || !slices.Equal(formats, formatVerbs(cells[i])) {
				return nil, fmt.Errorf("invalid %s translation for %q", lang.Code, key)
			}
			result[lang.Code][key] = cells[i]
		}
	}
	return result, nil
}

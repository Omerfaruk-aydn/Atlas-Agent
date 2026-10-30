package webverify

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	localURL   = regexp.MustCompile(`(?i)https?://(?:localhost|127(?:\.\d{1,3}){3}|\[::1\]|[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:localhost|test))(?::\d{1,5})?`)
	bareLocal  = regexp.MustCompile(`(?i)(?:^|[\s"'(])((?:localhost|127\.0\.0\.1):\d{2,5})\b`)
)

// DiscoverURL finds the address of the running product. Later texts win, so
// pass the request first and the most recent dev-server output last; a dev
// server prints its address when it starts. The origin is returned without
// a path.
func DiscoverURL(texts ...string) string {
	found := ""
	for _, text := range texts {
		text = ansiEscape.ReplaceAllString(text, "")
		if m := localURL.FindAllString(text, -1); len(m) > 0 {
			found = strings.ToLower(strings.TrimRight(m[len(m)-1], "/"))
		}
		if m := bareLocal.FindAllStringSubmatch(text, -1); len(m) > 0 {
			found = "http://" + strings.ToLower(m[len(m)-1][1])
		}
	}
	return found
}

// uiExtensions are file types that make up what a user sees.
var uiExtensions = map[string]bool{
	".html": true, ".htm": true, ".css": true, ".scss": true, ".sass": true,
	".less": true, ".tsx": true, ".jsx": true, ".vue": true, ".svelte": true,
	".astro": true, ".mdx": true, ".svg": true,
}

// scriptExtensions are ambiguous: front end or back end.
var scriptExtensions = map[string]bool{".js": true, ".ts": true, ".mjs": true}

// uiWords hint that a request is about something users see in a browser.
// They are matched as whole words, in English and Turkish.
var uiWords = []string{
	"web", "website", "webpage", "landing", "frontend", "front-end", "ui", "ux",
	"page", "layout", "responsive", "css", "html", "button", "form", "dashboard",
	"navbar", "modal", "component", "browser", "mobile", "screen", "style", "theme",
	"sayfa", "sayfası", "arayüz", "arayuz", "tasarım", "tasarim", "site", "sitesi",
	"buton", "düğme", "form", "ekran", "görünüm", "gorunum", "duyarlı", "menü",
}

var wordSplit = regexp.MustCompile(`[^\p{L}\p{N}-]+`)

// IsWebTask decides whether a request and the files it changed look like
// web or UI work. UI files alone are enough; a script file needs a UI word
// in the request, since it may as well be a server or a build script.
func IsWebTask(request string, changedFiles []string) bool {
	script := false
	for _, file := range changedFiles {
		ext := strings.ToLower(filepath.Ext(file))
		if uiExtensions[ext] {
			return true
		}
		if scriptExtensions[ext] {
			script = true
		}
	}
	if !script {
		return false
	}
	words := map[string]bool{}
	for _, w := range wordSplit.Split(strings.ToLower(request), -1) {
		words[w] = true
	}
	for _, hint := range uiWords {
		if words[hint] {
			return true
		}
	}
	return false
}

package codemap

import (
	"regexp"
	"strings"
)

// extractNotebook reads a Jupyter notebook. Its code cells are read as the
// kernel's language and its Markdown cells as a document, each line placed
// at the line of the notebook's JSON that holds it, so a definition's line
// is where it is in the file.
func extractNotebook(out *File, content string) {
	total := out.Lines
	code := make([]string, total+1)
	text := make([]string, total+1)
	cellType := map[string]string{}
	lang := "python"
	jsonWalk(content, func(keys []string, value string, line int) {
		switch {
		case len(keys) == 3 && keys[0] == "cells" && keys[2] == "cell_type":
			cellType[keys[1]] = value
		case len(keys) >= 3 && keys[0] == "cells" && keys[2] == "source":
			target := code
			switch cellType[keys[1]] {
			case "markdown":
				target = text
			case "code":
			default:
				return
			}
			for k, l := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
				if n := line + k; n >= 1 && n <= total {
					target[n] = l
				}
			}
		case len(keys) == 3 && keys[0] == "metadata" && (keys[1] == "kernelspec" || keys[1] == "language_info") && (keys[2] == "language" || keys[2] == "name"):
			if l := strings.ToLower(value); languages[l] != nil {
				lang = l
			}
		}
	})
	for n, l := range code {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "%") || strings.HasPrefix(t, "!") {
			// IPython magics and shell escapes.
			code[n] = ""
			if strings.HasPrefix(t, "!") {
				scanCommand(out, t[1:], n)
			}
		}
	}
	extractCode(out, languages[lang], strings.Join(code[1:], "\n"))
	if doc := strings.Join(text[1:], "\n"); strings.TrimSpace(doc) != "" {
		md := File{Path: out.Path, Language: "markdown", Lines: total}
		extractMarkdown(&md, text[1:], false)
		finishSections(&md)
		out.Symbols = append(out.Symbols, md.Symbols...)
		out.Refs = append(out.Refs, md.Refs...)
		out.Links = append(out.Links, md.Links...)
	}
}

var (
	godotResourceRe = regexp.MustCompile(`\bpath="([^"]+)"`)
	godotNodeRe     = regexp.MustCompile(`name="([^"]+)"`)
)

// extractGodot reads a Godot scene or resource: the scripts it attaches
// and the scenes, textures and sounds it loads.
func extractGodot(out *File, content string) {
	for i, line := range strings.Split(content, "\n") {
		for _, m := range godotResourceRe.FindAllStringSubmatch(line, -1) {
			target := m[1]
			if strings.HasSuffix(target, ".gd") || strings.HasSuffix(target, ".cs") {
				out.Imports = append(out.Imports, Import{Spec: target, Line: i + 1})
				continue
			}
			addLink(out, FileLink{Target: target, Kind: LinkLoads, Line: i + 1})
		}
		if strings.HasPrefix(line, "[node ") {
			if name := godotNodeRe.FindStringSubmatch(line); name != nil {
				out.Symbols = append(out.Symbols, Symbol{Name: name[1], Kind: "node", Line: i + 1, EndLine: i + 1, Signature: clipSignature(line), Exported: true})
			}
		}
	}
}

// Package documents extracts bounded, source-addressable document text.
package documents

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxDocumentBytes  = 50 * 1024 * 1024
	maxExtractedBytes = 2 * 1024 * 1024
)

type (
	Section struct {
		Source string `json:"source"`
		Text   string `json:"text"`
	}
	Result struct {
		extracted int
		Format    string    `json:"format"`
		Sections  []Section `json:"sections"`
		Warnings  []string  `json:"warnings,omitempty"`
		Truncated bool      `json:"truncated"`
	}
)

func (r *Result) add(section Section) {
	remaining := maxExtractedBytes - r.extracted
	if remaining <= len(section.Source) || len(r.Sections) >= 65536 {
		r.Truncated = true
		return
	}
	remaining -= len(section.Source)
	if len(section.Text) > remaining {
		section.Text = section.Text[:remaining]
		for !utf8.ValidString(section.Text) && len(section.Text) > 0 {
			section.Text = section.Text[:len(section.Text)-1]
		}
		r.Truncated = true
	}
	r.extracted += len(section.Source) + len(section.Text)
	r.Sections = append(r.Sections, section)
}

func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".docx", ".xlsx", ".pptx", ".ipynb":
		return true
	}
	return false
}

func Extract(ctx context.Context, path string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxDocumentBytes {
		return Result{}, errors.New("document must be a regular file up to 50 MiB")
	}
	ext := strings.ToLower(filepath.Ext(path))
	r := Result{Format: strings.TrimPrefix(ext, "."), Sections: []Section{}}
	if ext == ".pdf" {
		return pdf(ctx, path)
	}
	if ext == ".ipynb" {
		data, err := readLimited(path, maxDocumentBytes)
		if err != nil {
			return r, err
		}
		err = notebook(data, &r)
		return bounded(r), err
	}
	if ext != ".docx" && ext != ".xlsx" && ext != ".pptx" {
		return r, errors.New("unsupported document format")
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return r, errors.New("invalid Office document")
	}
	defer z.Close()
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		total += f.UncompressedSize64
		if len(files) >= 4096 || f.UncompressedSize64 > maxExtractedBytes || total > 64*1024*1024 {
			return r, errors.New("office archive expansion exceeds limits")
		}
		files[f.Name] = f
	}
	get := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, fmt.Errorf("missing document part: %s", name)
		}
		rd, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rd.Close()
		return io.ReadAll(io.LimitReader(rd, maxExtractedBytes+1))
	}
	switch ext {
	case ".docx":
		data, e := get("word/document.xml")
		if e != nil {
			return r, e
		}
		err = paragraphs(data, "paragraph", &r)
	case ".pptx":
		names := []string{}
		for name := range files {
			if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
				names = append(names, name)
			}
		}
		sort.Slice(names, func(i, j int) bool { return slideNumber(names[i]) < slideNumber(names[j]) })
		for _, name := range names {
			data, e := get(name)
			if e != nil {
				return r, e
			}
			if e = paragraphs(data, fmt.Sprintf("slide:%d/paragraph", slideNumber(name)), &r); e != nil {
				return r, e
			}
		}
	case ".xlsx":
		err = workbook(get, &r)
	}
	if err != nil {
		return r, fmt.Errorf("extract %s: %w", r.Format, err)
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	return bounded(r), nil
}

func slideNumber(path string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "slide"), ".xml"))
	return n
}

func paragraphs(data []byte, source string, r *Result) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	var text strings.Builder
	index := 0
	inText := false
	for {
		t, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch token := t.(type) {
		case xml.StartElement:
			switch token.Name.Local {
			case "t":
				inText = true
			case "tab":
				text.WriteString("\t")
			case "br":
				text.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				text.Write(token)
			}
		case xml.EndElement:
			switch token.Name.Local {
			case "t":
				inText = false
			case "p":
				index++
				if text.Len() > 0 {
					r.add(Section{Source: fmt.Sprintf("%s:%d", source, index), Text: text.String()})
					text.Reset()
				}
			}
		}
	}
	return nil
}

type richText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (t richText) value() string {
	v := t.Text
	for _, r := range t.Runs {
		v += r.Text
	}
	return v
}

func workbook(get func(string) ([]byte, error), r *Result) error {
	var shared struct {
		Items []richText `xml:"si"`
	}
	if data, err := get("xl/sharedStrings.xml"); err == nil {
		if err := xml.Unmarshal(data, &shared); err != nil {
			return err
		}
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	data, err := get("xl/workbook.xml")
	if err != nil {
		return err
	}
	if err := xml.Unmarshal(data, &book); err != nil {
		return err
	}
	var rel struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	data, err = get("xl/_rels/workbook.xml.rels")
	if err != nil {
		return err
	}
	if err := xml.Unmarshal(data, &rel); err != nil {
		return err
	}
	targets := map[string]string{}
	for _, v := range rel.Items {
		if v.Mode != "External" {
			target := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("xl/"+v.Target)), "/")
			if strings.HasPrefix(v.Target, "/") {
				target = strings.TrimPrefix(v.Target, "/")
			}
			if !strings.HasPrefix(target, "xl/") {
				return errors.New("invalid sheet target")
			}
			targets[v.ID] = target
		}
	}
	for _, sheet := range book.Sheets {
		target := targets[sheet.ID]
		if target == "" {
			return errors.New("sheet relationship missing")
		}
		data, err := get(target)
		if err != nil {
			return err
		}
		var doc struct {
			Rows []struct {
				Cells []struct {
					Ref     string   `xml:"r,attr"`
					Type    string   `xml:"t,attr"`
					Value   string   `xml:"v"`
					Inline  richText `xml:"is"`
					Formula string   `xml:"f"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			return err
		}
		for _, row := range doc.Rows {
			for _, cell := range row.Cells {
				v := cell.Value
				if cell.Type == "s" {
					index, err := strconv.Atoi(v)
					if err != nil || index < 0 || index >= len(shared.Items) {
						return errors.New("invalid shared string index")
					}
					v = shared.Items[index].value()
				}
				if cell.Type == "inlineStr" {
					v = cell.Inline.value()
				}
				if cell.Formula != "" {
					v += " [formula: " + cell.Formula + "; cached value, not recalculated]"
				}
				if v != "" {
					r.add(Section{Source: "sheet:" + sheet.Name + "/cell:" + cell.Ref, Text: v})
				}
			}
		}
	}
	return nil
}

func notebook(data []byte, r *Result) error {
	var doc struct {
		Format int `json:"nbformat"`
		Cells  []struct {
			Type    string          `json:"cell_type"`
			Source  json.RawMessage `json:"source"`
			Outputs []struct {
				Text json.RawMessage `json:"text"`
			} `json:"outputs"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Format != 4 {
		return errors.New("only notebook format 4 is supported")
	}
	join := func(raw json.RawMessage) string {
		var lines []string
		if json.Unmarshal(raw, &lines) == nil {
			return strings.Join(lines, "")
		}
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	for i, cell := range doc.Cells {
		r.add(Section{Source: fmt.Sprintf("cell:%d/%s", i+1, cell.Type), Text: join(cell.Source)})
		for j, out := range cell.Outputs {
			if text := join(out.Text); text != "" {
				r.add(Section{Source: fmt.Sprintf("cell:%d/output:%d", i+1, j+1), Text: text})
			}
		}
	}
	r.Warnings = append(r.Warnings, "Notebook code was not executed; rich outputs and embedded images are not extracted.")
	return nil
}

func bounded(r Result) Result {
	total := 0
	sections := []Section{}
	for _, s := range r.Sections {
		n := len(s.Source) + len(s.Text)
		if total+n > maxExtractedBytes {
			r.Truncated = true
			break
		}
		total += n
		sections = append(sections, s)
	}
	r.Sections = sections
	if r.Truncated {
		r.Warnings = append(r.Warnings, "Extraction reached its 2 MiB output limit.")
	}
	return r
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxExtractedBytes {
		b.overflow = true
		return 0, errors.New("PDF output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func pdf(ctx context.Context, path string) (Result, error) {
	r := Result{Format: "pdf", Sections: []Section{}}
	converter, err := exec.LookPath("pdftotext")
	if err != nil {
		return r, errors.New("PDF extraction requires pdftotext (Poppler) on PATH; no converter was installed")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return r, err
	}
	command := exec.CommandContext(ctx, converter, "-layout", "-enc", "UTF-8", path, "-")
	var out limitedBuffer
	command.Stdout = &out
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		if out.overflow {
			return r, errors.New("PDF extraction exceeds 2 MiB")
		}
		return r, errors.New("PDF conversion failed; file may be encrypted or invalid")
	}
	pages := strings.Split(strings.TrimSuffix(out.String(), "\f"), "\f")
	empty := 0
	for i, page := range pages {
		if strings.TrimSpace(page) == "" {
			empty++
		}
		r.add(Section{Source: fmt.Sprintf("page:%d", i+1), Text: strings.TrimSpace(page)})
	}
	if empty > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d pages had no extractable text; scanned pages may require OCR.", empty))
	}
	r.Warnings = append(r.Warnings, "Text extraction does not verify visual completeness; images and layout may contain additional information.")
	return bounded(r), nil
}

func (r Result) Text() string {
	var b strings.Builder
	for _, s := range r.Sections {
		fmt.Fprintf(&b, "[%s]\n%s\n", s.Source, s.Text)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "WARNING: %s\n", w)
	}
	return b.String()
}

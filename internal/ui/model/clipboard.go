package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clipboard"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/documents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/fsext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/media"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	_ "golang.org/x/image/webp"
)

const maxClipboardFiles = 32

type clipboardMsg struct {
	gen         uint64
	text        string
	attachments []message.Attachment
	err         error
}

func (m *UI) pasteClipboard() tea.Cmd {
	gen := m.draftGen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if files, err := clipboard.ReadFiles(); err == nil && len(files) > 0 {
			return prepareFiles(ctx, gen, files)
		}
		if data, err := clipboard.Read(clipboard.FormatImage); err == nil && len(data) > 0 {
			if int64(len(data)) > common.MaxAttachmentSize {
				return clipboardMsg{gen: gen, err: errors.New("image exceeds 5 MB")}
			}
			if err := validateClipboardImage(data); err != nil {
				return clipboardMsg{gen: gen, err: err}
			}
			name := fmt.Sprintf("clipboard_%d.png", time.Now().UnixNano())
			return clipboardMsg{gen: gen, attachments: []message.Attachment{{FileName: name, FilePath: name, MimeType: mimeOf(data), Content: data}}}
		}
		if data, err := clipboard.Read(clipboard.FormatText); err == nil && len(data) > 0 {
			return preparePaste(ctx, gen, string(data))
		}
		return clipboardMsg{gen: gen, err: clipboard.ErrEmpty}
	}
}

func preparePaste(ctx context.Context, gen uint64, text string) clipboardMsg {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return clipboardMsg{gen: gen, text: text}
	}
	if int64(len(text)) > common.MaxAttachmentSize {
		return clipboardMsg{gen: gen, err: errors.New("paste exceeds 5 MB")}
	}
	paths := clipboardPaths(text)
	allFiles := len(paths) > 0
	for _, path := range paths {
		if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
			allFiles = false
			break
		}
	}
	if allFiles {
		return prepareFiles(ctx, gen, paths)
	}
	if hasPasteExceededThreshold(tea.PasteMsg{Content: text}) {
		name := fmt.Sprintf("paste_%d.txt", time.Now().UnixNano())
		return clipboardMsg{gen: gen, attachments: []message.Attachment{{FileName: name, FilePath: name, MimeType: "text/plain", Content: []byte(text)}}}
	}
	return clipboardMsg{gen: gen, text: text}
}

func clipboardPaths(text string) []string {
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		path := strings.Trim(strings.TrimSpace(line), "\"'")
		if strings.HasPrefix(path, "file://") {
			u, err := url.Parse(path)
			if err != nil || u.Host != "" && u.Host != "localhost" {
				paths = nil
				break
			}
			path = filepath.FromSlash(u.Path)
			if len(path) > 3 && path[0] == '\\' && path[2] == ':' {
				path = path[1:]
			}
		}
		if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
			paths = nil
			break
		}
		paths = append(paths, path)
	}
	if len(paths) > 0 {
		return paths
	}
	return fsext.ParsePastedFiles(text)
}

func prepareFiles(ctx context.Context, gen uint64, paths []string) clipboardMsg {
	result := clipboardMsg{gen: gen}
	if len(paths) > maxClipboardFiles {
		result.err = errors.New("a paste can contain at most 32 files")
		return result
	}
	var total int64
	seen := make(map[string]bool)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			result.err = err
			break
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			result.err = err
			continue
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		attachment, err := prepareAttachment(ctx, abs)
		if err != nil {
			result.err = fmt.Errorf("%s: %w", filepath.Base(abs), err)
			continue
		}
		total += int64(len(attachment.Content))
		if total > 20*1024*1024 {
			result.err = errors.New("combined attachments exceed 20 MB")
			break
		}
		result.attachments = append(result.attachments, attachment)
	}
	return result
}

// prepareAttachment makes provider-compatible content instead of forwarding
// unsupported binary formats directly to a language model.
func prepareAttachment(ctx context.Context, path string) (message.Attachment, error) {
	a := message.Attachment{FilePath: path, FileName: filepath.Base(path)}
	st, err := os.Stat(path)
	if err != nil {
		return a, err
	}
	if !st.Mode().IsRegular() {
		return a, errors.New("only regular files can be attached")
	}
	if media.Supported(path) {
		if st.Size() > media.MaxVideoBytes {
			return a, errors.New("video exceeds 2 GB")
		}
		a.MimeType = "text/plain"
		a.Content = []byte(fmt.Sprintf("Local video attachment: %q (%d bytes). The video has not been decoded yet. Use the video tool with file_path to inspect metadata, sample timestamped frames or transcribe its audio. Report only evidence actually obtained.", path, st.Size()))
		return a, nil
	}
	if st.Size() > common.MaxAttachmentSize {
		return a, errors.New("file exceeds 5 MB")
	}
	if documents.Supported(path) {
		result, err := documents.Extract(ctx, path)
		if err != nil {
			return a, err
		}
		a.MimeType = "text/plain"
		a.Content = []byte(result.Text())
		if int64(len(a.Content)) > common.MaxAttachmentSize {
			return a, errors.New("extracted document exceeds 5 MB")
		}
		return a, nil
	}
	a.Content, err = media.ReadBounded(path, common.MaxAttachmentSize)
	if err != nil {
		return a, err
	}
	a.MimeType = http.DetectContentType(a.Content[:min(512, len(a.Content))])
	if a.IsImage() {
		switch a.MimeType {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
			if err := validateClipboardImage(a.Content); err != nil {
				return a, err
			}
			return a, nil
		}
	}
	if utf8.Valid(a.Content) && !strings.ContainsRune(string(a.Content), '\x00') {
		a.MimeType = "text/plain"
		return a, nil
	}
	a.MimeType = "text/plain"
	a.Content = []byte(fmt.Sprintf("Local binary file attachment: %q (%d bytes). Its binary content was not sent to the model. Use a suitable file inspection tool when needed.", path, st.Size()))
	return a, nil
}

func validateClipboardImage(data []byte) error {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return errors.New("image header is invalid or unsupported")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return errors.New("image dimensions exceed the supported limit")
	}
	return nil
}

func (m *UI) handleClipboard(msg clipboardMsg) tea.Cmd {
	if msg.gen != m.draftGen {
		return nil
	}
	var cmds []tea.Cmd
	var total int64
	for _, a := range m.attachments.List() {
		total += int64(len(a.Content))
	}
	for _, a := range msg.attachments {
		if a.IsImage() {
			if reason := m.imageSupportRefusal(); reason != "" {
				cmds = append(cmds, util.ReportWarn(reason))
				continue
			}
		}
		duplicate := false
		for _, existing := range m.attachments.List() {
			if existing.FilePath == a.FilePath && bytes.Equal(existing.Content, a.Content) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if len(m.attachments.List()) >= maxClipboardFiles || total+int64(len(a.Content)) > 20*1024*1024 {
			msg.err = errors.New("the composer is limited to 32 files and 20 MB of attachment content")
			break
		}
		total += int64(len(a.Content))
		m.attachments.Update(a)
	}
	if msg.text != "" {
		prev := m.textarea.Height()
		m.textarea.DeleteSelection()
		m.textarea.InsertString(msg.text)
		cmds = append(cmds, m.handleTextareaHeightChange(prev))
		m.checkBangModeAfterPaste()
	}
	if msg.err != nil {
		if errors.Is(msg.err, clipboard.ErrEmpty) {
			cmds = append(cmds, util.ReportInfo(m.com.Text("Clipboard contains no usable files, image or text.")))
		} else {
			cmds = append(cmds, util.ReportWarn(fmt.Sprintf(m.com.Text("Some clipboard content could not be attached: %v"), msg.err)))
		}
	}
	m.updateLayoutAndSize()
	return tea.Batch(cmds...)
}

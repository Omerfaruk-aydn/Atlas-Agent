package browser

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
)

type browserDownload struct {
	ID, Name, State string
	Started         time.Time
}

func (s *chromedpSession) armDownloads(ctx context.Context) error {
	tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
	if err != nil {
		return err
	}
	frames := map[string]bool{}
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		frame, _ := node["frame"].(map[string]any)
		id, _ := frame["id"].(string)
		if id != "" {
			frames[id] = true
		}
		children, _ := node["childFrames"].([]any)
		for _, child := range children {
			if child, ok := child.(map[string]any); ok {
				walk(child)
			}
		}
	}
	root, _ := tree["frameTree"].(map[string]any)
	walk(root)
	s.mu.Lock()
	s.downloadFrames, s.downloads = frames, map[string]browserDownload{}
	s.mu.Unlock()
	return nil
}

// Browser-level events are filtered to the frames armed by download_start;
// downloads from unrelated tabs cannot satisfy a workflow condition.
func (s *chromedpSession) handleDownloadEvent(event any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev := event.(type) {
	case *cdpbrowser.EventDownloadWillBegin:
		if !s.downloadFrames[string(ev.FrameID)] || len(s.downloads) >= 128 {
			return
		}
		s.downloads[ev.GUID] = browserDownload{ID: ev.GUID, Name: ev.SuggestedFilename, State: "inProgress", Started: time.Now()}
	case *cdpbrowser.EventDownloadProgress:
		if download, ok := s.downloads[ev.GUID]; ok {
			download.State = string(ev.State)
			s.downloads[ev.GUID] = download
		}
	}
}

func (s *chromedpSession) completedDownload(p Request) (browserDownload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found browserDownload
	for _, download := range s.downloads {
		if p.DownloadID != "" && download.ID != p.DownloadID {
			continue
		}
		if !download.Started.After(p.NewerThan) || download.Name != filepath.Base(p.Paths[0]) {
			continue
		}
		if download.State == "canceled" {
			return browserDownload{}, errors.New("download_canceled: the browser did not complete the transfer")
		}
		if download.State != "completed" {
			continue
		}
		if found.ID != "" {
			return browserDownload{}, errors.New("ambiguous_download: specify download_id")
		}
		found = download
	}
	return found, nil
}

package browser

import (
	"net/url"
	"time"
)

// NetworkEntry deliberately omits bodies, cookies, headers and query strings.
type NetworkEntry struct {
	RequestID  string    `json:"request_id"`
	URL        string    `json:"url,omitempty"`
	Status     int       `json:"status,omitempty"`
	Failed     bool      `json:"failed,omitempty"`
	Time       time.Time `json:"time"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL]"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (s *chromedpSession) appendNetwork(entry NetworkEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if started, ok := s.requests[entry.RequestID]; ok {
		entry.DurationMS = time.Since(started).Milliseconds()
	}
	s.network = append(s.network, entry)
	if len(s.network) > 200 {
		s.network = s.network[len(s.network)-200:]
	}
}

func (s *chromedpSession) startRequest(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requests == nil {
		s.requests = map[string]time.Time{}
	}
	if len(s.requests) >= 500 {
		s.requestTrackingIncomplete = true
		for key := range s.requests {
			delete(s.requests, key)
			break
		}
	}
	s.requests[id] = time.Now()
}

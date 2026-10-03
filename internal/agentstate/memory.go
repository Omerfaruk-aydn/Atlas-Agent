package agentstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type (
	Source struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	Memory struct {
		ID         string   `json:"id"`
		Text       string   `json:"text"`
		SessionID  string   `json:"session_id"`
		Sources    []Source `json:"sources"`
		RecordedAt int64    `json:"recorded_at"`
		ValidUntil int64    `json:"valid_until,omitempty"`
		Superseded bool     `json:"superseded"`
		Status     string   `json:"status,omitempty"`
	}
)

func ReadSource(root, path string) ([]byte, error) {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return nil, errors.New("source must be project-relative")
	}
	clean := filepath.Clean(path)
	if len(clean) > 4096 || strings.ContainsAny(clean, "*?:") {
		return nil, errors.New("source must be a bounded literal file path")
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, errors.New("source escapes project")
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("source symlinks are refused")
		}
	}
	info, err := os.Stat(current)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 512*1024 {
		return nil, errors.New("source exceeds 512 KiB or is not a file")
	}
	f, err := os.Open(current)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 512*1024+1))
	if len(data) > 512*1024 {
		return nil, errors.New("source exceeds size limit")
	}
	return data, err
}
func Fingerprint(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func SaveMemory(ctx context.Context, s *Store, ns, root string, m Memory, paths []string) error {
	if m.ID == "" || m.Text == "" || len(m.Text) > 4000 || m.SessionID == "" || len(paths) < 1 || len(paths) > 16 {
		return errors.New("memory requires text, originating session and 1-16 source files")
	}
	m.Sources = nil
	for _, path := range paths {
		data, err := ReadSource(root, path)
		if err != nil {
			return err
		}
		m.Sources = append(m.Sources, Source{Path: filepath.ToSlash(filepath.Clean(path)), SHA256: Fingerprint(data)})
	}
	m.RecordedAt = time.Now().Unix()
	m.Status = ""
	return s.Put(ctx, ns, m.ID, 0, m)
}

func Memories(ctx context.Context, s *Store, ns, root, query string) ([]Memory, error) {
	rows, err := s.List(ctx, ns)
	if err != nil {
		return nil, err
	}
	out := []Memory{}
	readBytes := 0
	for _, r := range rows {
		var m Memory
		if err := json.Unmarshal(r.Payload, &m); err != nil {
			return nil, err
		}
		if !strings.Contains(strings.ToLower(m.Text), strings.ToLower(query)) {
			continue
		}
		m.Status = "current"
		if m.Superseded {
			m.Status = "superseded"
		} else if m.ValidUntil > 0 && m.ValidUntil <= time.Now().Unix() {
			m.Status = "expired"
		} else {
			for _, source := range m.Sources {
				if readBytes >= 8*1024*1024 {
					m.Status = "unchecked"
					break
				}
				data, err := ReadSource(root, source.Path)
				readBytes += len(data)
				if err != nil || Fingerprint(data) != source.SHA256 {
					m.Status = "stale"
					break
				}
			}
		}
		out = append(out, m)
	}
	return out, nil
}

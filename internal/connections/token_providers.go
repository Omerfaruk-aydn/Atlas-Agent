package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var tokenConnectionSecrets = map[string]string{
	"slack":   slackToken,
	"discord": discordToken,
	"github":  githubToken,
	"notion":  notionToken,
}

var tokenTestURLs = map[string]string{
	"slack":   "https://slack.com/api/auth.test",
	"discord": "https://discord.com/api/v10/users/@me",
	"github":  "https://api.github.com/user",
	"notion":  "https://api.notion.com/v1/users/me",
}

func (s *Service) testTokenConnection(ctx context.Context, id string) error {
	secretName := tokenConnectionSecrets[id]
	token, err := s.secrets().Get(secretName)
	if err != nil || token == "" {
		return errors.New("save the connection token first")
	}
	endpoint := tokenTestURLs[id]
	if override := s.ProviderTestURLs[id]; override != "" {
		endpoint = override
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("could not create provider verification request")
	}
	switch id {
	case "discord":
		req.Header.Set("Authorization", "Bot "+token)
	default:
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	if id == "github" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	if id == "notion" {
		req.Header.Set("Notion-Version", "2022-06-28")
	}
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("provider could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("provider rejected the credentials")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	if id == "slack" {
		var result struct {
			OK bool `json:"ok"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil || !result.OK {
			return errors.New("Slack rejected the credentials")
		}
	}
	return nil
}

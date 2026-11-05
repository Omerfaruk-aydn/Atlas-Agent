package connections

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// ActionRequest is the common input for connected-content operations.
type ActionRequest struct {
	Action        string     `json:"action"`
	Channel       string     `json:"channel,omitempty"`
	Text          string     `json:"text,omitempty"`
	Query         string     `json:"query,omitempty"`
	Owner         string     `json:"owner,omitempty"`
	Repository    string     `json:"repository,omitempty"`
	Number        int        `json:"number,omitempty"`
	Title         string     `json:"title,omitempty"`
	To            string     `json:"to,omitempty"`
	Subject       string     `json:"subject,omitempty"`
	Start         string     `json:"start,omitempty"`
	End           string     `json:"end,omitempty"`
	MessageID     string     `json:"message_id,omitempty"`
	PageID        string     `json:"page_id,omitempty"`
	DatabaseID    string     `json:"database_id,omitempty"`
	FileID        string     `json:"file_id,omitempty"`
	DocumentID    string     `json:"document_id,omitempty"`
	SpreadsheetID string     `json:"spreadsheet_id,omitempty"`
	Range         string     `json:"range,omitempty"`
	VideoID       string     `json:"video_id,omitempty"`
	FilePath      string     `json:"file_path,omitempty"`
	Privacy       string     `json:"privacy,omitempty"`
	MIMEType      string     `json:"mime_type,omitempty"`
	Values        [][]string `json:"values,omitempty"`
	Limit         int        `json:"limit,omitempty"`
}

var actionAPIURLs = map[string]string{
	"slack": "https://slack.com/api", "discord": "https://discord.com/api/v10",
	"github": "https://api.github.com", "notion": "https://api.notion.com/v1",
	"google": "https://gmail.googleapis.com/gmail/v1/users/me", "outlook": "https://graph.microsoft.com/v1.0/me",
	"google-calendar": "https://www.googleapis.com",
	"google-drive":    "https://www.googleapis.com", "google-drive-upload": "https://www.googleapis.com",
	"google-docs": "https://docs.googleapis.com/v1", "google-sheets": "https://sheets.googleapis.com/v4",
	"google-youtube": "https://www.googleapis.com/youtube/v3", "google-youtube-upload": "https://www.googleapis.com/upload/youtube/v3",
}

// Execute performs a real provider content operation using the encrypted
// credentials. Results are bounded JSON values, never authorization headers.
func (s *Service) Execute(ctx context.Context, id string, action ActionRequest) (any, error) {
	if len(action.Text) > 100000 || len(action.Query) > 2000 {
		return nil, errors.New("content is too large")
	}
	if action.Limit <= 0 || action.Limit > 50 {
		action.Limit = 10
	}
	token, err := s.actionToken(ctx, id)
	if err != nil {
		return nil, err
	}
	switch id {
	case "slack":
		return s.slackAction(ctx, token, action)
	case "discord":
		return s.discordAction(ctx, token, action)
	case "github":
		return s.githubAction(ctx, token, action)
	case "notion":
		return s.notionAction(ctx, token, action)
	case "google":
		return s.gmailCalendarAction(ctx, token, action)
	case "outlook":
		return s.outlookAction(ctx, token, action)
	default:
		return nil, errors.New("this connection has no content operations")
	}
}

func (s *Service) actionToken(ctx context.Context, id string) (string, error) {
	if name := tokenConnectionSecrets[id]; name != "" {
		token, err := s.secrets().Get(name)
		if err != nil || token == "" {
			return "", errors.New("connect the account in Settings > Connections first")
		}
		return token, nil
	}
	if id == "google" {
		config, token, err := s.googleConfig(ctx)
		if err != nil {
			return "", err
		}
		refreshed, err := config.TokenSource(ctx, token).Token()
		if err != nil {
			return "", errors.New("could not refresh the Google authorization")
		}
		if refreshed.AccessToken != token.AccessToken || refreshed.RefreshToken != token.RefreshToken {
			data, _ := json.Marshal(refreshed)
			if err := s.secrets().Set(googleToken, string(data)); err != nil {
				return "", err
			}
		}
		return refreshed.AccessToken, nil
	}
	if id == "outlook" {
		clientID, _ := s.secrets().Get(outlookClientID)
		clientSecret, _ := s.secrets().Get(outlookSecret)
		raw, _ := s.secrets().Get(outlookToken)
		var token oauth2.Token
		if clientID == "" || raw == "" || json.Unmarshal([]byte(raw), &token) != nil {
			return "", errors.New("authorize the Microsoft account first")
		}
		config := &oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: s.outlookEndpoint(), Scopes: outlookScopes}
		refreshed, err := config.TokenSource(ctx, &token).Token()
		if err != nil {
			return "", errors.New("could not refresh the Microsoft authorization")
		}
		if refreshed.AccessToken != token.AccessToken || refreshed.RefreshToken != token.RefreshToken {
			data, _ := json.Marshal(refreshed)
			if err := s.secrets().Set(outlookToken, string(data)); err != nil {
				return "", err
			}
		}
		return refreshed.AccessToken, nil
	}
	return "", errors.New("unknown connection")
}

func (s *Service) providerJSON(ctx context.Context, id, token, method, path string, body any) (any, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	base := actionAPIURLs[id]
	if override := s.ProviderAPIURLs[id]; override != "" {
		base = strings.TrimRight(override, "/")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, input)
	if err != nil {
		return nil, errors.New("could not create provider request")
	}
	req.Header.Set("Accept", "application/json")
	if id == "discord" {
		if user, ok := strings.CutPrefix(token, "user "); ok {
			req.Header.Set("Authorization", "Bearer "+user)
		} else {
			req.Header.Set("Authorization", "Bot "+token)
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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
		return nil, errors.New("provider could not be reached")
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 2<<20)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, limited)
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	var result any
	if err := json.NewDecoder(limited).Decode(&result); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]any{"status": "completed", "http_status": resp.StatusCode}, nil
		}
		return nil, errors.New("provider returned an invalid response")
	}
	if id == "slack" {
		if object, ok := result.(map[string]any); ok && object["ok"] == false {
			return nil, errors.New("the Slack workspace rejected the operation")
		}
	}
	return result, nil
}

func (s *Service) slackAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	switch a.Action {
	case "list_channels":
		return s.providerJSON(ctx, "slack", token, http.MethodGet, "/conversations.list?limit="+strconv.Itoa(a.Limit)+"&types=public_channel,private_channel", nil)
	case "read_messages":
		if a.Channel == "" {
			return nil, errors.New("channel is required")
		}
		return s.providerJSON(ctx, "slack", token, http.MethodGet, "/conversations.history?channel="+url.QueryEscape(a.Channel)+"&limit="+strconv.Itoa(a.Limit), nil)
	case "send_message":
		if a.Channel == "" || strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("channel and text are required")
		}
		return s.providerJSON(ctx, "slack", token, http.MethodPost, "/chat.postMessage", map[string]any{"channel": a.Channel, "text": a.Text})
	}
	return nil, errors.New("a Slack action must be list_channels, read_messages or send_message")
}

func (s *Service) discordAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	if a.Channel == "" {
		return nil, errors.New("channel is required")
	}
	path := "/channels/" + url.PathEscape(a.Channel) + "/messages"
	switch a.Action {
	case "read_messages":
		return s.providerJSON(ctx, "discord", token, http.MethodGet, path+"?limit="+strconv.Itoa(a.Limit), nil)
	case "send_message":
		if strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("text is required")
		}
		return s.providerJSON(ctx, "discord", token, http.MethodPost, path, map[string]any{"content": a.Text})
	}
	return nil, errors.New("a Discord action must be read_messages or send_message")
}

func (s *Service) githubAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	repoPath := ""
	if a.Owner != "" && a.Repository != "" {
		repoPath = "/repos/" + url.PathEscape(a.Owner) + "/" + url.PathEscape(a.Repository)
	}
	switch a.Action {
	case "list_repositories":
		return s.providerJSON(ctx, "github", token, http.MethodGet, "/user/repos?per_page="+strconv.Itoa(a.Limit)+"&sort=updated", nil)
	case "list_issues", "list_pull_requests":
		if repoPath == "" {
			return nil, errors.New("owner and repository are required")
		}
		suffix := "/issues?state=all&per_page=" + strconv.Itoa(a.Limit)
		if a.Action == "list_pull_requests" {
			suffix = "/pulls?state=all&per_page=" + strconv.Itoa(a.Limit)
		}
		return s.providerJSON(ctx, "github", token, http.MethodGet, repoPath+suffix, nil)
	case "create_issue", "create_pull_request":
		if repoPath == "" || strings.TrimSpace(a.Title) == "" {
			return nil, errors.New("owner, repository and title are required")
		}
		if a.Action == "create_issue" {
			return s.providerJSON(ctx, "github", token, http.MethodPost, repoPath+"/issues", map[string]any{"title": a.Title, "body": a.Text})
		}
		if a.Channel == "" || a.Query == "" {
			return nil, errors.New("query is the head branch and channel is the base branch for a pull request")
		}
		return s.providerJSON(ctx, "github", token, http.MethodPost, repoPath+"/pulls", map[string]any{"title": a.Title, "body": a.Text, "head": a.Query, "base": a.Channel})
	case "comment_issue":
		if repoPath == "" || a.Number <= 0 || strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("owner, repository, number and text are required")
		}
		return s.providerJSON(ctx, "github", token, http.MethodPost, repoPath+"/issues/"+strconv.Itoa(a.Number)+"/comments", map[string]any{"body": a.Text})
	}
	return nil, errors.New("unsupported GitHub action")
}

func (s *Service) notionAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	switch a.Action {
	case "search":
		return s.providerJSON(ctx, "notion", token, http.MethodPost, "/search", map[string]any{"query": a.Query, "page_size": a.Limit})
	case "get_page":
		if a.PageID == "" {
			return nil, errors.New("page_id is required")
		}
		return s.providerJSON(ctx, "notion", token, http.MethodGet, "/pages/"+url.PathEscape(a.PageID), nil)
	case "read_content":
		if a.PageID == "" {
			return nil, errors.New("page_id is required")
		}
		return s.providerJSON(ctx, "notion", token, http.MethodGet, "/blocks/"+url.PathEscape(a.PageID)+"/children?page_size="+strconv.Itoa(a.Limit), nil)
	case "query_database":
		if a.DatabaseID == "" {
			return nil, errors.New("database_id is required")
		}
		return s.providerJSON(ctx, "notion", token, http.MethodPost, "/databases/"+url.PathEscape(a.DatabaseID)+"/query", map[string]any{"page_size": a.Limit})
	case "create_page":
		if a.DatabaseID == "" || strings.TrimSpace(a.Title) == "" {
			return nil, errors.New("database_id and title are required")
		}
		return s.providerJSON(ctx, "notion", token, http.MethodPost, "/pages", map[string]any{
			"parent":     map[string]any{"database_id": a.DatabaseID},
			"properties": map[string]any{"title": map[string]any{"title": []any{map[string]any{"text": map[string]any{"content": a.Title}}}}},
		})
	case "append_content":
		if a.PageID == "" || strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("page_id and text are required")
		}
		return s.providerJSON(ctx, "notion", token, http.MethodPatch, "/blocks/"+url.PathEscape(a.PageID)+"/children", map[string]any{"children": []any{map[string]any{
			"object": "block", "type": "paragraph", "paragraph": map[string]any{"rich_text": []any{map[string]any{"text": map[string]any{"content": a.Text}}}},
		}}})
	}
	return nil, errors.New("unsupported Notion action")
}

func (s *Service) gmailCalendarAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	switch a.Action {
	case "list_messages":
		path := "/messages?maxResults=" + strconv.Itoa(a.Limit)
		if a.Query != "" {
			path += "&q=" + url.QueryEscape(a.Query)
		}
		return s.providerJSON(ctx, "google", token, http.MethodGet, path, nil)
	case "read_message":
		if a.MessageID == "" {
			return nil, errors.New("message_id is required")
		}
		return s.providerJSON(ctx, "google", token, http.MethodGet, "/messages/"+url.PathEscape(a.MessageID)+"?format=full", nil)
	case "send_email":
		if a.To == "" || a.Subject == "" || strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("to, subject and text are required")
		}
		raw := "To: " + a.To + "\r\nSubject: " + a.Subject + "\r\nContent-Type: text/plain; charset=UTF-8\r\nMIME-Version: 1.0\r\n\r\n" + a.Text
		return s.providerJSON(ctx, "google", token, http.MethodPost, "/messages/send", map[string]any{"raw": base64.RawURLEncoding.EncodeToString([]byte(raw))})
	case "list_events":
		return s.providerJSON(ctx, "google-calendar", token, http.MethodGet, "/calendar/v3/calendars/primary/events?maxResults="+strconv.Itoa(a.Limit)+"&singleEvents=true&orderBy=startTime&timeMin="+url.QueryEscape(time.Now().UTC().Format(time.RFC3339)), nil)
	case "create_event":
		if err := validateEvent(a); err != nil {
			return nil, err
		}
		return s.providerJSON(ctx, "google-calendar", token, http.MethodPost, "/calendar/v3/calendars/primary/events", calendarBody(a))
	}
	return s.googleWorkspaceAction(ctx, token, a)
}

func calendarBody(a ActionRequest) map[string]any {
	return map[string]any{"summary": a.Title, "description": a.Text, "start": map[string]any{"dateTime": a.Start}, "end": map[string]any{"dateTime": a.End}}
}

func validateEvent(a ActionRequest) error {
	start, startErr := time.Parse(time.RFC3339, a.Start)
	end, endErr := time.Parse(time.RFC3339, a.End)
	if strings.TrimSpace(a.Title) == "" || startErr != nil || endErr != nil || !end.After(start) {
		return errors.New("title and RFC3339 start/end times are required; end must be after start")
	}
	return nil
}

func (s *Service) outlookAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	switch a.Action {
	case "list_messages":
		path := "/messages?$top=" + strconv.Itoa(a.Limit)
		if a.Query != "" {
			path += "&$search=" + url.QueryEscape("\""+a.Query+"\"")
		}
		return s.providerJSON(ctx, "outlook", token, http.MethodGet, path, nil)
	case "read_message":
		if a.MessageID == "" {
			return nil, errors.New("message_id is required")
		}
		return s.providerJSON(ctx, "outlook", token, http.MethodGet, "/messages/"+url.PathEscape(a.MessageID), nil)
	case "send_email":
		if a.To == "" || a.Subject == "" || strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("to, subject and text are required")
		}
		return s.providerJSON(ctx, "outlook", token, http.MethodPost, "/sendMail", map[string]any{"message": map[string]any{
			"subject": a.Subject, "body": map[string]any{"contentType": "Text", "content": a.Text},
			"toRecipients": []any{map[string]any{"emailAddress": map[string]any{"address": a.To}}},
		}})
	case "list_events":
		return s.providerJSON(ctx, "outlook", token, http.MethodGet, "/calendar/events?$top="+strconv.Itoa(a.Limit)+"&$orderby=start/dateTime", nil)
	case "create_event":
		if err := validateEvent(a); err != nil {
			return nil, err
		}
		return s.providerJSON(ctx, "outlook", token, http.MethodPost, "/calendar/events", map[string]any{
			"subject": a.Title, "body": map[string]any{"contentType": "Text", "content": a.Text},
			"start": map[string]any{"dateTime": a.Start, "timeZone": "UTC"},
			"end":   map[string]any{"dateTime": a.End, "timeZone": "UTC"},
		})
	}
	return nil, errors.New("an Outlook action must be list_messages, read_message, send_email, list_events or create_event")
}

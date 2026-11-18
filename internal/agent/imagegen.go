package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	antigravityoauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/antigravity"
)

const (
	imageTimeout = 4 * time.Minute
	// The Codex backend only serves its own model names; the image tool runs
	// inside a normal turn of this model.
	chatGPTImageHost = "gpt-6.1-sol"
	// Antigravity's Cloud Code backend serves this image model to Google AI
	// accounts (checked 2026-11-13 on a g1-pro-tier login).
	antigravityImageModel = "gemini-3.1-flash-image"
	maxSSELine            = 64 << 20
)

// imageGenerator makes pictures with the subscriptions the person is already
// signed in to, so no image API key is needed. ChatGPT goes first, then
// Antigravity; ATLAS_IMAGE_PROVIDER=chatgpt|antigravity pins one.
func (c *coordinator) imageGenerator() tools.ImageGenerator {
	return func(ctx context.Context, prompt, aspect string) ([]byte, string, error) {
		ctx, cancel := context.WithTimeout(ctx, imageTimeout)
		defer cancel()

		order := []string{"chatgpt", "antigravity"}
		if only := strings.ToLower(strings.TrimSpace(os.Getenv("ATLAS_IMAGE_PROVIDER"))); only != "" {
			order = []string{only}
		}
		var errs []string
		for _, id := range order {
			cfg, ok := c.signedIn(ctx, id)
			if !ok {
				errs = append(errs, id+": not signed in")
				continue
			}
			var (
				data []byte
				mime string
				err  error
			)
			switch id {
			case "chatgpt":
				data, mime, err = chatGPTImage(ctx, cfg, prompt, aspect)
			case "antigravity":
				data, mime, err = antigravityImage(ctx, cfg, prompt, aspect)
			default:
				err = fmt.Errorf("unknown image provider")
			}
			if err == nil {
				return data, mime, nil
			}
			errs = append(errs, id+": "+err.Error())
		}
		return nil, "", errors.New(strings.Join(errs, "; "))
	}
}

// signedIn returns the provider's config with a usable access token, refreshing
// it first when it has expired.
func (c *coordinator) signedIn(ctx context.Context, id string) (config.ProviderConfig, bool) {
	cfg, ok := c.cfg.Config().Providers.Get(id)
	if !ok || cfg.OAuthToken == nil || cfg.OAuthToken.AccessToken == "" {
		return cfg, false
	}
	if cfg.OAuthToken.IsExpired() {
		if err := c.cfg.RefreshOAuthToken(ctx, config.ScopeGlobal, id); err != nil {
			return cfg, false
		}
		cfg, ok = c.cfg.Config().Providers.Get(id)
		if !ok || cfg.OAuthToken == nil {
			return cfg, false
		}
	}
	return cfg, true
}

func chatGPTImage(ctx context.Context, cfg config.ProviderConfig, prompt, aspect string) ([]byte, string, error) {
	size := "1024x1024"
	switch aspect {
	case "3:4", "9:16":
		size = "1024x1536"
	case "4:3", "16:9":
		size = "1536x1024"
	}
	opts := tools.ImageRequestFrom(ctx)
	instructions := "You make pictures. Call the image_generation tool once with the user's description. Add no text to the picture."
	if opts.Text {
		instructions = "You make pictures. Call the image_generation tool once with the user's description. Render every quoted text exactly as written, spelled correctly and legible; add no other text."
	}
	if opts.Designer {
		instructions = "You are a senior product designer making one app screen. If the subject is a real event, place or product, search the web first to get the facts right, and think about how the best real apps in this category look. Then call the image_generation tool once. Draw the screen the way a shipped App Store app looks: native components, one job per screen, generous space. Render every quoted text exactly as written, spelled correctly and legible; add no other text."
	}
	content := []any{map[string]any{"type": "input_text", "text": prompt}}
	for _, r := range opts.References {
		content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + cmpMime(r.Mime) + ";base64," + base64.StdEncoding.EncodeToString(r.Data)})
	}
	quality := "medium"
	if opts.Quality != "" {
		quality = opts.Quality
	}
	toolList := []any{map[string]any{"type": "image_generation", "size": size, "quality": quality}}
	if opts.Designer {
		toolList = append([]any{map[string]any{"type": "web_search"}}, toolList...)
	}
	build := func(tl []any) ([]byte, error) {
		return json.Marshal(map[string]any{
			"model":        chatGPTImageHost,
			"instructions": instructions,
			"input": []any{map[string]any{
				"type": "message", "role": "user",
				"content": content,
			}},
			"tools":       tl,
			"tool_choice": "auto",
			"stream":      true,
			"store":       false,
		})
	}
	body, err := build(toolList)
	if err != nil {
		return nil, "", err
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://chatgpt.com/backend-api/codex"
	}
	send := func(payload []byte) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Authorization", "Bearer "+cfg.OAuthToken.AccessToken)
		req.Header.Set("originator", "codex_cli_rs")
		if cfg.OAuthToken.AccountID != "" {
			req.Header.Set("chatgpt-account-id", cfg.OAuthToken.AccountID)
		}
		return http.DefaultClient.Do(req)
	}
	resp, err := send(body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusBadRequest && opts.Designer {
		// A backend that refuses web search still paints.
		resp.Body.Close()
		if body, err = build(toolList[len(toolList)-1:]); err != nil {
			return nil, "", err
		}
		if resp, err = send(body); err != nil {
			return nil, "", err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var said strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), maxSSELine)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  struct {
				Type   string `json:"type"`
				Result string `json:"result"`
			} `json:"item"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "response.output_item.done" && ev.Item.Type == "image_generation_call" && ev.Item.Result != "":
			img, err := base64.StdEncoding.DecodeString(ev.Item.Result)
			if err != nil {
				return nil, "", fmt.Errorf("decode image: %w", err)
			}
			return img, "image/png", nil
		case ev.Type == "response.output_text.delta":
			said.WriteString(ev.Delta)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, "", err
	}
	if said.Len() > 0 {
		return nil, "", fmt.Errorf("no image came back; the model said: %s", strings.TrimSpace(said.String()))
	}
	return nil, "", errors.New("no image came back")
}

func antigravityImage(ctx context.Context, cfg config.ProviderConfig, prompt, aspect string) ([]byte, string, error) {
	project := cfg.ExtraParams["project"]
	if project == "" {
		project = cfg.OAuthToken.AccountID
	}
	if project == "" {
		return nil, "", errors.New("the login has no Cloud project; sign in again")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" || base == "https://cloudcode-pa.googleapis.com" {
		base = "https://daily-cloudcode-pa.googleapis.com"
	}
	opts := tools.ImageRequestFrom(ctx)
	lead := "Generate an image, with no text in it: "
	if opts.Text {
		lead = "Generate an image. Render every quoted text exactly as written, spelled correctly and legible; add no other text. "
	}
	parts := []any{map[string]any{"text": lead + prompt}}
	for _, r := range opts.References {
		parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": cmpMime(r.Mime), "data": base64.StdEncoding.EncodeToString(r.Data)}})
	}
	gen := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	if aspect != "" && aspect != "1:1" {
		gen["imageConfig"] = map[string]any{"aspectRatio": aspect}
	}
	body, err := json.Marshal(map[string]any{
		"project":     project,
		"model":       antigravityImageModel,
		"userAgent":   "antigravity",
		"requestId":   "agent-" + uuid.NewString(),
		"requestType": "agent",
		"request": map[string]any{
			"contents":         []any{map[string]any{"role": "user", "parts": parts}},
			"generationConfig": gen,
		},
	})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1internal:streamGenerateContent?alt=sse", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+cfg.OAuthToken.AccessToken)
	for k, v := range antigravityoauth.RequestHeaders() {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var said strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), maxSSELine)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Response *struct {
				Candidates []struct {
					Content struct {
						Parts []struct {
							Text       string `json:"text"`
							InlineData *struct {
								MimeType string `json:"mimeType"`
								Data     string `json:"data"`
							} `json:"inlineData"`
						} `json:"parts"`
					} `json:"content"`
				} `json:"candidates"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &ev) != nil || ev.Response == nil {
			continue
		}
		for _, cand := range ev.Response.Candidates {
			for _, part := range cand.Content.Parts {
				if part.InlineData != nil && part.InlineData.Data != "" {
					img, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
					if err != nil {
						return nil, "", fmt.Errorf("decode image: %w", err)
					}
					return img, cmpMime(part.InlineData.MimeType), nil
				}
				said.WriteString(part.Text)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, "", err
	}
	if said.Len() > 0 {
		return nil, "", fmt.Errorf("no image came back; the model said: %s", strings.TrimSpace(said.String()))
	}
	return nil, "", errors.New("no image came back")
}

func cmpMime(m string) string {
	if m == "" {
		return "image/png"
	}
	return m
}

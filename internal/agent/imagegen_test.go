package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
)

func imageTestConfig(baseURL string) config.ProviderConfig {
	return config.ProviderConfig{
		BaseURL:     baseURL,
		ExtraParams: map[string]string{"project": "proj-1"},
		OAuthToken:  &oauth.Token{AccessToken: "tok", AccountID: "acct-1"},
	}
}

func TestChatGPTImageReadsToolResult(t *testing.T) {
	t.Parallel()
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/responses", r.URL.Path)
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		require.Equal(t, "acct-1", r.Header.Get("chatgpt-account-id"))
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &got))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\""+png+"\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer srv.Close()

	data, mime, err := chatGPTImage(context.Background(), imageTestConfig(srv.URL), "a ferry", "16:9")
	require.NoError(t, err)
	require.Equal(t, "image/png", mime)
	require.Equal(t, "\x89PNG fake", string(data))
	tool := got["tools"].([]any)[0].(map[string]any)
	require.Equal(t, "image_generation", tool["type"])
	require.Equal(t, "1536x1024", tool["size"])
}

func TestChatGPTImageSurfacesRefusal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"I can't make that.\"}\n\n")
	}))
	defer srv.Close()
	_, _, err := chatGPTImage(context.Background(), imageTestConfig(srv.URL), "x", "1:1")
	require.ErrorContains(t, err, "I can't make that.")

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"detail":"model not supported"}`)
	}))
	defer bad.Close()
	_, _, err = chatGPTImage(context.Background(), imageTestConfig(bad.URL), "x", "1:1")
	require.ErrorContains(t, err, "status 400")
}

func TestAntigravityImageReadsInlineData(t *testing.T) {
	t.Parallel()
	jpg := base64.StdEncoding.EncodeToString([]byte("\xff\xd8 fake jpeg"))
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1internal:streamGenerateContent", r.URL.Path)
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		require.NotEmpty(t, r.Header.Get("Client-Metadata"))
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &got))
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"here\"}]}}]}}\n\n")
		_, _ = io.WriteString(w, "data: {\"response\": {\"candidates\": [{\"content\": {\"parts\": [{\"inlineData\": {\"mimeType\": \"image/jpeg\", \"data\": \""+jpg+"\"}}]}}]}}\n\n")
	}))
	defer srv.Close()

	data, mime, err := antigravityImage(context.Background(), imageTestConfig(srv.URL), "a bakery", "9:16")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", mime)
	require.Equal(t, "\xff\xd8 fake jpeg", string(data))
	require.Equal(t, "proj-1", got["project"])
	require.Equal(t, antigravityImageModel, got["model"])
	gen := got["request"].(map[string]any)["generationConfig"].(map[string]any)
	require.Equal(t, "9:16", gen["imageConfig"].(map[string]any)["aspectRatio"])
}

func TestImageRequestAllowsTextAndReferences(t *testing.T) {
	t.Parallel()
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &got))
		if r.URL.Path == "/responses" {
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\""+png+"\"}}\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\""+png+"\"}}]}}]}}\n\n")
	}))
	defer srv.Close()
	ctx := tools.WithImageRequest(context.Background(), tools.ImageRequest{Text: true, Quality: "high", References: []tools.ImageRef{{Data: []byte("ref"), Mime: "image/png"}}})

	_, _, err := chatGPTImage(ctx, imageTestConfig(srv.URL), "a screen", "9:16")
	require.NoError(t, err)
	require.Contains(t, got["instructions"], "Render every quoted text exactly")
	content := got["input"].([]any)[0].(map[string]any)["content"].([]any)
	require.Len(t, content, 2)
	require.Equal(t, "input_image", content[1].(map[string]any)["type"])
	require.Equal(t, "data:image/png;base64,cmVm", content[1].(map[string]any)["image_url"])
	require.Equal(t, "high", got["tools"].([]any)[0].(map[string]any)["quality"])

	_, _, err = antigravityImage(ctx, imageTestConfig(srv.URL), "a screen", "9:16")
	require.NoError(t, err)
	parts := got["request"].(map[string]any)["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	require.Len(t, parts, 2)
	require.Contains(t, parts[0].(map[string]any)["text"], "Render every quoted text exactly")
	require.Equal(t, "cmVm", parts[1].(map[string]any)["inlineData"].(map[string]any)["data"])

	// Without the request the old behaviour holds: no text in the picture.
	_, _, err = chatGPTImage(context.Background(), imageTestConfig(srv.URL), "a ferry", "1:1")
	require.NoError(t, err)
	require.Contains(t, got["instructions"], "Add no text")
}

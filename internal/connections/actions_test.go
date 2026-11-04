package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestContentOperations(t *testing.T) {
	tests := []struct {
		id, action, method, path string
		request                  ActionRequest
	}{
		{id: "slack", action: "send_message", method: http.MethodPost, path: "/chat.postMessage", request: ActionRequest{Action: "send_message", Channel: "C123", Text: "hello"}},
		{id: "discord", action: "read_messages", method: http.MethodGet, path: "/channels/C123/messages", request: ActionRequest{Action: "read_messages", Channel: "C123"}},
		{id: "github", action: "create_issue", method: http.MethodPost, path: "/repos/acme/project/issues", request: ActionRequest{Action: "create_issue", Owner: "acme", Repository: "project", Title: "Bug", Text: "Details"}},
		{id: "notion", action: "append_content", method: http.MethodPatch, path: "/blocks/page-1/children", request: ActionRequest{Action: "append_content", PageID: "page-1", Text: "Content"}},
		{id: "google", action: "send_email", method: http.MethodPost, path: "/messages/send", request: ActionRequest{Action: "send_email", To: "user@example.com", Subject: "Hello", Text: "Body"}},
		{id: "outlook", action: "send_email", method: http.MethodPost, path: "/sendMail", request: ActionRequest{Action: "send_email", To: "user@example.com", Subject: "Hello", Text: "Body"}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			called := false
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != test.method || r.URL.Path != test.path || r.Header.Get("Authorization") == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if test.id == "slack" || test.id == "discord" || test.id == "github" || test.id == "notion" || test.id == "google" {
					_, _ = w.Write([]byte(`{"ok":true,"id":"created"}`))
					return
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			defer provider.Close()
			svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
			svc.ProviderAPIURLs = map[string]string{test.id: provider.URL}
			if name := tokenConnectionSecrets[test.id]; name != "" {
				require.NoError(t, svc.Save(test.id, SaveRequest{Token: "token"}))
			} else if test.id == "google" {
				require.NoError(t, svc.secrets().Set(googleClientID, "client"))
				require.NoError(t, svc.secrets().Set(googleSecret, "secret"))
				require.NoError(t, svc.secrets().Set(googleToken, `{"access_token":"token","refresh_token":"refresh","expiry":"2099-01-01T00:00:00Z"}`))
			} else {
				require.NoError(t, svc.secrets().Set(outlookClientID, "client"))
				require.NoError(t, svc.secrets().Set(outlookToken, `{"access_token":"token","refresh_token":"refresh","expiry":"2099-01-01T00:00:00Z"}`))
			}
			result, err := svc.Execute(context.Background(), test.id, test.request)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, called)
		})
	}
}

func TestCalendarOperations(t *testing.T) {
	for _, id := range []string{"google", "outlook"} {
		t.Run(id, func(t *testing.T) {
			var body map[string]any
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, _ = w.Write([]byte(`{"id":"event-1"}`))
			}))
			defer provider.Close()
			svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
			svc.ProviderAPIURLs = map[string]string{id: provider.URL, "google-calendar": provider.URL}
			if id == "google" {
				require.NoError(t, svc.secrets().Set(googleClientID, "client"))
				require.NoError(t, svc.secrets().Set(googleSecret, "secret"))
				require.NoError(t, svc.secrets().Set(googleToken, `{"access_token":"token","refresh_token":"refresh","expiry":"2099-01-01T00:00:00Z"}`))
			} else {
				require.NoError(t, svc.secrets().Set(outlookClientID, "client"))
				require.NoError(t, svc.secrets().Set(outlookToken, `{"access_token":"token","refresh_token":"refresh","expiry":"2099-01-01T00:00:00Z"}`))
			}
			_, err := svc.Execute(context.Background(), id, ActionRequest{Action: "create_event", Title: "Meeting", Start: time.Now().Add(time.Hour).Format(time.RFC3339), End: time.Now().Add(2 * time.Hour).Format(time.RFC3339)})
			require.NoError(t, err)
			if id == "google" {
				require.Equal(t, "Meeting", body["summary"])
			} else {
				require.Equal(t, "Meeting", body["subject"])
			}
		})
	}
}

func TestContentOperationValidation(t *testing.T) {
	t.Parallel()
	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	require.NoError(t, svc.Save("slack", SaveRequest{Token: "token"}))
	_, err := svc.Execute(context.Background(), "slack", ActionRequest{Action: "send_message"})
	require.ErrorContains(t, err, "channel")
	_, err = svc.Execute(context.Background(), "slack", ActionRequest{Action: "unknown"})
	require.ErrorContains(t, err, "action")
}

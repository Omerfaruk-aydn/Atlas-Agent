package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGoogleWorkspaceContentOperations(t *testing.T) {
	tests := []struct {
		action, method, path string
		request              ActionRequest
	}{
		{action: "list_drive_files", method: http.MethodGet, path: "/drive/v3/files", request: ActionRequest{Action: "list_drive_files"}},
		{action: "create_document", method: http.MethodPost, path: "/documents", request: ActionRequest{Action: "create_document", Title: "Report"}},
		{action: "append_document", method: http.MethodPost, path: "/documents/doc-1:batchUpdate", request: ActionRequest{Action: "append_document", DocumentID: "doc-1", Text: "Text"}},
		{action: "create_spreadsheet", method: http.MethodPost, path: "/spreadsheets", request: ActionRequest{Action: "create_spreadsheet", Title: "Budget"}},
		{action: "read_cells", method: http.MethodGet, path: "/spreadsheets/sheet-1/values/A1:B2", request: ActionRequest{Action: "read_cells", SpreadsheetID: "sheet-1", Range: "A1:B2"}},
		{action: "update_cells", method: http.MethodPut, path: "/spreadsheets/sheet-1/values/A1:B2", request: ActionRequest{Action: "update_cells", SpreadsheetID: "sheet-1", Range: "A1:B2", Values: [][]string{{"A", "B"}}}},
		{action: "list_youtube_videos", method: http.MethodGet, path: "/search", request: ActionRequest{Action: "list_youtube_videos"}},
		{action: "update_youtube_video", method: http.MethodPut, path: "/videos", request: ActionRequest{Action: "update_youtube_video", VideoID: "video-1", Title: "Title", Privacy: "unlisted"}},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			called := false
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				require.Equal(t, test.method, r.Method)
				require.Equal(t, test.path, r.URL.Path)
				require.NotEmpty(t, r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"id":"created"}`))
			}))
			defer provider.Close()
			svc := googleActionTestService(t, provider.URL)
			result, err := svc.Execute(context.Background(), "google", test.request)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, called)
		})
	}
}

func TestGoogleDriveAndYouTubeUploads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("video-content"), 0o600))
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/upload/drive/v3/files":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseMultipartForm(1<<20))
			require.Contains(t, r.MultipartForm.Value["metadata"][0], `"name":"video.mp4"`)
			_, _ = w.Write([]byte(`{"id":"drive-file"}`))
		case "/videos":
			require.Equal(t, http.MethodPost, r.Method)
			w.Header().Set("Location", provider.URL+"/upload-location")
			_, _ = w.Write([]byte(`{}`))
		case "/upload-location":
			require.Equal(t, http.MethodPut, r.Method)
			_, _ = w.Write([]byte(`{"id":"youtube-video"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer provider.Close()
	svc := googleActionTestService(t, provider.URL)

	result, err := svc.Execute(context.Background(), "google", ActionRequest{Action: "upload_drive_file", FilePath: path})
	require.NoError(t, err)
	require.Equal(t, "drive-file", result.(map[string]any)["id"])

	result, err = svc.Execute(context.Background(), "google", ActionRequest{Action: "upload_youtube_video", FilePath: path, Title: "Video", Privacy: "private"})
	require.NoError(t, err)
	require.Equal(t, "youtube-video", result.(map[string]any)["id"])
}

func TestGoogleWorkspaceUploadValidation(t *testing.T) {
	t.Parallel()
	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	require.NoError(t, svc.secrets().Set(googleClientID, "client"))
	require.NoError(t, svc.secrets().Set(googleSecret, "secret"))
	require.NoError(t, svc.secrets().Set(googleToken, `{"access_token":"token","refresh_token":"refresh","expiry":"2099-01-01T00:00:00Z"}`))
	_, err := svc.Execute(context.Background(), "google", ActionRequest{Action: "upload_youtube_video", Title: "Video"})
	require.ErrorContains(t, err, "file_path")
	_, err = svc.Execute(context.Background(), "google", ActionRequest{Action: "update_cells"})
	require.ErrorContains(t, err, "spreadsheet_id")
}

func googleActionTestService(t *testing.T, providerURL string) *Service {
	t.Helper()
	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	for _, id := range []string{"google-drive", "google-drive-upload", "google-docs", "google-sheets", "google-youtube", "google-youtube-upload"} {
		if svc.ProviderAPIURLs == nil {
			svc.ProviderAPIURLs = map[string]string{}
		}
		svc.ProviderAPIURLs[id] = providerURL
	}
	require.NoError(t, svc.secrets().Set(googleClientID, "client"))
	require.NoError(t, svc.secrets().Set(googleSecret, "secret"))
	require.NoError(t, svc.secrets().Set(googleToken, `{"access_token":"token","refresh_token":"refresh","expiry":"2099-01-01T00:00:00Z"}`))
	return svc
}

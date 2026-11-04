package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Service) googleWorkspaceAction(ctx context.Context, token string, a ActionRequest) (any, error) {
	switch a.Action {
	case "list_drive_files":
		path := "/drive/v3/files?pageSize=" + strconv.Itoa(a.Limit) + "&fields=" + url.QueryEscape("files(id,name,mimeType,size,modifiedTime,webViewLink)")
		if a.Query != "" {
			path += "&q=" + url.QueryEscape(a.Query)
		}
		return s.providerJSON(ctx, "google-drive", token, http.MethodGet, path, nil)
	case "upload_drive_file":
		return s.uploadDriveFile(ctx, token, a)
	case "create_document":
		if strings.TrimSpace(a.Title) == "" {
			return nil, errors.New("title is required")
		}
		return s.providerJSON(ctx, "google-docs", token, http.MethodPost, "/documents", map[string]any{"title": a.Title})
	case "append_document":
		if a.DocumentID == "" || strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("document_id and text are required")
		}
		body := map[string]any{"requests": []any{map[string]any{"insertText": map[string]any{"text": a.Text, "endOfSegmentLocation": map[string]any{}}}}}
		return s.providerJSON(ctx, "google-docs", token, http.MethodPost, "/documents/"+url.PathEscape(a.DocumentID)+":batchUpdate", body)
	case "create_spreadsheet":
		if strings.TrimSpace(a.Title) == "" {
			return nil, errors.New("title is required")
		}
		return s.providerJSON(ctx, "google-sheets", token, http.MethodPost, "/spreadsheets", map[string]any{"properties": map[string]any{"title": a.Title}})
	case "read_cells":
		if a.SpreadsheetID == "" || a.Range == "" {
			return nil, errors.New("spreadsheet_id and range are required")
		}
		return s.providerJSON(ctx, "google-sheets", token, http.MethodGet, "/spreadsheets/"+url.PathEscape(a.SpreadsheetID)+"/values/"+url.PathEscape(a.Range), nil)
	case "update_cells":
		if a.SpreadsheetID == "" || a.Range == "" || len(a.Values) == 0 {
			return nil, errors.New("spreadsheet_id, range and values are required")
		}
		path := "/spreadsheets/" + url.PathEscape(a.SpreadsheetID) + "/values/" + url.PathEscape(a.Range) + "?valueInputOption=USER_ENTERED"
		return s.providerJSON(ctx, "google-sheets", token, http.MethodPut, path, map[string]any{"range": a.Range, "majorDimension": "ROWS", "values": a.Values})
	case "list_youtube_videos":
		return s.providerJSON(ctx, "google-youtube", token, http.MethodGet, "/search?part=snippet&forMine=true&type=video&maxResults="+strconv.Itoa(a.Limit), nil)
	case "upload_youtube_video":
		return s.uploadYouTubeVideo(ctx, token, a)
	case "update_youtube_video":
		if a.VideoID == "" || strings.TrimSpace(a.Title) == "" {
			return nil, errors.New("video_id and title are required")
		}
		body := map[string]any{"id": a.VideoID, "snippet": map[string]any{"title": a.Title, "description": a.Text, "categoryId": "22"}, "status": map[string]any{"privacyStatus": youtubePrivacy(a.Privacy)}}
		return s.providerJSON(ctx, "google-youtube", token, http.MethodPut, "/videos?part=snippet,status", body)
	}
	return nil, errors.New("unsupported Google Workspace action")
}

func (s *Service) uploadDriveFile(ctx context.Context, token string, a ActionRequest) (any, error) {
	file, info, contentType, err := openUploadFile(a.FilePath, a.MIMEType)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = info.Name()
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	go func() {
		metadata, createErr := multipartWriter.CreateFormField("metadata")
		if createErr == nil {
			_, createErr = fmt.Fprintf(metadata, `{"name":%s}`, strconv.Quote(title))
		}
		var content io.Writer
		if createErr == nil {
			content, createErr = multipartWriter.CreateFormFile("file", info.Name())
		}
		if createErr == nil {
			_, createErr = io.Copy(content, file)
		}
		if closeErr := multipartWriter.Close(); createErr == nil {
			createErr = closeErr
		}
		_ = writer.CloseWithError(createErr)
	}()
	defer reader.Close()
	path := s.providerBaseURL("google-drive-upload") + "/upload/drive/v3/files?uploadType=multipart&fields=id,name,mimeType,webViewLink"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, reader)
	if err != nil {
		return nil, errors.New("could not create Drive upload request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req.Header.Set("X-Upload-Content-Type", contentType)
	return s.decodeUploadResponse(req)
}

func (s *Service) uploadYouTubeVideo(ctx context.Context, token string, a ActionRequest) (any, error) {
	if strings.TrimSpace(a.Title) == "" {
		return nil, errors.New("title is required for a YouTube upload")
	}
	file, info, contentType, err := openUploadFile(a.FilePath, a.MIMEType)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	metadata, err := json.Marshal(map[string]any{"snippet": map[string]any{"title": a.Title, "description": a.Text, "categoryId": "22"}, "status": map[string]any{"privacyStatus": youtubePrivacy(a.Privacy)}})
	if err != nil {
		return nil, err
	}
	path := s.providerBaseURL("google-youtube-upload") + "/videos?uploadType=resumable&part=snippet,status"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(metadata))
	if err != nil {
		return nil, errors.New("could not initialize YouTube upload")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", contentType)
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(info.Size(), 10))
	resp, err := s.uploadHTTPClient().Do(req)
	if err != nil {
		return nil, errors.New("YouTube upload could not be initialized")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("YouTube upload initialization returned HTTP %d", resp.StatusCode)
	}
	uploadURL := resp.Header.Get("Location")
	resp.Body.Close()
	if uploadURL == "" {
		return nil, errors.New("YouTube did not return an upload location")
	}
	upload, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, file)
	if err != nil {
		return nil, errors.New("could not create YouTube upload request")
	}
	upload.Header.Set("Authorization", "Bearer "+token)
	upload.Header.Set("Content-Type", contentType)
	upload.ContentLength = info.Size()
	return s.decodeUploadResponse(upload)
}

func openUploadFile(path, requestedMIME string) (*os.File, os.FileInfo, string, error) {
	if strings.TrimSpace(path) == "" || len(path) > 4096 {
		return nil, nil, "", errors.New("file_path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, "", errors.New("the local file could not be opened")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		file.Close()
		return nil, nil, "", errors.New("file_path must identify a non-empty regular file")
	}
	contentType := strings.TrimSpace(requestedMIME)
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(path))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return file, info, contentType, nil
}

func youtubePrivacy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public", "unlisted":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "private"
	}
}

func (s *Service) providerBaseURL(id string) string {
	if override := s.ProviderAPIURLs[id]; override != "" {
		return strings.TrimRight(override, "/")
	}
	return actionAPIURLs[id]
}

func (s *Service) uploadHTTPClient() *http.Client {
	if s.HTTP == nil {
		return &http.Client{}
	}
	client := *s.HTTP
	client.Timeout = 0
	return &client
}

func (s *Service) decodeUploadResponse(req *http.Request) (any, error) {
	resp, err := s.uploadHTTPClient().Do(req)
	if err != nil {
		return nil, errors.New("provider upload could not be completed")
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 2<<20)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, limited)
		return nil, fmt.Errorf("provider upload returned HTTP %d", resp.StatusCode)
	}
	var result any
	if err := json.NewDecoder(limited).Decode(&result); err != nil {
		return nil, errors.New("provider returned an invalid upload response")
	}
	return result, nil
}

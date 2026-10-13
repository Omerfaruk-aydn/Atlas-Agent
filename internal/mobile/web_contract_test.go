package mobile

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPhoneShellServesIOSMetadataAndRasterIcons(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	p := newPhone(t, g)
	get := func(path string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, p.base+path, nil)
		if err != nil {
			return nil, err
		}
		return p.client.Do(req)
	}
	resp, err := get("/")
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	require.Contains(t, string(data), `name="apple-mobile-web-app-capable" content="yes"`)
	require.Contains(t, string(data), `rel="apple-touch-icon" sizes="180x180"`)
	require.Contains(t, string(data), `viewport-fit=cover`)
	for path, size := range map[string]int{"/apple-touch-icon.png": 180, "/icon-192.png": 192, "/icon-512.png": 512} {
		resp, err := get(path)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, err)
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, size, cfg.Width)
		require.Equal(t, size, cfg.Height)
	}
	resp, err = get("/manifest.webmanifest")
	require.NoError(t, err)
	defer resp.Body.Close()
	var manifest struct {
		ID       string `json:"id"`
		StartURL string `json:"start_url"`
		Scope    string `json:"scope"`
		Display  string `json:"display"`
		Theme    string `json:"theme_color"`
		Name     string `json:"name"`
		Short    string `json:"short_name"`
		Icons    []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&manifest))
	require.Equal(t, "/", manifest.ID)
	require.Equal(t, "/", manifest.StartURL, "the install URL must never contain a pairing nonce or credential")
	require.Equal(t, "/", manifest.Scope)
	require.Equal(t, "standalone", manifest.Display)
	require.NotEmpty(t, manifest.Name)
	require.NotEmpty(t, manifest.Short)
	require.Equal(t, "#ffffff", manifest.Theme)
	purposes := map[string]bool{}
	for _, icon := range manifest.Icons {
		purposes[icon.Sizes+" "+icon.Purpose] = true
	}
	for _, want := range []string{"192x192 any", "512x512 any", "192x192 maskable", "512x512 maskable"} {
		require.True(t, purposes[want], "manifest icon %s missing", want)
	}

	// The offline shell never caches API answers, and the page loads no
	// third-party code.
	resp, err = get("/sw.js")
	require.NoError(t, err)
	sw, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	require.Contains(t, string(sw), `"/api/"`)
	resp, err = get("/")
	require.NoError(t, err)
	page, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	require.NotContains(t, string(page), "https://")
	require.NotContains(t, string(page), "http://")
}

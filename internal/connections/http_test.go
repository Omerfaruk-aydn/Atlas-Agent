package connections

import (
	"net/http"
	"testing"
)

// httpGet performs a GET with the test context. A nil client uses the
// default client; a custom client keeps its redirect policy.
func httpGet(t *testing.T, client *http.Client, rawURL string) (*http.Response, error) {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

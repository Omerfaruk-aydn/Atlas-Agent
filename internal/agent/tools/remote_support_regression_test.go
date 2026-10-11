package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

func TestRemoteSupportFailedNativeOperationsAreErrors(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"input", "launch"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := newSupportFixture(t, []support.Capability{support.CapInput})
			_, client, err := f.ctl.Target()
			require.NoError(t, err)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(nodes.Operation{ID: "op-test", State: "failed", Effect: "unknown", Error: "backend failed"})
			}))
			defer srv.Close()
			client.BaseURL, client.HTTP = srv.URL, srv.Client()
			got := callSupport(t, NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl }), "native-failure", map[string]any{"action": action, "device_id": f.remote.RemoteDeviceID, "argv": []string{"fixture"}, "input_action": "key", "key": "escape"})
			require.True(t, got.resp.IsError)
			require.Equal(t, false, got.body["ok"])
			require.Equal(t, "unknown", got.outcome.Dispatch)
			require.Equal(t, string(support.CodeOutcomeUnknown), errCode(got))
		})
	}
}

func TestRemoteSupportStatusRequiresLiveRead(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapSystemInfo})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(nodes.APIError{Code: "connection_lost", Message: "unavailable", Effect: "none"})
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	got := callSupport(t, NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl }), "status", map[string]any{"action": "status"})
	require.True(t, got.resp.IsError)
	require.Equal(t, "not_verified", got.body["verification"])
	view := got.body["result"].(map[string]any)
	require.Equal(t, true, view["stale"])
	require.Equal(t, "unavailable", view["state"])
}


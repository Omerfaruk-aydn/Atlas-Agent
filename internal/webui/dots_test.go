package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
	"github.com/stretchr/testify/require"
)

func TestDotsAPI(t *testing.T) {
	store, err := dots.Open(t.TempDir())
	require.NoError(t, err)
	tts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/voices":
			_, _ = io.WriteString(w, `{"voices":[{"voice_id":"v1","name":"Ada","labels":{"gender":"female"}}]}`)
		case strings.HasPrefix(r.URL.Path, "/v1/text-to-speech/v1/stream"):
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = io.WriteString(w, "ID3audio")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(tts.Close)

	h := newHarness(t, func(o *Options) {
		o.Dots = store
		o.Voices = &dots.Voices{Key: func() string { return "key" }, BaseURL: tts.URL}
	})
	b := h.signIn()

	var list dotsList
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, "/api/dots", nil, &list))
	require.Empty(t, list.Dots)
	require.True(t, list.VoiceConfigured)
	require.Contains(t, list.Shapes, "bloom")

	var d dots.Dot
	require.Equal(t, http.StatusCreated, b.json(http.MethodPost, "/api/dots", map[string]any{"name": "Selin", "shape": "bloom", "color": "#3b7cf2"}, &d))
	require.Equal(t, "Selin", d.Name)

	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, "/api/dots", map[string]any{"name": ""}, nil))

	// Full access needs the accepted warning and the typed name.
	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, "/api/dots/"+d.ID+"/full-access", map[string]any{"minutes": 60, "confirm_name": "Selin"}, nil))
	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, "/api/dots/"+d.ID+"/full-access", map[string]any{"minutes": 60, "confirm_name": "Deniz", "accepted": true}, nil))
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, "/api/dots/"+d.ID+"/full-access", map[string]any{"minutes": 60, "confirm_name": "selin", "accepted": true}, &d))
	require.NotEmpty(t, d.FullAccess.Until)
	var revoked dots.Dot
	require.Equal(t, http.StatusOK, b.json(http.MethodDelete, "/api/dots/"+d.ID+"/full-access", nil, &revoked))
	require.True(t, revoked.FullAccess.GrantedAt.IsZero())

	require.Equal(t, http.StatusOK, b.json(http.MethodPatch, "/api/dots/"+d.ID, map[string]any{"role": "Araştırmacı"}, &d))
	require.Equal(t, "Araştırmacı", d.Role)

	var voices []dots.Voice
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, "/api/dots/voices", nil, &voices))
	require.Equal(t, []dots.Voice{{ID: "v1", Name: "Ada", Gender: "female"}}, voices)

	status, body := b.raw(http.MethodPost, "/api/dots/speak", map[string]any{"voice_id": "v1", "text": "Merhaba"})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "ID3audio", body)

	// The client page is served at /dots.
	rsp := b.request(http.MethodGet, "/dots", nil)
	page, _ := io.ReadAll(rsp.Body)
	rsp.Body.Close()
	require.Contains(t, string(page), "Atlas Dots")

	require.Equal(t, http.StatusNoContent, b.json(http.MethodDelete, "/api/dots/"+d.ID, nil, nil))
	require.Equal(t, http.StatusNotFound, b.json(http.MethodGet, "/api/dots/"+d.ID, nil, nil))
}

func TestDotsAutomationAPI(t *testing.T) {
	store, err := dots.Open(t.TempDir())
	require.NoError(t, err)
	auto, err := dots.OpenAutomation(t.TempDir())
	require.NoError(t, err)
	h := newHarness(t, func(o *Options) {
		o.Dots = store
		o.Automation = auto
	})
	b := h.signIn()

	var d dots.Dot
	require.Equal(t, http.StatusCreated, b.json(http.MethodPost, "/api/dots", map[string]any{"name": "Nöbetçi"}, &d))
	base := "/api/dots/" + d.ID

	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, base+"/triggers", map[string]any{"name": "x", "cron": "bad", "prompt": "p"}, nil))
	var trg dots.Trigger
	require.Equal(t, http.StatusCreated, b.json(http.MethodPost, base+"/triggers", map[string]any{"name": "Sabah", "cron": "0 9 * * *", "prompt": "özet"}, &trg))
	require.False(t, trg.NextFireAt.IsZero())
	var hook dots.Trigger
	require.Equal(t, http.StatusCreated, b.json(http.MethodPost, base+"/triggers", map[string]any{"name": "Hook", "kind": "webhook", "prompt": "işle"}, &hook))
	require.NotEmpty(t, hook.Token)

	var list []dots.Trigger
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, base+"/triggers", nil, &list))
	require.Len(t, list, 2)
	require.Equal(t, http.StatusOK, b.json(http.MethodPatch, base+"/triggers/"+trg.ID, map[string]any{"enabled": false}, &trg))
	require.False(t, trg.Enabled)

	var budget dots.Budget
	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPut, base+"/budget", map[string]any{"limit_usd": -3}, nil))
	require.Equal(t, http.StatusOK, b.json(http.MethodPut, base+"/budget", map[string]any{"limit_usd": 2.5}, &budget))
	require.InDelta(t, 2.5, budget.LimitUSD, 1e-9)

	// The webhook needs no session cookie, only its token.
	anon := h.anonymous()
	require.Equal(t, http.StatusNotFound, anon.json(http.MethodPost, "/hooks/dots/nope", nil, nil))
	require.Equal(t, http.StatusAccepted, anon.json(http.MethodPost, "/hooks/dots/"+hook.Token, nil, nil))
	var runs []dots.Run
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, base+"/runs", nil, &runs))
	require.Len(t, runs, 1)
	require.Equal(t, dots.SourceWebhook, runs[0].Source)

	require.Equal(t, http.StatusNoContent, b.json(http.MethodDelete, base+"/triggers/"+trg.ID, nil, nil))
	require.Equal(t, http.StatusNotFound, b.json(http.MethodDelete, base+"/triggers/"+trg.ID, nil, nil))
}

// anonymous returns a client with no session, like another program calling
// a webhook.
func (h *harness) anonymous() *browser {
	return &browser{t: h.t, h: h, http: &http.Client{Timeout: time.Minute}}
}

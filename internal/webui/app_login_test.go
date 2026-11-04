package webui

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/accountlogin"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

func TestAccountLoginStoresTheToken(t *testing.T) {
	h := newHarness(t)
	h.gw.logins.supported = func(string) bool { return true }
	finish := make(chan struct{})
	h.gw.logins.start = func(context.Context, string) (*accountlogin.Session, error) {
		return accountlogin.NewSession("https://example.test/device", "ABCD-1234", func(ctx context.Context, progress func(string)) (*oauth.Token, error) {
			progress("waiting for the browser")
			select {
			case <-finish:
				return &oauth.Token{AccessToken: "access", RefreshToken: "refresh"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}), nil
	}
	b := h.signIn()
	ws := b.open(h.project)
	path := "/api/workspaces/" + ws.ID + "/providers/fake/login"

	var view loginView
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, path, map[string]any{}, &view))
	require.Equal(t, loginWaiting, view.State)
	require.Equal(t, "https://example.test/device", view.URL)
	require.Equal(t, "ABCD-1234", view.UserCode)

	close(finish)
	require.Eventually(t, func() bool {
		var now loginView
		b.json(http.MethodGet, path, nil, &now)
		return now.State != loginWaiting
	}, 10*time.Second, 20*time.Millisecond)

	var done loginView
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, path, nil, &done))
	require.Equal(t, loginDone, done.State, done.Error)
	require.Empty(t, done.URL)
}

func TestAccountLoginReportsAFailedWait(t *testing.T) {
	h := newHarness(t)
	h.gw.logins.start = func(context.Context, string) (*accountlogin.Session, error) {
		return accountlogin.NewSession("https://example.test", "", func(context.Context, func(string)) (*oauth.Token, error) {
			return nil, errors.New("access denied")
		}), nil
	}
	b := h.signIn()
	ws := b.open(h.project)
	path := "/api/workspaces/" + ws.ID + "/providers/chatgpt/login"

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, path, map[string]any{}, nil))
	require.Eventually(t, func() bool {
		var now loginView
		b.json(http.MethodGet, path, nil, &now)
		return now.State == loginFailed && now.Error == "access denied"
	}, 10*time.Second, 20*time.Millisecond)
}

func TestAccountLoginCancelStopsTheFlow(t *testing.T) {
	h := newHarness(t)
	stopped := make(chan struct{})
	h.gw.logins.start = func(context.Context, string) (*accountlogin.Session, error) {
		return accountlogin.NewSession("https://example.test", "", func(ctx context.Context, _ func(string)) (*oauth.Token, error) {
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		}), nil
	}
	b := h.signIn()
	ws := b.open(h.project)
	path := "/api/workspaces/" + ws.ID + "/providers/claude/login"

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, path, map[string]any{}, nil))
	require.Equal(t, http.StatusNoContent, b.json(http.MethodDelete, path, nil, nil))
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the sign-in kept running after it was cancelled")
	}
	require.Equal(t, http.StatusNotFound, b.json(http.MethodGet, path, nil, nil))
}

func TestAccountLoginRefusesAKeyProvider(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, "/api/workspaces/"+ws.ID+"/providers/groq/login", map[string]any{}, nil))
}

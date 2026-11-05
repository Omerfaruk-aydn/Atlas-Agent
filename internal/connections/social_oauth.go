package connections

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

var socialOAuth = map[string]struct {
	clientID, secret, token string
	endpoint                oauth2.Endpoint
	scopes                  []string
}{
	"slack": {
		clientID: slackClientID, secret: slackSecret, token: slackToken,
		endpoint: oauth2.Endpoint{AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access"},
		scopes:   []string{"channels:read", "channels:history", "chat:write", "groups:read", "groups:history"},
	},
	"github": {
		clientID: githubClientID, secret: githubSecret, token: githubToken,
		endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token"},
		scopes:   []string{"repo", "read:user"},
	},
	"notion": {
		clientID: notionClientID, secret: notionSecret, token: notionToken,
		endpoint: oauth2.Endpoint{AuthURL: "https://api.notion.com/v1/oauth/authorize", TokenURL: "https://api.notion.com/v1/oauth/token"},
	},
	"discord": {
		clientID: discordClientID, secret: discordSecret, token: discordToken,
		endpoint: oauth2.Endpoint{AuthURL: "https://discord.com/oauth2/authorize", TokenURL: "https://discord.com/api/oauth2/token"},
		scopes:   []string{"identify", "guilds"},
	},
}

// StartAccountOAuth starts the provider's real account authorization flow.
func (s *Service) StartAccountOAuth(ctx context.Context, id string) (OAuthStart, error) {
	provider, ok := socialOAuth[id]
	if !ok {
		return OAuthStart{}, errors.New("this connection does not support account OAuth")
	}
	clientID, clientSecret := s.oauthApp(id)
	if clientID == "" || clientSecret == "" {
		return OAuthStart{}, errors.New("save the OAuth client ID and client secret first")
	}
	endpoint := provider.endpoint
	if override := s.SocialOAuthEndpoints[id]; override.AuthURL != "" {
		endpoint = override
	}
	config := &oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: endpoint, Scopes: provider.scopes}
	listener, err := listenLoopback(ctx)
	if err != nil {
		return OAuthStart{}, errors.New("could not open the local OAuth callback listener")
	}
	state, err := randomOAuthValue()
	if err != nil {
		_ = listener.Close()
		return OAuthStart{}, errors.New("could not initialize account authorization")
	}
	verifier := oauth2.GenerateVerifier()
	config.RedirectURL = "http://" + listener.Addr().String() + "/callback"
	s.oauthMu.Lock()
	if s.oauthPending == nil {
		s.oauthPending = make(map[string]pendingGoogleOAuth)
	}
	s.oauthPending[state] = pendingGoogleOAuth{verifier: verifier, service: s}
	s.oauthMu.Unlock()

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	var closeOnce sync.Once
	closeListener := func() { closeOnce.Do(func() { _ = listener.Close() }) }
	timer := time.AfterFunc(5*time.Minute, func() {
		closeListener()
		s.oauthMu.Lock()
		delete(s.oauthPending, state)
		s.oauthMu.Unlock()
	})
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		s.oauthMu.Lock()
		entry, valid := s.oauthPending[r.URL.Query().Get("state")]
		if valid {
			delete(s.oauthPending, r.URL.Query().Get("state"))
		}
		s.oauthMu.Unlock()
		if !valid || entry.service != s {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Atlas: authorization could not be verified. Return to Atlas and try again."))
			return
		}
		defer closeListener()
		if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Atlas: authorization was not completed. Return to Atlas and try again."))
			return
		}
		exchangeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		token, exchangeErr := config.Exchange(exchangeCtx, r.URL.Query().Get("code"), oauth2.VerifierOption(entry.verifier))
		if exchangeErr != nil || token.AccessToken == "" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Atlas: token exchange failed. Return to Atlas and try again."))
			return
		}
		stored := token.AccessToken
		if id == "discord" {
			stored = "user " + token.AccessToken
		}
		if s.secrets().Set(provider.token, stored) != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Atlas: the encrypted credential could not be saved. Return to Atlas and try again."))
			return
		}
		_, _ = w.Write([]byte("Atlas: account connected. You can close this window and return to Atlas."))
	})
	go func() {
		_ = server.Serve(listener)
		timer.Stop()
	}()
	options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if id == "notion" {
		options = append(options, oauth2.SetAuthURLParam("owner", "user"))
	}
	return OAuthStart{AuthURL: config.AuthCodeURL(state, options...)}, nil
}

package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

var outlookScopes = []string{"offline_access", "User.Read", "Mail.ReadWrite", "Mail.Send", "Calendars.ReadWrite"}

var microsoftEndpoint = oauth2.Endpoint{
	AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
	TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
}

func (s *Service) outlookEndpoint() oauth2.Endpoint {
	if s.OutlookEndpoint.AuthURL != "" {
		return s.OutlookEndpoint
	}
	return microsoftEndpoint
}

// StartOutlookOAuth authorizes a Microsoft personal or work account through
// the common tenant. A client secret is optional for desktop public clients.
func (s *Service) StartOutlookOAuth(ctx context.Context) (OAuthStart, error) {
	clientID, clientSecret := s.oauthApp("outlook")
	if clientID == "" {
		return OAuthStart{}, errors.New("save a Microsoft application client ID first")
	}
	listener, err := listenLoopback(ctx)
	if err != nil {
		return OAuthStart{}, errors.New("could not open the local OAuth callback listener")
	}
	state, err := randomOAuthValue()
	if err != nil {
		_ = listener.Close()
		return OAuthStart{}, errors.New("could not initialize Microsoft authorization")
	}
	verifier := oauth2.GenerateVerifier()
	config := &oauth2.Config{
		ClientID: clientID, ClientSecret: clientSecret, Endpoint: s.outlookEndpoint(),
		RedirectURL: "http://" + listener.Addr().String() + "/callback", Scopes: outlookScopes,
	}
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
		entry, ok := s.oauthPending[r.URL.Query().Get("state")]
		if ok {
			delete(s.oauthPending, r.URL.Query().Get("state"))
		}
		s.oauthMu.Unlock()
		if !ok || entry.service != s {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Atlas: authorization could not be verified. Return to Atlas and try again."))
			return
		}
		defer closeListener()
		if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Atlas: Microsoft authorization was not completed. Return to Atlas and try again."))
			return
		}
		exchangeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		token, exchangeErr := config.Exchange(exchangeCtx, r.URL.Query().Get("code"), oauth2.VerifierOption(entry.verifier))
		if exchangeErr != nil || token.RefreshToken == "" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Atlas: Microsoft token exchange failed. Return to Atlas and try again."))
			return
		}
		data, marshalErr := json.Marshal(token)
		if marshalErr != nil || s.secrets().Set(outlookToken, string(data)) != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Atlas: the encrypted credential could not be saved. Return to Atlas and try again."))
			return
		}
		_, _ = w.Write([]byte("Atlas: Microsoft is connected. You can close this window and return to Atlas."))
	})
	go func() {
		_ = server.Serve(listener)
		timer.Stop()
	}()
	return OAuthStart{AuthURL: config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))}, nil
}

func (s *Service) testOutlook(ctx context.Context) error {
	clientID, clientSecret := s.oauthApp("outlook")
	if clientID == "" {
		return errors.New("save a Microsoft application client ID first")
	}
	raw, err := s.secrets().Get(outlookToken)
	if err != nil || raw == "" {
		return errors.New("authorize your Microsoft account first")
	}
	var token oauth2.Token
	if json.Unmarshal([]byte(raw), &token) != nil || token.AccessToken == "" {
		return errors.New("the stored Microsoft authorization is invalid; authorize again")
	}
	config := &oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: s.outlookEndpoint(), Scopes: outlookScopes}
	refreshed, err := config.TokenSource(ctx, &token).Token()
	if err != nil {
		return errors.New("Microsoft authorization could not be refreshed; authorize again")
	}
	if refreshed.AccessToken != token.AccessToken || refreshed.RefreshToken != token.RefreshToken {
		data, marshalErr := json.Marshal(refreshed)
		if marshalErr != nil || s.secrets().Set(outlookToken, string(data)) != nil {
			return errors.New("refreshed Microsoft authorization could not be saved")
		}
	}
	endpoint := "https://graph.microsoft.com/v1.0/me?$select=id,displayName"
	if s.OutlookUserURL != "" {
		endpoint = s.OutlookUserURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("could not create Microsoft verification request")
	}
	req.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Microsoft Graph could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("Microsoft rejected the authorization")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Microsoft Graph returned HTTP %d", resp.StatusCode)
	}
	return nil
}

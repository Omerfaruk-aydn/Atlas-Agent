package connections

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var googleScopes = []string{
	"openid",
	"email",
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/calendar.events",
	"https://www.googleapis.com/auth/drive",
	"https://www.googleapis.com/auth/documents",
	"https://www.googleapis.com/auth/spreadsheets",
	"https://www.googleapis.com/auth/youtube",
}

// OAuthStart is a short-lived authorization URL. It never contains the
// client secret or an access token.
type OAuthStart struct {
	AuthURL string `json:"auth_url"`
}

type pendingGoogleOAuth struct {
	verifier  string
	returnURL string
	service   *Service
}

// StartGoogleOAuth starts a loopback-only callback listener and returns the
// Google consent URL. State and PKCE bind the callback to this authorization
// attempt; each state is accepted at most once.
func (s *Service) StartGoogleOAuth(ctx context.Context, returnURL string) (OAuthStart, error) {
	clientID, clientSecret := s.oauthApp("google")
	if clientID == "" {
		return OAuthStart{}, errors.New("save your Google OAuth client ID first")
	}
	if clientSecret == "" {
		return OAuthStart{}, errors.New("save your Google OAuth client secret first")
	}
	returnURL, err := normalizeOAuthReturnURL(returnURL)
	if err != nil {
		return OAuthStart{}, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return OAuthStart{}, errors.New("could not open the local OAuth callback listener")
	}
	state, err := randomOAuthValue()
	if err != nil {
		_ = listener.Close()
		return OAuthStart{}, errors.New("could not initialize Google authorization")
	}
	verifier := oauth2.GenerateVerifier()
	endpoint := google.Endpoint
	if s.GoogleEndpoint.AuthURL != "" {
		endpoint = s.GoogleEndpoint
	}
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  "http://" + listener.Addr().String() + "/callback",
		Scopes:       googleScopes,
	}
	pending := pendingGoogleOAuth{verifier: verifier, returnURL: returnURL, service: s}
	s.oauthMu.Lock()
	if s.oauthPending == nil {
		s.oauthPending = make(map[string]pendingGoogleOAuth)
	}
	s.oauthPending[state] = pending
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
			_, _ = w.Write([]byte("Atlas: Google authorization was not completed. Return to Atlas and try again."))
			return
		}
		exchangeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		token, exchangeErr := config.Exchange(exchangeCtx, r.URL.Query().Get("code"), oauth2.VerifierOption(entry.verifier))
		if exchangeErr != nil || token.RefreshToken == "" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Atlas: Google token exchange failed. Return to Atlas and try again."))
			return
		}
		data, marshalErr := json.Marshal(token)
		if marshalErr != nil || s.secrets().Set(googleToken, string(data)) != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Atlas: the encrypted credential could not be saved. Return to Atlas and try again."))
			return
		}
		if entry.returnURL != "" {
			http.Redirect(w, r, entry.returnURL, http.StatusSeeOther)
			return
		}
		_, _ = w.Write([]byte("Atlas: Google is connected. You can close this window and return to Atlas."))
	})
	go func() {
		_ = server.Serve(listener)
		timer.Stop()
	}()
	return OAuthStart{AuthURL: config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))}, nil
}

func normalizeOAuthReturnURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return "", errors.New("OAuth return URL must be a local Atlas address")
	}
	return u.Scheme + "://" + u.Host + "/", nil
}

func randomOAuthValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (s *Service) googleConfig(ctx context.Context) (*oauth2.Config, *oauth2.Token, error) {
	clientID, clientSecret := s.oauthApp("google")
	if clientID == "" {
		return nil, nil, errors.New("save your Google OAuth client ID first")
	}
	if clientSecret == "" {
		return nil, nil, errors.New("save your Google OAuth client secret first")
	}
	raw, err := s.secrets().Get(googleToken)
	if err != nil || raw == "" {
		return nil, nil, errors.New("authorize your Google account first")
	}
	var token oauth2.Token
	if json.Unmarshal([]byte(raw), &token) != nil || token.AccessToken == "" {
		return nil, nil, errors.New("the stored Google authorization is invalid; authorize again")
	}
	endpoint := google.Endpoint
	if s.GoogleEndpoint.AuthURL != "" {
		endpoint = s.GoogleEndpoint
	}
	return &oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: endpoint, Scopes: googleScopes}, &token, nil
}

func (s *Service) testGoogle(ctx context.Context) error {
	config, token, err := s.googleConfig(ctx)
	if err != nil {
		return err
	}
	refreshed, err := config.TokenSource(ctx, token).Token()
	if err != nil {
		return errors.New("Google authorization could not be refreshed; authorize again")
	}
	if refreshed.AccessToken != token.AccessToken || refreshed.RefreshToken != token.RefreshToken {
		data, marshalErr := json.Marshal(refreshed)
		if marshalErr != nil || s.secrets().Set(googleToken, string(data)) != nil {
			return errors.New("refreshed Google authorization could not be saved")
		}
	}
	endpoint := "https://openidconnect.googleapis.com/v1/userinfo"
	if s.GoogleUserInfoURL != "" {
		endpoint = s.GoogleUserInfoURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("could not create Google verification request")
	}
	req.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Google could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("Google rejected the authorization")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Google returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func validGoogleClientID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	_, err := url.ParseRequestURI("https://clients.google/" + value)
	return err == nil
}

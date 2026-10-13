package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/stretchr/testify/require"
)

const sourceFixture = "Beyzanur Akalın\nMerhabalar, mezun bölümüne giriş yapabilirsiniz.\nKullanıcı Adı:\tgraduate-fixture\nParola:\tSynthetic-Mail-Pass-73!\nÖğrenci İşleri Bilgi Sistemi:\thttp://ois.topkapi.edu.tr\n"

func sourceContext(t *testing.T, id, instruction string) context.Context {
	t.Helper()
	t.Cleanup(func() { vault.Ephemeral.PurgeSession(id) })
	return WithUserInstruction(context.WithValue(t.Context(), SessionIDContextKey, id), instruction)
}

func TestSourceCredentialImportsAuthorizedMailWithoutSecondPassword(t *testing.T) {
	ctx := sourceContext(t, "source-mail", "Beyzanur un göndermiş olduğu mail e git ve ordaki kullanıcı adını ve şifreyi al ve ois topkapı sitesine git ve mezun bölümüne girip ordaki bilgiler ile orayı doldur")
	text := importSourceCredential(ctx, "https://mail.google.com/mail/u/1/#inbox/message-fixture", sourceFixture)
	require.NotContains(t, text, "Synthetic-Mail-Pass-73!")
	require.Contains(t, text, "credential_request")
	require.Contains(t, text, "https://ois.topkapi.edu.tr")
	spec := vault.CredentialSpec{SessionID: "source-mail", Origin: "https://ois.topkapi.edu.tr", Account: "graduate-fixture", Target: vault.CredentialTarget{TabID: "t", Document: "d", Selector: "#pw", NodeToken: "n"}}
	wrong := spec
	wrong.Origin = "https://attacker.example.test"
	_, _, err := vault.Ephemeral.Request(wrong)
	require.Error(t, err)
	req, _, err := vault.Ephemeral.Request(spec)
	require.NoError(t, err)
	_, err = vault.Ephemeral.Use(req.ID, spec.SessionID, spec.Origin, spec.Account, func(secret string) (bool, error) {
		require.Equal(t, "Synthetic-Mail-Pass-73!", secret)
		return true, nil
	})
	require.NoError(t, err)
	text = importSourceCredential(ctx, "https://mail.google.com/mail/u/1/#inbox/message-fixture", sourceFixture)
	require.Contains(t, text, "already_imported")
	require.False(t, vault.Ephemeral.ChatHasCredential(spec.SessionID))
}

func TestSourceCredentialRequiresHumanSourceAndDestination(t *testing.T) {
	for _, tt := range []struct {
		name, instruction, url, text string
	}{
		{"no authorization", "Beyzanur un gönderdiği maili oku", "https://mail.google.com/", sourceFixture},
		{"wrong source", "Beyzanur un gönderdiği maildeki şifre ile ois topkapı sitesine gir", "https://attacker.example.test/", sourceFixture},
		{"wrong sender", "Ali nin gönderdiği maildeki şifre ile ois topkapı sitesine gir", "https://mail.google.com/", sourceFixture},
		{"wrong destination", "Beyzanur un gönderdiği maildeki şifre ile başka bir siteye gir", "https://mail.google.com/", sourceFixture},
		{"extra domain label", "Beyzanur un gönderdiği maildeki şifre ile ois topkapı sitesine gir", "https://mail.google.com/", sourceFixture + "https://ois.topkapi.attacker.test\n"},
		{"conflicting credentials", "Beyzanur un gönderdiği maildeki şifre ile ois topkapı sitesine gir", "https://mail.google.com/", sourceFixture + "Parola: Other-Synthetic-Pass!\n"},
		{"negative", "Beyzanur un gönderdiği maildeki şifre ile ois topkapı sitesine girme", "https://mail.google.com/", sourceFixture},
		{"sender only in body", "Ali nin gönderdiği maildeki şifre ile ois topkapı sitesine gir", "https://mail.google.com/", sourceFixture + "Merhaba Ali\n"},
		{"wrong document", "Use the password from the email document at https://docs.example.test/requested to sign in at https://login.example.test", "https://docs.example.test/unrelated", sourceFixture},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sourceContext(t, "source-"+tt.name, tt.instruction)
			if tt.name == "extra domain label" {
				// Only the malicious link remains, not the legitimate destination.
				tt.text = "Beyzanur\nKullanıcı Adı: fixture\nParola: Synthetic-Other!\nhttps://ois.topkapi.attacker.test\n"
			}
			out := importSourceCredential(ctx, tt.url, tt.text)
			require.False(t, vault.Ephemeral.ChatHasCredential("source-"+tt.name))
			if tt.name == "conflicting credentials" {
				require.NotContains(t, out, "Synthetic-Mail-Pass-73!")
				require.NotContains(t, out, "Other-Synthetic-Pass!")
			}
		})
	}
}

func TestSourceCredentialLiveBrowserReadsAndFillsWithoutPhone(t *testing.T) {
	// Real Chrome reads a synthetic message and fills a separate local
	// login page. No user browser, real mail or account is accessed.
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<input id="username"><input id="pw" type="password">`)
	}))
	defer login.Close()
	mail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<article id="message" style="white-space:pre-wrap">Fixture sender
Username: fixture-user
Password: Synthetic-Live-Mail-🙂-73!
</article>`)
	}))
	defer mail.Close()
	l := newLiveTool(t)
	t.Cleanup(func() { vault.Ephemeral.PurgeSession(l.id) })
	l.ctx = WithUserInstruction(l.ctx, "Read the email document at "+mail.URL+" and use its password to sign in at "+login.URL)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, mail.URL))
	resp := l.ok(`{"action":"text","selector":"#message"}`)
	require.NotContains(t, resp.Content+resp.Metadata, "Synthetic-Live-Mail-🙂-73!")
	require.Contains(t, resp.Content, "credential_request")
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, login.URL))
	resp = l.ok(`{"action":"credential_request","selector":"#pw","account":"fixture-user"}`)
	handle := metaOf(t, resp)["credential_id"].(string)
	input, err := json.Marshal(BrowserParams{Action: "vault_fill", Selector: "#pw", Account: "fixture-user", CredentialID: handle})
	require.NoError(t, err)
	resp = l.ok(string(input))
	require.Contains(t, resp.Content, "fill_dispatched")
	value, err := l.session().Eval(`document.querySelector('#pw').value === 'Synthetic-Live-Mail-🙂-73!'`)
	require.NoError(t, err)
	require.Equal(t, "true", value)
	resp = l.run(string(input))
	require.True(t, resp.IsError)
	_, ok := l.session().(browser.SecretFiller)
	require.True(t, ok)
}

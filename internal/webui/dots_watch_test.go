package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFileFingerprint(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "inbox"), 0o755))
	a, _, err := fileFingerprint(root, "inbox/*.txt")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, "inbox", "a.txt"), []byte("hi"), 0o644))
	b, extra, err := fileFingerprint(root, "inbox/*.txt")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	require.Contains(t, extra, "inbox/a.txt")

	// A file that does not match changes nothing.
	require.NoError(t, os.WriteFile(filepath.Join(root, "inbox", "a.md"), []byte("x"), 0o644))
	c, _, err := fileFingerprint(root, "inbox/*.txt")
	require.NoError(t, err)
	require.Equal(t, b, c)

	// A directory covers everything inside it.
	d, extra, err := fileFingerprint(root, "inbox")
	require.NoError(t, err)
	require.NotEqual(t, c, d)
	require.Contains(t, extra, "inbox/a.md")

	_, _, err = fileFingerprint(root, "../x")
	require.Error(t, err)
}

func TestPageFingerprint(t *testing.T) {
	t.Parallel()
	body := `<html><script>var nonce="1"</script><p>Fiyat: 100&nbsp;TL</p></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, extra, err := pageFingerprint(ctx, srv.URL)
	require.NoError(t, err)
	require.Contains(t, extra, "Fiyat: 100 TL")
	require.NotContains(t, extra, "nonce")

	body = `<html><script>var nonce="2"</script><p>Fiyat: 100&nbsp;TL</p></html>`
	b, _, err := pageFingerprint(ctx, srv.URL)
	require.NoError(t, err)
	require.Equal(t, a, b, "script changes are not content changes")

	body = `<html><p>Fiyat: 90 TL</p></html>`
	c, _, err := pageFingerprint(ctx, srv.URL)
	require.NoError(t, err)
	require.NotEqual(t, b, c)
}

func TestTelegramApprovals(t *testing.T) {
	t.Parallel()
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "sendMessage":
			_ = json.NewDecoder(r.Body).Decode(&sent)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
		case "getUpdates":
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":7,"callback_query":{"id":"q1","data":"k1:allow","message":{"message_id":42,"chat":{"id":99}}}},
				{"update_id":8,"callback_query":{"id":"q2","data":"k1:allow","message":{"message_id":42,"chat":{"id":5}}}}]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer srv.Close()
	tg := &TelegramApprovals{Token: "t", ChatID: "99", BaseURL: srv.URL}
	ctx := context.Background()

	ref, err := tg.Ask(ctx, Approval{Dot: "Selin", Title: "bash: rm", Key: "k1"})
	require.NoError(t, err)
	require.Equal(t, "42", ref)
	require.Contains(t, sent["text"], "Selin")

	answers, err := tg.Answers(ctx)
	require.NoError(t, err)
	require.Equal(t, []ApprovalAnswer{{Key: "k1", Action: "allow", Ref: "42"}}, answers, "presses from other chats are ignored")
	require.Equal(t, int64(9), tg.offset)
	require.NoError(t, tg.Close(ctx, ref, "ok"))
}

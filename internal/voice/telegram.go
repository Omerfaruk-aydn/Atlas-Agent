package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TelegramNotifier sends written fallback messages through a Telegram bot.
type TelegramNotifier struct {
	Token   Secret
	ChatID  string
	BaseURL string
	HTTP    *http.Client
}

// Channel implements [Notifier].
func (t *TelegramNotifier) Channel() string { return ChannelTelegram }

// Notify implements [Notifier].
func (t *TelegramNotifier) Notify(ctx context.Context, text string) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	body, _ := json.Marshal(map[string]string{"chat_id": t.ChatID, "text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token.Reveal()+"/sendMessage", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build telegram request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// The URL carries the token; never return the transport error.
		return fmt.Errorf("telegram request failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}
	return nil
}

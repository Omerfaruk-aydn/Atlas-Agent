package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Approval is a permission request a dot is waiting on, as shown on the
// owner's phone.
type Approval struct {
	Dot    string
	Title  string
	Detail string
	// Key identifies the request in the answer that comes back.
	Key string
}

// ApprovalAnswer is the owner's answer to an [Approval].
type ApprovalAnswer struct {
	Key    string
	Action string // allow, allow_session or deny
	// Ref is the delivered message the answer came from.
	Ref string
}

// ApprovalChannel delivers approvals to the owner away from the screen and
// collects their answers. Ask returns a reference to the delivered
// message, which Close later rewrites to show how the request ended.
type ApprovalChannel interface {
	Ask(ctx context.Context, a Approval) (ref string, err error)
	Answers(ctx context.Context) ([]ApprovalAnswer, error)
	Close(ctx context.Context, ref, text string) error
}

// TelegramApprovals is an [ApprovalChannel] on a Telegram bot: each
// request is a message with Approve and Deny buttons, and answers are read
// with long polling, so it works without a public address.
type TelegramApprovals struct {
	Token   string
	ChatID  string
	BaseURL string
	HTTP    *http.Client

	offset int64
}

var errTelegram = errors.New("telegram request failed")

func (t *TelegramApprovals) call(ctx context.Context, method string, body any, out any) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 40 * time.Second}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token+"/"+method, strings.NewReader(string(raw)))
	if err != nil {
		return errTelegram
	}
	req.Header.Set("Content-Type", "application/json")
	rsp, err := client.Do(req)
	if err != nil {
		// The URL carries the token; never return the transport error.
		return errTelegram
	}
	defer rsp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(rsp.Body, 1<<20))
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &env) != nil || !env.OK {
		return fmt.Errorf("telegram %s: status %d %s", method, rsp.StatusCode, env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Ask implements [ApprovalChannel].
func (t *TelegramApprovals) Ask(ctx context.Context, a Approval) (string, error) {
	text := "🔐 " + a.Dot + " onayını bekliyor\n\n" + a.Title
	if a.Detail != "" {
		text += "\n\n" + a.Detail
	}
	button := func(label, action string) map[string]string {
		return map[string]string{"text": label, "callback_data": a.Key + ":" + action}
	}
	var msg struct {
		MessageID int64 `json:"message_id"`
	}
	err := t.call(ctx, "sendMessage", map[string]any{
		"chat_id": t.ChatID,
		"text":    truncateRunes(text, 3500),
		"reply_markup": map[string]any{"inline_keyboard": [][]map[string]string{
			{button("✅ İzin ver", "allow"), button("❌ Reddet", "deny")},
			{button("Bu oturumda hep izin ver", "allow_session")},
		}},
	}, &msg)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(msg.MessageID, 10), nil
}

// Answers implements [ApprovalChannel]. It waits up to 25 seconds for a
// button press and ignores presses from any chat but the owner's.
func (t *TelegramApprovals) Answers(ctx context.Context) ([]ApprovalAnswer, error) {
	var updates []struct {
		UpdateID      int64 `json:"update_id"`
		CallbackQuery *struct {
			ID      string `json:"id"`
			Data    string `json:"data"`
			Message *struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"callback_query"`
	}
	err := t.call(ctx, "getUpdates", map[string]any{
		"offset":          t.offset,
		"timeout":         25,
		"allowed_updates": []string{"callback_query"},
	}, &updates)
	if err != nil {
		return nil, err
	}
	var out []ApprovalAnswer
	for _, u := range updates {
		t.offset = max(t.offset, u.UpdateID+1)
		q := u.CallbackQuery
		if q == nil || q.Message == nil || strconv.FormatInt(q.Message.Chat.ID, 10) != t.ChatID {
			continue
		}
		_ = t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": q.ID}, nil)
		key, action, ok := strings.Cut(q.Data, ":")
		if !ok {
			continue
		}
		out = append(out, ApprovalAnswer{Key: key, Action: action, Ref: strconv.FormatInt(q.Message.MessageID, 10)})
	}
	return out, nil
}

// Close implements [ApprovalChannel]: it replaces the buttons with how the
// request ended.
func (t *TelegramApprovals) Close(ctx context.Context, ref, text string) error {
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return err
	}
	return t.call(ctx, "editMessageReplyMarkup", map[string]any{
		"chat_id":      t.ChatID,
		"message_id":   id,
		"reply_markup": map[string]any{"inline_keyboard": [][]map[string]string{{{"text": text, "callback_data": "done:none"}}}},
	}, nil)
}
